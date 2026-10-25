package ingestion

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/trustabl/trustabl/internal/models"
)

func writeTree(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	for rel, body := range files {
		full := filepath.Join(dir, rel)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func kindsByPath(t *testing.T, dir string) map[string]models.ComponentKind {
	t.Helper()
	m, err := Normalize(&Source{RootPath: dir}, nil)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]models.ComponentKind{}
	for _, c := range m.Components {
		got[c.Path] = c.Kind
	}
	return got
}

// The layout is the one a Harness-style adaptation run stages for the OpenCode
// executor: root-level guidance files and component folders that hold only a
// README stub until something is added.
func TestNormalize_StagedOpencodeHarnessTree(t *testing.T) {
	dir := writeTree(t, map[string]string{
		"systemprompt.md":                    "Reconcile the ledger.\n",
		"guardrails.md":                      "",
		"LongTermMEMORY.md":                  "",
		"tool_descriptions/tool_guidance.md": "",
		"scripts/summarize.py":               "print('x')\n",
		"scripts/README.md":                  "# scripts\n",
		"plugin/README.md":                   "# plugins\n",
		"plugin/guard.ts":                    "export default {}\n",
		"skills/README.md":                   "# skills\n",
		"sub_agents/README.md":               "# subagents\n",
		"sub_agents/reviewer/AGENT.md":       "---\nmode: subagent\n---\n",
	})
	got := kindsByPath(t, dir)
	want := map[string]models.ComponentKind{
		"systemprompt.md":                    models.ComponentHarnessGuidance,
		"guardrails.md":                      models.ComponentHarnessGuidance,
		"LongTermMEMORY.md":                  models.ComponentHarnessGuidance,
		"tool_descriptions/tool_guidance.md": models.ComponentHarnessGuidance,
		"plugin/guard.ts":                    models.ComponentOpencodePlugin,
		"sub_agents/reviewer/AGENT.md":       models.ComponentOpencodeAgent,
	}
	for p, k := range want {
		if got[p] != k {
			t.Errorf("%s: got kind %q, want %q", p, got[p], k)
		}
	}
	// README stubs and the helper script are not harness guidance components.
	for _, p := range []string{"scripts/README.md", "plugin/README.md", "skills/README.md", "sub_agents/README.md"} {
		if k, ok := got[p]; ok {
			t.Errorf("%s must not be classified, got %q", p, k)
		}
	}
}

func TestNormalize_StagedHarnessNeedsTheSignature(t *testing.T) {
	for name, files := range map[string]map[string]string{
		"guardrails.md alone":    {"guardrails.md": "x\n", "plugin/a.ts": "export default {}\n"},
		"systemprompt.md alone":  {"systemprompt.md": "x\n", "plugin/a.ts": "export default {}\n"},
		"the pair in two places": {"a/systemprompt.md": "x\n", "b/guardrails.md": "x\n"},
		"a bare plugin dir":      {"plugin/a.ts": "export default {}\n", "sub_agents/x/AGENT.md": "x\n"},
	} {
		got := kindsByPath(t, writeTree(t, files))
		for p, k := range got {
			switch k {
			case models.ComponentHarnessGuidance, models.ComponentOpencodePlugin:
				t.Errorf("%s: %s must not be classified, got %q", name, p, k)
			case models.ComponentOpencodeAgent:
				t.Errorf("%s: %s must not be classified, got %q", name, p, k)
			}
		}
	}
}

// A staged tree nested in a larger repository is recognised with its prefix,
// and only the files under that root are labelled.
func TestNormalize_StagedHarnessNestedRoot(t *testing.T) {
	dir := writeTree(t, map[string]string{
		"library/k1/harness/systemprompt.md": "x\n",
		"library/k1/harness/guardrails.md":   "x\n",
		"library/k1/harness/plugin/p.ts":     "export default {}\n",
		"library/other/plugin/q.ts":          "export default {}\n",
		"README.md":                          "# repo\n",
	})
	got := kindsByPath(t, dir)
	if got["library/k1/harness/systemprompt.md"] != models.ComponentHarnessGuidance {
		t.Errorf("nested systemprompt.md not labelled: %v", got)
	}
	if got["library/k1/harness/plugin/p.ts"] != models.ComponentOpencodePlugin {
		t.Errorf("nested plugin not labelled: %v", got)
	}
	if _, ok := got["library/other/plugin/q.ts"]; ok {
		t.Error("a plugin outside the staged root must not be labelled")
	}
}

func TestStagedHarnessRootsAreSortedAndDeepestWins(t *testing.T) {
	md := []string{
		"systemprompt.md", "guardrails.md",
		"deep/er/systemprompt.md", "deep/er/guardrails.md",
	}
	roots := stagedHarnessRoots(md)
	if len(roots) != 2 || roots[0] != "" || roots[1] != "deep/er/" {
		t.Fatalf("roots = %q, want [\"\" \"deep/er/\"]", roots)
	}
	rest, ok := relativeToHarnessRoot("deep/er/plugin/a.ts", roots)
	if !ok || rest != "plugin/a.ts" {
		t.Errorf("relative = %q %v, want plugin/a.ts relative to the deepest root", rest, ok)
	}
}
