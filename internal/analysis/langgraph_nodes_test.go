package analysis_test

import (
	"testing"

	"github.com/trustabl/trustabl/internal/analysis"
	"github.com/trustabl/trustabl/internal/models"
)

func nodeNames(tools []models.ToolDef) map[string]bool {
	m := map[string]bool{}
	for _, tl := range tools {
		if tl.Kind != models.KindLangGraphNode {
			continue
		}
		m[tl.Name] = true
	}
	return m
}

func TestDiscoverLangGraphNodes_BothCallShapes(t *testing.T) {
	src := `from langgraph.graph import StateGraph

def fetch(state):
    return state

def deploy(state):
    return state

builder = StateGraph(dict)
builder.add_node("fetch", fetch)
builder.add_node(deploy)
builder.add_node("again", fetch)
`
	tools := analysis.DiscoverLangGraphNodes([]analysis.ParsedFile{parsePyFile(t, "g.py", src)})
	got := nodeNames(tools)
	if len(tools) != 2 || !got["fetch"] || !got["deploy"] {
		t.Fatalf("want exactly fetch+deploy (deduped), got %+v", tools)
	}
	for _, tl := range tools {
		if tl.Line == 0 || tl.FilePath != "g.py" || tl.Language != models.LanguagePython {
			t.Errorf("bad location/language: %+v", tl)
		}
	}
}

func TestDiscoverLangGraphNodes_SkipsUnresolvable(t *testing.T) {
	src := `from langgraph.graph import StateGraph
from elsewhere import imported_node

class N:
    def run(self, state):
        return state

builder = StateGraph(dict)
builder.add_node("a", lambda s: s)
builder.add_node("b", imported_node)
builder.add_node("c", N().run)
`
	if tools := analysis.DiscoverLangGraphNodes([]analysis.ParsedFile{parsePyFile(t, "g.py", src)}); len(tools) != 0 {
		t.Fatalf("lambdas/imports/methods must be skipped, got %+v", tools)
	}
}

func TestDiscoverLangGraphNodes_RequiresLangChainImport(t *testing.T) {
	src := `def f(x):
    return x

class Registry:
    def add_node(self, *a): ...

r = Registry()
r.add_node("f", f)
`
	if tools := analysis.DiscoverLangGraphNodes([]analysis.ParsedFile{parsePyFile(t, "g.py", src)}); len(tools) != 0 {
		t.Fatalf("a file with no langgraph import must not yield nodes, got %+v", tools)
	}
}

// ─── LC-114 compile-link behaviour ──────────────────────────────────────────

func graphAgent(t *testing.T, src string) models.AgentDef {
	t.Helper()
	agents := analysis.DiscoverLangGraphGraphs([]analysis.ParsedFile{parsePyFile(t, "g.py", src)})
	if len(agents) == 0 {
		t.Fatal("no StateGraph discovered")
	}
	return agents[0]
}

func TestDiscoverLangGraphGraphs_BareCompileIsObserved(t *testing.T) {
	a := graphAgent(t, `from langgraph.graph import StateGraph
builder = StateGraph(dict)
app = builder.compile()
`)
	if a.Kwargs == nil || a.Opaque || len(a.Kwargs.Children) != 0 {
		t.Fatalf("bare compile() should yield an empty non-nil Kwargs tree, got %+v opaque=%v", a.Kwargs, a.Opaque)
	}
}

func TestDiscoverLangGraphGraphs_ChainedCompileIsUnobserved(t *testing.T) {
	a := graphAgent(t, `from langgraph.graph import StateGraph
app = StateGraph(dict).compile()
`)
	if a.Kwargs != nil {
		t.Fatalf("chained compile() is never linked; Kwargs must stay nil, got %+v", a.Kwargs)
	}
}

func TestDiscoverLangGraphGraphs_OpaqueCompile(t *testing.T) {
	a := graphAgent(t, `from langgraph.graph import StateGraph
builder = StateGraph(dict)
app = builder.compile(**cfg)
`)
	if !a.Opaque || a.Kwargs == nil {
		t.Fatalf("compile(**cfg) should be opaque, got %+v opaque=%v", a.Kwargs, a.Opaque)
	}
}

func TestDiscoverLangGraphGraphs_SubgraphIsUnobserved(t *testing.T) {
	agents := analysis.DiscoverLangGraphGraphs([]analysis.ParsedFile{parsePyFile(t, "g.py", `from langgraph.graph import StateGraph
sub_builder = StateGraph(dict)
sub = sub_builder.compile()

parent = StateGraph(dict)
parent.add_node("sub", sub)
app = parent.compile()
`)})
	if len(agents) != 2 {
		t.Fatalf("want 2 graphs, got %d", len(agents))
	}
	byVar := map[string]models.AgentDef{}
	for _, a := range agents {
		byVar[a.VarName] = a
	}
	if byVar["sub_builder"].Kwargs != nil {
		t.Errorf("a subgraph (compiled result passed to add_node) must stay unobserved, got %+v", byVar["sub_builder"].Kwargs)
	}
	if byVar["parent"].Kwargs == nil {
		t.Error("the parent graph is a resolved compile() and must be observed")
	}
}

func TestDiscoverLangGraphGraphs_CheckpointerKwargCaptured(t *testing.T) {
	a := graphAgent(t, `from langgraph.graph import StateGraph
builder = StateGraph(dict)
app = builder.compile(checkpointer=saver)
`)
	if a.Kwargs == nil || a.Kwargs.Children["checkpointer"] == nil {
		t.Fatalf("checkpointer kwarg not captured: %+v", a.Kwargs)
	}
}
