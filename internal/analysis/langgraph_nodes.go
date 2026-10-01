package analysis

import (
	sitter "github.com/smacker/go-tree-sitter"

	"github.com/trustabl/trustabl/internal/analysis/astutil"
	"github.com/trustabl/trustabl/internal/models"
)

// LangGraph node discovery.
//
// A function registered on a graph builder — <builder>.add_node("name", func) or
// add_node(func) — is not a @tool, so none of the tool-discovery passes see it,
// yet it runs automatically on every graph visit with no tool-call boundary.
// This pass surfaces each such function as a ToolDef of Kind
// KindLangGraphNode, pointing at the function_definition so the existing
// tool-scope body predicates (has_shell_call, has_write_call,
// has_dynamic_url_call, has_code_exec_call, has_body_text) resolve its body
// through the normal FindFunctionNode lookup.
//
// Known v1 limits: only a bare identifier naming a same-file, undecorated,
// top-level function is resolved. Lambdas, methods, imported callables,
// decorated functions and compiled subgraphs are skipped. Discovery is
// import-gated to the langchain / langgraph ecosystem.

// DiscoverLangGraphNodes emits one ToolDef per distinct same-file function
// registered through .add_node(...) in a langchain / langgraph-importing file.
func DiscoverLangGraphNodes(files []ParsedFile) []models.ToolDef {
	var out []models.ToolDef
	for _, pf := range files {
		if !fileImportsLangChain(pf) {
			continue
		}
		out = append(out, discoverLangGraphNodesInFile(pf)...)
	}
	return out
}

func discoverLangGraphNodesInFile(pf ParsedFile) []models.ToolDef {
	funcs := indexTopLevelFunctions(pf.Tree.RootNode(), pf.Source)
	if len(funcs) == 0 {
		return nil
	}
	var out []models.ToolDef
	seen := map[string]bool{}
	astutil.Walk(pf.Tree.RootNode(), func(n *sitter.Node) bool {
		if n.Type() != "call" {
			return true
		}
		fn := n.ChildByFieldName("function")
		if fn == nil || fn.Type() != "attribute" ||
			astutil.NodeText(fn.ChildByFieldName("attribute"), pf.Source) != "add_node" {
			return true
		}
		// add_node("name", func): callable is arg1. add_node(func): callable is arg0.
		callable := positionalArgNode(n, 1)
		if callable == nil {
			callable = positionalArgNode(n, 0)
		}
		if callable == nil || callable.Type() != "identifier" {
			return true
		}
		name := astutil.NodeText(callable, pf.Source)
		def := funcs[name]
		if def == nil || seen[name] {
			return true
		}
		seen[name] = true
		out = append(out, models.ToolDef{
			Name:     name,
			Kind:     models.KindLangGraphNode,
			Language: models.LanguagePython,
			Location: models.Location{
				FilePath: pf.RelPath,
				Line:     int(def.StartPoint().Row) + 1,
				EndLine:  int(def.EndPoint().Row) + 1,
			},
		})
		return true
	})
	return out
}
