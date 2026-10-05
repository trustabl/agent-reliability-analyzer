package scanner_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/trustabl/trustabl/internal/scanner"
)

// TestScanRun_PYD107_GlobalInstrumentationCredit is the end-to-end contract for
// the global-instrumentor credit: a Pydantic AI agent with no instrument= in a
// repo that wires observability fires PYD-107, and goes silent once the repo
// makes a process-wide instrumentation call — which is exactly what PYD-107's
// own fix text recommends. instrument=True stays silent (regression).
func TestScanRun_PYD107_GlobalInstrumentationCredit(t *testing.T) {
	const agentSrc = `
from pydantic_ai import Agent

agent = Agent("openai:gpt-4o")
`
	cases := []struct {
		name     string
		files    map[string]string
		wantFire bool
	}{
		{
			name: "fires with logfire.configure only",
			files: map[string]string{
				"agent.py": agentSrc,
				"obs.py":   "import logfire\n\nlogfire.configure()\n",
			},
			wantFire: true,
		},
		{
			name: "silent with logfire.instrument_pydantic_ai()",
			files: map[string]string{
				"agent.py": agentSrc,
				"obs.py":   "import logfire\n\nlogfire.configure()\nlogfire.instrument_pydantic_ai()\n",
			},
		},
		{
			name: "silent with Agent.instrument_all()",
			files: map[string]string{
				"agent.py": agentSrc,
				"obs.py":   "import logfire\nfrom pydantic_ai import Agent\n\nlogfire.configure()\nAgent.instrument_all()\n",
			},
		},
		{
			name: "silent with instrument=True",
			files: map[string]string{
				"agent.py": "from pydantic_ai import Agent\n\nagent = Agent(\"openai:gpt-4o\", instrument=True)\n",
				"obs.py":   "import logfire\n\nlogfire.configure()\n",
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			tc.files["pyproject.toml"] = "[project]\nname = \"f\"\ndependencies = [\"pydantic-ai\", \"logfire\"]\n"
			for rel, content := range tc.files {
				if err := os.WriteFile(filepath.Join(dir, rel), []byte(content), 0644); err != nil {
					t.Fatal(err)
				}
			}
			res, err := scanner.Run(scanner.Config{Target: dir, RulesFS: rulesFixture(t)})
			if err != nil {
				t.Fatalf("Run: %v", err)
			}
			var fired bool
			for _, f := range res.Findings {
				if f.RuleID == "PYD-107" {
					fired = true
				}
			}
			if fired != tc.wantFire {
				t.Errorf("PYD-107 fired=%v, want %v; findings=%+v", fired, tc.wantFire, res.Findings)
			}
		})
	}
}
