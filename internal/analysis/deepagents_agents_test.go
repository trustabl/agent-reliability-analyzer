package analysis_test

import (
	"testing"

	"github.com/trustabl/trustabl/internal/analysis"
	"github.com/trustabl/trustabl/internal/models"
)

const deepHeader = `from deepagents import create_deep_agent
from deepagents.backends import LocalShellBackend, CompositeBackend, StateBackend
`

// deepFacts runs discovery on src and returns the single agent's two LC-118 facts.
func deepFacts(t *testing.T, src string) (shape, gate string) {
	t.Helper()
	pf := parsePyFile(t, "agent.py", src)
	agents := analysis.DiscoverDeepAgents([]analysis.ParsedFile{pf})
	if len(agents) != 1 {
		t.Fatalf("got %d agents, want 1", len(agents))
	}
	a := agents[0]
	if a.SDK != models.SDKLangChain || a.Class != "DeepAgent" || a.Language != models.LanguagePython {
		t.Errorf("identity: got %s/%s/%s", a.SDK, a.Class, a.Language)
	}
	return a.DeepShellBackend, a.DeepExecuteGate
}

func TestDeepAgents_BackendShapes(t *testing.T) {
	cases := []struct {
		name, src, shape string
	}{
		{"direct", deepHeader + "a = create_deep_agent(model=m, backend=LocalShellBackend(root_dir='/w'))\n", "direct"},
		{"variable", deepHeader + "b = LocalShellBackend(root_dir='/w')\na = create_deep_agent(model=m, backend=b)\n", "direct"},
		{"variable in function", deepHeader + "def build():\n    b = LocalShellBackend()\n    return create_deep_agent(model=m, backend=b)\n", "direct"},
		{"module variable read in function", deepHeader + "b = LocalShellBackend()\ndef build():\n    return create_deep_agent(model=m, backend=b)\n", "direct"},
		{"composite default", deepHeader + "a = create_deep_agent(backend=CompositeBackend(default=LocalShellBackend(), routes={}))\n", "composite_default"},
		{"composite positional default", deepHeader + "a = create_deep_agent(backend=CompositeBackend(LocalShellBackend(), {}))\n", "composite_default"},
		{"composite route only", deepHeader + "a = create_deep_agent(backend=CompositeBackend(default=StateBackend(), routes={'/s/': LocalShellBackend()}))\n", ""},
		{"subclass without execute", deepHeader + "class S(LocalShellBackend):\n    pass\na = create_deep_agent(backend=S())\n", "subclass"},
		{"subclass overriding execute", deepHeader + "class S(LocalShellBackend):\n    def execute(self, command, *, timeout=None):\n        return None\na = create_deep_agent(backend=S())\n", ""},
		{"subclass overriding aexecute", deepHeader + "class S(LocalShellBackend):\n    async def aexecute(self, command):\n        return None\na = create_deep_agent(backend=S())\n", ""},
		{"state backend", deepHeader + "a = create_deep_agent(backend=StateBackend())\n", ""},
		{"no backend", deepHeader + "a = create_deep_agent(model=m)\n", ""},
		{"factory", deepHeader + "a = create_deep_agent(backend=make_backend())\n", ""},
		{"lambda", deepHeader + "a = create_deep_agent(backend=lambda rt: LocalShellBackend())\n", ""},
		{"variable assigned twice", deepHeader + "b = LocalShellBackend()\nb = StateBackend()\na = create_deep_agent(backend=b)\n", ""},
		{"variable assigned in if", deepHeader + "if X:\n    b = LocalShellBackend()\na = create_deep_agent(backend=b)\n", ""},
		{"variable assigned in try", deepHeader + "try:\n    b = LocalShellBackend()\nexcept Exception:\n    b = StateBackend()\na = create_deep_agent(backend=b)\n", ""},
		{"parameter shadows module binding", deepHeader + "b = LocalShellBackend()\ndef build(b):\n    return create_deep_agent(backend=b)\n", ""},
		{"async def builder", deepHeader + "async def build():\n    return create_deep_agent(backend=LocalShellBackend())\n", "direct"},
		{"module alias", "import deepagents as d\nfrom deepagents import backends\na = d.create_deep_agent(backend=backends.LocalShellBackend())\n", "direct"},
		{"import alias", "from deepagents import create_deep_agent as cda\nfrom deepagents.backends import LocalShellBackend as LSB\na = cda(backend=LSB())\n", "direct"},
		{"qualified", "import deepagents\nimport deepagents.backends\na = deepagents.create_deep_agent(backend=deepagents.backends.LocalShellBackend())\n", "direct"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			shape, _ := deepFacts(t, tc.src)
			if shape != tc.shape {
				t.Errorf("DeepShellBackend: got %q, want %q", shape, tc.shape)
			}
		})
	}
}

