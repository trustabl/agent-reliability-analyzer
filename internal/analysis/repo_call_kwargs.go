package analysis

import (
	"sort"
	"strings"

	sitter "github.com/smacker/go-tree-sitter"

	"github.com/trustabl/trustabl/internal/analysis/astutil"
	"github.com/trustabl/trustabl/internal/models"
	"github.com/trustabl/trustabl/internal/pathclass"
)

// Call-site keyword discovery (AGCFG-019, AGCFG-020, AGCFG-021). It records a call that passes
// one keyword (Python) or object property (TypeScript) from a closed table with a value the
// engine can judge from the call text alone. Nothing else is recorded, so the fact list stays
// small and a rule can only ask about the shapes below.
//
// Python keywords (any callee unless noted):
//
//   - env: only on StdioServerParameters, when the file imports from `mcp` and its command (the
//     `command` keyword, or the first positional argument) is not a literal: a variable, a
//     config lookup or any other expression. A string literal, an f-string with no
//     substitution or sys.executable names the project's own server and is not recorded. Judged value
//     environ: os.environ, a name imported from os as environ, os.environ.copy(), dict(os.environ),
//     dict(**os.environ), or a dict display with a `**os.environ` entry.
//   - warn_only, ssrf_protection_enabled, trust_remote_code: judged value true (the literal True)
//     or false (the literal False). A variable, an expression or a non-literal is not recorded.
//
// TypeScript: the env property of an options object passed to StdioClientTransport (imported
// from @modelcontextprotocol/sdk/client/stdio.js), judged environ when it is process.env or an
// object with a `...process.env` entry, and only when the options object's `command` is not a string
// literal or a template with no substitution. The other three keywords are Python-only.
//
// Not judged: a value held in a variable (`env=env`), a `**kwargs` splat, a callee reached through
// a name the file does not import, any call under a test path. The callee is recorded as the last
// name of the call (`mcp.StdioServerParameters` is StdioServerParameters).

const (
	mcpStdioClassPy  = "StdioServerParameters"
	mcpStdioClassTS  = "StdioClientTransport"
	mcpStdioTSModule = "@modelcontextprotocol/sdk/client/stdio.js"
)

// callKwargBoolNames are the Python keywords judged for a True or False literal.
var callKwargBoolNames = map[string]bool{
	"warn_only": true, "ssrf_protection_enabled": true, "trust_remote_code": true,
}

// DiscoverCallKwargs returns the recorded call-site keywords of the given Python and
// TypeScript/JavaScript files, sorted by path and line.
func DiscoverCallKwargs(py, ts []ParsedFile) []models.CallKwargHit {
	var out []models.CallKwargHit
	for _, pf := range py {
		if pf.Tree == nil || pathclass.Classify(pf.RelPath) == models.OriginTest {
			continue
		}
		out = append(out, discoverPyCallKwargs(pf)...)
	}
	for _, pf := range ts {
		if pf.Tree == nil || pathclass.Classify(pf.RelPath) == models.OriginTest {
			continue
		}
		out = append(out, discoverTSCallKwargs(pf)...)
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].FilePath != out[j].FilePath {
			return out[i].FilePath < out[j].FilePath
		}
		return out[i].Line < out[j].Line
	})
	return out
}

func mcpImportModule(mod string) bool { return mod == "mcp" || strings.HasPrefix(mod, "mcp.") }

