package analysis

import (
	"path"
	"sort"
	"strings"

	sitter "github.com/smacker/go-tree-sitter"

	"github.com/trustabl/trustabl/internal/analysis/astutil"
	"github.com/trustabl/trustabl/internal/models"
)

// Server-entrypoint discovery (Python) and run-call reachability.
//
// It records which functions a server framework invokes on behalf of an
// external request or job, and stamps the agent run calls that are reachable
// from them (AgentRunCallDef.ServerReachable), so a rule can tell "an agent run
// inside a request handler" from "an agent run in a script". The agent_server_reachable predicate (LC-116, LangGraph in-memory
// checkpointer on a server path) reads the stamp; the entrypoint list itself is
// a reported fact.
//
// Recognized entrypoints (each gated on a real import of the framework):
//
//   - FastAPI: @<recv>.get/post/put/patch/delete/head/options/websocket and
//     @<recv>.api_route. recv is resolved in-file: bound to FastAPI(...) /
//     APIRouter(...) it is accepted; bound to anything else it is rejected;
//     unbound (imported from another module, e.g. `from app.main import app`)
//     it is accepted when the file imports fastapi. That last case is a small,
//     documented false positive: any object with a `.get(...)` decorator in a
//     fastapi-importing file counts.
//   - Flask: @<recv>.route/get/post/put/patch/delete, recv bound to Flask(...) /
//     Blueprint(...) or unbound in a file that imports flask.
//   - Celery: @<app>.task, @shared_task, @celery.shared_task.
//   - Dramatiq: @dramatiq.actor (or bare @actor imported from dramatiq).
//
// When an unbound recv sits in a file importing BOTH fastapi and flask, fastapi
// wins, but only for the methods present in both sets (get/post/put/patch/
// delete). Flask-only `route` stays flask; fastapi-only methods (websocket,
// api_route, head, options) stay fastapi.
//
// Entrypoints under a directory segment in entrypointSkipDirs (examples, demo,
// docs, ...) are not recorded. The list is local to this detector; pathclass
// (a test-only classifier) is deliberately not widened.
//
// `if __name__ == "__main__"` and typer/click/argparse mark a file as runnable
// but never suppress an http/worker entrypoint (uvicorn.run(app) lives in
// __main__), so v1 records nothing for them.
//
// Deferred: Django, Starlette Route(), RQ, Temporal, serverless handlers,
// add_api_route / add_url_rule, class-based views, and TypeScript.

// entrypointSkipDirs are directory segments (case-insensitive) beneath which
// entrypoints are not recorded: shipped samples, not production surfaces.
var entrypointSkipDirs = map[string]bool{
	"examples": true, "example": true, "demo": true, "demos": true,
	"samples": true, "cookbook": true, "tutorial": true, "tutorials": true,
	"docs": true,
}

var (
	fastAPIEntrypointMethods = map[string]bool{
		"get": true, "post": true, "put": true, "patch": true, "delete": true,
		"head": true, "options": true, "websocket": true, "api_route": true,
	}
	flaskEntrypointMethods = map[string]bool{
		"route": true, "get": true, "post": true, "put": true, "patch": true, "delete": true,
	}
)

type entrypointFileImports struct{ fastapi, flask, celery, dramatiq bool }

func moduleIs(root string) func(string) bool {
	return func(mod string) bool { return mod == root || strings.HasPrefix(mod, root+".") }
}

// DiscoverEntrypoints returns the server entrypoints in files, sorted by
// (FilePath, Line, Route, Framework) so the output is deterministic regardless
// of input order.
func DiscoverEntrypoints(files []ParsedFile) []models.EntrypointDef {
	var out []models.EntrypointDef
	for _, pf := range files {
		if entrypointPathSkipped(pf.RelPath) {
			continue
		}
		imp := entrypointFileImports{
			fastapi:  fileImportsModule(pf, moduleIs("fastapi")),
			flask:    fileImportsModule(pf, moduleIs("flask")),
			celery:   fileImportsModule(pf, moduleIs("celery")),
			dramatiq: fileImportsModule(pf, moduleIs("dramatiq")),
		}
		if !imp.fastapi && !imp.flask && !imp.celery && !imp.dramatiq {
			continue
		}
		out = append(out, discoverEntrypointsInFile(pf, imp)...)
	}
	sort.SliceStable(out, func(i, j int) bool {
		a, b := out[i], out[j]
		if a.FilePath != b.FilePath {
			return a.FilePath < b.FilePath
		}
		if a.Line != b.Line {
			return a.Line < b.Line
		}
		if a.Route != b.Route {
			return a.Route < b.Route
		}
		return a.Framework < b.Framework
	})
	return out
}

