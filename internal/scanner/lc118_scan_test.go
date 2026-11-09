package scanner_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/trustabl/trustabl/internal/models"
	"github.com/trustabl/trustabl/internal/scanner"
)

// TestScan_LC118_DeepAgentHostShell runs real .py source through the full
// scanner, so a discovery change that stops producing the Deep Agent shape
// LC-118 reads fails here instead of silently killing the rule.
func TestScan_LC118_DeepAgentHostShell(t *testing.T) {
	const header = "from deepagents import create_deep_agent\nfrom deepagents.backends import LocalShellBackend\n\n"
	scan := func(t *testing.T, files map[string]string) models.ScanResult {
		t.Helper()
		dir := t.TempDir()
		files["pyproject.toml"] = "[project]\nname = \"f\"\ndependencies = [\"deepagents\"]\n"
		for rel, body := range files {
			full := filepath.Join(dir, rel)
			if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(full, []byte(body), 0o644); err != nil {
				t.Fatal(err)
			}
		}
		res, err := scanner.Run(scanner.Config{Target: dir, RulesFS: rulesFixture(t)})
		if err != nil {
			t.Fatalf("scanner.Run: %v", err)
		}
		return res
	}
	lc118 := func(res models.ScanResult) []models.Finding {
		var out []models.Finding
		for _, f := range res.Findings {
			if f.RuleID == "LC-118" {
				out = append(out, f)
			}
		}
		return out
	}

	t.Run("fires once on a host shell with no interrupt_on", func(t *testing.T) {
		res := scan(t, map[string]string{"agent.py": header +
			"agent = create_deep_agent(\n    model=m,\n    backend=LocalShellBackend(root_dir=\"/w\"),\n)\n"})
		got := lc118(res)
		if len(got) != 1 {
			t.Fatalf("got %d LC-118 findings, want 1: %v", len(got), res.Findings)
		}
		f := got[0]
		if f.FilePath != "agent.py" || f.StartLine != 4 || f.Severity != models.SeverityHigh || f.Confidence != 0.6 {
			t.Errorf("finding: file=%q line=%d sev=%v conf=%v", f.FilePath, f.StartLine, f.Severity, f.Confidence)
		}
	})

	t.Run("one finding for the main agent when subagents are listed", func(t *testing.T) {
		res := scan(t, map[string]string{"agent.py": header +
			"agent = create_deep_agent(\n    backend=LocalShellBackend(),\n    subagents=[{\"name\": \"a\", \"description\": \"d\"}],\n)\n"})
		if got := lc118(res); len(got) != 1 {
			t.Fatalf("got %d LC-118 findings, want 1", len(got))
		}
	})

	t.Run("silent when interrupt_on gates execute", func(t *testing.T) {
		res := scan(t, map[string]string{"agent.py": header +
			"agent = create_deep_agent(backend=LocalShellBackend(), interrupt_on={\"execute\": True})\n"})
		if got := lc118(res); len(got) != 0 {
			t.Errorf("got %d LC-118 findings, want 0", len(got))
		}
	})

	t.Run("silent when a subagent dict inherits a gated parent", func(t *testing.T) {
		res := scan(t, map[string]string{"agent.py": header +
			"agent = create_deep_agent(\n    backend=LocalShellBackend(),\n    interrupt_on={\"execute\": True},\n    subagents=[{\"name\": \"a\", \"description\": \"d\"}],\n)\n"})
		if got := lc118(res); len(got) != 0 {
			t.Errorf("got %d LC-118 findings, want 0", len(got))
		}
	})

	t.Run("silent for a subagent override under a gated parent (documented gap)", func(t *testing.T) {
		res := scan(t, map[string]string{"agent.py": header +
			"agent = create_deep_agent(\n    backend=LocalShellBackend(),\n    interrupt_on={\"execute\": True},\n" +
			"    subagents=[{\"name\": \"a\", \"description\": \"d\", \"interrupt_on\": {\"write_file\": True}}],\n)\n"})
		if got := lc118(res); len(got) != 0 {
			t.Errorf("got %d LC-118 findings, want 0 (override case is a documented gap)", len(got))
		}
	})

	t.Run("silent for a sandbox backend", func(t *testing.T) {
		res := scan(t, map[string]string{"agent.py": "from deepagents import create_deep_agent\nfrom deepagents.backends import StateBackend\n\n" +
			"agent = create_deep_agent(backend=StateBackend())\n"})
		if got := lc118(res); len(got) != 0 {
			t.Errorf("got %d LC-118 findings, want 0", len(got))
		}
	})

	t.Run("silent for a comment and for a string", func(t *testing.T) {
		res := scan(t, map[string]string{"agent.py": header +
			"# agent = create_deep_agent(backend=LocalShellBackend())\nDOC = \"create_deep_agent(backend=LocalShellBackend())\"\n"})
		if got := lc118(res); len(got) != 0 {
			t.Errorf("got %d LC-118 findings, want 0", len(got))
		}
	})

	t.Run("async_create_deep_agent is not matched", func(t *testing.T) {
		res := scan(t, map[string]string{"agent.py": header +
			"agent = async_create_deep_agent(backend=LocalShellBackend())\n"})
		if got := lc118(res); len(got) != 0 {
			t.Errorf("got %d LC-118 findings, want 0", len(got))
		}
	})

	t.Run("test path: reported but marked test origin and not scored", func(t *testing.T) {
		res := scan(t, map[string]string{"tests/test_agent.py": header +
			"agent = create_deep_agent(backend=LocalShellBackend())\n"})
		got := lc118(res)
		if len(got) != 1 {
			t.Fatalf("got %d LC-118 findings, want 1 (reported, not dropped)", len(got))
		}
		if got[0].Origin != models.OriginTest {
			t.Errorf("Origin: got %q, want %q", got[0].Origin, models.OriginTest)
		}
	})
}
