package analysis

import (
	"strings"

	sitter "github.com/smacker/go-tree-sitter"

	"github.com/trustabl/trustabl/internal/analysis/astutil"
	"github.com/trustabl/trustabl/internal/models"
)

// claimParam is the reserved parameter an EBTE-style tool takes its
// ToolInvocationExplanation in.
const claimParam = "claim"

// claimSinkCallees are the bare callees whose arguments are output, not a
// decision: a claim that only reaches one of them is passed through.
var claimSinkCallees = map[string]bool{
	"print": true, "str": true, "repr": true, "pprint": true,
	"json.dumps": true, "json.dump": true, "pprint.pprint": true,
}

// claimSinkMethods are logger method names. Matched on the method alone
// (logger.info, self.log.debug, logging.warning), since the receiver's type is
// not resolved.
var claimSinkMethods = map[string]bool{
	"debug": true, "info": true, "warning": true, "warn": true,
	"error": true, "exception": true, "critical": true, "log": true,
}

// pythonClaimUse classifies how a Python tool function uses its `claim`
// parameter, returning one of the models.ClaimUse* values, or "" when the
// function declares no such parameter.
//
// dec is the function's decorated_definition, or nil. A tool with more than
// one decorator is ClaimUseWrapped: the extra decorator may strip and check the
// claim before the body runs, and that cannot be seen from here.
//
// Each read of `claim` in the body is followed outward to the first thing that
// consumes it. The innermost call it is an argument of decides: an output call
// (print, a logger method, json.dumps, str, repr) makes it a passthrough read,
// any other call makes it checked. A read that reaches a return statement
// without passing through a call is a passthrough read; one that reaches any
// other statement (a condition, a comparison, an assignment) is checked. The
// fact is checked if any read is, passthrough if every read is, unused if
// there are none. Anything not recognized as output counts as checked, so an
// unusual shape is silent rather than a false finding.
func pythonClaimUse(fn, dec *sitter.Node, src []byte) string {
	declared := false
	for _, p := range astutil.FunctionParams(fn, src) {
		if p == claimParam {
			declared = true
			break
		}
	}
	if !declared {
		return ""
	}
	if dec != nil && len(astutil.Decorators(dec)) > 1 {
		return models.ClaimUseWrapped
	}
	body := fn.ChildByFieldName("body")
	if body == nil {
		return models.ClaimUseUnused
	}

	reads, passthrough := 0, 0
	astutil.Walk(body, func(n *sitter.Node) bool {
		if n.Type() != "identifier" || astutil.NodeText(n, src) != claimParam || !isClaimRead(n) {
			return true
		}
		reads++
		if claimReadIsPassthrough(n, fn, src) {
			passthrough++
		}
		return true
	})
	switch {
	case reads == 0:
		return models.ClaimUseUnused
	case passthrough == reads:
		return models.ClaimUsePassthrough
	default:
		return models.ClaimUseChecked
	}
}

// wrappedFuncClaimUse classifies the claim of a function that a factory call
// registers as a tool (StructuredTool.from_function(fn), Tool(fn),
// FunctionTool(fn)), returning one of the models.ClaimUse* values, or "" when
// the function declares no `claim` parameter. dec is the function's
// decorated_definition, or nil. No decorator registers such a tool, so any
// decorator on it may check the claim before the body runs: ClaimUseWrapped.
func wrappedFuncClaimUse(fn, dec *sitter.Node, src []byte) string {
	if dec != nil && len(astutil.Decorators(dec)) > 0 {
		for _, p := range astutil.FunctionParams(fn, src) {
			if p == claimParam {
				return models.ClaimUseWrapped
			}
		}
		return ""
	}
	return pythonClaimUse(fn, nil, src)
}

// stampFactoryClaim sets the claim_use fact for a tool a factory call builds
// from the same-file function wrappedName, and returns the tool's parameter
// names. fnDef is the resolved undecorated function, or nil. A decorated
// wrapped function is not in indexTopLevelFunctions, so it is looked up here
// for its parameters and claim fact only, and a claim checked by its decorator
// is not reported as absent.
func stampFactoryClaim(fnDef *sitter.Node, wrappedName string, pf ParsedFile, params []string, facts map[string]string) []string {
	fn, dec := fnDef, (*sitter.Node)(nil)
	if fn == nil && wrappedName != "" {
		fn, dec = topLevelDecoratedFunction(pf.Tree.RootNode(), wrappedName, pf.Source)
		if fn != nil {
			params = toolParamNames(fn, pf.Source)
		}
	}
	if fn == nil {
		return params
	}
	if use := wrappedFuncClaimUse(fn, dec, pf.Source); use != "" {
		facts[models.FactClaimUse] = use
	}
	return params
}

