package analysis

import (
	"bytes"
	"strings"

	sitter "github.com/smacker/go-tree-sitter"

	"github.com/trustabl/trustabl/internal/analysis/astutil"
	"github.com/trustabl/trustabl/internal/models"
)

var tsHandlerKeys = map[string]bool{"execute": true, "handler": true, "func": true, "callback": true, "invoke": true,
	"run": true, "call": true, "_call": true}

func isTSFunc(t string) bool {
	return t == "arrow_function" || t == "function_expression" || t == "function" || t == "generator_function" ||
		t == "function_declaration" || t == "generator_function_declaration" || t == "method_definition"
}

func collectTSFuncs(root *sitter.Node, src []byte) map[string]*sitter.Node {
	out := map[string]*sitter.Node{}
	astutil.Walk(root, func(n *sitter.Node) bool {
		switch n.Type() {
		case "function_declaration":
			if nm := n.ChildByFieldName("name"); nm != nil {
				out[astutil.NodeText(nm, src)] = n
			}
		case "variable_declarator":
			v := n.ChildByFieldName("value")
			if v != nil && (v.Type() == "arrow_function" || v.Type() == "function_expression" || v.Type() == "function") {
				out[astutil.NodeText(n.ChildByFieldName("name"), src)] = v
			}
		}
		return true
	})
	return out
}

func (fc *taintFileCtx) tsHandlerOf(n *sitter.Node, depth int) []*sitter.Node {
	if n == nil || depth > 3 {
		return nil
	}
	switch {
	case isTSFunc(n.Type()):
		return []*sitter.Node{n}
	case n.Type() == "identifier":
		if f := fc.tsFuncs[astutil.NodeText(n, fc.pf.Source)]; f != nil {
			return []*sitter.Node{f}
		}
	case n.Type() == "object":
		var out []*sitter.Node
		for i := 0; i < int(n.NamedChildCount()); i++ {
			c := n.NamedChild(i)
			switch c.Type() {
			case "pair":
				if tsHandlerKeys[tsKeyText(c.ChildByFieldName("key"), fc.pf.Source)] {
					out = append(out, fc.tsHandlerOf(c.ChildByFieldName("value"), depth+1)...)
				}
			case "method_definition":
				if tsHandlerKeys[tsKeyText(c.ChildByFieldName("name"), fc.pf.Source)] {
					out = append(out, c)
				}
			}
		}
		return out
	}
	return nil
}

func tsKeyText(k *sitter.Node, src []byte) string {
	if k == nil {
		return ""
	}
	return strings.Trim(astutil.NodeText(k, src), "\"'`")
}

func (fc *taintFileCtx) tsToolFlows(t models.ToolDef) []models.TaintFlow {
	root := fc.pf.Tree.RootNode()
	var handlers []*sitter.Node
	var toolNode *sitter.Node
	astutil.Walk(root, func(n *sitter.Node) bool {
		if len(handlers) > 0 {
			return false
		}
		if n.Type() != "call_expression" && n.Type() != "new_expression" {
			return true
		}
		if int(n.StartPoint().Row)+1 != t.Line || int(n.EndPoint().Row)+1 != t.EndLine {
			return true
		}
		args := n.ChildByFieldName("arguments")
		if args == nil {
			return true
		}
		var hs []*sitter.Node
		for i := 0; i < int(args.NamedChildCount()); i++ {
			hs = append(hs, fc.tsHandlerOf(args.NamedChild(i), 0)...)
		}
		if len(hs) > 0 {
			handlers, toolNode = hs, n
		}
		return true
	})
	if len(handlers) == 0 {
		return nil
	}
	excluded := tsNonStringParams(toolNode, fc.pf.Source)
	declared := map[string]bool{}
	for _, p := range t.ParamNames {
		declared[p] = true
	}
	merged := map[flowKey]models.TaintFlow{}
	for _, h := range handlers {
		e := &taintEngine{src: fc.pf.Source, file: fc, flows: map[flowKey]models.TaintFlow{}, excludedSet: excluded}
		st := e.tsSources(h, declared)
		body := h.ChildByFieldName("body")
		if body == nil {
			continue
		}
		e.envOK = bytes.Contains(e.src[body.StartByte():body.EndByte()], []byte("process.env"))
		if len(st) == 0 && e.tsArgs == "" && !e.envOK {
			continue
		}
		if body.Type() == "statement_block" {
			e.tsBlock(body, st)
		} else {
			e.scan2(body, st)
			if e.envOK {
				// an arrow function with an expression body returns that expression
				for _, o := range e.expr(body, st) {
					e.record(o, models.SinkReturnValue, "return", astutil.NodeLine(body))
				}
			}
		}
		for k, v := range e.flows {
			merged[k] = v
		}
	}
	e := &taintEngine{flows: merged}
	return e.sortedFlows()
}

