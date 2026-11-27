package analysis

import (
	"bytes"
	"strings"

	sitter "github.com/smacker/go-tree-sitter"

	"github.com/trustabl/trustabl/internal/analysis/astutil"
	"github.com/trustabl/trustabl/internal/models"
)

// ---- file context ----

// collectPyAliases maps a local import name to its canonical dotted prefix so a
// sink written `sp.run` or `run` (from subprocess import run) is matched.
func collectPyAliases(root *sitter.Node, src []byte) map[string]string {
	out := map[string]string{}
	astutil.Walk(root, func(n *sitter.Node) bool {
		switch n.Type() {
		case "import_statement":
			for i := 0; i < int(n.NamedChildCount()); i++ {
				c := n.NamedChild(i)
				switch c.Type() {
				case "aliased_import":
					nm := astutil.NodeText(c.ChildByFieldName("name"), src)
					al := astutil.NodeText(c.ChildByFieldName("alias"), src)
					if nm != "" && al != "" {
						out[al] = nm
					}
				}
			}
		case "import_from_statement":
			mod := astutil.NodeText(n.ChildByFieldName("module_name"), src)
			if mod == "" || strings.HasPrefix(mod, ".") {
				return true
			}
			for i := 0; i < int(n.NamedChildCount()); i++ {
				c := n.NamedChild(i)
				if c == n.ChildByFieldName("module_name") {
					continue
				}
				switch c.Type() {
				case "dotted_name":
					nm := astutil.NodeText(c, src)
					out[nm] = mod + "." + nm
				case "aliased_import":
					nm := astutil.NodeText(c.ChildByFieldName("name"), src)
					al := astutil.NodeText(c.ChildByFieldName("alias"), src)
					if nm != "" && al != "" {
						out[al] = mod + "." + nm
					}
				}
			}
		}
		return true
	})
	return out
}

func collectPyEnums(root *sitter.Node, src []byte) map[string]bool {
	out := map[string]bool{}
	for _, c := range astutil.FindAll(root, "class_definition") {
		sup := c.ChildByFieldName("superclasses")
		if sup != nil && strings.Contains(astutil.NodeText(sup, src), "Enum") {
			out[astutil.NodeText(c.ChildByFieldName("name"), src)] = true
		}
	}
	return out
}

var memoryCtors = map[string]bool{"Memory": true, "MemoryClient": true, "AsyncMemory": true, "AsyncMemoryClient": true, "from_config": true}

// collectMemoryVars records the names bound to a mem0-style client in the file.
func collectMemoryVars(root *sitter.Node, src []byte, py bool) map[string]bool {
	out := map[string]bool{}
	if py {
		for _, a := range astutil.FindAll(root, "assignment") {
			l, r := a.ChildByFieldName("left"), a.ChildByFieldName("right")
			if l == nil || r == nil || r.Type() != "call" {
				continue
			}
			if memoryCtors[lastSegment(astutil.NodeText(r.ChildByFieldName("function"), src))] {
				out[astutil.NodeText(l, src)] = true
			}
		}
		return out
	}
	for _, d := range astutil.FindAll(root, "variable_declarator") {
		v := d.ChildByFieldName("value")
		if v == nil {
			continue
		}
		if v.Type() == "await_expression" && v.NamedChildCount() > 0 {
			v = v.NamedChild(0)
		}
		var callee string
		switch v.Type() {
		case "new_expression":
			callee = astutil.NodeText(v.ChildByFieldName("constructor"), src)
		case "call_expression":
			callee = astutil.NodeText(v.ChildByFieldName("function"), src)
		}
		if memoryCtors[lastSegment(callee)] {
			out[astutil.NodeText(d.ChildByFieldName("name"), src)] = true
		}
	}
	return out
}

// ---- tool entry ----

