package analysis

import (
	"strings"

	sitter "github.com/smacker/go-tree-sitter"

	"github.com/trustabl/trustabl/internal/analysis/astutil"
	"github.com/trustabl/trustabl/internal/models"
)

// Deep Agents (the `deepagents` package) Python agent discovery.
//
// A Deep Agent is built by one call, `create_deep_agent(model=, tools=,
// backend=, interrupt_on=, ...)`. The emitted AgentDef carries SDK=SDKLangChain
// (the graph it returns is a LangGraph graph, so the langchain policy pack
// applies) and the normalized Class "DeepAgent". The class is deliberately
// separate from "ReactAgent" / "CreateAgent": the langchain_agent rules read
// those two, and folding Deep Agents into them would change which existing
// rules fire.
//
// Discovery is import-bound: only a callee that a `from deepagents import
// create_deep_agent [as X]` statement bound (or `deepagents.create_deep_agent`
// after `import deepagents [as alias]`) is an agent, so a same-named function
// defined in the repo is never claimed. A file that imports nothing from
// deepagents is not walked.
//
// Two facts are stamped on the AgentDef for LC-118 (see models.AgentDef):
//
//   - DeepShellBackend: whether backend= resolves, deterministically and in this
//     file, to a host shell. LocalShellBackend(...) directly, a variable bound
//     exactly once (unconditionally, in the call's own function or at module
//     level) to such a call, a CompositeBackend whose DEFAULT is one of those
//     (a LocalShellBackend only inside routes does not expose execute: see
//     deepagents supports_execution), or a same-file subclass that does not
//     override execute / aexecute. Everything else (factories, lambdas, other
//     modules, reassigned or conditional bindings, an overriding subclass) is
//     left unresolved rather than guessed.
//   - DeepExecuteGate: what interrupt_on says about "execute", read only when it
//     is a literal dict in the call. See models.AgentDef.DeepExecuteGate.
//
// create_deep_agent is the only constructor matched. async_create_deep_agent is
// not defined by the library.

// isDeepAgentsModule reports whether a dotted module path belongs to the
// deepagents package: `deepagents`, `deepagents.*`. The dot boundary keeps
// `deepagents_cli` and unrelated `deepagentsx` packages out.
func isDeepAgentsModule(mod string) bool {
	return mod == "deepagents" || strings.HasPrefix(mod, "deepagents.")
}

// fileImportsDeepAgents reports whether pf imports the deepagents package via a
// real import statement (AST-based, not a source substring).
func fileImportsDeepAgents(pf ParsedFile) bool {
	return fileImportsModule(pf, isDeepAgentsModule)
}

// deepAgentsImports records how a file binds deepagents symbols.
type deepAgentsImports struct {
	names map[string]string // local name -> name it has in the deepagents module
	roots map[string]bool   // local names that are, or may be, deepagents modules
}

func collectDeepAgentsImports(pf ParsedFile) deepAgentsImports {
	imp := deepAgentsImports{names: map[string]string{}, roots: map[string]bool{}}
	astutil.Walk(pf.Tree.RootNode(), func(n *sitter.Node) bool {
		switch n.Type() {
		case "import_from_statement":
			modNode := n.ChildByFieldName("module_name")
			if !isDeepAgentsModule(astutil.NodeText(modNode, pf.Source)) {
				return true
			}
			for i := 0; i < int(n.ChildCount()); i++ {
				c := n.Child(i)
				if modNode != nil && c.StartByte() == modNode.StartByte() {
					continue
				}
				switch c.Type() {
				case "dotted_name":
					name := astutil.NodeText(c, pf.Source)
					imp.names[name] = name
					imp.roots[name] = true
				case "aliased_import":
					name := astutil.NodeText(c.ChildByFieldName("name"), pf.Source)
					alias := astutil.NodeText(c.ChildByFieldName("alias"), pf.Source)
					imp.names[alias] = name
					imp.roots[alias] = true
				}
			}
		case "import_statement":
			for i := 0; i < int(n.ChildCount()); i++ {
				c := n.Child(i)
				switch c.Type() {
				case "dotted_name":
					mod := astutil.NodeText(c, pf.Source)
					if isDeepAgentsModule(mod) {
						imp.roots[strings.SplitN(mod, ".", 2)[0]] = true
					}
				case "aliased_import":
					if isDeepAgentsModule(astutil.NodeText(c.ChildByFieldName("name"), pf.Source)) {
						imp.roots[astutil.NodeText(c.ChildByFieldName("alias"), pf.Source)] = true
					}
				}
			}
		}
		return true
	})
	return imp
}