// tsNonStringParams finds schema keys declared as number, boolean or enum.
func tsNonStringParams(call *sitter.Node, src []byte) map[string]bool {
	out := map[string]bool{}
	astutil.Walk(call, func(n *sitter.Node) bool {
		if isTSFunc(n.Type()) {
			return false
		}
		if n.Type() != "pair" {
			return true
		}
		v := astutil.NodeText(n.ChildByFieldName("value"), src)
		for _, p := range []string{"z.number(", "z.boolean(", "z.enum(", "z.nativeEnum(", "z.literal(", "z.coerce.number(", "z.int(", "z.bigint("} {
			if strings.HasPrefix(v, p) {
				out[tsKeyText(n.ChildByFieldName("key"), src)] = true
			}
		}
		return true
	})
	return out
}

func (e *taintEngine) tsSources(h *sitter.Node, declared map[string]bool) taintState {
	st := taintState{}
	params := h.ChildByFieldName("parameters")
	var first *sitter.Node
	if params != nil && params.NamedChildCount() > 0 {
		first = params.NamedChild(0)
	} else if p := h.ChildByFieldName("parameter"); p != nil {
		first = p
	}
	if first == nil {
		return st
	}
	line := astutil.NodeLine(first)
	pat := first
	if first.Type() == "required_parameter" || first.Type() == "optional_parameter" {
		pat = first.NamedChild(0)
	}
	if pat == nil {
		return st
	}
	allowed := func(k string) bool {
		return !e.excludedSet[k] && (len(declared) == 0 || declared[k])
	}
	switch pat.Type() {
	case "identifier":
		e.tsArgs = astutil.NodeText(pat, e.src)
		e.tsArgsLine = line
		if len(declared) > 0 {
			e.tsArgsKeys = declared
		}
	case "object_pattern":
		for i := 0; i < int(pat.NamedChildCount()); i++ {
			c := pat.NamedChild(i)
			switch c.Type() {
			case "shorthand_property_identifier_pattern":
				k := astutil.NodeText(c, e.src)
				if allowed(k) {
					st[k] = []taintOrigin{{param: k, srcLine: astutil.NodeLine(c)}}
				}
			case "object_assignment_pattern":
				if l := c.ChildByFieldName("left"); l != nil {
					k := astutil.NodeText(l, e.src)
					if allowed(k) {
						st[k] = []taintOrigin{{param: k, srcLine: astutil.NodeLine(c)}}
					}
				}
			case "pair_pattern":
				k := tsKeyText(c.ChildByFieldName("key"), e.src)
				v := c.ChildByFieldName("value")
				if v != nil && v.Type() == "assignment_pattern" {
					v = v.ChildByFieldName("left")
				}
				if v != nil && v.Type() == "identifier" && allowed(k) {
					st[astutil.NodeText(v, e.src)] = []taintOrigin{{param: k, srcLine: astutil.NodeLine(c)}}
				}
			}
		}
	}
	return st
}

// ---- statements ----

func tsTermType(t string) bool {
	return t == "throw_statement" || t == "return_statement" || t == "continue_statement" || t == "break_statement"
}

func tsTerminates(n *sitter.Node) bool {
	if n == nil {
		return false
	}
	if n.Type() != "statement_block" {
		return tsTermType(n.Type())
	}
	c := int(n.NamedChildCount())
	return c > 0 && tsTermType(n.NamedChild(c-1).Type())
}

func (e *taintEngine) tsBlock(n *sitter.Node, st taintState) taintState {
	if n == nil {
		return st
	}
	if n.Type() != "statement_block" {
		return e.tsStmt(n, st)
	}
	for i := 0; i < int(n.NamedChildCount()); i++ {
		if e.over() {
			return st
		}
		st = e.tsStmt(n.NamedChild(i), st)
	}
	return st
}