func (fc *taintFileCtx) pyToolFlows(t models.ToolDef) []models.TaintFlow {
	fn := FindFunctionNode(t, fc.pf)
	if fn == nil {
		return nil
	}
	body := fn.ChildByFieldName("body")
	if body == nil {
		return nil
	}
	e := &taintEngine{src: fc.pf.Source, py: true, file: fc, flows: map[flowKey]models.TaintFlow{}}
	st := taintState{}
	declared := map[string]bool{}
	for _, p := range t.ParamNames {
		declared[p] = true
	}
	params := fn.ChildByFieldName("parameters")
	first := true
	if params != nil {
		for i := 0; i < int(params.NamedChildCount()); i++ {
			p := params.NamedChild(i)
			name, ann, def := pyParam(p, e.src)
			if name == "" {
				continue
			}
			if name == "self" || name == "cls" {
				continue
			}
			line := astutil.NodeLine(p)
			if first && t.Kind == models.KindClaudeSDKTool && len(t.InputKeys) > 0 {
				e.dictName = name
				e.dictKeys = map[string]bool{}
				for _, k := range t.InputKeys {
					e.dictKeys[k] = true
				}
				e.tsArgsLine = line
				first = false
				continue
			}
			first = false
			if !declared[name] || pyParamExcluded(name, ann, def, fc) {
				continue
			}
			st[name] = []taintOrigin{{param: name, srcLine: line}}
		}
	}
	if len(st) == 0 && e.dictName == "" {
		return nil
	}
	e.pyBlock(body, st)
	return e.sortedFlows()
}

// pyParam returns a parameter's name, annotation text and default node.
func pyParam(p *sitter.Node, src []byte) (name, ann string, def *sitter.Node) {
	switch p.Type() {
	case "identifier":
		return astutil.NodeText(p, src), "", nil
	case "typed_parameter":
		if p.NamedChildCount() > 0 && p.NamedChild(0).Type() == "identifier" {
			name = astutil.NodeText(p.NamedChild(0), src)
		}
		ann = astutil.NodeText(p.ChildByFieldName("type"), src)
	case "default_parameter":
		name = astutil.NodeText(p.ChildByFieldName("name"), src)
		def = p.ChildByFieldName("value")
	case "typed_default_parameter":
		name = astutil.NodeText(p.ChildByFieldName("name"), src)
		ann = astutil.NodeText(p.ChildByFieldName("type"), src)
		def = p.ChildByFieldName("value")
	}
	return
}

func pyParamExcluded(name, ann string, def *sitter.Node, fc *taintFileCtx) bool {
	a := strings.ReplaceAll(ann, " ", "")
	if a == "" {
		switch name {
		case "ctx", "context", "run_context", "runtime", "config":
			return true
		}
	}
	for _, m := range []string{"Context", "RunnableConfig", "Runtime", "Injected", "BaseStore"} {
		if strings.Contains(a, m) {
			return true
		}
	}
	base := a
	if strings.HasPrefix(base, "Optional[") && strings.HasSuffix(base, "]") {
		base = base[9 : len(base)-1]
	}
	base = strings.TrimSuffix(strings.TrimSuffix(base, "|None"), "|None")
	base = strings.TrimPrefix(base, "None|")
	switch base {
	case "int", "float", "bool", "complex":
		return true
	}
	if strings.HasPrefix(base, "Literal[") || strings.HasPrefix(base, "typing.Literal[") || fc.enums[base] {
		return true
	}
	if a == "" && def != nil {
		switch def.Type() {
		case "integer", "float", "true", "false":
			return true
		}
	}
	return false
}

// ---- statements ----

func pyTerminates(blk *sitter.Node) bool {
	if blk == nil {
		return false
	}
	if blk.Type() != "block" {
		return pyTermType(blk.Type())
	}
	n := int(blk.NamedChildCount())
	if n == 0 {
		return false
	}
	return pyTermType(blk.NamedChild(n - 1).Type())
}

