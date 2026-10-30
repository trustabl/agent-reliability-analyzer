package analysis_test

import (
	"testing"

	"github.com/trustabl/trustabl/internal/analysis"
	"github.com/trustabl/trustabl/internal/models"
)

// A Strands Agent(...) is emitted as one AgentDef with SDK=SDKStrands, the
// normalized Class "StrandsAgent", its name= label, its variable name, and its
// constructor kwargs.
func TestStrandsAgent_ConstructorCaptured(t *testing.T) {
	src := `from strands import Agent

agent = Agent(
    model="us.anthropic.claude-sonnet-4-20250514-v1:0",
    name="weather",
    system_prompt="Answer weather questions.",
)
`
	pf := parsePyFile(t, "agent.py", src)
	agents := analysis.DiscoverStrandsAgents([]analysis.ParsedFile{pf})
	if len(agents) != 1 {
		t.Fatalf("got %d agents, want 1", len(agents))
	}
	a := agents[0]
	if a.SDK != models.SDKStrands || a.Class != "StrandsAgent" || a.Language != models.LanguagePython {
		t.Errorf("got SDK=%q Class=%q Language=%q, want strands / StrandsAgent / python", a.SDK, a.Class, a.Language)
	}
	if a.Name != "weather" || a.VarName != "agent" {
		t.Errorf("got Name=%q VarName=%q, want weather / agent", a.Name, a.VarName)
	}
	if a.Line != 3 {
		t.Errorf("Line: got %d, want 3", a.Line)
	}
	if kw := a.Kwargs.Children["system_prompt"]; kw == nil || kw.Value == nil {
		t.Fatalf("system_prompt kwarg not captured: %+v", a.Kwargs)
	}
}

// `import strands` and an aliased import both bind the Strands Agent.
func TestStrandsAgent_ModuleAndAliasImports(t *testing.T) {
	src := `import strands
import strands as s
from strands import Agent as StrandsAgent

a = strands.Agent()
b = s.Agent()
c = StrandsAgent()
`
	pf := parsePyFile(t, "agents.py", src)
	if got := analysis.DiscoverStrandsAgents([]analysis.ParsedFile{pf}); len(got) != 3 {
		t.Fatalf("got %d agents, want 3: %+v", len(got), got)
	}
}

// The name `Agent` is shared with the OpenAI Agents SDK and Pydantic AI. Only
// the Strands-bound callee is claimed, even in a file that imports both.
func TestStrandsAgent_OtherSDKsAgentNotClaimed(t *testing.T) {
	src := `from pydantic_ai import Agent
from strands import Agent as StrandsAgent

pyd = Agent("openai:gpt-4o")
st = StrandsAgent(name="st")
`
	pf := parsePyFile(t, "mixed.py", src)
	got := analysis.DiscoverStrandsAgents([]analysis.ParsedFile{pf})
	if len(got) != 1 || got[0].Name != "st" {
		t.Fatalf("got %+v, want only the Strands agent st", got)
	}

	oai := parsePyFile(t, "oai.py", "from agents import Agent\n\nagent = Agent(name=\"ops\")\n")
	if got := analysis.DiscoverStrandsAgents([]analysis.ParsedFile{oai}); len(got) != 0 {
		t.Errorf("an OpenAI Agent was claimed as Strands: %+v", got)
	}
}

// A function decorated with the Strands `tool` is a Strands tool in every
// spelling: bare, called with arguments, and module-qualified.
func TestKindFromDecorators_StrandsTool(t *testing.T) {
	src := `import strands
from strands import tool


@tool
def get_weather(city: str) -> str:
    """Weather for a city."""
    return city


@tool(name="forecast")
def get_forecast(city: str) -> str:
    """Forecast for a city."""
    return city


@strands.tool
def get_alerts(city: str) -> str:
    """Alerts for a city."""
    return city
`
	kinds := kindByName(t, "tools.py", src)
	for _, name := range []string{"get_weather", "get_forecast", "get_alerts"} {
		if kinds[name] != models.KindStrandsTool {
			t.Errorf("%s: got %q, want %q", name, kinds[name], models.KindStrandsTool)
		}
	}
}

// Regression guard: a LangChain `@tool` keeps its kind when no Strands import
// is present, and resolves by import when both SDKs are imported.
func TestKindFromDecorators_StrandsLeavesLangChainAlone(t *testing.T) {
	lc := `from langchain_core.tools import tool


@tool
def search(q: str) -> str:
    """Search."""
    return q
`
	if k := kindByName(t, "lc.py", lc)["search"]; k != models.KindLangChainTool {
		t.Errorf("LangChain-only file: got %q, want %q", k, models.KindLangChainTool)
	}

	both := `from langchain_core.tools import tool as lc_tool
from strands import tool


@lc_tool
def search(q: str) -> str:
    """Search."""
    return q


@tool
def lookup(q: str) -> str:
    """Lookup."""
    return q
`
	kinds := kindByName(t, "both.py", both)
	if kinds["search"] != models.KindLangChainTool || kinds["lookup"] != models.KindStrandsTool {
		t.Errorf("both imported: got search=%q lookup=%q, want langchain_tool / strands_tool", kinds["search"], kinds["lookup"])
	}
}