// topLevelDecoratedFunction finds the module-level decorated function named
// name, returning the function_definition and its decorated_definition, or
// nils. indexTopLevelFunctions indexes undecorated functions only; this lookup
// is kept separate so the claim fact can see a decorated wrapped function
// without changing what the other callers of that index resolve.
func topLevelDecoratedFunction(root *sitter.Node, name string, src []byte) (fn, dec *sitter.Node) {
	for i := 0; i < int(root.NamedChildCount()); i++ {
		c := root.NamedChild(i)
		if c.Type() != "decorated_definition" {
			continue
		}
		if f := astutil.FunctionDef(c); f != nil && astutil.FunctionName(f, src) == name {
			return f, c
		}
	}
	return nil, nil
}

// isClaimRead rejects an identifier spelled `claim` that is not a read of the
// variable: the attribute name in `x.claim`, or the keyword in `f(claim=...)`.
func isClaimRead(n *sitter.Node) bool {
	p := n.Parent()
	if p == nil {
		return true
	}
	switch p.Type() {
	case "attribute":
		if a := p.ChildByFieldName("attribute"); a != nil && a.Equal(n) {
			return false
		}
	case "keyword_argument":
		if k := p.ChildByFieldName("name"); k != nil && k.Equal(n) {
			return false
		}
	}
	return true
}

// claimReadIsPassthrough follows one read of `claim` outward, stopping at fn.
func claimReadIsPassthrough(n, fn *sitter.Node, src []byte) bool {
	cur := n
	for p := n.Parent(); p != nil && !p.Equal(fn); cur, p = p, p.Parent() {
		switch p.Type() {
		case "call":
			args := p.ChildByFieldName("arguments")
			if args == nil || !args.Equal(cur) {
				// claim is the callee's receiver (claim.get(...)): the call
				// yields a value read from the claim, so keep following it.
				continue
			}
			return isClaimSinkCallee(p.ChildByFieldName("function"), src)
		case "return_statement":
			return true
		case "expression_statement", "assignment", "augmented_assignment",
			"if_statement", "elif_clause", "while_statement", "assert_statement",
			"raise_statement", "for_statement", "with_statement", "match_statement":
			return false
		}
	}
	return false
}

func isClaimSinkCallee(callee *sitter.Node, src []byte) bool {
	if callee == nil {
		return false
	}
	text := astutil.NodeText(callee, src)
	if claimSinkCallees[text] {
		return true
	}
	if callee.Type() == "attribute" {
		if i := strings.LastIndex(text, "."); i >= 0 {
			return claimSinkMethods[text[i+1:]]
		}
	}
	return false
}

// openAIToolHasInputGuardrail reports whether an OpenAI Agents SDK tool has a
// tool input guardrail attached, either as the decorator kwarg
// (@function_tool(tool_input_guardrails=[...])) or by assignment anywhere in the
// file (send_email.tool_input_guardrails = [...], the shape the SDK's own
// examples use). An input guardrail runs before the tool with the call's
// arguments, so it is where a mediation step can read the claim. Whether it
// actually checks the claim is not verified.
func openAIToolHasInputGuardrail(t models.ToolDef, root *sitter.Node, src []byte) bool {
	if _, ok := t.Config["tool_input_guardrails"]; ok {
		return true
	}
	found := false
	astutil.Walk(root, func(n *sitter.Node) bool {
		if found {
			return false
		}
		if n.Type() != "assignment" {
			return true
		}
		left := n.ChildByFieldName("left")
		if left == nil || left.Type() != "attribute" {
			return true
		}
		obj, attr := left.ChildByFieldName("object"), left.ChildByFieldName("attribute")
		if obj != nil && attr != nil && obj.Type() == "identifier" &&
			astutil.NodeText(obj, src) == t.Name && astutil.NodeText(attr, src) == "tool_input_guardrails" {
			found = true
			return false
		}
		return true
	})
	return found
}