func pyTermType(t string) bool {
	return t == "raise_statement" || t == "return_statement" || t == "continue_statement" || t == "break_statement"
}

func (e *taintEngine) pyBlock(blk *sitter.Node, st taintState) taintState {
	if blk == nil {
		return st
	}
	if blk.Type() != "block" {
		return e.pyStmt(blk, st)
	}
	for i := 0; i < int(blk.NamedChildCount()); i++ {
		if e.over() {
			return st
		}
		st = e.pyStmt(blk.NamedChild(i), st)
	}
	return st
}

func (e *taintEngine) pyStmt(n *sitter.Node, st taintState) taintState {
	switch n.Type() {
	case "function_definition", "class_definition", "decorated_definition", "pass_statement", "import_statement", "import_from_statement":
		return st
	case "if_statement":
		return e.pyIf(n, st)
	case "for_statement":
		left, right := n.ChildByFieldName("left"), n.ChildByFieldName("right")
		e.scan(right, st)
		body := n.ChildByFieldName("body")
		return e.loop(st, func(s taintState) taintState {
			if left != nil {
				e.pyBind(left, e.expr(right, s), s, astutil.NodeLine(n))
			}
			return e.pyBlock(body, s)
		})
	case "while_statement":
		e.scan(n.ChildByFieldName("condition"), st)
		return e.loop(st, func(s taintState) taintState { return e.pyBlock(n.ChildByFieldName("body"), s) })
	case "try_statement":
		out := st
		for i := 0; i < int(n.NamedChildCount()); i++ {
			c := n.NamedChild(i)
			switch c.Type() {
			case "block":
				out = unionState(out, e.pyBlock(c, out.clone()))
			case "except_clause", "except_group_clause", "else_clause", "finally_clause":
				for j := 0; j < int(c.NamedChildCount()); j++ {
					if b := c.NamedChild(j); b.Type() == "block" {
						out = unionState(out, e.pyBlock(b, out.clone()))
					}
				}
			}
		}
		return out
	case "with_statement":
		for i := 0; i < int(n.NamedChildCount()); i++ {
			c := n.NamedChild(i)
			if c.Type() == "with_clause" {
				e.scan(c, st)
			}
		}
		return e.pyBlock(n.ChildByFieldName("body"), st)
	case "match_statement":
		out := st
		for _, b := range astutil.FindAll(n, "block") {
			out = unionState(out, e.pyBlock(b, out.clone()))
		}
		return out
	case "assert_statement":
		e.scan(n, st)
		out := st.clone()
		killNames(out, e.mentioned(n, st))
		return out
	case "expression_statement":
		if e.hasParen(n) { // no '(' means no call, so no sink to find
			e.scan(n, st)
		}
		return e.pyEffects(n, st)
	default:
		e.scan(n, st)
		return st
	}
}

func (e *taintEngine) loop(st taintState, body func(taintState) taintState) taintState {
	e.loopDepth++
	defer func() { e.loopDepth-- }()
	out := unionState(st, body(st.clone()))
	if e.loopDepth <= taintMaxLoopDepth {
		out = unionState(out, body(out.clone()))
	}
	return out
}

