package analysis

import (
	"sort"
	"strings"

	sitter "github.com/smacker/go-tree-sitter"

	"github.com/trustabl/trustabl/internal/analysis/astutil"
	"github.com/trustabl/trustabl/internal/models"
	"github.com/trustabl/trustabl/internal/pathclass"
)

// MCP server bind discovery (MCP-203). It records a run call, or an SDK HTTP app
// helper call, that starts an MCP server on an HTTP transport with a LITERAL
// wildcard host (0.0.0.0, :: or [::]) when the same file names no authentication
// and no host or origin allow-list.
//
// Why only those: the MCP specification's Streamable HTTP section says servers
// "SHOULD bind only to localhost (127.0.0.1) rather than all network interfaces
// (0.0.0.0)" and "SHOULD implement proper authentication", and both SDKs switch
// their DNS-rebinding check on only for a loopback host (python-sdk v1.27.0 /
// v2.3.0 `transport_security is None and host in ("127.0.0.1", "localhost",
// "::1")`; typescript-sdk v1.29.0 createMcpExpressApp). A host from a variable, an
// f-string or an environment lookup, a default host, and a loopback literal are
// not judged. The host text is reduced to this judgment and never stored.
//
// Python receivers, for FastMCP(...) / MCPServer(...) imported from `mcp.server.fastmcp`,
// `mcp.server.mcpserver`, `mcp.server` or `fastmcp`: a variable bound in the same file to the
// constructor; the constructor call itself (`FastMCP(...).run(...)`); or a call to a one-hop
// same-file factory, a top-level function whose single `return` is the constructor call or a
// variable assigned once to it (`make().run(...)`). Run methods: `run` (transport keyword),
// `run_sse_async` and `run_streamable_http_async` (transport implied). Credit: an `auth`, `token_verifier` or
// `auth_server_provider` argument on the constructor, or `transport_security` on
// the constructor or the run call. A `**kwargs` splat on either call is not read
// and silences the hit. A factory that is imported, decorated, nested, defined twice, has more than one `return`, or
// calls another factory is not followed; a server variable bound in another file is not seen.
//
// TypeScript: createMcpExpressApp from "@modelcontextprotocol/sdk/server/express.js"
// (SDK v1; the v2 packages are not read). Credit: an `allowedHosts` property, an
// object spread in the options, or any import from
// "@modelcontextprotocol/sdk/server/auth/...". Files under a test path are skipped.

const (
	mcpBindReasonPython = "MCP server run call uses an HTTP transport with a wildcard host and no authentication or host allow-list in the file"
	mcpBindReasonTS     = "createMcpExpressApp is given a wildcard host and the file names no allowedHosts and imports no SDK auth helper"
)

var mcpServerClassNames = map[string]bool{"FastMCP": true, "MCPServer": true}

var mcpHTTPTransports = map[string]bool{"sse": true, "http": true, "streamable-http": true}

// DiscoverMCPServerBinds returns the unprotected wildcard binds in the given
// Python and TypeScript/JavaScript files, sorted by path and line.
func DiscoverMCPServerBinds(py, ts []ParsedFile) []models.MCPServerBind {
	var out []models.MCPServerBind
	for _, pf := range py {
		if pf.Tree == nil || pathclass.Classify(pf.RelPath) == models.OriginTest {
			continue
		}
		out = append(out, discoverPyMCPServerBinds(pf)...)
	}
	for _, pf := range ts {
		if pf.Tree == nil || pathclass.Classify(pf.RelPath) == models.OriginTest {
			continue
		}
		out = append(out, discoverTSMCPServerBinds(pf)...)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].FilePath != out[j].FilePath {
			return out[i].FilePath < out[j].FilePath
		}
		return out[i].Line < out[j].Line
	})
	return out
}

func isWildcardHost(v string) bool { return v == "0.0.0.0" || v == "::" || v == "[::]" }