func discoverPyCallKwargs(pf ParsedFile) []models.CallKwargHit {
	src := string(pf.Source)
	if !strings.Contains(src, "ssrf_protection_enabled") && !strings.Contains(src, "warn_only") &&
		!strings.Contains(src, "trust_remote_code") && !strings.Contains(src, mcpStdioClassPy) {
		return nil
	}
	osAliases := pyImportAliases(pf, func(m string) bool { return m == "os" })
	osMods := map[string]bool{}
	environNames := map[string]bool{}
	for local, canon := range osAliases {
		switch canon {
		case "*":
			osMods[local] = true
		case "environ":
			environNames[local] = true
		}
	}
	mcpAliases := pyImportAliases(pf, mcpImportModule)
	var out []models.CallKwargHit
	astutil.Walk(pf.Tree.RootNode(), func(n *sitter.Node) bool {
		if n.Type() != "call" {
			return true
		}
		args := n.ChildByFieldName("arguments")
		if args == nil {
			return true
		}
		callee := pyCalleeTail(n.ChildByFieldName("function"), pf.Source)
		if callee == "" {
			return true
		}
		if canon, ok := mcpAliases[callee]; ok && canon != "*" {
			callee = canon
		}
		for i := 0; i < int(args.NamedChildCount()); i++ {
			kw := args.NamedChild(i)
			if kw.Type() != "keyword_argument" {
				continue
			}
			nameNode, valNode := kw.ChildByFieldName("name"), kw.ChildByFieldName("value")
			if nameNode == nil || valNode == nil {
				continue
			}
			name := astutil.NodeText(nameNode, pf.Source)
			value := ""
			switch {
			case name == "env" && callee == mcpStdioClassPy && len(mcpAliases) > 0:
				if pyIsEnviron(valNode, pf.Source, osMods, environNames) && pyCommandNonLiteral(args, pf.Source) {
					value = "environ"
				}
			case callKwargBoolNames[name]:
				switch strings.TrimSpace(astutil.NodeText(valNode, pf.Source)) {
				case "True":
					value = "true"
				case "False":
					value = "false"
				}
			}
			if value == "" {
				continue
			}
			out = append(out, models.CallKwargHit{
				FilePath: pf.RelPath, Line: int(n.StartPoint().Row) + 1, Language: models.LanguagePython,
				Callee: callee, Kwarg: name, Value: value,
			})
		}
		return true
	})
	return out
}

// pyCommandNonLiteral reports whether the launch command of a StdioServerParameters call is not a
// literal. The command is the `command` keyword or the first positional argument; a call that
// gives neither, or splats **kwargs, is not judged (false).
func pyCommandNonLiteral(args *sitter.Node, src []byte) bool {
	var cmd *sitter.Node
	for i := 0; i < int(args.NamedChildCount()); i++ {
		c := args.NamedChild(i)
		switch c.Type() {
		case "keyword_argument":
			if n := c.ChildByFieldName("name"); n != nil && astutil.NodeText(n, src) == "command" {
				cmd = c.ChildByFieldName("value")
			}
		case "dictionary_splat", "list_splat", "comment":
		default:
			if cmd == nil && i == 0 {
				cmd = c
			}
		}
	}
	if cmd == nil {
		return false
	}
	switch cmd.Type() {
	case "string":
		// An f-string with a substitution has an interpolation child.
		for i := 0; i < int(cmd.NamedChildCount()); i++ {
			if cmd.NamedChild(i).Type() == "interpolation" {
				return true
			}
		}
		return false
	case "concatenated_string":
		return false
	}
	return strings.TrimSpace(astutil.NodeText(cmd, src)) != "sys.executable"
}

// pyCalleeTail returns the last name of a call's function expression: the identifier, or the
// attribute of an attribute chain. Anything else (a call result, a subscript) returns "".
func pyCalleeTail(fn *sitter.Node, src []byte) string {
	if fn == nil {
		return ""
	}
	switch fn.Type() {
	case "identifier":
		return astutil.NodeText(fn, src)
	case "attribute":
		if a := fn.ChildByFieldName("attribute"); a != nil {
			return astutil.NodeText(a, src)
		}
	}
	return ""
}

