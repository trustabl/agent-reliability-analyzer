package analysis

import (
	"regexp"
	"sort"
	"strings"

	sitter "github.com/smacker/go-tree-sitter"

	"github.com/trustabl/trustabl/internal/analysis/astutil"
	"github.com/trustabl/trustabl/internal/models"
	"github.com/trustabl/trustabl/internal/pathclass"
)

// MCP code discovery (MCP-204 to MCP-210). Each pass reads one file at a time, only when the
// file imports an MCP SDK or client package, and records the site of one code pattern as an
// MCPCodeFact. Files under a test path are skipped, and comment text is blanked before any
// credit search so a comment cannot silence or trigger a rule.
//
// What each pass reads:
//
//   - prefix containment (TypeScript and JavaScript): `<path-like>.startsWith(<root-like>)`
//     where the root argument is not a string literal and its text names no separator.
//   - stdio client command (Python and TypeScript): StdioServerParameters, MCPServerStdio
//     params and MultiServerMCPClient entries; StdioClientTransport. A shell named with -c is a
//     hit; so is a command whose expression text names a request-like source (request, req,
//     body, payload, query, params, form, json, input), unless the file names an allow-list.
//     A command taken from a configuration object or a parameter is not judged.
//   - bearer without audience (Python): AuthSettings in a file that names token_verifier,
//     with validate_token_resource absent, None or False and no audience text in the file.
//   - client opens server URL (Python and TypeScript): a redirect handler (a function that
//     takes the URL and opens it) with no scheme or origin check in its body.
//   - shared transport (TypeScript, package @modelcontextprotocol/server): a server or an HTTP
//     transport constructed at module scope and connected or handed a request inside a function.
//   - DNS rebinding off (Python): TransportSecuritySettings(enable_dns_rebinding_protection=False)
//     in a file that names no token verifier or auth settings.
//   - package install specifier (TypeScript): npm or pnpm add/install whose specifier is built
//     from a value, in a file with no .test( or semver check.

var (
	tsPathLikeRe = regexp.MustCompile(`(?i)path|file|dir|target|real|resolved|normalized|absolute|requested`)
	urlLikeRe    = regexp.MustCompile(`(?i)pathname|url|href|route|endpoint`)
	untrustedRe  = regexp.MustCompile(`(?i)\b(request|req|body|payload|query|params|form|json|input)\b`)
	tsRootLikeRe = regexp.MustCompile(`(?i)dir|root|base|allowed|path|cwd|folder|workspace|sandbox`)
	allowListRe  = regexp.MustCompile(`(?i)allow(ed)?[_-]?(list|commands?|executables?)|whitelist`)
	audienceRe   = regexp.MustCompile(`(?i)\baud\b|audience`)
	authNamesRe  = regexp.MustCompile(`token_verifier|auth_server_provider|AuthSettings|\bauth\s*=`)
	validateRe   = regexp.MustCompile(`\.test\(|semver|\.valid\(|\.satisfies\(`)
	urlCheckRe   = regexp.MustCompile(`(?i)\.protocol|\.origin|\.hostname|\.host\b|\.scheme|\.netloc|urlparse|urlsplit|startswith\(\s*["']https?|startsWith\(\s*["']https?|new URL\(|allowed|allowlist`)
	shellNames   = map[string]bool{"bash": true, "sh": true, "zsh": true, "dash": true, "cmd": true, "cmd.exe": true, "powershell": true, "powershell.exe": true, "pwsh": true}
)

// DiscoverMCPCode returns the MCP code facts in the given Python and TypeScript or JavaScript
// files, sorted by path and line.
func DiscoverMCPCode(py, ts []ParsedFile) []models.MCPCodeFact {
	var out []models.MCPCodeFact
	for _, pf := range py {
		if pf.Tree == nil || pathclass.Classify(pf.RelPath) == models.OriginTest {
			continue
		}
		out = append(out, discoverPyMCPCode(pf)...)
	}
	for _, pf := range ts {
		if pf.Tree == nil || pathclass.Classify(pf.RelPath) == models.OriginTest {
			continue
		}
		out = append(out, discoverTSMCPCode(pf)...)
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].FilePath != out[j].FilePath {
			return out[i].FilePath < out[j].FilePath
		}
		if out[i].Line != out[j].Line {
			return out[i].Line < out[j].Line
		}
		return out[i].Kind < out[j].Kind
	})
	return out
}