func entrypointPathSkipped(rel string) bool {
	segs := strings.Split(rel, "/")
	for _, s := range segs[:len(segs)-1] { // directories only, never the filename
		if entrypointSkipDirs[strings.ToLower(s)] {
			return true
		}
	}
	return false
}

// entrypointReceiverBindings maps each in-file `<ident> = <call>` target to the
// framework of the constructor it is bound to, or "other" for any non-framework
// binding. A framework binding beats "other" when a name is bound both ways.
func entrypointReceiverBindings(pf ParsedFile) map[string]string {
	out := map[string]string{}
	astutil.Walk(pf.Tree.RootNode(), func(n *sitter.Node) bool {
		if n.Type() != "assignment" {
			return true
		}
		l, r := n.ChildByFieldName("left"), n.ChildByFieldName("right")
		if l == nil || l.Type() != "identifier" {
			return true
		}
		name := astutil.NodeText(l, pf.Source)
		fw := "other"
		if r != nil && r.Type() == "call" {
			callee := astutil.NodeText(r.ChildByFieldName("function"), pf.Source)
			if i := strings.LastIndex(callee, "."); i >= 0 {
				callee = callee[i+1:]
			}
			switch callee {
			case "FastAPI", "APIRouter":
				fw = "fastapi"
			case "Flask", "Blueprint":
				fw = "flask"
			case "Celery":
				fw = "celery"
			}
		}
		if cur, ok := out[name]; !ok || cur == "other" {
			out[name] = fw
		}
		return true
	})
	return out
}

func discoverEntrypointsInFile(pf ParsedFile, imp entrypointFileImports) []models.EntrypointDef {
	var bindings map[string]string
	var out []models.EntrypointDef
	astutil.Walk(pf.Tree.RootNode(), func(n *sitter.Node) bool {
		if n.Type() != "decorated_definition" {
			return true
		}
		fn := astutil.FunctionDef(n)
		if fn == nil {
			return true
		}
		for _, dec := range astutil.Decorators(n) {
			if bindings == nil {
				bindings = entrypointReceiverBindings(pf)
			}
			fw, kind, route, ok := classifyEntrypointDecorator(dec, pf, imp, bindings)
			if !ok {
				continue
			}
			out = append(out, models.EntrypointDef{
				Location: models.Location{
					FilePath: pf.RelPath,
					Line:     int(fn.StartPoint().Row) + 1,
					EndLine:  int(fn.EndPoint().Row) + 1,
				},
				Kind:      kind,
				Framework: fw,
				Route:     route,
				FuncName:  astutil.FunctionName(fn, pf.Source),
			})
		}
		return true
	})
	return out
}

// classifyEntrypointDecorator decides whether one decorator registers its
// function as a server entrypoint.
func classifyEntrypointDecorator(dec *sitter.Node, pf ParsedFile, imp entrypointFileImports, bindings map[string]string) (fw string, kind models.EntrypointKind, route string, ok bool) {
	if dec.NamedChildCount() == 0 {
		return "", "", "", false
	}
	expr := dec.NamedChild(0)
	var call *sitter.Node
	callee := expr
	if expr.Type() == "call" {
		call = expr
		callee = expr.ChildByFieldName("function")
	}
	if callee == nil {
		return "", "", "", false
	}
	routeOf := func() string {
		if call == nil {
			return ""
		}
		return positionalStringLiteral(call, 0, pf.Source)
	}

	switch callee.Type() {
	case "identifier":
		switch astutil.NodeText(callee, pf.Source) {
		case "shared_task":
			if imp.celery {
				return "celery", models.EntrypointWorker, "", true
			}
		case "actor":
			if imp.dramatiq {
				return "dramatiq", models.EntrypointWorker, "", true
			}
		}
	case "attribute":
		obj, attr := callee.ChildByFieldName("object"), callee.ChildByFieldName("attribute")
		if obj == nil || attr == nil || obj.Type() != "identifier" {
			return "", "", "", false
		}
		recv, method := astutil.NodeText(obj, pf.Source), astutil.NodeText(attr, pf.Source)
		if recv == "dramatiq" && method == "actor" && imp.dramatiq {
			return "dramatiq", models.EntrypointWorker, "", true
		}
		if recv == "celery" && method == "shared_task" && imp.celery {
			return "celery", models.EntrypointWorker, "", true
		}
		switch bindings[recv] {
		case "fastapi":
			if imp.fastapi && fastAPIEntrypointMethods[method] {
				return "fastapi", models.EntrypointHTTP, routeOf(), true
			}
		case "flask":
			if imp.flask && flaskEntrypointMethods[method] {
				return "flask", models.EntrypointHTTP, routeOf(), true
			}
		case "celery":
			if imp.celery && method == "task" {
				return "celery", models.EntrypointWorker, "", true
			}
		case "other":
			// bound in-file to something that is not a framework object
		default: // unbound: imported from another module
			switch {
			case imp.fastapi && fastAPIEntrypointMethods[method]:
				return "fastapi", models.EntrypointHTTP, routeOf(), true
			case imp.flask && flaskEntrypointMethods[method]:
				return "flask", models.EntrypointHTTP, routeOf(), true
			case imp.celery && method == "task":
				return "celery", models.EntrypointWorker, "", true
			}
		}
	}
	return "", "", "", false
}