func (e *taintEngine) tsBind(target *sitter.Node, os []taintOrigin, st taintState, line int) {
	if target == nil {
		return
	}
	switch target.Type() {
	case "identifier", "shorthand_property_identifier_pattern":
		nm := astutil.NodeText(target, e.src)
		if len(os) == 0 {
			delete(st, nm)
		} else {
			st[nm] = withVia(os, line, false)
		}
	case "object_pattern", "array_pattern":
		for i := 0; i < int(target.NamedChildCount()); i++ {
			c := target.NamedChild(i)
			if c.Type() == "pair_pattern" {
				c = c.ChildByFieldName("value")
			}
			if c != nil && c.Type() == "assignment_pattern" {
				c = c.ChildByFieldName("left")
			}
			e.tsBind(c, os, st, line)
		}
	case "rest_pattern":
		if target.NamedChildCount() > 0 {
			e.tsBind(target.NamedChild(0), os, st, line)
		}
	}
}

func (e *taintEngine) tsStmt(n *sitter.Node, st taintState) taintState {
	line := astutil.NodeLine(n)
	switch n.Type() {
	case "lexical_declaration", "variable_declaration":
		for i := 0; i < int(n.NamedChildCount()); i++ {
			d := n.NamedChild(i)
			if d.Type() != "variable_declarator" {
				continue
			}
			v := d.ChildByFieldName("value")
			if v != nil && e.hasParen(v) {
				e.scan2(v, st)
			}
			e.sanitizeKills2(v, st)
			e.tsBind(d.ChildByFieldName("name"), e.expr(v, st), st, line)
		}
		return st
	case "expression_statement":
		if e.hasParen(n) {
			e.scan2(n, st)
		}
		if n.NamedChildCount() > 0 {
			c := n.NamedChild(0)
			e.sanitizeKills2(c, st)
			switch c.Type() {
			case "assignment_expression":
				e.tsBind(c.ChildByFieldName("left"), e.expr(c.ChildByFieldName("right"), st), st, line)
			case "augmented_assignment_expression":
				l := c.ChildByFieldName("left")
				if l != nil && l.Type() == "identifier" {
					if os := e.expr(c.ChildByFieldName("right"), st); len(os) > 0 {
						nm := astutil.NodeText(l, e.src)
						st[nm] = mergeOrigins(st[nm], withVia(asComposite(os), line, false))
					}
				}
			}
		}
		return st
	case "return_statement":
		e.scan2(n, st)
		if e.envOK && e.fnDepth == 0 && n.NamedChildCount() > 0 {
			for _, o := range e.expr(n.NamedChild(0), st) {
				e.record(o, models.SinkReturnValue, "return", line)
			}
		}
		return st
	case "statement_block":
		return e.tsBlock(n, st)
	case "if_statement":
		return e.tsIf(n, st)
	case "for_statement":
		if init := n.ChildByFieldName("initializer"); init != nil {
			st = e.tsStmt(init, st)
		}
		e.scan2(n.ChildByFieldName("condition"), st)
		return e.loop(st, func(s taintState) taintState {
			s = e.tsBlock(n.ChildByFieldName("body"), s)
			e.scan2(n.ChildByFieldName("increment"), s)
			return s
		})
	case "for_in_statement":
		right := n.ChildByFieldName("right")
		e.scan2(right, st)
		return e.loop(st, func(s taintState) taintState {
			e.tsBind(n.ChildByFieldName("left"), e.expr(right, s), s, line)
			return e.tsBlock(n.ChildByFieldName("body"), s)
		})
	case "while_statement", "do_statement":
		e.scan2(n.ChildByFieldName("condition"), st)
		return e.loop(st, func(s taintState) taintState { return e.tsBlock(n.ChildByFieldName("body"), s) })
	case "try_statement":
		out := st
		for _, f := range []string{"body", "handler", "finalizer"} {
			c := n.ChildByFieldName(f)
			if c == nil {
				continue
			}
			if c.Type() == "catch_clause" || c.Type() == "finally_clause" {
				s2 := out.clone()
				if p := c.ChildByFieldName("parameter"); p != nil {
					killNames(s2, []string{astutil.NodeText(p, e.src)})
				}
				out = unionState(out, e.tsBlock(c.ChildByFieldName("body"), s2))
				continue
			}
			out = unionState(out, e.tsBlock(c, out.clone()))
		}
		return out
	case "switch_statement":
		e.scan2(n.ChildByFieldName("value"), st)
		out := st
		if body := n.ChildByFieldName("body"); body != nil {
			for i := 0; i < int(body.NamedChildCount()); i++ {
				s2 := out.clone()
				c := body.NamedChild(i)
				for j := 0; j < int(c.NamedChildCount()); j++ {
					s2 = e.tsStmt(c.NamedChild(j), s2)
				}
				out = unionState(out, s2)
			}
		}
		return out
	case "labeled_statement":
		if b := n.ChildByFieldName("body"); b != nil {
			return e.tsStmt(b, st)
		}
		return st
	case "function_declaration", "class_declaration", "generator_function_declaration", "import_statement", "empty_statement",
		"interface_declaration", "type_alias_declaration", "enum_declaration":
		return st
	default:
		e.scan2(n, st)
		return st
	}
}

