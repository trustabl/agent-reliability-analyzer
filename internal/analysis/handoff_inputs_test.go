package analysis_test

import (
	"testing"

	"github.com/trustabl/trustabl/internal/analysis"
	"github.com/trustabl/trustabl/internal/models"
)

func pyAgentsFromSrc(t *testing.T, src string) []models.AgentDef {
	t.Helper()
	pf := parsePyFile(t, "agents.py", src)
	files := []analysis.ParsedFile{pf}
	inv := models.RepoInventory{Agents: analysis.DiscoverAgents(files)}
	analysis.ResolveEdges(&inv, files)
	return inv.Agents
}

func TestHandoffInputs_Python(t *testing.T) {
	src := "from agents import Agent, handoff\nfrom agents.extensions import handoff_filters\n" +
		"a = Agent(name='a')\nb = Agent(name='b')\nc = Agent(name='c')\nd = Agent(name='d')\n" +
		"top = Agent(name='top', handoffs=[a, handoff(b, input_filter=handoff_filters.remove_all_tools), handoff(c), handoff(d, input_filter=None), other.agent])\n"
	var top models.AgentDef
	for _, a := range pyAgentsFromSrc(t, src) {
		if a.Name == "top" {
			top = a
		}
	}
	got := top.HandoffInputs
	want := []models.HandoffInput{{Target: "a"}, {Target: "b", HasInputFilter: true}, {Target: "c"}, {Target: "d"}}
	if len(got) != len(want) {
		t.Fatalf("got %+v, want %+v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("entry %d: got %+v, want %+v", i, got[i], want[i])
		}
	}
}

func TestHandoffInputs_TypeScript(t *testing.T) {
	src := "import { Agent, handoff } from \"@openai/agents\";\n" +
		"const a = new Agent({ name: 'a' });\nconst b = new Agent({ name: 'b' });\nconst c = new Agent({ name: 'c' });\n" +
		"const top = new Agent({ name: 'top', handoffs: [a, handoff(b, { inputFilter: removeAllTools }), handoff(c, {})] });\n"
	pf := parseTSForTest(t, "agents.ts", src)
	var top models.AgentDef
	for _, a := range analysis.DiscoverTSOpenAIAgents([]analysis.ParsedFile{pf}, nil) {
		if a.Name == "top" {
			top = a
		}
	}
	got := top.HandoffInputs
	want := []models.HandoffInput{{Target: "a"}, {Target: "b", HasInputFilter: true}, {Target: "c"}}
	if len(got) != len(want) {
		t.Fatalf("got %+v, want %+v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("entry %d: got %+v, want %+v", i, got[i], want[i])
		}
	}
}

func TestHandoffInputFilterConfigured(t *testing.T) {
	cases := []struct {
		name, path, src string
		ts, want        bool
	}{
		{"python run config", "r.py", "cfg = RunConfig(handoff_input_filter=keep_all)\n", false, true},
		{"python comment", "r.py", "# RunConfig(handoff_input_filter=keep_all)\n", false, false},
		{"python string", "r.py", "s = 'handoff_input_filter'\n", false, false},
		{"python test path", "tests/r.py", "cfg = RunConfig(handoff_input_filter=keep_all)\n", false, false},
		{"python per-handoff kwarg is not run level", "r.py", "h = handoff(a, input_filter=f)\n", false, false},
		{"ts pair", "r.ts", "const r = new Runner({ handoffInputFilter: keepAll });\n", true, true},
		{"ts shorthand", "r.ts", "const handoffInputFilter = f;\nnew Runner({ handoffInputFilter });\n", true, true},
		{"ts comment", "r.ts", "// new Runner({ handoffInputFilter: keepAll });\n", true, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			var py, ts []analysis.ParsedFile
			if c.ts {
				ts = []analysis.ParsedFile{parseTSForTest(t, c.path, c.src)}
			} else {
				py = []analysis.ParsedFile{parsePyFile(t, c.path, c.src)}
			}
			if got := analysis.DiscoverHandoffInputFilterConfigured(py, ts); got != c.want {
				t.Errorf("got %v, want %v", got, c.want)
			}
		})
	}
}