// maskedSource returns the file text with each comment node replaced by spaces.
func maskedSource(pf ParsedFile) string {
	b := append([]byte(nil), pf.Source...)
	astutil.Walk(pf.Tree.RootNode(), func(n *sitter.Node) bool {
		if n.Type() == "comment" {
			for i := n.StartByte(); i < n.EndByte() && int(i) < len(b); i++ {
				if b[i] != '\n' {
					b[i] = ' '
				}
			}
			return false
		}
		return true
	})
	return string(b)
}

func isFunctionNode(t string) bool {
	switch t {
	case "function_definition", "lambda", "function_declaration", "function_expression", "function",
		"arrow_function", "method_definition", "generator_function", "generator_function_declaration":
		return true
	}
	return false
}

func insideFunction(n *sitter.Node) bool {
	for p := n.Parent(); p != nil; p = p.Parent() {
		if isFunctionNode(p.Type()) {
			return true
		}
	}
	return false
}

func enclosingFunction(n *sitter.Node) *sitter.Node {
	for p := n.Parent(); p != nil; p = p.Parent() {
		if isFunctionNode(p.Type()) {
			return p
		}
	}
	return nil
}

// ─── Python ───────────────────────────────────────────────────────────────

// pyImportsFrom collects the local names bound by `from <module> import <name>` for modules
// accepted by match, mapped to the imported name, plus the local names of `import <module>`.
func pyImportsFrom(pf ParsedFile, match func(mod string) bool) (names map[string]string, modules map[string]bool) {
	names, modules = map[string]string{}, map[string]bool{}
	astutil.Walk(pf.Tree.RootNode(), func(n *sitter.Node) bool {
		switch n.Type() {
		case "import_from_statement":
			mod := astutil.NodeText(n.ChildByFieldName("module_name"), pf.Source)
			if !match(mod) {
				return true
			}
			for i := 0; i < int(n.NamedChildCount()); i++ {
				c := n.NamedChild(i)
				switch c.Type() {
				case "dotted_name":
					if c == n.ChildByFieldName("module_name") {
						continue
					}
					nm := astutil.NodeText(c, pf.Source)
					names[nm] = nm
				case "aliased_import":
					names[astutil.NodeText(c.ChildByFieldName("alias"), pf.Source)] = astutil.NodeText(c.ChildByFieldName("name"), pf.Source)
				}
			}
		case "import_statement":
			for i := 0; i < int(n.NamedChildCount()); i++ {
				c := n.NamedChild(i)
				switch c.Type() {
				case "dotted_name":
					if nm := astutil.NodeText(c, pf.Source); match(nm) {
						modules[strings.SplitN(nm, ".", 2)[0]] = true
					}
				case "aliased_import":
					if nm := astutil.NodeText(c.ChildByFieldName("name"), pf.Source); match(nm) {
						modules[astutil.NodeText(c.ChildByFieldName("alias"), pf.Source)] = true
					}
				}
			}
		}
		return true
	})
	return names, modules
}

func pyModRoot(mod, root string) bool { return mod == root || strings.HasPrefix(mod, root+".") }

// pyCalleeIs reports whether the call's callee is the imported class `canon`: a local name bound
// by from-import, or an attribute of an imported module.
func pyCalleeIs(call *sitter.Node, src []byte, canon string, names map[string]string, modules map[string]bool) bool {
	fn := call.ChildByFieldName("function")
	if fn == nil {
		return false
	}
	text := astutil.NodeText(fn, src)
	if i := strings.LastIndex(text, "."); i >= 0 {
		return text[i+1:] == canon && modules[strings.SplitN(text, ".", 2)[0]]
	}
	return names[text] == canon
}