func (e *taintEngine) pyIf(n *sitter.Node, st taintState) taintState {
	type branch struct {
		cond, blk *sitter.Node
	}
	branches := []branch{{n.ChildByFieldName("condition"), n.ChildByFieldName("consequence")}}
	var elseBlk *sitter.Node
	for i := 0; i < int(n.NamedChildCount()); i++ {
		c := n.NamedChild(i)
		switch c.Type() {
		case "elif_clause":
			branches = append(branches, branch{c.ChildByFieldName("condition"), c.ChildByFieldName("consequence")})
		case "else_clause":
			elseBlk = c.ChildByFieldName("body")
		}
	}
	cur := st
	var outs []taintState
	for _, b := range branches {
		e.scan(b.cond, cur)
		then := cur.clone()
		info := e.checkInfo(b.cond, cur)
		if info.single && info.positive {
			killNames(then, info.names)
		}
		res := e.pyBlock(b.blk, then)
		term := pyTerminates(b.blk)
		if !term {
			outs = append(outs, res)
		}
		if term {
			if ms := e.mentioned(b.cond, cur); len(ms) > 0 && !e.trivialTest(b.cond) && !(info.single && info.positive) {
				cur = cur.clone()
				killNames(cur, ms)
			}
		} else if info.single && !info.positive {
			// validated side is the else/fallthrough path
			cur = cur.clone()
			killNames(cur, info.names)
		}
	}
	fall := cur
	if elseBlk != nil {
		fall = e.pyBlock(elseBlk, cur.clone())
		if pyTerminates(elseBlk) {
			fall = nil
		}
	}
	res := taintState{}
	if fall != nil {
		res = fall
	}
	for _, o := range outs {
		res = unionState(res, o)
	}
	return res
}

type checkResult struct {
	names    []string
	positive bool
	single   bool
}

var pyMatchMethods = map[string]bool{"match": true, "fullmatch": true, "search": true, "startswith": true, "endswith": true,
	"isalnum": true, "isdigit": true, "isalpha": true, "isidentifier": true, "isnumeric": true, "is_relative_to": true, "issubset": true}

// checkInfo classifies a test of the form `name in X`, `name == X`, `re.match(.., name)`,
// with polarity. Compound tests are not classified.
func (e *taintEngine) checkInfo(test *sitter.Node, st taintState) checkResult {
	if test == nil {
		return checkResult{}
	}
	positive := true
	n := test
	for {
		switch n.Type() {
		case "parenthesized_expression":
			if n.NamedChildCount() == 0 {
				return checkResult{}
			}
			n = n.NamedChild(0)
			continue
		case "not_operator":
			positive = !positive
			n = n.ChildByFieldName("argument")
			if n == nil {
				return checkResult{}
			}
			continue
		}
		break
	}
	var names []string
	switch n.Type() {
	case "comparison_operator":
		txt := astutil.NodeText(n, e.src)
		neg := strings.Contains(txt, " not in ") || strings.Contains(txt, "!=") || strings.Contains(txt, " is not ")
		for i := 0; i < int(n.NamedChildCount()); i++ {
			c := n.NamedChild(i)
			if c.Type() == "identifier" {
				if _, ok := st[astutil.NodeText(c, e.src)]; ok {
					names = append(names, astutil.NodeText(c, e.src))
				}
			}
		}
		if neg {
			positive = !positive
		}
	case "call":
		fn := n.ChildByFieldName("function")
		if fn == nil || !pyMatchMethods[lastSegment(astutil.NodeText(fn, e.src))] {
			return checkResult{}
		}
		var cands []*sitter.Node
		if fn.Type() == "attribute" {
			cands = append(cands, fn.ChildByFieldName("object"))
		}
		if args := n.ChildByFieldName("arguments"); args != nil {
			for i := 0; i < int(args.NamedChildCount()); i++ {
				cands = append(cands, args.NamedChild(i))
			}
		}
		for _, c := range cands {
			if c != nil && c.Type() == "identifier" {
				if _, ok := st[astutil.NodeText(c, e.src)]; ok {
					names = append(names, astutil.NodeText(c, e.src))
				}
			}
		}
	default:
		return checkResult{}
	}
	if len(names) == 0 {
		return checkResult{}
	}
	return checkResult{names: names, positive: positive, single: true}
}