// is reports whether callee names the deepagents symbol target: a bare local
// name the file imported under that name, or <module>.<...>.target where the
// first segment is bound to a deepagents module.
func (imp deepAgentsImports) is(callee, target string) bool {
	dot := strings.IndexByte(callee, '.')
	if dot < 0 {
		return imp.names[callee] == target
	}
	return imp.roots[callee[:dot]] && callee[strings.LastIndexByte(callee, '.')+1:] == target
}

// deepCtx is the per-file state the backend and interrupt_on resolvers share.
type deepCtx struct {
	pf  deepAgentsImports
	src []byte
	// subclasses maps a same-file subclass of LocalShellBackend to true when it
	// overrides neither execute nor aexecute.
	subclasses map[string]bool
	// harness is true when the file registers a harness profile or sets
	// excluded_tools, either of which can remove the execute tool from the agent.
	harness bool
}

// DiscoverDeepAgents walks each ParsedFile and emits one AgentDef per
// create_deep_agent(...) call bound to the deepagents package.
func DiscoverDeepAgents(files []ParsedFile) []models.AgentDef {
	var out []models.AgentDef
	for _, pf := range files {
		if !fileImportsDeepAgents(pf) {
			continue
		}
		out = append(out, discoverDeepAgentsInFile(pf)...)
	}
	return out
}

func discoverDeepAgentsInFile(pf ParsedFile) []models.AgentDef {
	imp := collectDeepAgentsImports(pf)
	ctx := newDeepCtx(pf, imp)
	var out []models.AgentDef
	astutil.Walk(pf.Tree.RootNode(), func(n *sitter.Node) bool {
		if n.Type() != "call" {
			return true
		}
		if !imp.is(astutil.NodeText(n.ChildByFieldName("function"), pf.Source), "create_deep_agent") {
			return true
		}
		kwargs, opaque := extractCallKwargs(n, pf.Source)
		a := models.AgentDef{
			SDK:      models.SDKLangChain,
			Class:    "DeepAgent",
			Language: models.LanguagePython,
			Location: models.Location{
				FilePath: pf.RelPath,
				Line:     int(n.StartPoint().Row) + 1,
				EndLine:  int(n.EndPoint().Row) + 1,
			},
			Kwargs: kwargs,
			Opaque: opaque,
		}
		if nm := kwargStringLiteral(kwargs, "name"); nm != "" {
			a.Name = nm
		}
		if p := n.Parent(); p != nil && p.Type() == "assignment" {
			if l := p.ChildByFieldName("left"); l != nil && l.Type() == "identifier" {
				a.VarName = astutil.NodeText(l, pf.Source)
			}
		}
		if shape := ctx.shellShape(keywordArgNode(n, "backend", pf.Source), n, 0); shape != "" {
			a.DeepShellBackend = shape
			a.DeepExecuteGate = ctx.executeGate(n, opaque)
		}
		out = append(out, a)
		return true
	})
	return out
}

