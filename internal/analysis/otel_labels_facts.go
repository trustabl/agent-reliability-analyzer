package analysis

import (
	"slices"
	"strings"

	sitter "github.com/smacker/go-tree-sitter"

	"github.com/trustabl/trustabl/internal/analysis/astutil"
	"github.com/trustabl/trustabl/internal/models"
)

// otelLabelsPos is where a library call sits relative to its enclosing
// function. The walk up stops at the first function, lambda or method, so a
// finally block in an outer function does not count for a call inside a
// nested one.
type otelLabelsPos struct {
	fn           *sitter.Node // nearest enclosing function, lambda or method; nil at module scope
	inFinally    bool         // a finally clause lies between the call and fn
	inExcept     bool         // an except / catch clause lies between the call and fn
	defaultParam bool         // the call is a default parameter value of fn
	mainGuard    bool         // module scope, under `if __name__ == "__main__":`
	instanceInit bool         // TS/JS: initializer of a non-static class field, run once per instance
}

var otelLabelsFuncTypes = map[models.Language]map[string]bool{
	models.LanguagePython: {"function_definition": true, "lambda": true},
	models.LanguageTypeScript: {
		"function_declaration":           true,
		"function_expression":            true,
		"function":                       true,
		"arrow_function":                 true,
		"method_definition":              true,
		"generator_function_declaration": true,
		"generator_function":             true,
	},
}

func otelLabelsPosition(n *sitter.Node, src []byte, lang models.Language) otelLabelsPos {
	var pos otelLabelsPos
	funcs := otelLabelsFuncTypes[lang]
	for p := n.Parent(); p != nil; p = p.Parent() {
		t := p.Type()
		if funcs[t] {
			pos.fn = p
			return pos
		}
		switch t {
		case "finally_clause":
			pos.inFinally = true
		case "except_clause", "except_group_clause", "catch_clause":
			pos.inExcept = true
		case "default_parameter", "typed_default_parameter":
			pos.defaultParam = true
		case "public_field_definition", "field_definition":
			// tree-sitter-typescript / tree-sitter-javascript. A non-static
			// field initializer runs per instance; a static one runs once.
			if lang == models.LanguageTypeScript && !hasStaticModifier(p) {
				pos.instanceInit = true
			}
		case "if_statement":
			if lang == models.LanguagePython && isPyMainGuard(p, src) {
				pos.mainGuard = true
			}
		}
	}
	return pos
}

// hasStaticModifier reports whether a class field node carries the `static`
// keyword (an anonymous child of the field definition).
func hasStaticModifier(field *sitter.Node) bool {
	for i := 0; i < int(field.ChildCount()); i++ {
		if field.Child(i).Type() == "static" {
			return true
		}
	}
	return false
}

// isPyMainGuard reports whether an if statement is `if __name__ == "__main__":`
// in either operand order and quote style, with or without one pair of
// surrounding parentheses.
func isPyMainGuard(ifStmt *sitter.Node, src []byte) bool {
	cond := strings.Join(strings.Fields(astutil.NodeText(ifStmt.ChildByFieldName("condition"), src)), "")
	cond = strings.ReplaceAll(cond, "'", `"`)
	if len(cond) > 1 && cond[0] == '(' && cond[len(cond)-1] == ')' {
		cond = cond[1 : len(cond)-1]
	}
	return cond == `__name__=="__main__"` || cond == `"__main__"==__name__`
}

// resolvesToOtelLabelsRun reports whether a callee names the library's Run in
// this file: a bare name bound to Run by an import, or a member of a module or
// namespace alias. Never repo-wide (spec D5).
func resolvesToOtelLabelsRun(callee string, aliases map[string]string) bool {
	callee = strings.TrimSpace(callee)
	if i := strings.LastIndex(callee, "."); i >= 0 {
		recv, name := callee[:i], callee[i+1:]
		return name == "Run" && (aliases[recv] == "*" || isOtelLabelsPyModule(recv))
	}
	return aliases[callee] == "Run"
}