// trivialTest reports a truthiness, None or type test, which does not validate content.
func (e *taintEngine) trivialTest(test *sitter.Node) bool {
	n := test
	for n != nil && (n.Type() == "parenthesized_expression" || n.Type() == "not_operator") {
		if n.Type() == "not_operator" {
			n = n.ChildByFieldName("argument")
		} else if n.NamedChildCount() > 0 {
			n = n.NamedChild(0)
		} else {
			return false
		}
	}
	if n == nil {
		return false
	}
	if n.Type() == "identifier" {
		return true
	}
	txt := astutil.NodeText(n, e.src)
	return strings.HasPrefix(txt, "isinstance(") || strings.HasSuffix(txt, "is None") || strings.HasSuffix(txt, "is not None") ||
		strings.HasSuffix(txt, "== None") || strings.HasSuffix(txt, "!= None")
}

// mentioned lists the tainted names referenced in n.
func (e *taintEngine) mentioned(n *sitter.Node, st taintState) []string {
	seen := map[string]bool{}
	var out []string
	astutil.Walk(n, func(c *sitter.Node) bool {
		if c.Type() == "identifier" {
			nm := astutil.NodeText(c, e.src)
			if _, ok := st[nm]; ok && !seen[nm] {
				if p := c.Parent(); p != nil {
					if (p.Type() == "attribute" && p.ChildByFieldName("attribute") != nil && p.ChildByFieldName("attribute").Equal(c)) ||
						(p.Type() == "keyword_argument" && p.ChildByFieldName("name") != nil && p.ChildByFieldName("name").Equal(c)) {
						return true
					}
				}
				seen[nm] = true
				out = append(out, nm)
			}
		}
		return true
	})
	return out
}

// pyEffects applies assignment effects of an expression_statement.
func (e *taintEngine) pyEffects(n *sitter.Node, st taintState) taintState {
	if n.NamedChildCount() == 0 {
		return st
	}
	c := n.NamedChild(0)
	line := astutil.NodeLine(n)
	switch c.Type() {
	case "assignment":
		l, r := c.ChildByFieldName("left"), c.ChildByFieldName("right")
		if l == nil || r == nil {
			e.sanitizeKills(c, st)
			return st
		}
		os := e.expr(r, st)
		e.sanitizeKills(c, st)
		e.pyBind(l, os, st, line)
		if r.Type() == "assignment" { // a = b = p
			e.pyBind(r.ChildByFieldName("left"), os, st, line)
		}
	case "augmented_assignment":
		l, r := c.ChildByFieldName("left"), c.ChildByFieldName("right")
		if l != nil && l.Type() == "identifier" && r != nil {
			nm := astutil.NodeText(l, e.src)
			os := e.expr(r, st)
			if len(os) > 0 {
				st[nm] = mergeOrigins(st[nm], withVia(asComposite(os), line, false))
			}
		}
	default:
		e.sanitizeKills(c, st)
	}
	return st
}

// pyBind assigns origins to the identifiers of a target pattern; an empty
// origin list kills the name.
func (e *taintEngine) pyBind(target *sitter.Node, os []taintOrigin, st taintState, line int) {
	if target == nil {
		return
	}
	switch target.Type() {
	case "identifier":
		nm := astutil.NodeText(target, e.src)
		if len(os) == 0 {
			delete(st, nm)
		} else {
			st[nm] = withVia(os, line, false)
		}
	case "pattern_list", "tuple_pattern", "list_pattern", "expression_list":
		for i := 0; i < int(target.NamedChildCount()); i++ {
			e.pyBind(target.NamedChild(i), os, st, line)
		}
	case "list_splat_pattern":
		if target.NamedChildCount() > 0 {
			e.pyBind(target.NamedChild(0), os, st, line)
		}
	}
}

// sanitizeKills removes the names a normalising call is applied to.
func (e *taintEngine) sanitizeKills(n *sitter.Node, st taintState) {
	if !e.mayNormalize(n) {
		return
	}
	astutil.Walk(n, func(c *sitter.Node) bool {
		if c.Type() != "call" {
			return true
		}
		fn := c.ChildByFieldName("function")
		if fn == nil {
			return true
		}
		txt := e.canon(astutil.NodeText(fn, e.src))
		switch {
		case lastSegment(txt) == "resolve" && fn.Type() == "attribute":
			killNames(st, e.rootNames(fn.ChildByFieldName("object"), st))
		case txt == "os.path.realpath" || txt == "realpath":
			if args := c.ChildByFieldName("arguments"); args != nil && args.NamedChildCount() > 0 {
				killNames(st, e.rootNames(args.NamedChild(0), st))
			}
		}
		return true
	})
}