// pyPlainStringLiteral returns the content of a Python string literal that has
// no prefix and no interpolation, and false for anything else (an identifier, a
// call, an f-string, a concatenation).
func pyPlainStringLiteral(raw string) (string, bool) {
	raw = strings.TrimSpace(raw)
	if len(raw) < 2 {
		return "", false
	}
	q := raw[0]
	if (q != '"' && q != '\'') || raw[len(raw)-1] != q {
		return "", false
	}
	inner := raw[1 : len(raw)-1]
	if strings.ContainsRune(inner, rune(q)) || strings.ContainsRune(inner, '\\') {
		return "", false
	}
	return inner, true
}

func pyCallHasSplat(call *sitter.Node) bool {
	args := call.ChildByFieldName("arguments")
	if args == nil {
		return false
	}
	for i := 0; i < int(args.NamedChildCount()); i++ {
		if args.NamedChild(i).Type() == "dictionary_splat" {
			return true
		}
	}
	return false
}

// pyKwargSet reports whether the call passes the named keyword with a value other than None.
func pyKwargSet(call *sitter.Node, src []byte, name string) bool {
	v, ok := astutil.KwargValue(call, src, name)
	return ok && strings.TrimSpace(v) != "None"
}

type pyMCPImports struct {
	classes map[string]bool // local names bound to FastMCP / MCPServer
	roots   map[string]bool // local names bound to an MCP module (import fastmcp)
}

func mcpModuleOK(mod string) bool {
	return mod == "fastmcp" || strings.HasPrefix(mod, "fastmcp.") || mod == "mcp.server" ||
		strings.HasPrefix(mod, "mcp.server.")
}

func collectPyMCPImports(pf ParsedFile) pyMCPImports {
	imp := pyMCPImports{classes: map[string]bool{}, roots: map[string]bool{}}
	astutil.Walk(pf.Tree.RootNode(), func(n *sitter.Node) bool {
		switch n.Type() {
		case "import_from_statement":
			mod := astutil.NodeText(n.ChildByFieldName("module_name"), pf.Source)
			if !mcpModuleOK(mod) {
				return true
			}
			for i := 0; i < int(n.NamedChildCount()); i++ {
				c := n.NamedChild(i)
				switch c.Type() {
				case "dotted_name":
					if c == n.ChildByFieldName("module_name") {
						continue
					}
					if nm := astutil.NodeText(c, pf.Source); mcpServerClassNames[nm] {
						imp.classes[nm] = true
					}
				case "aliased_import":
					nm := astutil.NodeText(c.ChildByFieldName("name"), pf.Source)
					if mcpServerClassNames[nm] {
						imp.classes[astutil.NodeText(c.ChildByFieldName("alias"), pf.Source)] = true
					}
				}
			}
		case "import_statement":
			for i := 0; i < int(n.NamedChildCount()); i++ {
				c := n.NamedChild(i)
				switch c.Type() {
				case "dotted_name":
					if nm := astutil.NodeText(c, pf.Source); mcpModuleOK(nm) {
						imp.roots[strings.SplitN(nm, ".", 2)[0]] = true
					}
				case "aliased_import":
					if nm := astutil.NodeText(c.ChildByFieldName("name"), pf.Source); mcpModuleOK(nm) {
						imp.roots[astutil.NodeText(c.ChildByFieldName("alias"), pf.Source)] = true
					}
				}
			}
		}
		return true
	})
	return imp
}

func (imp pyMCPImports) isServerCall(call *sitter.Node, src []byte) bool {
	fn := call.ChildByFieldName("function")
	if fn == nil {
		return false
	}
	text := astutil.NodeText(fn, src)
	i := strings.LastIndex(text, ".")
	if i < 0 {
		return imp.classes[text]
	}
	return mcpServerClassNames[text[i+1:]] && imp.roots[strings.SplitN(text, ".", 2)[0]]
}

// pyRunMethods are the server methods that start an HTTP transport. `run` takes the transport
// as a keyword; the two async forms imply it (python-sdk v2.3.0 and v1.27.0 define both with a
// `host` parameter).
var pyRunMethods = map[string]string{
	"run":                       "",
	"run_sse_async":             "sse",
	"run_streamable_http_async": "streamable-http",
}