// pyIsEnviron reports whether the expression is the whole parent environment or a copy of it.
func pyIsEnviron(n *sitter.Node, src []byte, osMods, environNames map[string]bool) bool {
	if n == nil {
		return false
	}
	switch n.Type() {
	case "identifier":
		return environNames[astutil.NodeText(n, src)]
	case "attribute":
		obj, attr := n.ChildByFieldName("object"), n.ChildByFieldName("attribute")
		return obj != nil && attr != nil && obj.Type() == "identifier" &&
			osMods[astutil.NodeText(obj, src)] && astutil.NodeText(attr, src) == "environ"
	case "parenthesized_expression":
		return n.NamedChildCount() == 1 && pyIsEnviron(n.NamedChild(0), src, osMods, environNames)
	case "dictionary":
		for i := 0; i < int(n.NamedChildCount()); i++ {
			c := n.NamedChild(i)
			if c.Type() == "dictionary_splat" && c.NamedChildCount() == 1 &&
				pyIsEnviron(c.NamedChild(0), src, osMods, environNames) {
				return true
			}
		}
	case "call":
		fn := n.ChildByFieldName("function")
		if fn == nil {
			return false
		}
		if fn.Type() == "attribute" {
			if a := fn.ChildByFieldName("attribute"); a != nil && astutil.NodeText(a, src) == "copy" {
				return pyIsEnviron(fn.ChildByFieldName("object"), src, osMods, environNames)
			}
			return false
		}
		if fn.Type() == "identifier" && astutil.NodeText(fn, src) == "dict" {
			args := n.ChildByFieldName("arguments")
			if args == nil {
				return false
			}
			for i := 0; i < int(args.NamedChildCount()); i++ {
				c := args.NamedChild(i)
				if c.Type() == "dictionary_splat" && c.NamedChildCount() == 1 &&
					pyIsEnviron(c.NamedChild(0), src, osMods, environNames) {
					return true
				}
				if i == 0 && c.Type() != "keyword_argument" && pyIsEnviron(c, src, osMods, environNames) {
					return true
				}
			}
		}
	}
	return false
}

func discoverTSCallKwargs(pf ParsedFile) []models.CallKwargHit {
	root := pf.Tree.RootNode()
	locals := map[string]bool{}
	for local, canon := range astutil.TSImportAliasesAny(root, pf.Source, []string{mcpStdioTSModule}) {
		if canon == mcpStdioClassTS {
			locals[local] = true
		}
	}
	if len(locals) == 0 {
		return nil
	}
	var out []models.CallKwargHit
	astutil.Walk(root, func(n *sitter.Node) bool {
		if n.Type() != "new_expression" && n.Type() != "call_expression" {
			return true
		}
		fn := n.ChildByFieldName("constructor")
		if fn == nil {
			fn = n.ChildByFieldName("function")
		}
		if fn == nil || fn.Type() != "identifier" || !locals[astutil.NodeText(fn, pf.Source)] {
			return true
		}
		args := n.ChildByFieldName("arguments")
		if args == nil {
			return true
		}
		for i := 0; i < int(args.NamedChildCount()); i++ {
			opts := args.NamedChild(i)
			if opts.Type() != "object" {
				continue
			}
			if v := getObjectProperty(opts, "env", pf.Source); v != nil && tsIsProcessEnv(v, pf.Source) && tsCommandNonLiteral(opts, pf.Source) {
				out = append(out, models.CallKwargHit{
					FilePath: pf.RelPath, Line: int(n.StartPoint().Row) + 1, Language: models.LanguageTypeScript,
					Callee: mcpStdioClassTS, Kwarg: "env", Value: "environ",
				})
			}
		}
		return true
	})
	return out
}

// tsIsProcessEnv reports whether the expression is process.env (possibly parenthesized or cast) or
// an object display that spreads it.
func tsIsProcessEnv(n *sitter.Node, src []byte) bool {
	switch n.Type() {
	case "member_expression":
		return strings.TrimSpace(astutil.NodeText(n, src)) == "process.env"
	case "parenthesized_expression", "as_expression", "satisfies_expression", "non_null_expression":
		// A type cast (process.env as Record<string, string>) does not change the value.
		return n.NamedChildCount() >= 1 && tsIsProcessEnv(n.NamedChild(0), src)
	case "object":
		for i := 0; i < int(n.NamedChildCount()); i++ {
			c := n.NamedChild(i)
			if c.Type() == "spread_element" && c.NamedChildCount() == 1 && tsIsProcessEnv(c.NamedChild(0), src) {
				return true
			}
		}
	}
	return false
}

// tsCommandNonLiteral reports whether the options object has a command that is not a string
// literal or a template with no substitution. An object with no command property is not judged.
func tsCommandNonLiteral(opts *sitter.Node, src []byte) bool {
	c := getObjectProperty(opts, "command", src)
	if c == nil {
		return false
	}
	switch c.Type() {
	case "string":
		return false
	case "template_string":
		return strings.Contains(astutil.NodeText(c, src), "${")
	}
	return true
}