// rootNames returns tainted names reachable through simple wrappers.
func (e *taintEngine) rootNames(n *sitter.Node, st taintState) []string {
	if n == nil {
		return nil
	}
	switch n.Type() {
	case "identifier":
		nm := astutil.NodeText(n, e.src)
		if _, ok := st[nm]; ok {
			return []string{nm}
		}
	case "call":
		var out []string
		if args := n.ChildByFieldName("arguments"); args != nil {
			for i := 0; i < int(args.NamedChildCount()); i++ {
				out = append(out, e.rootNames(args.NamedChild(i), st)...)
			}
		}
		return out
	case "attribute":
		return e.rootNames(n.ChildByFieldName("object"), st)
	}
	return nil
}

// ---- expressions ----

func (e *taintEngine) canon(callee string) string {
	if e.file == nil || e.file.pyAliases == nil {
		return callee
	}
	first := callee
	rest := ""
	if i := strings.Index(callee, "."); i >= 0 {
		first, rest = callee[:i], callee[i:]
	}
	if m, ok := e.file.pyAliases[first]; ok {
		return m + rest
	}
	return callee
}

var pyWrapperFuncs = map[string]bool{
	"str": true, "repr": true, "ascii": true, "bytes": true, "bytearray": true, "list": true, "tuple": true, "set": true,
	"dict": true, "sorted": true, "reversed": true, "format": true,
	"Path": true, "PurePath": true, "PurePosixPath": true, "PureWindowsPath": true, "PosixPath": true, "WindowsPath": true,
	"pathlib.Path": true, "pathlib.PurePath": true, "pathlib.PurePosixPath": true, "pathlib.PureWindowsPath": true,
	"os.fspath": true, "os.path.join": true, "os.path.normpath": true, "os.path.abspath": true, "os.path.expanduser": true,
	"os.path.expandvars": true, "os.path.dirname": true, "os.path.splitext": true, "os.path.split": true, "os.path.relpath": true,
	"posixpath.join": true, "ntpath.join": true, "shlex.split": true, "json.dumps": true, "json.loads": true,
	"urllib.parse.unquote": true, "urllib.parse.unquote_plus": true, "copy.copy": true, "copy.deepcopy": true,
}

var pyMethodArgs = map[string]bool{"format": true, "format_map": true, "join": true, "replace": true, "joinpath": true,
	"with_name": true, "with_suffix": true, "with_stem": true, "removeprefix": true, "removesuffix": true,
	"ljust": true, "rjust": true, "center": true, "zfill": true}

var pyMethodRecv = map[string]bool{"strip": true, "lstrip": true, "rstrip": true, "lower": true, "upper": true, "casefold": true,
	"title": true, "capitalize": true, "swapcase": true, "encode": true, "decode": true, "split": true, "rsplit": true,
	"splitlines": true, "partition": true, "rpartition": true, "expandtabs": true, "get": true, "pop": true, "copy": true,
	"items": true, "values": true, "keys": true, "as_posix": true, "absolute": true, "expanduser": true}

func (e *taintEngine) expr(n *sitter.Node, st taintState) []taintOrigin {
	if !e.py {
		return e.tsExpr(n, st, 0)
	}
	return e.pyExpr(n, st, 0)
}