// otelLabelsRunSignal returns a run_construct signal when n builds the
// library's Run: Python `Run(...)`, TS `new Run(...)`.
//
// ModuleScope is true when the Run is built once rather than per run: no
// enclosing function (a Python class body or a TS/JS static class field
// included; a non-static TS/JS class field initializer runs per instance, like
// Python's `self.run = Run()` in __init__, and is not module scope), or — in Python only — a default
// parameter value, which Python evaluates once at definition time. A Python
// construct under `if __name__ == "__main__":` is a one-run script and is not
// module scope. RunIDConst is true when the run id is a string literal.
func otelLabelsRunSignal(n *sitter.Node, pf ParsedFile, lang models.Language, test bool, aliases map[string]string) (models.OtelLabelsSignal, bool) {
	var callee *sitter.Node
	switch {
	case lang == models.LanguagePython && n.Type() == "call":
		callee = n.ChildByFieldName("function")
	case lang == models.LanguageTypeScript && n.Type() == "new_expression":
		callee = n.ChildByFieldName("constructor")
	default:
		return models.OtelLabelsSignal{}, false
	}
	if callee == nil || !resolvesToOtelLabelsRun(astutil.NodeText(callee, pf.Source), aliases) {
		return models.OtelLabelsSignal{}, false
	}
	pos := otelLabelsPosition(n, pf.Source, lang)
	moduleScope := pos.fn == nil && !pos.mainGuard && !pos.instanceInit
	if lang == models.LanguagePython && pos.defaultParam {
		moduleScope = true
	}
	return models.OtelLabelsSignal{
		Kind:        models.OtelLabelsRunConstruct,
		File:        pf.RelPath,
		StartLine:   astutil.NodeLine(n),
		EndLine:     astutil.NodeEndLine(n),
		Language:    lang,
		TestOrigin:  test,
		ModuleScope: moduleScope,
		RunIDConst:  runIDConst(n, pf.Source, lang),
	}, true
}

// runIDConst reports whether a Run is given a literal run id: Python
// Run("x") / Run(run_id="x"), TS new Run("x"). Every run then shares that id.
func runIDConst(n *sitter.Node, src []byte, lang models.Language) bool {
	args := n.ChildByFieldName("arguments")
	if args == nil {
		return false
	}
	if lang == models.LanguagePython {
		if v, ok := astutil.KwargValue(n, src, "run_id"); ok {
			_, lit := pyStringLiteral(v)
			return lit
		}
		first := firstPositionalArg(args)
		if first == nil {
			return false
		}
		_, lit := pyStringLiteral(astutil.NodeText(first, src))
		return lit
	}
	first := firstPositionalArg(args)
	return first != nil && first.Type() == "string"
}

// otelLabelsErrorCallbacks are function names callback-based frameworks call
// on the failure path (LangChain's callback handlers, and the TypeScript
// spelling).
var otelLabelsErrorCallbacks = map[string]bool{
	"on_tool_error":   true,
	"on_chain_error":  true,
	"handleToolError": true,
}

// otelLabelsCallFacts stamps the position and literal facts a tool_span or
// run_end signal carries (spec §4.2).
func otelLabelsCallFacts(sig *models.OtelLabelsSignal, call *sitter.Node, src []byte, lang models.Language) {
	pos := otelLabelsPosition(call, src, lang)
	errorCallback := otelLabelsErrorCallbacks[otelLabelsFuncName(pos.fn, src)]
	switch sig.Kind {
	case models.OtelLabelsToolSpan:
		sig.FailureSafe = pos.inFinally || pos.inExcept || errorCallback ||
			isPromiseHandler(pos.fn, src, "finally", "catch")
		sig.HookUnknown = lang == models.LanguagePython && isSuccessHook(pos.fn, src)
		sig.AttemptConst = attemptConst(call, src, lang)
		sig.SideEffect = sideEffectLiteral(call, src, lang)
	case models.OtelLabelsRunEnd:
		// finally does not count: a fixed reason there is written on success
		// and failure alike (spec D9).
		sig.ErrorPath = pos.inExcept || errorCallback || isPromiseHandler(pos.fn, src, "catch")
		sig.ExitReason = exitReasonLiteral(call, src, lang)
	}
}