// Run-call reachability.
//
// ApplyEntrypointReachability stamps AgentRunCallDef.ServerReachable when a run
// call is (A) inside the entrypoint function, (B) inside a same-file top-level
// function called from the entrypoint body, or (C) inside a top-level function
// imported by name (`from pkg.mod import helper`, or relative) that the
// entrypoint calls. Every descendant call node of the entrypoint body is
// considered (under await, return, assignments, nested arguments). Entrypoints
// are visited in sorted order and the first match wins, so the result is
// deterministic. Mutates inv.AgentRunCalls in place.
//
// v1 limits, deliberately not a call graph:
//   - one hop per edge kind: a helper called by a helper is NOT followed;
//   - only bare-identifier callees: no `import pkg.mod as m; m.f()`, no
//     attribute / method calls (`self.svc.run()`), no class methods;
//   - no dependency injection (FastAPI Depends(...)), no callbacks, no
//     star imports;
//   - absolute imports use matchesModule, whose suffix match may be ambiguous;
//     the first parsed file (in input order) defining the named top-level
//     function wins;
//   - relative imports (`.mod`, `..mod`) are resolved from the importer's
//     directory to a file that must be in the scan set, else nothing is stamped.
//     `from . import name` is followed only when `name` is a top-level function
//     in the package's __init__.py; `from . import submodule` followed by
//     `submodule.f()` is not followed. Resolution never goes above the repo root.
func ApplyEntrypointReachability(inv *models.RepoInventory, parsed []ParsedFile) {
	if len(inv.Entrypoints) == 0 || len(inv.AgentRunCalls) == 0 {
		return
	}
	files := make(map[string]ParsedFile, len(parsed))
	for _, pf := range parsed {
		if _, ok := files[pf.RelPath]; !ok {
			files[pf.RelPath] = pf
		}
	}
	imports := buildImportsByFile(parsed)
	topFuncs := map[string]map[string]*sitter.Node{}
	topOf := func(rel string) map[string]*sitter.Node {
		if m, ok := topFuncs[rel]; ok {
			return m
		}
		pf, ok := files[rel]
		var m map[string]*sitter.Node
		if ok {
			m = indexTopLevelFunctions(pf.Tree.RootNode(), pf.Source)
		}
		topFuncs[rel] = m
		return m
	}
	defsByLine := map[string]map[int]*sitter.Node{}
	defAt := func(rel string, line int) *sitter.Node {
		m, ok := defsByLine[rel]
		if !ok {
			m = map[int]*sitter.Node{}
			if pf, found := files[rel]; found {
				for _, fn := range astutil.FindAll(pf.Tree.RootNode(), "function_definition") {
					if _, dup := m[int(fn.StartPoint().Row)+1]; !dup {
						m[int(fn.StartPoint().Row)+1] = fn
					}
				}
			}
			defsByLine[rel] = m
		}
		return m[line]
	}

	type span struct {
		file       string
		start, end int
		via        string
	}
	spansFor := func(ep models.EntrypointDef) []span {
		fn := defAt(ep.FilePath, ep.Line)
		if fn == nil {
			return nil
		}
		pf := files[ep.FilePath]
		spans := []span{{ep.FilePath, int(fn.StartPoint().Row) + 1, int(fn.EndPoint().Row) + 1, "direct"}}
		add := func(file string, def *sitter.Node, via string) {
			spans = append(spans, span{file, int(def.StartPoint().Row) + 1, int(def.EndPoint().Row) + 1, via})
		}
		astutil.Walk(fn, func(n *sitter.Node) bool {
			if n.Type() != "call" {
				return true
			}
			callee := n.ChildByFieldName("function")
			if callee == nil || callee.Type() != "identifier" {
				return true
			}
			name := astutil.NodeText(callee, pf.Source)
			if def := topOf(ep.FilePath)[name]; def != nil {
				add(ep.FilePath, def, "same_file")
				return true
			}
			if imp, ok := imports[ep.FilePath][name]; ok {
				if file, def := resolveImportedFunction(ep.FilePath, imp, parsed, files, topOf); def != nil {
					add(file, def, "import")
				}
			}
			return true
		})
		return spans
	}

	allSpans := make([][]span, len(inv.Entrypoints))
	for i, ep := range inv.Entrypoints {
		allSpans[i] = spansFor(ep)
	}
	for i := range inv.AgentRunCalls {
		rc := &inv.AgentRunCalls[i]
		for j, ep := range inv.Entrypoints {
			if rc.ServerReachable != nil {
				break
			}
			for _, s := range allSpans[j] {
				if s.file == rc.FilePath && s.start <= rc.Line && rc.Line <= s.end {
					rc.ServerReachable = &models.EntrypointRef{
						FilePath: ep.FilePath, Line: ep.Line, FuncName: ep.FuncName,
						Kind: ep.Kind, Framework: ep.Framework, Via: s.via,
					}
					break
				}
			}
		}
	}
}