// pyFactoryServers maps the name of each top-level, undecorated function in the file that
// returns an MCP server to its constructor call. A function counts when it has exactly one
// `return` (nested functions, lambdas and classes are not entered) and that return is either a
// FastMCP(...) / MCPServer(...) call or an identifier assigned exactly once in the function, to
// such a call. A name defined twice at the top level is dropped. One hop only: a factory that
// calls another factory is not followed.
func pyFactoryServers(pf ParsedFile, imp pyMCPImports) map[string]*sitter.Node {
	root := pf.Tree.RootNode()
	defs := map[string]int{}
	var fns []*sitter.Node
	for i := 0; i < int(root.NamedChildCount()); i++ {
		c := root.NamedChild(i)
		if c.Type() != "function_definition" {
			continue
		}
		name := astutil.NodeText(c.ChildByFieldName("name"), pf.Source)
		defs[name]++
		fns = append(fns, c)
	}
	out := map[string]*sitter.Node{}
	for _, fn := range fns {
		name := astutil.NodeText(fn.ChildByFieldName("name"), pf.Source)
		if defs[name] != 1 {
			continue
		}
		body := fn.ChildByFieldName("body")
		if body == nil {
			continue
		}
		var returns []*sitter.Node
		assigns := map[string][]*sitter.Node{}
		astutil.Walk(body, func(n *sitter.Node) bool {
			switch n.Type() {
			case "function_definition", "lambda", "class_definition":
				return false
			case "return_statement":
				returns = append(returns, n)
			case "assignment":
				if l := n.ChildByFieldName("left"); l != nil && l.Type() == "identifier" {
					id := astutil.NodeText(l, pf.Source)
					assigns[id] = append(assigns[id], n)
				}
			}
			return true
		})
		if len(returns) != 1 || returns[0].NamedChildCount() != 1 {
			continue
		}
		val := returns[0].NamedChild(0)
		switch val.Type() {
		case "call":
			if imp.isServerCall(val, pf.Source) {
				out[name] = val
			}
		case "identifier":
			as := assigns[astutil.NodeText(val, pf.Source)]
			if len(as) != 1 {
				continue
			}
			if r := as[0].ChildByFieldName("right"); r != nil && r.Type() == "call" && imp.isServerCall(r, pf.Source) {
				out[name] = r
			}
		}
	}
	return out
}

func discoverPyMCPServerBinds(pf ParsedFile) []models.MCPServerBind {
	imp := collectPyMCPImports(pf)
	if len(imp.classes) == 0 && len(imp.roots) == 0 {
		return nil
	}
	ctors := map[string]*sitter.Node{} // server variable -> constructor call
	astutil.Walk(pf.Tree.RootNode(), func(n *sitter.Node) bool {
		if n.Type() != "assignment" {
			return true
		}
		l, r := n.ChildByFieldName("left"), n.ChildByFieldName("right")
		if l == nil || r == nil || l.Type() != "identifier" || r.Type() != "call" {
			return true
		}
		if imp.isServerCall(r, pf.Source) {
			ctors[astutil.NodeText(l, pf.Source)] = r
		}
		return true
	})
	factories := pyFactoryServers(pf, imp)
	// receiverCtor resolves the object of a `.run(...)` call to a constructor call: a variable
	// bound in the file to a server constructor, the constructor call itself
	// (FastMCP(...).run(...)), or a call to a one-hop same-file factory (make().run(...)).
	receiverCtor := func(obj *sitter.Node) *sitter.Node {
		switch obj.Type() {
		case "identifier":
			return ctors[astutil.NodeText(obj, pf.Source)]
		case "call":
			if imp.isServerCall(obj, pf.Source) {
				return obj
			}
			if f := obj.ChildByFieldName("function"); f != nil && f.Type() == "identifier" {
				return factories[astutil.NodeText(f, pf.Source)]
			}
		}
		return nil
	}
	var out []models.MCPServerBind
	astutil.Walk(pf.Tree.RootNode(), func(n *sitter.Node) bool {
		if n.Type() != "call" {
			return true
		}
		fn := n.ChildByFieldName("function")
		if fn == nil || fn.Type() != "attribute" {
			return true
		}
		obj, attr := fn.ChildByFieldName("object"), fn.ChildByFieldName("attribute")
		if obj == nil || attr == nil {
			return true
		}
		impliedTransport, isRun := pyRunMethods[astutil.NodeText(attr, pf.Source)]
		if !isRun {
			return true
		}
		ctor := receiverCtor(obj)
		if ctor == nil || pyCallHasSplat(n) || pyCallHasSplat(ctor) {
			return true
		}
		transport := impliedTransport
		if transport == "" {
			tv, present := astutil.KwargValue(n, pf.Source, "transport")
			t, lit := pyPlainStringLiteral(tv)
			if !present || !lit || !mcpHTTPTransports[t] {
				return true
			}
			transport = t
		}
		// The run call's host wins; otherwise the v1 constructor's host applies.
		hv, hostPresent := astutil.KwargValue(n, pf.Source, "host")
		if !hostPresent {
			hv, hostPresent = astutil.KwargValue(ctor, pf.Source, "host")
		}
		host, lit := pyPlainStringLiteral(hv)
		if !hostPresent || !lit || !isWildcardHost(host) {
			return true
		}
		for _, k := range []string{"auth", "token_verifier", "auth_server_provider"} {
			if pyKwargSet(ctor, pf.Source, k) {
				return true
			}
		}
		if pyKwargSet(ctor, pf.Source, "transport_security") || pyKwargSet(n, pf.Source, "transport_security") {
			return true
		}
		out = append(out, models.MCPServerBind{
			FilePath: pf.RelPath, Line: astutil.NodeLine(n), Language: models.LanguagePython,
			Transport: transport, Reason: mcpBindReasonPython,
		})
		return true
	})
	return out
}

