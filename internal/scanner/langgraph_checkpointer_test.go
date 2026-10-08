package scanner_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/trustabl/trustabl/internal/scanner"
)

const lc116Graph = `from langgraph.graph import StateGraph
from langgraph.checkpoint.memory import MemorySaver

builder = StateGraph(dict)
graph = builder.compile(checkpointer=MemorySaver())
`

const lc116API = `from fastapi import FastAPI
from graph import graph

api = FastAPI()


@api.post("/chat")
def chat(q: str):
    return graph.invoke({"q": q}, config={"configurable": {"thread_id": q}})
`

// lc116Scan writes files into a temp repo, scans it with the fixture rules and
// returns the LC-116 findings' file paths.
func lc116Scan(t *testing.T, files map[string]string) []string {
	t.Helper()
	dir := t.TempDir()
	files["pyproject.toml"] = "[project]\nname = \"f\"\ndependencies = [\"langgraph\", \"fastapi\"]\n"
	for rel, content := range files {
		p := filepath.Join(dir, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	res, err := scanner.Run(scanner.Config{Target: dir, RulesFS: rulesFixture(t)})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	var got []string
	for _, f := range res.Findings {
		if f.RuleID == "LC-116" {
			got = append(got, f.FilePath)
		}
	}
	return got
}

// TestScanRun_LC116 runs the whole pipeline: the graph compiled with MemorySaver
// in graph.py, run from a FastAPI route in api.py.
func TestScanRun_LC116(t *testing.T) {
	t.Run("fires across files, attributed to the graph", func(t *testing.T) {
		got := lc116Scan(t, map[string]string{"graph.py": lc116Graph, "api.py": lc116API})
		if len(got) != 1 || got[0] != "graph.py" {
			t.Fatalf("LC-116 findings = %v, want [graph.py]", got)
		}
	})
	t.Run("silent with langgraph.json", func(t *testing.T) {
		got := lc116Scan(t, map[string]string{"graph.py": lc116Graph, "api.py": lc116API, "langgraph.json": `{"graphs": {"g": "./graph.py:graph"}}`})
		if len(got) != 0 {
			t.Fatalf("LC-116 fired on a LangGraph Platform repo: %v", got)
		}
	})
	t.Run("silent for a FastAPI test app under tests/", func(t *testing.T) {
		got := lc116Scan(t, map[string]string{"graph.py": lc116Graph, "tests/test_api.py": lc116API})
		if len(got) != 0 {
			t.Fatalf("LC-116 fired on a test app: %v", got)
		}
	})
	t.Run("silent for a durable saver", func(t *testing.T) {
		durable := `from langgraph.graph import StateGraph
from langgraph.checkpoint.postgres import PostgresSaver

builder = StateGraph(dict)
graph = builder.compile(checkpointer=PostgresSaver(conn))
`
		got := lc116Scan(t, map[string]string{"graph.py": durable, "api.py": lc116API})
		if len(got) != 0 {
			t.Fatalf("LC-116 fired on PostgresSaver: %v", got)
		}
	})
}