// pyLiteralBindings maps each identifier assigned exactly once in the file, to a plain string
// literal, to that literal.
func pyLiteralBindings(pf ParsedFile) map[string]string {
	counts := map[string]int{}
	lits := map[string]string{}
	astutil.Walk(pf.Tree.RootNode(), func(n *sitter.Node) bool {
		if n.Type() != "assignment" {
			return true
		}
		l, r := n.ChildByFieldName("left"), n.ChildByFieldName("right")
		if l == nil || l.Type() != "identifier" {
			return true
		}
		id := astutil.NodeText(l, pf.Source)
		counts[id]++
		if r != nil {
			if v, ok := pyPlainStringLiteral(astutil.NodeText(r, pf.Source)); ok {
				lits[id] = v
			}
		}
		return true
	})
	out := map[string]string{}
	for id, v := range lits {
		if counts[id] == 1 {
			out[id] = v
		}
	}
	return out
}

func baseCommand(v string) string {
	v = strings.ToLower(strings.TrimSpace(v))
	if i := strings.LastIndexAny(v, `/\`); i >= 0 {
		v = v[i+1:]
	}
	return v
}

func hasShellFlag(text string) bool {
	for _, f := range []string{`"-c"`, `'-c'`, `"/c"`, `'/c'`, `"/C"`, `'/C'`, `-Command`, `"-lc"`, `'-lc'`} {
		if strings.Contains(text, f) {
			return true
		}
	}
	return false
}

func discoverPyMCPCode(pf ParsedFile) []models.MCPCodeFact {
	names, modules := pyImportsFrom(pf, func(m string) bool {
		return pyModRoot(m, "mcp") || pyModRoot(m, "fastmcp") || pyModRoot(m, "agents") ||
			pyModRoot(m, "langchain_mcp_adapters") || pyModRoot(m, "pydantic_ai")
	})
	authNames, authMods := pyImportsFrom(pf, func(m string) bool { return pyModRoot(m, "mcp") || pyModRoot(m, "fastmcp") })
	if len(names) == 0 && len(modules) == 0 && len(authNames) == 0 && len(authMods) == 0 {
		return nil
	}
	masked := maskedSource(pf)
	var out []models.MCPCodeFact
	add := func(k models.MCPCodeKind, n *sitter.Node) {
		out = append(out, models.MCPCodeFact{Kind: k, FilePath: pf.RelPath, Line: astutil.NodeLine(n), Language: models.LanguagePython})
	}
	lits := pyLiteralBindings(pf)
	stdioClasses := []string{"StdioServerParameters", "MCPServerStdio", "MultiServerMCPClient"}
	allowListed := allowListRe.MatchString(masked)

	astutil.Walk(pf.Tree.RootNode(), func(n *sitter.Node) bool {
		if n.Type() != "call" {
			return true
		}
		// MCP-205: stdio client command.
		for _, c := range stdioClasses {
			if pyCalleeIs(n, pf.Source, c, names, modules) {
				if pyStdioCommandHit(n, pf, lits, allowListed) {
					add(models.MCPCodeStdioCommand, n)
				}
				break
			}
		}
		// MCP-206: AuthSettings with a token verifier and no audience check.
		if pyCalleeIs(n, pf.Source, "AuthSettings", authNames, authMods) && !pyCallHasSplat(n) &&
			strings.Contains(masked, "token_verifier") && !audienceRe.MatchString(masked) {
			v, present := astutil.KwargValue(n, pf.Source, "validate_token_resource")
			if v = strings.TrimSpace(v); !present || v == "None" || v == "False" {
				add(models.MCPCodeBearerNoAudience, n)
			}
		}
		// MCP-209: DNS rebinding protection switched off with no authentication in the file.
		if pyCalleeIs(n, pf.Source, "TransportSecuritySettings", authNames, authMods) && !pyCallHasSplat(n) {
			if v, present := astutil.KwargValue(n, pf.Source, "enable_dns_rebinding_protection"); present && strings.TrimSpace(v) == "False" &&
				!authNamesRe.MatchString(masked) {
				add(models.MCPCodeDNSRebindingOff, n)
			}
		}
		return true
	})
	out = append(out, pyClientOpensURL(pf, masked)...)
	return out
}

// pyStdioCommandHit judges one stdio client construction.
func pyStdioCommandHit(call *sitter.Node, pf ParsedFile, lits map[string]string, allowListed bool) bool {
	var values []*sitter.Node
	if args := call.ChildByFieldName("arguments"); args != nil {
		for i := 0; i < int(args.NamedChildCount()); i++ {
			c := args.NamedChild(i)
			if c.Type() == "keyword_argument" && astutil.NodeText(c.ChildByFieldName("name"), pf.Source) == "command" {
				values = append(values, c.ChildByFieldName("value"))
			}
		}
	}
	astutil.Walk(call, func(n *sitter.Node) bool {
		if n.Type() != "pair" {
			return true
		}
		if k, ok := pyPlainStringLiteral(astutil.NodeText(n.ChildByFieldName("key"), pf.Source)); ok && k == "command" {
			values = append(values, n.ChildByFieldName("value"))
		}
		return true
	})
	callText := astutil.NodeText(call, pf.Source)
	for _, v := range values {
		if v == nil {
			continue
		}
		raw := astutil.NodeText(v, pf.Source)
		lit, isLit := pyPlainStringLiteral(raw)
		if !isLit && v.Type() == "identifier" {
			lit, isLit = lits[raw]
		}
		if isLit {
			if shellNames[baseCommand(lit)] && hasShellFlag(callText) {
				return true
			}
			continue
		}
		if allowListed || !untrustedRe.MatchString(raw) {
			continue
		}
		return true
	}
	return false
}

var pyOpenerCallees = map[string]bool{
	"webbrowser.open": true, "webbrowser.open_new": true, "webbrowser.open_new_tab": true,
	"os.startfile": true, "os.system": true,
}

// pyClientOpensURL finds a function that takes one URL parameter and passes it to an OS opener
// with no scheme or origin check in its body (MCP-207). The file must import an MCP client module.
func pyClientOpensURL(pf ParsedFile, masked string) []models.MCPCodeFact {
	_, mods := pyImportsFrom(pf, func(m string) bool { return pyModRoot(m, "mcp") })
	names, _ := pyImportsFrom(pf, func(m string) bool { return pyModRoot(m, "mcp") })
	if len(mods) == 0 && len(names) == 0 {
		return nil
	}
	var out []models.MCPCodeFact
	astutil.Walk(pf.Tree.RootNode(), func(n *sitter.Node) bool {
		if n.Type() != "function_definition" {
			return true
		}
		params := astutil.FunctionParams(n, pf.Source)
		var urlParams []string
		for _, p := range params {
			if p != "self" && p != "cls" {
				urlParams = append(urlParams, p)
			}
		}
		if len(urlParams) != 1 {
			return true
		}
		body := n.ChildByFieldName("body")
		if body == nil {
			return true
		}
		bodyText := astutil.NodeText(body, pf.Source)
		if urlCheckRe.MatchString(bodyText) {
			return true
		}
		var site *sitter.Node
		astutil.Walk(body, func(c *sitter.Node) bool {
			if site != nil || c.Type() != "call" {
				return site == nil
			}
			fn := c.ChildByFieldName("function")
			if fn == nil {
				return true
			}
			callee := astutil.NodeText(fn, pf.Source)
			opener := pyOpenerCallees[callee]
			if !opener && (strings.HasPrefix(callee, "subprocess.") || callee == "os.popen") {
				t := astutil.NodeText(c, pf.Source)
				opener = strings.Contains(t, "xdg-open") || strings.Contains(t, `"open"`) || strings.Contains(t, `'open'`) || strings.Contains(t, `"start"`)
			}
			if opener && containsIdentifier(c, pf.Source, urlParams[0]) {
				site = c
				return false
			}
			return true
		})
		if site != nil {
			out = append(out, models.MCPCodeFact{Kind: models.MCPCodeClientOpensURL, FilePath: pf.RelPath, Line: astutil.NodeLine(site), Language: models.LanguagePython})
		}
		return true
	})
	_ = masked
	return out
}

func containsIdentifier(root *sitter.Node, src []byte, name string) bool {
	found := false
	astutil.Walk(root, func(n *sitter.Node) bool {
		if found {
			return false
		}
		if (n.Type() == "identifier") && astutil.NodeText(n, src) == name {
			found = true
		}
		return !found
	})
	return found
}

// ─── TypeScript and JavaScript ────────────────────────────────────────────

func tsIsLiteralString(n *sitter.Node) bool {
	if n == nil {
		return false
	}
	return n.Type() == "string" || (n.Type() == "template_string" && n.NamedChildCount() == 0)
}

func tsLiteralValue(n *sitter.Node, src []byte) string {
	raw := astutil.NodeText(n, src)
	if len(raw) < 2 {
		return ""
	}
	return raw[1 : len(raw)-1]
}

// tsLiteralBindings maps identifiers declared exactly once in the file, with a string literal
// value, to that literal.
func tsLiteralBindings(pf ParsedFile) map[string]string {
	counts := map[string]int{}
	lits := map[string]string{}
	astutil.Walk(pf.Tree.RootNode(), func(n *sitter.Node) bool {
		if n.Type() != "variable_declarator" {
			return true
		}
		nm := n.ChildByFieldName("name")
		if nm == nil || nm.Type() != "identifier" {
			return true
		}
		id := astutil.NodeText(nm, pf.Source)
		counts[id]++
		if v := n.ChildByFieldName("value"); tsIsLiteralString(v) {
			lits[id] = tsLiteralValue(v, pf.Source)
		}
		return true
	})
	out := map[string]string{}
	for id, v := range lits {
		if counts[id] == 1 {
			out[id] = v
		}
	}
	return out
}

func tsAsConst(n *sitter.Node) *sitter.Node {
	for n != nil && (n.Type() == "parenthesized_expression" || n.Type() == "as_expression" || n.Type() == "non_null_expression") {
		if n.NamedChildCount() == 0 {
			break
		}
		n = n.NamedChild(0)
	}
	return n
}

func discoverTSMCPCode(pf ParsedFile) []models.MCPCodeFact {
	root := pf.Tree.RootNode()
	mcpImports := astutil.TSImportAliasesMatch(root, pf.Source, func(m string) bool { return strings.HasPrefix(m, "@modelcontextprotocol/") })
	v2Imports := astutil.TSImportAliasesMatch(root, pf.Source, func(m string) bool {
		return m == "@modelcontextprotocol/server" || strings.HasPrefix(m, "@modelcontextprotocol/server/")
	})
	if len(mcpImports) == 0 {
		return nil
	}
	masked := maskedSource(pf)
	var out []models.MCPCodeFact
	add := func(k models.MCPCodeKind, n *sitter.Node) {
		out = append(out, models.MCPCodeFact{Kind: k, FilePath: pf.RelPath, Line: int(n.StartPoint().Row) + 1, Language: models.LanguageTypeScript})
	}
	lits := tsLiteralBindings(pf)
	allowListed := allowListRe.MatchString(masked)
	stdioLocals := map[string]bool{}
	for local, canon := range mcpImports {
		if canon == "StdioClientTransport" {
			stdioLocals[local] = true
		}
	}
	execLocals := execCalleeNames()
	validated := validateRe.MatchString(masked)

	astutil.Walk(root, func(n *sitter.Node) bool {
		switch n.Type() {
		case "call_expression":
			fn := n.ChildByFieldName("function")
			if fn == nil {
				return true
			}
			// MCP-204: containment by string prefix.
			if fn.Type() == "member_expression" {
				if prop := fn.ChildByFieldName("property"); prop != nil && astutil.NodeText(prop, pf.Source) == "startsWith" {
					if tsPrefixContainmentHit(n, fn, pf) {
						add(models.MCPCodePrefixContainment, n)
					}
				}
			}
			// MCP-210: npm or pnpm install with a built specifier.
			if !validated && execLocals[astutil.NodeText(fn, pf.Source)] && tsPkgInstallHit(n, pf) {
				add(models.MCPCodePkgInstallSpec, n)
			}
		case "new_expression":
			ctor := n.ChildByFieldName("constructor")
			if ctor != nil && stdioLocals[astutil.NodeText(ctor, pf.Source)] && tsStdioCommandHit(n, pf, lits, allowListed) {
				add(models.MCPCodeStdioCommand, n)
			}
		}
		return true
	})
	out = append(out, tsClientOpensURL(pf)...)
	if len(v2Imports) > 0 {
		out = append(out, tsSharedTransport(pf, v2Imports)...)
	}
	return out
}

func tsPrefixContainmentHit(call, fn *sitter.Node, pf ParsedFile) bool {
	recv := fn.ChildByFieldName("object")
	args := call.ChildByFieldName("arguments")
	if recv == nil || args == nil || args.NamedChildCount() != 1 {
		return false
	}
	arg := args.NamedChild(0)
	if arg.Type() == "string" || arg.Type() == "template_string" || arg.Type() == "number" {
		return false
	}
	at := astutil.NodeText(arg, pf.Source)
	rt := astutil.NodeText(recv, pf.Source)
	if !tsPathLikeRe.MatchString(rt) || !tsRootLikeRe.MatchString(at) || urlLikeRe.MatchString(rt) || urlLikeRe.MatchString(at) {
		return false
	}
	low := strings.ToLower(at)
	if strings.Contains(low, "sep") || strings.Contains(at, `"/"`) || strings.Contains(at, `'/'`) || strings.Contains(at, `"\\"`) ||
		strings.Contains(at, "+ `") || strings.HasSuffix(strings.TrimSpace(at), "/") {
		return false
	}
	return true
}

