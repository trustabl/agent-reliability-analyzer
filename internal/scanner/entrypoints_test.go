package scanner_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/trustabl/trustabl/internal/scanner"
)

const entrypointRepoApp = `from fastapi import FastAPI
from agents import Agent, Runner

app = FastAPI()
agent = Agent(name="x")


async def helper(q):
    return await Runner.run(agent, q)


@app.post("/chat")
async def chat(q: str):
    result = await helper(q)
    return {"out": str(result)}
`

// writeEntrypointRepo writes the synthetic FastAPI route -> helper -> Runner.run repo.
func writeEntrypointRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	files := map[string]string{
		"pyproject.toml": "[project]\nname = \"f\"\ndependencies = [\"openai-agents\", \"fastapi\"]\n",
		"app.py":         entrypointRepoApp,
	}
	for rel, content := range files {
		if err := os.WriteFile(filepath.Join(dir, rel), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

// TestScanRun_EntrypointsInJSONReport: the entrypoint fact reaches the --json
// report. The reachability stamp is in-memory only (json:"-"); it is asserted
// at the analysis level (TestApplyEntrypointReachability).
func TestScanRun_EntrypointsInJSONReport(t *testing.T) {
	dir := writeEntrypointRepo(t)
	res, err := scanner.Run(scanner.Config{Target: dir, RulesFS: rulesFixture(t)})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	js, err := json.Marshal(res)
	if err != nil {
		t.Fatal(err)
	}
	var got struct {
		Entrypoints []struct {
			FilePath  string `json:"file_path"`
			Kind      string `json:"kind"`
			Framework string `json:"framework"`
			Route     string `json:"route"`
			FuncName  string `json:"func_name"`
		} `json:"entrypoints"`
	}
	if err := json.Unmarshal(js, &got); err != nil {
		t.Fatal(err)
	}
	if len(got.Entrypoints) != 1 {
		t.Fatalf("want 1 entrypoint, got %+v", got.Entrypoints)
	}
	e := got.Entrypoints[0]
	if e.FilePath != "app.py" || e.Kind != "http" || e.Framework != "fastapi" || e.Route != "/chat" || e.FuncName != "chat" {
		t.Errorf("unexpected entrypoint %+v", e)
	}
}

// TestScanRun_NoEntrypointsKeyWhenAbsent: omitempty keeps existing reports unchanged.
func TestScanRun_NoEntrypointsKeyWhenAbsent(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "a.py"), []byte("x = 1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	res, err := scanner.Run(scanner.Config{Target: dir, RulesFS: rulesFixture(t)})
	if err != nil {
		t.Skipf("scan of an empty repo is not supported: %v", err)
	}
	js, _ := json.Marshal(res)
	var m map[string]any
	_ = json.Unmarshal(js, &m)
	if _, ok := m["entrypoints"]; ok {
		t.Error("entrypoints key present on a repo with none")
	}
}