func (e *taintEngine) tsIf(n *sitter.Node, st taintState) taintState {
	cond := n.ChildByFieldName("condition")
	e.scan2(cond, st)
	then := st.clone()
	info := e.tsCheckInfo(cond, st)
	if info.single && info.positive {
		killNames(then, info.names)
	}
	cons := n.ChildByFieldName("consequence")
	res := e.tsBlock(cons, then)
	term := tsTerminates(cons)
	cur := st
	if term {
		if ms := e.mentioned(cond, st); len(ms) > 0 && !e.tsTrivial(cond) && !(info.single && info.positive) {
			cur = st.clone()
			killNames(cur, ms)
		}
	} else if info.single && !info.positive {
		cur = st.clone()
		killNames(cur, info.names)
	}
	if term && e.tsArgs != "" && !e.tsTrivial(cond) && !(info.single && info.positive) {
		// A guard on args.key ends that key's flow for the rest of the handler.
		for _, k := range e.tsArgKeysMentioned(cond) {
			if e.killedKeys == nil {
				e.killedKeys = map[string]bool{}
			}
			e.killedKeys[k] = true
		}
	}
	fall := cur
	if alt := n.ChildByFieldName("alternative"); alt != nil {
		var body *sitter.Node
		if alt.NamedChildCount() > 0 {
			body = alt.NamedChild(0)
		}
		fall = e.tsBlock(body, cur.clone())
		if tsTerminates(body) {
			fall = nil
		}
	}
	out := taintState{}
	if fall != nil {
		out = fall
	}
	if !term {
		out = unionState(out, res)
	}
	return out
}

var tsMatchMethods = map[string]bool{"includes": true, "has": true, "test": true, "startsWith": true, "endsWith": true,
	"match": true, "indexOf": true, "exec": true}

func tsStrip(n *sitter.Node, src []byte) (*sitter.Node, bool) {
	positive := true
	for n != nil {
		switch {
		case n.Type() == "parenthesized_expression" && n.NamedChildCount() > 0:
			n = n.NamedChild(0)
		case n.Type() == "unary_expression" && astutil.NodeText(n.ChildByFieldName("operator"), src) == "!":
			positive = !positive
			n = n.ChildByFieldName("argument")
		default:
			return n, positive
		}
	}
	return nil, positive
}

