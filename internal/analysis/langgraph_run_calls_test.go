package analysis_test

import (
	"testing"

	"github.com/trustabl/trustabl/internal/analysis"
	"github.com/trustabl/trustabl/internal/models"
)

// LangGraph checkpointer capture and run-call anchoring (LC-116 inputs).

const lgGraphPrelude = `from langgraph.graph import StateGraph
from langgraph.checkpoint.memory import MemorySaver, InMemorySaver
from langgraph.checkpoint.postgres import PostgresSaver

builder = StateGraph(dict)
`

func lgGraphAgent(t *testing.T, src string) models.AgentDef {
	t.Helper()
	agents := analysis.DiscoverLangGraphGraphs([]analysis.ParsedFile{parsePyFile(t, "graph.py", src)})
	if len(agents) != 1 {
		t.Fatalf("expected 1 StateGraph agent, got %d", len(agents))
	}
	return agents[0]
}

func TestLangGraphCheckpointerClass(t *testing.T) {
	cases := []struct{ name, src, want string }{
		{"MemorySaver()", lgGraphPrelude + "graph = builder.compile(checkpointer=MemorySaver())\n", "MemorySaver"},
		{"MemorySaver(serde=...)", lgGraphPrelude + "graph = builder.compile(checkpointer=MemorySaver(serde=my_serde))\n", "MemorySaver"},
		{"InMemorySaver()", lgGraphPrelude + "graph = builder.compile(checkpointer=InMemorySaver())\n", "InMemorySaver"},
		{"variable bound to MemorySaver", lgGraphPrelude + "memory = MemorySaver()\ngraph = builder.compile(checkpointer=memory)\n", "MemorySaver"},
		{"aliased import", "from langgraph.graph import StateGraph\nfrom langgraph.checkpoint.memory import MemorySaver as MS\nbuilder = StateGraph(dict)\ngraph = builder.compile(checkpointer=MS())\n", "MemorySaver"},
		{"module-qualified", "import langgraph.checkpoint.memory as mem\nfrom langgraph.graph import StateGraph\nbuilder = StateGraph(dict)\ngraph = builder.compile(checkpointer=mem.MemorySaver())\n", "MemorySaver"},
		{"PostgresSaver()", lgGraphPrelude + "graph = builder.compile(checkpointer=PostgresSaver(conn))\n", "PostgresSaver"},
		{"PostgresSaver.from_conn_string", lgGraphPrelude + "cp = PostgresSaver.from_conn_string(DB_URI)\ngraph = builder.compile(checkpointer=cp)\n", "PostgresSaver"},
		{"unresolved name", lgGraphPrelude + "graph = builder.compile(checkpointer=get_saver())\n", ""},
		{"bare unbound name", lgGraphPrelude + "def build(cp):\n    return builder.compile(checkpointer=cp)\n", ""},
		{"local class named MemorySaver", "from langgraph.graph import StateGraph\nclass MemorySaver: pass\nbuilder = StateGraph(dict)\ngraph = builder.compile(checkpointer=MemorySaver())\n", ""},
		{"conflicting bindings", lgGraphPrelude + "cp = MemorySaver()\nif PROD:\n    cp = PostgresSaver(conn)\ngraph = builder.compile(checkpointer=cp)\n", ""},
		{"runtime-chosen saver", lgGraphPrelude + "cp = MemorySaver() if DEV else PostgresSaver(conn)\ngraph = builder.compile(checkpointer=cp)\n", ""},
		{"no checkpointer", lgGraphPrelude + "graph = builder.compile()\n", ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := lgGraphAgent(t, c.src).CheckpointerClass; got != c.want {
				t.Errorf("CheckpointerClass = %q, want %q", got, c.want)
			}
		})
	}
}

func TestLangGraphCheckpointerClass_Prebuilt(t *testing.T) {
	src := "from langgraph.prebuilt import create_react_agent\nfrom langgraph.checkpoint.memory import MemorySaver\nagent = create_react_agent(model, tools, checkpointer=MemorySaver())\n"
	agents := analysis.DiscoverLangChainAgents([]analysis.ParsedFile{parsePyFile(t, "agent.py", src)})
	if len(agents) != 1 || agents[0].CheckpointerClass != "MemorySaver" || agents[0].VarName != "agent" {
		t.Fatalf("unexpected prebuilt agent: %+v", agents)
	}
}

func lgRunCalls(t *testing.T, files ...analysis.ParsedFile) []models.AgentRunCallDef {
	t.Helper()
	var out []models.AgentRunCallDef
	for _, rc := range analysis.DiscoverAgentRunCalls(files) {
		if rc.SDK == models.SDKLangChain {
			out = append(out, rc)
		}
	}
	return out
}