// resolveImportedFunction finds the top-level function an import binding names,
// in a file that is part of the scan set. Returns ("", nil) when unresolvable.
func resolveImportedFunction(importer string, imp importBinding, parsed []ParsedFile, files map[string]ParsedFile, topOf func(string) map[string]*sitter.Node) (string, *sitter.Node) {
	file := resolveImportedName(importer, imp, parsed, files, func(rel, name string) bool {
		return topOf(rel)[name] != nil
	})
	if file == "" {
		return "", nil
	}
	return file, topOf(file)[imp.name]
}

// resolveImportedName returns the scanned file that defines the name an import
// binding refers to, where "defines" is decided by has(file, name). Returns ""
// when unresolvable. Relative imports are resolved from the importer's
// directory (a candidate that exists but does not define the name stops the
// search: no guessing); absolute imports use matchesModule, first parsed file
// in input order wins.
func resolveImportedName(importer string, imp importBinding, parsed []ParsedFile, files map[string]ParsedFile, has func(rel, name string) bool) string {
	if strings.HasPrefix(imp.module, ".") {
		for _, cand := range relativeImportCandidates(importer, imp.module) {
			if _, ok := files[cand]; !ok {
				continue
			}
			if has(cand, imp.name) {
				return cand
			}
			return "" // the target exists but does not define the name: stop, no guessing
		}
		return ""
	}
	for _, pf := range parsed {
		if !matchesModule(pf.RelPath, imp.module) {
			continue
		}
		if has(pf.RelPath, imp.name) {
			return pf.RelPath
		}
	}
	return ""
}

// relativeImportCandidates turns a relative module (".mod", "..pkg.mod", ".")
// into the ordered file paths it could denote, relative to the repo root. The
// caller only accepts a candidate present in the scan set.
func relativeImportCandidates(importer, module string) []string {
	dots := len(module) - len(strings.TrimLeft(module, "."))
	rest := strings.ReplaceAll(module[dots:], ".", "/")
	dir := path.Dir(importer)
	for i := 1; i < dots; i++ {
		if dir == "." || dir == "" {
			return nil // would climb above the repo root
		}
		dir = path.Dir(dir)
	}
	if dir == "." {
		dir = ""
	}
	if rest == "" {
		return []string{path.Join(dir, "__init__.py")}
	}
	return []string{path.Join(dir, rest+".py"), path.Join(dir, rest, "__init__.py")}
}
