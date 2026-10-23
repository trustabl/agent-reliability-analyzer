package ingestion

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/trustabl/trustabl/internal/models"
)

func TestShouldSkipDir_WalksHarnessDirs(t *testing.T) {
	for _, name := range []string{".agents", ".opencode", ".claude", ".codex"} {
		if shouldSkipDir(name) {
			t.Errorf("%s must be walked", name)
		}
	}
	// Other dot-directories and caches stay skipped.
	for _, name := range []string{".git", ".idea", ".cache", "node_modules"} {
		if !shouldSkipDir(name) {
			t.Errorf("%s must stay skipped", name)
		}
	}
}

// Codex and OpenCode harness files are inventoried. A skill under
// .agents/skills/ is found exactly like one under skills/, and files that only
// look similar (a bare plugin/ dir, anything under node_modules) are not labelled.
func TestNormalize_HarnessFilesClassified(t *testing.T) {
	dir := t.TempDir()
	for rel, body := range map[string]string{
		".agents/skills/fetcher/SKILL.md":   "---\nname: fetcher\n---\n",
		"skills/fetcher/SKILL.md":           "---\nname: fetcher\n---\n",
		".codex/hooks.json":                 `{"hooks":{}}`,
		"app/.codex/hooks.json":             `{"hooks":{}}`,
		".opencode/plugin/guard.ts":         "export default {}\n",
		".opencode/plugins/guard.js":        "export default {}\n",
		".opencode/agent/reviewer.md":       "# reviewer\n",
		".opencode/agents/helper.md":        "# helper\n",
		"plugin/other.ts":                   "export default {}\n",
		".opencode/notes/readme.md":         "# notes\n",
		".opencode/node_modules/x/SKILL.md": "# skipped\n",
		".idea/SKILL.md":                    "# skipped\n",
	} {
		full := filepath.Join(dir, rel)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	m, err := Normalize(&Source{RootPath: dir}, nil)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]models.ComponentKind{}
	for _, c := range m.Components {
		got[c.Path] = c.Kind
	}
	want := map[string]models.ComponentKind{
		".agents/skills/fetcher/SKILL.md": models.ComponentSkill,
		"skills/fetcher/SKILL.md":         models.ComponentSkill,
		".codex/hooks.json":               models.ComponentCodexHooks,
		"app/.codex/hooks.json":           models.ComponentCodexHooks,
		".opencode/plugin/guard.ts":       models.ComponentOpencodePlugin,
		".opencode/plugins/guard.js":      models.ComponentOpencodePlugin,
		".opencode/agent/reviewer.md":     models.ComponentOpencodeAgent,
		".opencode/agents/helper.md":      models.ComponentOpencodeAgent,
	}
	for p, k := range want {
		if got[p] != k {
			t.Errorf("%s: got kind %q, want %q", p, got[p], k)
		}
	}
	for _, p := range []string{
		"plugin/other.ts", ".opencode/notes/readme.md",
		".opencode/node_modules/x/SKILL.md", ".idea/SKILL.md",
	} {
		if k, ok := got[p]; ok {
			t.Errorf("%s must not be classified, got %q", p, k)
		}
	}
}

func TestInOpencodeDir(t *testing.T) {
	cases := []struct {
		path string
		want bool
	}{
		{".opencode/plugin/a.ts", true},
		{"pkg/.opencode/plugins/a.js", true},
		{".opencode/plugin", false}, // a file named plugin, nothing under it
		{"plugin/a.ts", false},
		{".opencode/other/a.ts", false},
	}
	for _, c := range cases {
		if got := inOpencodeDir(c.path, "plugin", "plugins"); got != c.want {
			t.Errorf("inOpencodeDir(%q) = %v, want %v", c.path, got, c.want)
		}
	}
}
