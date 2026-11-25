package analysis_test

import (
	"testing"

	"github.com/trustabl/trustabl/internal/analysis"
	"github.com/trustabl/trustabl/internal/models"
)

const lcCreateAgentPrelude = `from langchain.agents import create_agent
from langchain_core.runnables import RunnableConfig

agent = create_agent(model, tools=[])
`

const lcExecutorPrelude = `from langchain.agents import AgentExecutor, create_tool_calling_agent

executor = AgentExecutor(agent=create_tool_calling_agent(llm, tools, prompt), tools=tools)
`

// runCallCredit discovers the LangChain agents and run calls in files, applies
// the run-call credit and returns the agents.
func runCallCredit(t *testing.T, files ...analysis.ParsedFile) []models.AgentDef {
	t.Helper()
	agents := analysis.DiscoverLangChainAgents(files)
	if len(agents) == 0 {
		t.Fatal("no LangChain agent discovered")
	}
	analysis.ApplyRunCallInstrumentation(agents, analysis.DiscoverAgentRunCalls(files))
	return agents
}

func TestApplyRunCallInstrumentation_Credits(t *testing.T) {
	cases := []struct{ name, src, call string }{
		{"dict config", lcCreateAgentPrelude + "agent.invoke({'messages': []}, config={'callbacks': [handler]})\n", "agent.invoke"},
		{"RunnableConfig", lcCreateAgentPrelude + "agent.invoke({'messages': []}, config=RunnableConfig(callbacks=[handler]))\n", "agent.invoke"},
		{"LangWatch callback", lcCreateAgentPrelude + "import langwatch\n\n@langwatch.trace()\ndef run(q):\n    return agent.invoke({'messages': [q]}, config=RunnableConfig(callbacks=[langwatch.get_current_trace().get_langchain_callback()]))\n", "agent.invoke"},
		{"callbacks variable", lcCreateAgentPrelude + "agent.stream({'messages': []}, config={'callbacks': handlers})\n", "agent.stream"},
		{"AgentExecutor", lcExecutorPrelude + "executor.invoke({'input': 'hi'}, config={'callbacks': [handler]})\n", "executor.invoke"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			agents := runCallCredit(t, parsePyFile(t, "agent.py", tc.src))
			got := agents[0].InstrumentedBy
			if got == nil || got.Kwarg != "callbacks" || got.Call != tc.call+" config callbacks" || got.File != "agent.py" || got.Line == 0 {
				t.Fatalf("InstrumentedBy = %+v, want callbacks credit from %s", got, tc.call)
			}
		})
	}
}

func TestApplyRunCallInstrumentation_NoCredit(t *testing.T) {
	cases := map[string]string{
		"no config":               lcCreateAgentPrelude + "agent.invoke({'messages': []})\n",
		"config without cbs":      lcCreateAgentPrelude + "agent.invoke({'messages': []}, config={'recursion_limit': 5})\n",
		"empty callbacks":         lcCreateAgentPrelude + "agent.invoke({'messages': []}, config={'callbacks': []})\n",
		"None callbacks":          lcCreateAgentPrelude + "agent.invoke({'messages': []}, config=RunnableConfig(callbacks=None))\n",
		"opaque config":           lcCreateAgentPrelude + "agent.invoke({'messages': []}, config=cfg)\n",
		"callbacks not in config": lcCreateAgentPrelude + "agent.invoke({'messages': []}, callbacks=[handler])\n",
		"other receiver":          lcCreateAgentPrelude + "other.invoke({'messages': []}, config={'callbacks': [handler]})\n",
	}
	for name, src := range cases {
		t.Run(name, func(t *testing.T) {
			if got := runCallCredit(t, parsePyFile(t, "agent.py", src))[0].InstrumentedBy; got != nil {
				t.Fatalf("InstrumentedBy = %+v, want none", got)
			}
		})
	}
}

// A run call credits only the agent it runs: a second agent in the same file
// stays uncredited.
func TestApplyRunCallInstrumentation_OnlyTheRunAgent(t *testing.T) {
	src := lcCreateAgentPrelude + "second = create_agent(model, tools=[])\nagent.invoke({'messages': []}, config={'callbacks': [handler]})\nsecond.invoke({'messages': []})\n"
	agents := runCallCredit(t, parsePyFile(t, "agent.py", src))
	for _, a := range agents {
		credited := a.InstrumentedBy != nil
		if credited != (a.VarName == "agent") {
			t.Errorf("%s: credited = %v", a.VarName, credited)
		}
	}
}

// The run call may live in another file that imports the agent.
func TestApplyRunCallInstrumentation_CrossFile(t *testing.T) {
	def := parsePyFile(t, "graph.py", lcCreateAgentPrelude)
	run := parsePyFile(t, "server.py", "from graph import agent\n\ndef handle(q):\n    return agent.invoke({'messages': [q]}, config={'callbacks': [handler]})\n")
	agents := runCallCredit(t, def, run)
	if got := agents[0].InstrumentedBy; got == nil || got.File != "server.py" {
		t.Fatalf("InstrumentedBy = %+v, want the server.py run call", got)
	}
}

// An agent already credited by a process-wide call keeps that credit.
func TestApplyRunCallInstrumentation_KeepsGlobalCredit(t *testing.T) {
	files := []analysis.ParsedFile{parsePyFile(t, "agent.py", lcCreateAgentPrelude+"agent.invoke({}, config={'callbacks': [h]})\n")}
	agents := analysis.DiscoverLangChainAgents(files)
	global := &models.GlobalInstrumentation{Kwarg: "callbacks", Call: "langchaininstrumentor.instrument", File: "obs.py", Line: 2}
	agents[0].InstrumentedBy = global
	analysis.ApplyRunCallInstrumentation(agents, analysis.DiscoverAgentRunCalls(files))
	if agents[0].InstrumentedBy != global {
		t.Fatalf("InstrumentedBy = %+v, want the global credit kept", agents[0].InstrumentedBy)
	}
}