func TestDeepAgents_ExecuteGate(t *testing.T) {
	call := func(extra string) string {
		return deepHeader + "from langchain.agents.middleware import HumanInTheLoopMiddleware\n" +
			"a = create_deep_agent(backend=LocalShellBackend()" + extra + ")\n"
	}
	cases := []struct {
		name, src, gate string
	}{
		{"missing", call(""), "absent"},
		{"None", call(", interrupt_on=None"), "absent"},
		{"empty dict", call(", interrupt_on={}"), "empty"},
		{"other tools", call(`, interrupt_on={"write_file": True}`), "other_tools"},
		{"execute False", call(`, interrupt_on={"execute": False}`), "execute_false"},
		{"execute True", call(`, interrupt_on={"execute": True}`), "gated"},
		{"execute single quotes", call(`, interrupt_on={'execute': True}`), "gated"},
		{"execute config", call(`, interrupt_on={"execute": {"allowed_decisions": ["approve"]}}`), "gated"},
		{"execute config call", call(`, interrupt_on={"execute": InterruptOnConfig(allowed_decisions=["approve"])}`), "gated"},
		{"execute value is a name", call(`, interrupt_on={"execute": flag}`), "unjudged"},
		{"interrupt_on is a name", call(", interrupt_on=cfg"), "unjudged"},
		{"interrupt_on is a call", call(", interrupt_on=build()"), "unjudged"},
		{"dict splat", call(", interrupt_on={**base}"), "unjudged"},
		{"non-literal key", call(", interrupt_on={name: True}"), "unjudged"},
		{"HITL middleware", call(", middleware=[HumanInTheLoopMiddleware(interrupt_on={'execute': True})]"), "unjudged"},
		{"middleware not a list", call(", middleware=mw"), "unjudged"},
		{"middleware with a name", call(", middleware=[mw]"), "unjudged"},
		{"middleware without HITL", call(", middleware=[Other()]"), "absent"},
		{"kwargs splat", call(", **cfg"), "unjudged"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			shape, gate := deepFacts(t, tc.src)
			if shape != "direct" {
				t.Fatalf("DeepShellBackend: got %q, want direct", shape)
			}
			if gate != tc.gate {
				t.Errorf("DeepExecuteGate: got %q, want %q", gate, tc.gate)
			}
		})
	}
}

func TestDeepAgents_HarnessProfileIsNotJudged(t *testing.T) {
	src := deepHeader + `from deepagents import HarnessProfile, register_harness_profile
register_harness_profile("m", HarnessProfile(excluded_tools=frozenset({"execute"})))
a = create_deep_agent(backend=LocalShellBackend())
`
	if _, gate := deepFacts(t, src); gate != "unjudged" {
		t.Errorf("DeepExecuteGate: got %q, want unjudged", gate)
	}
}

func TestDeepAgents_ImportGate(t *testing.T) {
	cases := map[string]string{
		"local function":        "def create_deep_agent(**kw):\n    return kw\nx = create_deep_agent(backend=1)\n",
		"other package":         "from other import create_deep_agent\nx = create_deep_agent(backend=1)\n",
		"deepagents_cli":        "from deepagents_cli import create_deep_agent\nx = create_deep_agent(backend=1)\n",
		"async name":            deepHeader + "x = async_create_deep_agent(backend=LocalShellBackend())\n",
		"own LocalShellBackend": "from deepagents import create_deep_agent\nfrom mine import LocalShellBackend\nx = create_deep_agent(backend=LocalShellBackend())\n",
		"comment":               deepHeader + "# x = create_deep_agent(backend=LocalShellBackend())\n",
		"string":                deepHeader + "s = \"create_deep_agent(backend=LocalShellBackend())\"\n",
	}
	for name, src := range cases {
		t.Run(name, func(t *testing.T) {
			pf := parsePyFile(t, "agent.py", src)
			agents := analysis.DiscoverDeepAgents([]analysis.ParsedFile{pf})
			for _, a := range agents {
				if a.DeepShellBackend != "" {
					t.Errorf("resolved a shell backend (%q) where none should be", a.DeepShellBackend)
				}
			}
			if name == "local function" || name == "other package" || name == "deepagents_cli" || name == "comment" || name == "string" || name == "async name" {
				if len(agents) != 0 {
					t.Errorf("got %d agents, want 0", len(agents))
				}
			}
		})
	}
}

func TestDeepAgents_VarNameAndLocation(t *testing.T) {
	pf := parsePyFile(t, "agent.py", deepHeader+"\nagent = create_deep_agent(name=\"ops\", backend=LocalShellBackend())\n")
	agents := analysis.DiscoverDeepAgents([]analysis.ParsedFile{pf})
	if len(agents) != 1 {
		t.Fatalf("got %d agents", len(agents))
	}
	a := agents[0]
	if a.VarName != "agent" || a.Name != "ops" || a.FilePath != "agent.py" || a.Line != 4 {
		t.Errorf("got var=%q name=%q file=%q line=%d", a.VarName, a.Name, a.FilePath, a.Line)
	}
}