func newDeepCtx(pf ParsedFile, imp deepAgentsImports) *deepCtx {
	c := &deepCtx{pf: imp, src: pf.Source, subclasses: map[string]bool{}}
	astutil.Walk(pf.Tree.RootNode(), func(n *sitter.Node) bool {
		switch n.Type() {
		case "class_definition":
			supers := n.ChildByFieldName("superclasses")
			if supers == nil {
				return true
			}
			for i := 0; i < int(supers.NamedChildCount()); i++ {
				if imp.is(astutil.NodeText(supers.NamedChild(i), pf.Source), "LocalShellBackend") {
					name := astutil.NodeText(n.ChildByFieldName("name"), pf.Source)
					c.subclasses[name] = !classDefinesMethod(n.ChildByFieldName("body"), pf.Source, "execute", "aexecute")
				}
			}
		case "call":
			if lastSegment(astutil.NodeText(n.ChildByFieldName("function"), pf.Source)) == "register_harness_profile" {
				c.harness = true
			}
		case "keyword_argument":
			if astutil.NodeText(n.ChildByFieldName("name"), pf.Source) == "excluded_tools" {
				c.harness = true
			}
		}
		return true
	})
	return c
}

// classDefinesMethod reports whether a class body directly defines a method
// with one of the given names (plain or decorated).
func classDefinesMethod(body *sitter.Node, src []byte, names ...string) bool {
	if body == nil {
		return false
	}
	for i := 0; i < int(body.NamedChildCount()); i++ {
		c := body.NamedChild(i)
		if c.Type() == "decorated_definition" {
			c = c.ChildByFieldName("definition")
		}
		if c == nil || c.Type() != "function_definition" {
			continue
		}
		fn := astutil.NodeText(c.ChildByFieldName("name"), src)
		for _, want := range names {
			if fn == want {
				return true
			}
		}
	}
	return false
}

// shellShape resolves the backend= expression n to "direct", "composite_default",
// "subclass" or "" (not a host shell, or not resolvable here). at is the
// create_deep_agent call, used to find the scope a variable name is read in.
func (c *deepCtx) shellShape(n, at *sitter.Node, depth int) string {
	if n == nil || depth > 3 {
		return ""
	}
	switch n.Type() {
	case "call":
		callee := astutil.NodeText(n.ChildByFieldName("function"), c.src)
		if c.pf.is(callee, "LocalShellBackend") {
			return "direct"
		}
		if plain, ok := c.subclasses[callee]; ok && plain {
			return "subclass"
		}
		if c.pf.is(callee, "CompositeBackend") {
			def := keywordArgNode(n, "default", c.src)
			if def == nil {
				def = positionalArgNode(n, 0)
			}
			if c.shellShape(def, at, depth+1) != "" {
				return "composite_default"
			}
		}
	case "identifier":
		if rhs := c.singleBinding(astutil.NodeText(n, c.src), at); rhs != nil {
			return c.shellShape(rhs, at, depth+1)
		}
	}
	return ""
}

// singleBinding returns the right-hand side of the one unconditional assignment
// to name in the scope the call at sits in: its enclosing function when that
// function assigns name, otherwise the module. More than one assignment, an
// assignment under if / try / for / while / match, a function parameter of the
// same name, or none at all yields nil.
func (c *deepCtx) singleBinding(name string, at *sitter.Node) *sitter.Node {
	var fn *sitter.Node
	for p := at.Parent(); p != nil; p = p.Parent() {
		if p.Type() == "function_definition" {
			fn = p
			break
		}
	}
	if fn != nil {
		if params := fn.ChildByFieldName("parameters"); params != nil && subtreeHasIdentifier(params, name, c.src) {
			return nil
		}
		if rhs, found := c.bindingIn(fn.ChildByFieldName("body"), fn, name); found {
			return rhs
		}
	}
	root := at
	for root.Parent() != nil {
		root = root.Parent()
	}
	rhs, _ := c.bindingIn(root, root, name)
	return rhs
}