func (e *taintEngine) tsCheckInfo(test *sitter.Node, st taintState) checkResult {
	n, positive := tsStrip(test, e.src)
	if n == nil {
		return checkResult{}
	}
	var names []string
	add := func(c *sitter.Node) {
		if c != nil && c.Type() == "identifier" {
			if _, ok := st[astutil.NodeText(c, e.src)]; ok {
				names = append(names, astutil.NodeText(c, e.src))
			}
		}
	}
	switch n.Type() {
	case "binary_expression":
		op := astutil.NodeText(n.ChildByFieldName("operator"), e.src)
		switch op {
		case "==", "===", "!=", "!==", "in":
		default:
			return checkResult{}
		}
		add(n.ChildByFieldName("left"))
		add(n.ChildByFieldName("right"))
		if op == "!=" || op == "!==" {
			positive = !positive
		}
	case "call_expression":
		fn := n.ChildByFieldName("function")
		if fn == nil || fn.Type() != "member_expression" || !tsMatchMethods[astutil.NodeText(fn.ChildByFieldName("property"), e.src)] {
			return checkResult{}
		}
		add(fn.ChildByFieldName("object"))
		if args := n.ChildByFieldName("arguments"); args != nil {
			for i := 0; i < int(args.NamedChildCount()); i++ {
				add(args.NamedChild(i))
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

func (e *taintEngine) tsTrivial(test *sitter.Node) bool {
	n, _ := tsStrip(test, e.src)
	if n == nil {
		return false
	}
	if n.Type() == "identifier" {
		return true
	}
	txt := astutil.NodeText(n, e.src)
	return strings.Contains(txt, "typeof ") || strings.HasSuffix(txt, "== null") || strings.HasSuffix(txt, "== undefined") ||
		strings.HasSuffix(txt, "!= null") || strings.HasSuffix(txt, "!= undefined") || strings.HasPrefix(txt, "Array.isArray(")
}

func (e *taintEngine) sanitizeKills2(n *sitter.Node, st taintState) {
	if !e.mayNormalize(n) {
		return
	}
	astutil.Walk(n, func(c *sitter.Node) bool {
		if isTSFunc(c.Type()) {
			return false
		}
		if c.Type() != "call_expression" {
			return true
		}
		fn := c.ChildByFieldName("function")
		if fn == nil {
			return true
		}
		txt := astutil.NodeText(fn, e.src)
		m := lastSegment(txt)
		if txt == "path.resolve" || txt == "fs.realpathSync" || txt == "realpathSync" || txt == "fs.realpath" || txt == "fs.promises.realpath" ||
			m == "parse" || m == "safeParse" || m == "parseAsync" || m == "safeParseAsync" {
			if args := c.ChildByFieldName("arguments"); args != nil {
				for i := 0; i < int(args.NamedChildCount()); i++ {
					if a := args.NamedChild(i); a.Type() == "identifier" {
						delete(st, astutil.NodeText(a, e.src))
					}
				}
			}
		}
		return true
	})
}

// ---- expressions ----

var tsWrapperFuncs = map[string]bool{"String": true, "Buffer.from": true, "JSON.stringify": true, "JSON.parse": true,
	"path.join": true, "path.normalize": true, "path.posix.join": true, "path.win32.join": true, "decodeURIComponent": true,
	"Object.values": true, "Object.entries": true, "Object.keys": true, "Object.assign": true, "Object.fromEntries": true,
	"Array.from": true}

var tsMethodArgs = map[string]bool{"concat": true, "replace": true, "replaceAll": true, "join": true, "padStart": true, "padEnd": true}

var tsMethodRecv = map[string]bool{"toString": true, "trim": true, "trimStart": true, "trimEnd": true, "toLowerCase": true,
	"toUpperCase": true, "slice": true, "substring": true, "substr": true, "split": true, "at": true, "charAt": true,
	"repeat": true, "normalize": true, "valueOf": true, "flat": true}

func (e *taintEngine) tsExpr(n *sitter.Node, st taintState, depth int) []taintOrigin {
	if n == nil || depth > taintMaxExprDepth || e.over() {
		return nil
	}
	d := depth + 1
	switch n.Type() {
	case "identifier":
		return st[astutil.NodeText(n, e.src)]
	case "template_string":
		var os []taintOrigin
		subs, subLen := 0, 0
		for i := 0; i < int(n.NamedChildCount()); i++ {
			c := n.NamedChild(i)
			if c.Type() == "template_substitution" {
				subs++
				subLen += len(astutil.NodeText(c, e.src))
				if c.NamedChildCount() > 0 {
					os = mergeOrigins(os, e.tsExpr(c.NamedChild(0), st, d))
				}
			}
		}
		if len(os) == 0 {
			return nil
		}
		if subs > 1 || len(astutil.NodeText(n, e.src))-2 > subLen {
			return asComposite(os)
		}
		return os
	case "binary_expression":
		op := astutil.NodeText(n.ChildByFieldName("operator"), e.src)
		os := mergeOrigins(e.tsExpr(n.ChildByFieldName("left"), st, d), e.tsExpr(n.ChildByFieldName("right"), st, d))
		switch op {
		case "+":
			if len(os) > 0 {
				return asComposite(os)
			}
		case "??", "||", "&&":
			return os
		}
		return nil
	case "parenthesized_expression", "as_expression", "non_null_expression", "satisfies_expression", "await_expression",
		"spread_element", "type_assertion":
		if n.NamedChildCount() > 0 {
			return e.tsExpr(n.NamedChild(0), st, d)
		}
	case "ternary_expression":
		return mergeOrigins(e.tsExpr(n.ChildByFieldName("consequence"), st, d), e.tsExpr(n.ChildByFieldName("alternative"), st, d))
	case "array":
		var os []taintOrigin
		for i := 0; i < int(n.NamedChildCount()); i++ {
			os = mergeOrigins(os, e.tsExpr(n.NamedChild(i), st, d))
		}
		return asElem(os)
	case "object":
		var os []taintOrigin
		for i := 0; i < int(n.NamedChildCount()); i++ {
			c := n.NamedChild(i)
			switch c.Type() {
			case "pair":
				os = mergeOrigins(os, e.tsExpr(c.ChildByFieldName("value"), st, d))
			case "shorthand_property_identifier":
				os = mergeOrigins(os, st[astutil.NodeText(c, e.src)])
			case "spread_element":
				os = mergeOrigins(os, e.tsExpr(c, st, d))
			}
		}
		return asElem(os)
	case "member_expression":
		obj := n.ChildByFieldName("object")
		if e.mode.readsEnvSecrets() && obj != nil && astutil.NodeText(obj, e.src) == "process.env" {
			return e.tsEnvSecret(astutil.NodeText(n.ChildByFieldName("property"), e.src), n)
		}
		if e.tsArgs != "" && obj != nil && obj.Type() == "identifier" && astutil.NodeText(obj, e.src) == e.tsArgs {
			return e.tsArgOrigin(astutil.NodeText(n.ChildByFieldName("property"), e.src), n)
		}
		return dropElem(e.tsExpr(obj, st, d))
	case "subscript_expression":
		obj := n.ChildByFieldName("object")
		if e.mode.readsEnvSecrets() && obj != nil && astutil.NodeText(obj, e.src) == "process.env" {
			if idx := n.ChildByFieldName("index"); idx != nil && idx.Type() == "string" {
				return e.tsEnvSecret(tsKeyText(idx, e.src), n)
			}
			return nil
		}
		if e.tsArgs != "" && obj != nil && obj.Type() == "identifier" && astutil.NodeText(obj, e.src) == e.tsArgs {
			if idx := n.ChildByFieldName("index"); idx != nil && idx.Type() == "string" {
				return e.tsArgOrigin(tsKeyText(idx, e.src), n)
			}
			return nil
		}
		return dropElem(e.tsExpr(obj, st, d))
	case "call_expression":
		return e.tsCallExpr(n, st, d)
	}
	return nil
}

// tsEnvSecret returns the origin of a process.env read when the variable name passes the secret-name test.
func (e *taintEngine) tsEnvSecret(name string, at *sitter.Node) []taintOrigin {
	if !IsSecretEnvName(name) {
		return nil
	}
	return []taintOrigin{{param: name, kind: models.SourceSecretEnv, srcLine: astutil.NodeLine(at)}}
}

func (e *taintEngine) tsArgOrigin(key string, n *sitter.Node) []taintOrigin {
	if e.excludedSet[key] || e.killedKeys[key] || (e.tsArgsKeys != nil && !e.tsArgsKeys[key]) {
		return nil
	}
	return []taintOrigin{{param: key, srcLine: astutil.NodeLine(n)}}
}

func (e *taintEngine) tsCallExpr(n *sitter.Node, st taintState, d int) []taintOrigin {
	fn := n.ChildByFieldName("function")
	if fn == nil {
		return nil
	}
	args := n.ChildByFieldName("arguments")
	argOrigins := func() []taintOrigin {
		var os []taintOrigin
		if args != nil {
			for i := 0; i < int(args.NamedChildCount()); i++ {
				os = mergeOrigins(os, e.tsExpr(args.NamedChild(i), st, d))
			}
		}
		return os
	}
	txt := astutil.NodeText(fn, e.src)
	if tsWrapperFuncs[txt] {
		return argOrigins()
	}
	if fn.Type() == "member_expression" {
		meth := astutil.NodeText(fn.ChildByFieldName("property"), e.src)
		recv := fn.ChildByFieldName("object")
		switch {
		case tsMethodArgs[meth]:
			os := mergeOrigins(dropElem(e.tsExpr(recv, st, d)), argOrigins())
			if meth == "join" || meth == "concat" {
				return asComposite(os)
			}
			return os
		case tsMethodRecv[meth]:
			if meth == "slice" || meth == "substring" || meth == "substr" || meth == "at" || meth == "charAt" {
				// a part of a secret, as in key.slice(0, 4), is not followed
				return dropKind(dropElem(e.tsExpr(recv, st, d)), models.SourceSecretEnv)
			}
			return dropElem(e.tsExpr(recv, st, d))
		}
	}
	return nil
}

// ---- sink scan ----

func tsFuncParamNames(f *sitter.Node, src []byte) []string {
	var out []string
	var visit func(*sitter.Node)
	visit = func(n *sitter.Node) {
		if n == nil {
			return
		}
		switch n.Type() {
		case "identifier", "shorthand_property_identifier_pattern":
			out = append(out, astutil.NodeText(n, src))
			return
		case "type_annotation":
			return
		}
		for i := 0; i < int(n.NamedChildCount()); i++ {
			visit(n.NamedChild(i))
		}
	}
	visit(f.ChildByFieldName("parameters"))
	visit(f.ChildByFieldName("parameter"))
	return out
}

var tsCallbackMethods = map[string]bool{"map": true, "forEach": true, "filter": true, "flatMap": true, "some": true, "every": true, "find": true}

func (e *taintEngine) scan2(n *sitter.Node, st taintState) {
	if n == nil || e.over() {
		return
	}
	switch {
	case isTSFunc(n.Type()):
		s2 := st.clone()
		killNames(s2, tsFuncParamNames(n, e.src))
		if os, ok := e.cb[n.StartByte()]; ok {
			if ps := tsFuncParamNames(n, e.src); len(ps) > 0 {
				s2[ps[0]] = os
			}
		}
		body := n.ChildByFieldName("body")
		e.fnDepth++
		if body != nil && body.Type() == "statement_block" {
			e.tsBlock(body, s2)
		} else {
			e.scan2(body, s2)
		}
		e.fnDepth--
		return
	case n.Type() == "class_declaration" || n.Type() == "class":
		return
	case n.Type() == "new_expression":
		e.tsNewSink(n, st)
	case n.Type() == "call_expression":
		fn := n.ChildByFieldName("function")
		if fn != nil && fn.Type() == "member_expression" && tsCallbackMethods[astutil.NodeText(fn.ChildByFieldName("property"), e.src)] {
			if os := e.expr(fn.ChildByFieldName("object"), st); len(os) > 0 {
				if args := n.ChildByFieldName("arguments"); args != nil && args.NamedChildCount() > 0 {
					if cb := args.NamedChild(0); isTSFunc(cb.Type()) {
						if e.cb == nil {
							e.cb = map[uint32][]taintOrigin{}
						}
						e.cb[cb.StartByte()] = dropElemKeep(os)
					}
				}
			}
		}
		e.tsSinks(n, st)
	}
	for i := 0; i < int(n.NamedChildCount()); i++ {
		e.scan2(n.NamedChild(i), st)
	}
}

// dropElemKeep marks elements of a container as plain values for a callback parameter.
func dropElemKeep(os []taintOrigin) []taintOrigin {
	out := make([]taintOrigin, 0, len(os))
	for _, o := range os {
		o.elem = false
		out = append(out, o)
	}
	return out
}

func tsArgNodes(call *sitter.Node) []*sitter.Node {
	args := call.ChildByFieldName("arguments")
	var out []*sitter.Node
	if args == nil {
		return out
	}
	for i := 0; i < int(args.NamedChildCount()); i++ {
		out = append(out, args.NamedChild(i))
	}
	return out
}

// tsObjectProps returns the values of the object arguments whose key is in keys.
func (e *taintEngine) tsObjectProps(args []*sitter.Node, keys map[string]bool) []*sitter.Node {
	var out []*sitter.Node
	for _, a := range args {
		if a.Type() != "object" {
			continue
		}
		for i := 0; i < int(a.NamedChildCount()); i++ {
			c := a.NamedChild(i)
			switch c.Type() {
			case "pair":
				if keys[tsKeyText(c.ChildByFieldName("key"), e.src)] {
					out = append(out, c.ChildByFieldName("value"))
				}
			case "shorthand_property_identifier":
				if keys[astutil.NodeText(c, e.src)] {
					out = append(out, c)
				}
			}
		}
	}
	return out
}

func (e *taintEngine) tsNewSink(n *sitter.Node, st taintState) {
	ctor := astutil.NodeText(n.ChildByFieldName("constructor"), e.src)
	if ctor == "Function" {
		e.hit(models.SinkCodeExec, "new Function", tsArgNodes(n), st, false, astutil.NodeLine(n))
	}
}

var tsFSQualifiers = map[string]bool{"fs": true, "fsPromises": true, "fsp": true, "promises": true, "fs.promises": true}

var tsFSWrite = map[string]bool{"writeFile": true, "writeFileSync": true, "appendFile": true, "appendFileSync": true,
	"createWriteStream": true, "unlink": true, "unlinkSync": true, "rm": true, "rmSync": true, "rmdir": true, "rmdirSync": true,
	"mkdir": true, "mkdirSync": true, "rename": true, "renameSync": true, "copyFile": true, "copyFileSync": true, "cp": true,
	"cpSync": true, "symlink": true, "symlinkSync": true, "truncate": true, "truncateSync": true, "chmod": true, "chmodSync": true}

var tsFSRead = map[string]bool{"readFile": true, "readFileSync": true, "readdir": true, "readdirSync": true,
	"createReadStream": true, "stat": true, "statSync": true, "lstat": true, "lstatSync": true, "access": true, "accessSync": true,
	"open": true, "openSync": true, "opendir": true, "opendirSync": true, "readlink": true, "readlinkSync": true, "exists": true, "existsSync": true}

// bare (destructured import) names that are distinctive enough to count
var tsFSBare = map[string]bool{"writeFile": true, "writeFileSync": true, "appendFile": true, "appendFileSync": true,
	"createWriteStream": true, "readFile": true, "readFileSync": true, "readdir": true, "readdirSync": true,
	"createReadStream": true, "unlinkSync": true, "rmSync": true, "mkdirSync": true, "copyFileSync": true, "statSync": true}

var tsTwoPath = map[string]bool{"rename": true, "renameSync": true, "copyFile": true, "copyFileSync": true, "cp": true, "cpSync": true,
	"symlink": true, "symlinkSync": true}

func (e *taintEngine) tsSinks(call *sitter.Node, st taintState) {
	fn := call.ChildByFieldName("function")
	if fn == nil {
		return
	}
	raw := astutil.NodeText(fn, e.src)
	line := astutil.NodeLine(call)
	args := tsArgNodes(call)
	meth, recv := raw, ""
	if fn.Type() == "member_expression" {
		meth = astutil.NodeText(fn.ChildByFieldName("property"), e.src)
		recv = astutil.NodeText(fn.ChildByFieldName("object"), e.src)
	}
	first := args
	if len(first) > 1 {
		first = first[:1]
	}
	switch {
	case TSShellCallees[raw] || raw == "execa" || raw == "execaSync" || raw == "execaCommand" ||
		((recv == "cp" || recv == "childProcess" || recv == "cproc") && TSShellCallees[meth]):
		e.hit(models.SinkShell, raw, args, st, false, line)
		return
	case raw == "eval" || raw == "vm.runInNewContext" || raw == "vm.runInThisContext" || raw == "vm.runInContext" || raw == "vm.compileFunction":
		e.hit(models.SinkCodeExec, raw, first, st, false, line)
		return
	}
	qualified := recv != "" && tsFSQualifiers[recv]
	if (recv == "" && tsFSBare[meth]) || qualified {
		paths := first
		if tsTwoPath[meth] && len(args) > 1 {
			paths = args[:2]
		}
		switch {
		case tsFSWrite[meth]:
			e.hit(models.SinkFileWrite, raw, paths, st, false, line)
			return
		case tsFSRead[meth]:
			e.hit(models.SinkPathIO, raw, paths, st, false, line)
			return
		}
	}
	if recv == "" {
		return
	}
	if vectorMethods[meth] {
		nodes := e.tsObjectProps(args, vectorKwargs)
		if (meth == "similaritySearch" || meth == "similaritySearchWithScore") && len(args) > 2 {
			nodes = append(nodes, args[2])
		}
		e.hit(models.SinkVectorFilter, raw, nodes, st, false, line)
	}
	if sqlMethods[meth] && meth != "get" {
		e.hit(models.SinkSQLExecute, raw, first, st, false, line)
	}
	if storeMethods[meth] && e.receiverIsStore(recv) {
		e.hit(models.SinkStoreNamespace, raw, first, st, false, line)
	}
	if memoryMethods[meth] && e.receiverIsMemory(recv) {
		e.hit(models.SinkMemoryScopeKey, raw, e.tsObjectProps(args, memoryScopeKeys), st, true, line)
	}
}

// tsArgKeysMentioned lists the declared keys read as args.key or args["key"] in n.
func (e *taintEngine) tsArgKeysMentioned(n *sitter.Node) []string {
	var out []string
	astutil.Walk(n, func(c *sitter.Node) bool {
		switch c.Type() {
		case "member_expression":
			if o := c.ChildByFieldName("object"); o != nil && o.Type() == "identifier" && astutil.NodeText(o, e.src) == e.tsArgs {
				out = append(out, astutil.NodeText(c.ChildByFieldName("property"), e.src))
			}
		case "subscript_expression":
			if o := c.ChildByFieldName("object"); o != nil && o.Type() == "identifier" && astutil.NodeText(o, e.src) == e.tsArgs {
				if idx := c.ChildByFieldName("index"); idx != nil && idx.Type() == "string" {
					out = append(out, tsKeyText(idx, e.src))
				}
			}
		}
		return true
	})
	return out
}