const (
	tsMCPExpressModule = "@modelcontextprotocol/sdk/server/express.js"
	tsMCPAuthPrefix    = "@modelcontextprotocol/sdk/server/auth/"
)

// tsObjectHasKey reports whether an object literal names key (as a pair or a
// shorthand property), and whether it holds a spread element.
func tsObjectHasKey(obj *sitter.Node, key string, src []byte) (has, spread bool) {
	for i := 0; i < int(obj.NamedChildCount()); i++ {
		c := obj.NamedChild(i)
		switch c.Type() {
		case "spread_element":
			spread = true
		case "shorthand_property_identifier":
			if astutil.NodeText(c, src) == key {
				has = true
			}
		case "pair":
			if k := c.ChildByFieldName("key"); k != nil {
				t := astutil.NodeText(k, src)
				if t == key || t == `"`+key+`"` || t == `'`+key+`'` {
					has = true
				}
			}
		}
	}
	return has, spread
}

func discoverTSMCPServerBinds(pf ParsedFile) []models.MCPServerBind {
	root := pf.Tree.RootNode()
	locals := map[string]bool{}
	for local, canon := range astutil.TSImportAliasesAny(root, pf.Source, []string{tsMCPExpressModule}) {
		if canon == "createMcpExpressApp" {
			locals[local] = true
		}
	}
	if len(locals) == 0 {
		return nil
	}
	authImported := len(astutil.TSImportAliasesMatch(root, pf.Source, func(m string) bool {
		return strings.HasPrefix(m, tsMCPAuthPrefix)
	})) > 0
	if authImported {
		return nil
	}
	var out []models.MCPServerBind
	astutil.Walk(root, func(n *sitter.Node) bool {
		if n.Type() != "call_expression" {
			return true
		}
		fn := n.ChildByFieldName("function")
		if fn == nil || fn.Type() != "identifier" || !locals[astutil.NodeText(fn, pf.Source)] {
			return true
		}
		args := n.ChildByFieldName("arguments")
		if args == nil || args.NamedChildCount() == 0 || args.NamedChild(0).Type() != "object" {
			return true
		}
		opts := args.NamedChild(0)
		if has, spread := tsObjectHasKey(opts, "allowedHosts", pf.Source); has || spread {
			return true
		}
		h := getObjectProperty(opts, "host", pf.Source)
		if h == nil || !isWildcardHost(tsStringLiteralText(h, pf.Source)) {
			return true
		}
		out = append(out, models.MCPServerBind{
			FilePath: pf.RelPath, Line: int(n.StartPoint().Row) + 1, Language: models.LanguageTypeScript,
			Transport: "express", Reason: mcpBindReasonTS,
		})
		return true
	})
	return out
}