func otelLabelsFuncName(fn *sitter.Node, src []byte) string {
	if fn == nil {
		return ""
	}
	return astutil.NodeText(fn.ChildByFieldName("name"), src)
}

// isPromiseHandler reports whether fn is passed directly to a promise method
// such as `.finally(...)` or `.catch(...)`.
func isPromiseHandler(fn *sitter.Node, src []byte, methods ...string) bool {
	if fn == nil {
		return false
	}
	args := fn.Parent()
	if args == nil || args.Type() != "arguments" {
		return false
	}
	call := args.Parent()
	if call == nil || call.Type() != "call_expression" {
		return false
	}
	member := call.ChildByFieldName("function")
	if member == nil || member.Type() != "member_expression" {
		return false
	}
	return slices.Contains(methods, astutil.NodeText(member.ChildByFieldName("property"), src))
}

// isSuccessHook reports whether fn is an `on_tool_end` method of a class that
// subclasses OpenAI Agents' RunHooks or AgentHooks. Whether those hooks run
// after a tool raises is unverified (spec O8), so such a label is neither safe
// nor unsafe (spec D10).
func isSuccessHook(fn *sitter.Node, src []byte) bool {
	if fn == nil || fn.Type() != "function_definition" || otelLabelsFuncName(fn, src) != "on_tool_end" {
		return false
	}
	for p := fn.Parent(); p != nil; p = p.Parent() {
		switch p.Type() {
		case "class_definition":
			bases := astutil.NodeText(p.ChildByFieldName("superclasses"), src)
			return strings.Contains(bases, "RunHooks") || strings.Contains(bases, "AgentHooks")
		case "function_definition", "lambda":
			return false
		}
	}
	return false
}

func attemptConst(call *sitter.Node, src []byte, lang models.Language) bool {
	if lang == models.LanguagePython {
		v, ok := astutil.KwargValue(call, src, "attempt")
		return ok && isIntLiteral(v)
	}
	v := tsOptionValue(call, src, "attempt")
	return v != nil && v.Type() == "number"
}

func isIntLiteral(s string) bool {
	s = strings.TrimSpace(s)
	if s == "" {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

// tsOptionValue returns the value node of a property in the options object
// passed as the call's second argument. Nil when the property is absent,
// written as shorthand (`{ attempt }` is a variable), or the argument is not an
// object literal.
func tsOptionValue(call *sitter.Node, src []byte, key string) *sitter.Node {
	args := call.ChildByFieldName("arguments")
	if args == nil || args.NamedChildCount() < 2 {
		return nil
	}
	obj := args.NamedChild(1)
	if obj.Type() != "object" {
		return nil
	}
	for i := 0; i < int(obj.NamedChildCount()); i++ {
		p := obj.NamedChild(i)
		if p.Type() != "pair" {
			continue
		}
		if strings.Trim(astutil.NodeText(p.ChildByFieldName("key"), src), `"'`) == key {
			return p.ChildByFieldName("value")
		}
	}
	return nil
}

func sideEffectLiteral(call *sitter.Node, src []byte, lang models.Language) string {
	if lang == models.LanguagePython {
		v, ok := astutil.KwargValue(call, src, "side_effect")
		if !ok {
			return ""
		}
		s, _ := pyStringLiteral(v)
		return s
	}
	return tsStringValue(tsOptionValue(call, src, "sideEffect"), src)
}

// exitReasonLiteral returns the literal exit reason: Python's exit_reason=
// keyword (required by the library), TS markRunEnd's second argument.
func exitReasonLiteral(call *sitter.Node, src []byte, lang models.Language) string {
	if lang == models.LanguagePython {
		v, ok := astutil.KwargValue(call, src, "exit_reason")
		if !ok {
			return ""
		}
		s, _ := pyStringLiteral(v)
		return s
	}
	args := call.ChildByFieldName("arguments")
	if args == nil || args.NamedChildCount() < 2 {
		return ""
	}
	return tsStringValue(args.NamedChild(1), src)
}