// bindingIn looks for assignments to name directly in scope (not in nested
// functions, classes or lambdas). found reports whether any exists; rhs is set
// only when there is exactly one and it is unconditional.
func (c *deepCtx) bindingIn(start, scope *sitter.Node, name string) (rhs *sitter.Node, found bool) {
	if start == nil {
		return nil, false
	}
	count := 0
	var hit *sitter.Node
	conditional := false
	var rec func(n *sitter.Node)
	rec = func(n *sitter.Node) {
		for i := 0; i < int(n.NamedChildCount()); i++ {
			ch := n.NamedChild(i)
			switch ch.Type() {
			case "function_definition", "class_definition", "lambda":
				continue
			case "assignment":
				l, r := ch.ChildByFieldName("left"), ch.ChildByFieldName("right")
				if l != nil && l.Type() == "identifier" && r != nil && astutil.NodeText(l, c.src) == name {
					count++
					hit = r
					if underConditional(ch, scope) {
						conditional = true
					}
				}
			}
			rec(ch)
		}
	}
	rec(start)
	if count == 0 {
		return nil, false
	}
	if count > 1 || conditional {
		return nil, true
	}
	return hit, true
}

func underConditional(n, scope *sitter.Node) bool {
	for p := n.Parent(); p != nil && p.StartByte() >= scope.StartByte() && p != scope; p = p.Parent() {
		switch p.Type() {
		case "if_statement", "try_statement", "for_statement", "while_statement", "match_statement":
			return true
		}
	}
	return false
}

func subtreeHasIdentifier(n *sitter.Node, name string, src []byte) bool {
	found := false
	astutil.Walk(n, func(c *sitter.Node) bool {
		if c.Type() == "identifier" && astutil.NodeText(c, src) == name {
			found = true
		}
		return !found
	})
	return found
}

// executeGate classifies what the create_deep_agent call says about approval of
// the execute tool. See models.AgentDef.DeepExecuteGate for the values.
func (c *deepCtx) executeGate(call *sitter.Node, opaque bool) string {
	// Agent(**config), a harness profile or excluded_tools in the file, or a
	// middleware= value that is not a literal list of calls can all change which
	// tools exist or whether they pause; none is read, so none is judged.
	if opaque || c.harness {
		return "unjudged"
	}
	if mw := keywordArgNode(call, "middleware", c.src); mw != nil {
		if mw.Type() != "list" {
			return "unjudged"
		}
		for i := 0; i < int(mw.NamedChildCount()); i++ {
			el := mw.NamedChild(i)
			if el.Type() == "comment" {
				continue
			}
			if el.Type() != "call" || lastSegment(astutil.NodeText(el.ChildByFieldName("function"), c.src)) == "HumanInTheLoopMiddleware" {
				return "unjudged"
			}
		}
	}
	node := keywordArgNode(call, "interrupt_on", c.src)
	if node == nil || node.Type() == "none" {
		return "absent"
	}
	if node.Type() != "dictionary" {
		return "unjudged"
	}
	gate, pairs := "", 0
	for i := 0; i < int(node.NamedChildCount()); i++ {
		p := node.NamedChild(i)
		switch p.Type() {
		case "comment":
			continue
		case "pair":
		default:
			return "unjudged" // **splat or comprehension
		}
		pairs++
		key := p.ChildByFieldName("key")
		if key == nil || key.Type() != "string" {
			return "unjudged"
		}
		text := astutil.NodeText(key, c.src)
		if text == "" || (text[0] != '"' && text[0] != '\'') {
			return "unjudged" // prefixed (f-string, bytes) or otherwise not a plain literal
		}
		if strings.Trim(text, `"'`) != "execute" {
			continue
		}
		val := p.ChildByFieldName("value")
		switch {
		case val == nil:
			return "unjudged"
		case val.Type() == "false":
			gate = "execute_false"
		case val.Type() == "true", val.Type() == "dictionary", val.Type() == "call":
			gate = "gated"
		default:
			return "unjudged"
		}
	}
	switch {
	case pairs == 0:
		return "empty"
	case gate != "":
		return gate
	}
	return "other_tools"
}