func execCalleeNames() map[string]bool {
	m := map[string]bool{}
	for _, c := range []string{"execFileSync", "execFile", "spawn", "spawnSync", "execa", "execaSync"} {
		m[c] = true
		m["child_process."+c] = true
		m["cp."+c] = true
	}
	return m
}

func tsPkgInstallHit(call *sitter.Node, pf ParsedFile) bool {
	args := call.ChildByFieldName("arguments")
	if args == nil || args.NamedChildCount() < 2 {
		return false
	}
	first := args.NamedChild(0)
	if !tsIsLiteralString(first) {
		return false
	}
	switch tsLiteralValue(first, pf.Source) {
	case "npm", "pnpm", "npm.cmd", "pnpm.cmd":
	default:
		return false
	}
	arr := args.NamedChild(1)
	if arr.Type() != "array" {
		return false
	}
	verb := false
	dynamic := false
	for i := 0; i < int(arr.NamedChildCount()); i++ {
		e := arr.NamedChild(i)
		if tsIsLiteralString(e) {
			switch tsLiteralValue(e, pf.Source) {
			case "add", "install", "i":
				verb = true
			}
			continue
		}
		if verb && (e.Type() == "identifier" || e.Type() == "template_string" || e.Type() == "binary_expression" || e.Type() == "member_expression") {
			dynamic = true
		}
	}
	return verb && dynamic
}