func (e *taintEngine) pyExpr(n *sitter.Node, st taintState, depth int) []taintOrigin {
	if n == nil || depth > taintMaxExprDepth || e.over() {
		return nil
	}
	d := depth + 1
	switch n.Type() {
	case "identifier":
		return st[astutil.NodeText(n, e.src)]
	case "string":
		var os []taintOrigin
		subs := 0
		subLen := 0
		for i := 0; i < int(n.NamedChildCount()); i++ {
			c := n.NamedChild(i)
			if c.Type() == "interpolation" {
				subs++
				subLen += len(astutil.NodeText(c, e.src))
				if c.NamedChildCount() > 0 {
					os = mergeOrigins(os, e.pyExpr(c.NamedChild(0), st, d))
				}
			}
		}
		if len(os) == 0 {
			return nil
		}
		txt := astutil.NodeText(n, e.src)
		inner := strings.TrimLeft(txt, "fFrRbBuU")
		inner = strings.Trim(inner, "\"'")
		if subs > 1 || len(inner) > subLen {
			return asComposite(os)
		}
		return os
	case "concatenated_string", "boolean_operator", "expression_list", "tuple", "list", "set":
		var os []taintOrigin
		for i := 0; i < int(n.NamedChildCount()); i++ {
			os = mergeOrigins(os, e.pyExpr(n.NamedChild(i), st, d))
		}
		if n.Type() == "tuple" || n.Type() == "list" || n.Type() == "set" || n.Type() == "expression_list" {
			return asElem(os)
		}
		return os
	case "binary_operator":
		os := mergeOrigins(e.pyExpr(n.ChildByFieldName("left"), st, d), e.pyExpr(n.ChildByFieldName("right"), st, d))
		if len(os) > 0 {
			return asComposite(os)
		}
		return nil
	case "parenthesized_expression", "await", "list_splat", "dictionary_splat", "named_expression", "keyword_argument":
		if n.Type() == "keyword_argument" {
			return e.pyExpr(n.ChildByFieldName("value"), st, d)
		}
		if n.NamedChildCount() > 0 {
			return e.pyExpr(n.NamedChild(int(n.NamedChildCount())-1), st, d)
		}
	case "conditional_expression":
		c := int(n.NamedChildCount())
		if c >= 3 {
			return mergeOrigins(e.pyExpr(n.NamedChild(0), st, d), e.pyExpr(n.NamedChild(c-1), st, d))
		}
	case "dictionary":
		var os []taintOrigin
		for i := 0; i < int(n.NamedChildCount()); i++ {
			c := n.NamedChild(i)
			if c.Type() == "pair" {
				os = mergeOrigins(os, e.pyExpr(c.ChildByFieldName("key"), st, d))
				os = mergeOrigins(os, e.pyExpr(c.ChildByFieldName("value"), st, d))
			} else {
				os = mergeOrigins(os, e.pyExpr(c, st, d))
			}
		}
		return asElem(os)
	case "attribute":
		return dropElem(e.pyExpr(n.ChildByFieldName("object"), st, d))
	case "subscript":
		v := n.ChildByFieldName("value")
		if e.dictName != "" && v != nil && v.Type() == "identifier" && astutil.NodeText(v, e.src) == e.dictName {
			return e.dictKey(n.ChildByFieldName("subscript"))
		}
		return dropElem(e.pyExpr(v, st, d))
	case "call":
		return e.pyCallExpr(n, st, d)
	}
	return nil
}

func (e *taintEngine) dictKey(k *sitter.Node) []taintOrigin {
	if k == nil || k.Type() != "string" {
		return nil
	}
	key := strings.Trim(astutil.NodeText(k, e.src), "\"'")
	if !e.dictKeys[key] {
		return nil
	}
	return []taintOrigin{{param: key, srcLine: astutil.NodeLine(k)}}
}