func TestDiscoverAgentRunCalls_LangGraph(t *testing.T) {
	compiled := lgGraphPrelude + "graph = builder.compile(checkpointer=MemorySaver())\n"
	cases := []struct {
		name   string
		src    string
		callee []string
	}{
		{"invoke on compiled var", compiled + "graph.invoke({'x': 1})\n", []string{"graph.invoke"}},
		{"async and streaming methods", compiled + "async def f():\n    await graph.ainvoke(s)\n    async for e in graph.astream(s):\n        pass\n    graph.stream(s)\n    graph.batch([s])\n    await graph.abatch([s])\n    graph.astream_events(s)\n",
			[]string{"graph.ainvoke", "graph.astream", "graph.stream", "graph.batch", "graph.abatch", "graph.astream_events"}},
		{"direct chain builder.compile().invoke()", lgGraphPrelude + "builder.compile(checkpointer=MemorySaver()).invoke(s)\n", []string{"builder.compile(checkpointer=MemorySaver()).invoke"}},
		{"prebuilt agent", "from langgraph.prebuilt import create_react_agent\nagent = create_react_agent(model, tools)\nagent.invoke(s)\n", []string{"agent.invoke"}},
		{"LLM / chain / unknown receiver ignored", "from langchain_openai import ChatOpenAI\nfrom langchain_core.prompts import ChatPromptTemplate\nllm = ChatOpenAI()\nchain = prompt | llm\nllm.invoke('hi')\nchain.invoke({})\nretriever.invoke('q')\nsomething.stream('x')\n", nil},
		{"non-run method on graph ignored", compiled + "graph.get_state(cfg)\n", nil},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			calls := lgRunCalls(t, parsePyFile(t, "graph.py", c.src))
			if len(calls) != len(c.callee) {
				t.Fatalf("got %d LangGraph run calls, want %d: %+v", len(calls), len(c.callee), calls)
			}
			for i, rc := range calls {
				if rc.Callee != c.callee[i] {
					t.Errorf("call %d Callee = %q, want %q", i, rc.Callee, c.callee[i])
				}
				if rc.AgentFilePath != "" {
					t.Errorf("same-file call has AgentFilePath %q", rc.AgentFilePath)
				}
			}
			if len(calls) > 0 && c.name != "prebuilt agent" && calls[0].AgentVarName != "builder" {
				t.Errorf("AgentVarName = %q, want builder (the builder variable)", calls[0].AgentVarName)
			}
		})
	}
}

func TestDiscoverAgentRunCalls_LangGraphCrossFile(t *testing.T) {
	graph := parsePyFile(t, "svc/graph.py", lgGraphPrelude+"graph = builder.compile(checkpointer=MemorySaver())\n")
	for _, imp := range []string{"from svc.graph import graph", "from .graph import graph", "from svc.graph import graph as g"} {
		recv := "graph"
		if imp == "from svc.graph import graph as g" {
			recv = "g"
		}
		api := parsePyFile(t, "svc/api.py", imp+"\n"+recv+".invoke(s)\n")
		calls := lgRunCalls(t, graph, api)
		if len(calls) != 1 || calls[0].FilePath != "svc/api.py" || calls[0].AgentFilePath != "svc/graph.py" || calls[0].AgentVarName != "builder" {
			t.Errorf("%s: unexpected calls %+v", imp, calls)
		}
	}

	// A graph compiled inside a function is not importable: not resolved.
	local := parsePyFile(t, "svc/graph.py", lgGraphPrelude+"def make():\n    graph = builder.compile()\n    return graph\n")
	api := parsePyFile(t, "svc/api.py", "from svc.graph import graph\ngraph.invoke(s)\n")
	if calls := lgRunCalls(t, local, api); len(calls) != 0 {
		t.Errorf("non-module-level compile resolved cross-file: %+v", calls)
	}
}

// TestLangGraphRunCallReachability runs discovery + entrypoints + the stamp as
// the scanner does and checks which LangGraph run calls are server-reachable.
func TestLangGraphRunCallReachability(t *testing.T) {
	graphFile := lgGraphPrelude + "graph = builder.compile(checkpointer=MemorySaver())\n"
	cases := []struct {
		name  string
		files map[string]string
		want  string // expected stamp Via; "" = not stamped
	}{
		{"same-file FastAPI route", map[string]string{
			"app.py": graphFile + "from fastapi import FastAPI\napi = FastAPI()\n\n@api.post('/chat')\nasync def chat(q: str):\n    return await graph.ainvoke({'q': q})\n",
		}, "direct"},
		{"cross-file FastAPI handler", map[string]string{
			"graph.py": graphFile,
			"api.py":   "from fastapi import FastAPI\nfrom graph import graph\napi = FastAPI()\n\n@api.post('/chat')\ndef chat(q: str):\n    return graph.invoke({'q': q})\n",
		}, "direct"},
		{"Celery task", map[string]string{
			"tasks.py": graphFile + "from celery import Celery\ncapp = Celery('x')\n\n@capp.task\ndef run(q):\n    return graph.invoke({'q': q})\n",
		}, "direct"},
		{"same-file helper called from the route", map[string]string{
			"app.py": graphFile + "from fastapi import FastAPI\napi = FastAPI()\n\ndef helper(q):\n    return graph.invoke({'q': q})\n\n@api.get('/chat')\ndef chat(q: str):\n    return helper(q)\n",
		}, "same_file"},
		{"prebuilt agent from a route", map[string]string{
			"app.py": "from fastapi import FastAPI\nfrom langgraph.prebuilt import create_react_agent\nfrom langgraph.checkpoint.memory import MemorySaver\napi = FastAPI()\nagent = create_react_agent(model, tools, checkpointer=MemorySaver())\n\n@api.post('/chat')\ndef chat(q: str):\n    return agent.invoke({'messages': [q]})\n",
		}, "direct"},
		{"standalone script", map[string]string{
			"run.py": graphFile + "if __name__ == '__main__':\n    graph.invoke({'q': 'hi'})\n",
		}, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			var files []analysis.ParsedFile
			for _, name := range sortedKeys(c.files) {
				files = append(files, parsePyFile(t, name, c.files[name]))
			}
			inv := reach(files)
			var lg []models.AgentRunCallDef
			for _, rc := range inv.AgentRunCalls {
				if rc.SDK == models.SDKLangChain {
					lg = append(lg, rc)
				}
			}
			if len(lg) != 1 {
				t.Fatalf("expected 1 LangGraph run call, got %d: %+v", len(lg), lg)
			}
			got := ""
			if lg[0].ServerReachable != nil {
				got = lg[0].ServerReachable.Via
			}
			if got != c.want {
				t.Errorf("stamp Via = %q, want %q", got, c.want)
			}
		})
	}
}