func tsStdioCommandHit(n *sitter.Node, pf ParsedFile, lits map[string]string, allowListed bool) bool {
	args := n.ChildByFieldName("arguments")
	if args == nil || args.NamedChildCount() == 0 || args.NamedChild(0).Type() != "object" {
		return false
	}
	opts := args.NamedChild(0)
	cmd := getObjectProperty(opts, "command", pf.Source)
	if cmd == nil {
		return false
	}
	cmd = tsAsConst(cmd)
	raw := astutil.NodeText(cmd, pf.Source)
	lit, isLit := "", tsIsLiteralString(cmd)
	if isLit {
		lit = tsLiteralValue(cmd, pf.Source)
	} else if cmd.Type() == "identifier" {
		lit, isLit = lits[raw]
	}
	if isLit {
		return shellNames[baseCommand(lit)] && hasShellFlag(astutil.NodeText(opts, pf.Source))
	}
	return !allowListed && untrustedRe.MatchString(raw)
}

var tsOpenerCallees = map[string]bool{
	"exec": true, "execSync": true, "spawn": true, "spawnSync": true, "execFile": true, "execFileSync": true,
	"child_process.exec": true, "child_process.execSync": true, "child_process.spawn": true, "child_process.spawnSync": true,
}

func tsClientOpensURL(pf ParsedFile) []models.MCPCodeFact {
	root := pf.Tree.RootNode()
	openLocals := map[string]bool{}
	for local := range astutil.TSImportAliasesAny(root, pf.Source, []string{"open", "opn", "open-url"}) {
		openLocals[local] = true
	}
	var out []models.MCPCodeFact
	astutil.Walk(root, func(n *sitter.Node) bool {
		var name, params *sitter.Node
		var body *sitter.Node
		switch n.Type() {
		case "method_definition":
			name, params, body = n.ChildByFieldName("name"), n.ChildByFieldName("parameters"), n.ChildByFieldName("body")
		case "pair":
			v := n.ChildByFieldName("value")
			if v != nil && (v.Type() == "arrow_function" || v.Type() == "function_expression" || v.Type() == "function") {
				name, params, body = n.ChildByFieldName("key"), v.ChildByFieldName("parameters"), v.ChildByFieldName("body")
			}
		case "function_declaration":
			name, params, body = n.ChildByFieldName("name"), n.ChildByFieldName("parameters"), n.ChildByFieldName("body")
		}
		if name == nil || body == nil || params == nil || astutil.NodeText(name, pf.Source) != "redirectToAuthorization" || params.NamedChildCount() < 1 {
			return true
		}
		p := params.NamedChild(0)
		pn := p.ChildByFieldName("pattern")
		if pn == nil {
			pn = p
		}
		param := astutil.NodeText(pn, pf.Source)
		if urlCheckRe.MatchString(astutil.NodeText(body, pf.Source)) {
			return true
		}
		var site *sitter.Node
		astutil.Walk(body, func(c *sitter.Node) bool {
			if site != nil {
				return false
			}
			if c.Type() != "call_expression" {
				return true
			}
			fn := c.ChildByFieldName("function")
			if fn == nil {
				return true
			}
			callee := astutil.NodeText(fn, pf.Source)
			if (openLocals[callee] || tsOpenerCallees[callee]) && containsIdentifier(c.ChildByFieldName("arguments"), pf.Source, param) {
				site = c
				return false
			}
			return true
		})
		if site != nil {
			out = append(out, models.MCPCodeFact{Kind: models.MCPCodeClientOpensURL, FilePath: pf.RelPath, Line: int(site.StartPoint().Row) + 1, Language: models.LanguageTypeScript})
		}
		return true
	})
	return out
}