func (e *taintEngine) pyCallExpr(n *sitter.Node, st taintState, d int) []taintOrigin {
	fn := n.ChildByFieldName("function")
	if fn == nil {
		return nil
	}
	args := n.ChildByFieldName("arguments")
	argOrigins := func() []taintOrigin {
		var os []taintOrigin
		if args == nil {
			return nil
		}
		for i := 0; i < int(args.NamedChildCount()); i++ {
			os = mergeOrigins(os, e.pyExpr(args.NamedChild(i), st, d))
		}
		return os
	}
	txt := astutil.NodeText(fn, e.src)
	if fn.Type() == "attribute" {
		recv := fn.ChildByFieldName("object")
		meth := astutil.NodeText(fn.ChildByFieldName("attribute"), e.src)
		if e.dictName != "" && recv != nil && recv.Type() == "identifier" && astutil.NodeText(recv, e.src) == e.dictName && meth == "get" &&
			args != nil && args.NamedChildCount() > 0 {
			return e.dictKey(args.NamedChild(0))
		}
		c := e.canon(txt)
		if pyWrapperFuncs[c] {
			return argOrigins()
		}
		switch {
		case pyMethodArgs[meth]:
			os := mergeOrigins(dropElem(e.pyExpr(recv, st, d)), argOrigins())
			if meth == "format" || meth == "format_map" || meth == "join" {
				return asComposite(os)
			}
			return os
		case pyMethodRecv[meth]:
			return dropElem(e.pyExpr(recv, st, d))
		}
		return nil
	}
	if pyWrapperFuncs[e.canon(txt)] || pyWrapperFuncs[txt] {
		return argOrigins()
	}
	return nil
}

// ---- sink scan ----

// scan walks the expression parts of a statement and records sink flows.
func (e *taintEngine) scan(n *sitter.Node, st taintState) {
	if n == nil || e.over() {
		return
	}
	switch n.Type() {
	case "function_definition", "class_definition", "decorated_definition":
		return
	case "lambda":
		s2 := st.clone()
		if p := n.ChildByFieldName("parameters"); p != nil {
			for i := 0; i < int(p.NamedChildCount()); i++ {
				nm, _, _ := pyParam(p.NamedChild(i), e.src)
				delete(s2, nm)
			}
		}
		e.scan(n.ChildByFieldName("body"), s2)
		return
	case "list_comprehension", "set_comprehension", "dictionary_comprehension", "generator_expression":
		s2 := st.clone()
		for i := 0; i < int(n.NamedChildCount()); i++ {
			c := n.NamedChild(i)
			if c.Type() == "for_in_clause" {
				e.scan(c.ChildByFieldName("right"), s2)
				e.pyBind(c.ChildByFieldName("left"), e.expr(c.ChildByFieldName("right"), s2), s2, astutil.NodeLine(c))
			}
		}
		for i := 0; i < int(n.NamedChildCount()); i++ {
			if c := n.NamedChild(i); c.Type() != "for_in_clause" {
				e.scan(c, s2)
			}
		}
		return
	case "call":
		e.pySinks(n, st)
	case "named_expression":
		if nm := n.ChildByFieldName("name"); nm != nil {
			e.scan(n.ChildByFieldName("value"), st)
			e.pyBind(nm, e.expr(n.ChildByFieldName("value"), st), st, astutil.NodeLine(n))
			return
		}
	}
	for i := 0; i < int(n.NamedChildCount()); i++ {
		e.scan(n.NamedChild(i), st)
	}
}

// mayNormalize is a cheap text pre-check so the sanitizer walk only runs on
// statements that mention a normalising call.
func (e *taintEngine) mayNormalize(n *sitter.Node) bool {
	if n == nil {
		return false
	}
	b := e.src[n.StartByte():n.EndByte()]
	for _, w := range []string{"resolve", "realpath", "parse", "Parse"} {
		if strings.Contains(string(b), w) {
			return true
		}
	}
	return false
}

// hasParen reports whether the node's text contains '(': a cheap test that a
// statement can hold no call.
func (e *taintEngine) hasParen(n *sitter.Node) bool {
	return bytes.IndexByte(e.src[n.StartByte():n.EndByte()], '(') >= 0
}