// One tools=[...] list carries all three Strands tool shapes: a same-file @tool
// function resolves to its ToolDef, a risky strands_tools tool becomes a
// HostedToolRef, a benign one stays an external ToolRef, and an MCPClient
// variable becomes an MCPServerRef with its transport.
func TestResolveEdges_StrandsToolsList(t *testing.T) {
	src := `import strands_tools
from mcp import StdioServerParameters, stdio_client
from strands import Agent, tool
from strands.tools.mcp import MCPClient
from strands_tools import calculator, http_request
from strands_tools.shell import shell as run_shell


@tool
def get_weather(city: str) -> str:
    """Weather for a city."""
    return city


docs = MCPClient(lambda: stdio_client(StdioServerParameters(command="uvx", args=["docs-mcp"])))

agent = Agent(tools=[get_weather, http_request, run_shell, strands_tools.python_repl, calculator, docs])
`
	pf := parsePyFile(t, "agent.py", src)
	files := []analysis.ParsedFile{pf}
	inv := models.RepoInventory{
		Tools:  analysis.DiscoverToolsFromParsed(files),
		Agents: analysis.DiscoverStrandsAgents(files),
	}
	analysis.ResolveEdges(&inv, files)

	if len(inv.Agents) != 1 {
		t.Fatalf("got %d agents, want 1", len(inv.Agents))
	}
	a := inv.Agents[0]
	if a.Opaque {
		t.Fatalf("agent marked opaque")
	}

	hosted := map[string]bool{}
	for _, h := range a.HostedToolRefs {
		hosted[h.Class] = true
	}
	for _, want := range []string{"http_request", "shell", "python_repl"} {
		if !hosted[want] {
			t.Errorf("HostedToolRefs %+v: missing %s", a.HostedToolRefs, want)
		}
	}
	if len(a.HostedToolRefs) != 3 {
		t.Errorf("HostedToolRefs: got %d, want 3 (calculator is benign)", len(a.HostedToolRefs))
	}
	for _, h := range inv.HostedTools {
		if h.SDK != models.SDKStrands {
			t.Errorf("hosted tool %s: SDK %q, want strands", h.Class, h.SDK)
		}
	}

	refs := map[string]models.ToolRef{}
	for _, r := range a.ToolRefs {
		refs[r.Name] = r
	}
	if r := refs["get_weather"]; r.Resolved == nil || r.Resolved.Kind != models.KindStrandsTool {
		t.Errorf("get_weather: got %+v, want resolved strands_tool", r)
	}
	if r, ok := refs["calculator"]; !ok || !r.External {
		t.Errorf("calculator: got %+v, want an external ToolRef", r)
	}
	if len(a.ToolRefs) != 2 {
		t.Errorf("ToolRefs: got %+v, want get_weather and calculator only", a.ToolRefs)
	}

	if len(a.MCPServerRefs) != 1 || len(inv.MCPServers) != 1 {
		t.Fatalf("MCP: refs=%+v servers=%+v, want one", a.MCPServerRefs, inv.MCPServers)
	}
	m := inv.MCPServers[0]
	if m.Class != "MCPClient" || m.Transport != "stdio" || m.SDK != models.SDKStrands {
		t.Errorf("MCP server: got %+v, want MCPClient / stdio / strands", m)
	}
}

// The documented MCP shape, tools=client.list_tools_sync(), records the server
// instead of marking the agent opaque; an inline MCPClient(url=...) is a
// streamable HTTP server.
func TestResolveEdges_StrandsMCPShapes(t *testing.T) {
	src := `from strands import Agent
from strands.tools.mcp import MCPClient

client = MCPClient(url="https://mcp.example.com/mcp")
with client:
    listed = Agent(tools=client.list_tools_sync())

inline = Agent(tools=[MCPClient(url="https://other.example.com/mcp")])
`
	pf := parsePyFile(t, "mcp.py", src)
	files := []analysis.ParsedFile{pf}
	inv := models.RepoInventory{Agents: analysis.DiscoverStrandsAgents(files)}
	analysis.ResolveEdges(&inv, files)

	if len(inv.Agents) != 2 {
		t.Fatalf("got %d agents, want 2", len(inv.Agents))
	}
	for _, a := range inv.Agents {
		if a.Opaque {
			t.Errorf("agent at line %d marked opaque", a.Line)
		}
		if len(a.MCPServerRefs) != 1 {
			t.Errorf("agent at line %d: MCPServerRefs %+v, want one", a.Line, a.MCPServerRefs)
		}
	}
	for _, m := range inv.MCPServers {
		if m.Transport != "streamable_http" {
			t.Errorf("MCP server at line %d: transport %q, want streamable_http", m.Line, m.Transport)
		}
	}
}

// Any other non-list tools= value is still opaque.
func TestResolveEdges_StrandsOpaqueTools(t *testing.T) {
	src := `from strands import Agent

agent = Agent(tools=load_tools())
`
	pf := parsePyFile(t, "opaque.py", src)
	files := []analysis.ParsedFile{pf}
	inv := models.RepoInventory{Agents: analysis.DiscoverStrandsAgents(files)}
	analysis.ResolveEdges(&inv, files)
	if len(inv.Agents) != 1 || !inv.Agents[0].Opaque {
		t.Fatalf("got %+v, want one opaque agent", inv.Agents)
	}
}