// tsSharedTransport finds a module-scope server or HTTP transport that a function connects or
// hands a request to (MCP-208).
func tsSharedTransport(pf ParsedFile, imports map[string]string) []models.MCPCodeFact {
	root := pf.Tree.RootNode()
	type decl struct {
		node *sitter.Node
		kind string // "server" or "transport"
	}
	decls := map[string]decl{}
	astutil.Walk(root, func(n *sitter.Node) bool {
		if n.Type() != "variable_declarator" || insideFunction(n) {
			return true
		}
		nm, v := n.ChildByFieldName("name"), n.ChildByFieldName("value")
		if nm == nil || v == nil || nm.Type() != "identifier" || v.Type() != "new_expression" {
			return true
		}
		ctor := v.ChildByFieldName("constructor")
		if ctor == nil {
			return true
		}
		canon, ok := imports[astutil.NodeText(ctor, pf.Source)]
		if !ok {
			return true
		}
		switch {
		case canon == "McpServer" || canon == "Server":
			decls[astutil.NodeText(nm, pf.Source)] = decl{n, "server"}
		case strings.Contains(canon, "Transport") && strings.Contains(canon, "HTTP"):
			decls[astutil.NodeText(nm, pf.Source)] = decl{n, "transport"}
		}
		return true
	})
	if len(decls) == 0 {
		return nil
	}
	seen := map[string]bool{}
	var out []models.MCPCodeFact
	astutil.Walk(root, func(n *sitter.Node) bool {
		if n.Type() != "call_expression" || !insideFunction(n) {
			return true
		}
		fn := n.ChildByFieldName("function")
		if fn == nil || fn.Type() != "member_expression" {
			return true
		}
		obj, prop := fn.ChildByFieldName("object"), fn.ChildByFieldName("property")
		if obj == nil || prop == nil || obj.Type() != "identifier" {
			return true
		}
		d, ok := decls[astutil.NodeText(obj, pf.Source)]
		if !ok || seen[astutil.NodeText(obj, pf.Source)] {
			return true
		}
		p := astutil.NodeText(prop, pf.Source)
		if (d.kind == "server" && p == "connect") || (d.kind == "transport" && p == "handleRequest") {
			seen[astutil.NodeText(obj, pf.Source)] = true
			out = append(out, models.MCPCodeFact{Kind: models.MCPCodeSharedTransport, FilePath: pf.RelPath, Line: int(d.node.StartPoint().Row) + 1, Language: models.LanguageTypeScript})
		}
		return true
	})
	return out
}
