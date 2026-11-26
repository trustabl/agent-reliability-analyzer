package scanner_test

import (
	"os"
	"path/filepath"
	"testing"
)

// Static batch 5 over the corpus fixture: each rule fires once, at the first
// non-test site, and the pinned judge, the test-path judge, the forbidding
// sentence and the declared installs do not add findings.
func TestScan_StaticBatch5Fixture(t *testing.T) {
	files := map[string]string{}
	root := "../../testdata/corpus/static-batch5-fixture"
	err := filepath.Walk(root, func(p string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() || filepath.Base(p) == "SOURCE.md" {
			return err
		}
		rel, _ := filepath.Rel(root, p)
		b, rerr := os.ReadFile(p)
		files[filepath.ToSlash(rel)] = string(b)
		return rerr
	})
	if err != nil {
		t.Fatal(err)
	}
	res := scanFiles(t, files)
	want := map[string]string{"AGCFG-022": "AGENTS.md", "AGCFG-023": "AGENTS.md", "OBS-015": "app.py", "OBS-016": ""}
	for id, file := range want {
		got := findingsFor(res, id)
		if len(got) != 1 || got[0] != file {
			t.Errorf("%s: got %v, want one finding at %q", id, got, file)
		}
	}
}

// A repo that writes gen_ai.response.model and an identifier to a span clears
// OBS-015 and OBS-016.
func TestScan_StaticBatch5SpanWritesClearTraceRules(t *testing.T) {
	files := map[string]string{
		"requirements.txt": "openai-agents==0.3.0\nopentelemetry-sdk==1.27.0\n",
		"app.py": `import subprocess
from agents import Agent, function_tool
from opentelemetry import trace
from opentelemetry.sdk.trace import TracerProvider

trace.set_tracer_provider(TracerProvider())


@function_tool
def run_shell(command: str) -> str:
    """Run a shell command."""
    return subprocess.run(command, shell=True, capture_output=True, text=True).stdout


agent = Agent(name="ops", instructions="x", tools=[run_shell])


def judge_answer(client, span, text):
    resp = client.chat.completions.create(model="gpt-4o-latest", messages=[])
    span.set_attribute("gen_ai.response.model", resp.model)
    span.set_attribute("gen_ai.tool.call.id", "c1")
    return resp
`,
	}
	res := scanFiles(t, files)
	for _, id := range []string{"OBS-015", "OBS-016"} {
		if got := findingsFor(res, id); len(got) != 0 {
			t.Errorf("%s: got %v, want none", id, got)
		}
	}
}
