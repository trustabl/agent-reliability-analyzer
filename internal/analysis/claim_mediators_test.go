package analysis_test

import (
	"testing"

	"github.com/trustabl/trustabl/internal/analysis"
	"github.com/trustabl/trustabl/internal/models"
)

// mediatedClaimUse discovers the tools in files (decorated tools and Pydantic AI
// Tool(fn) factories), applies the mediator pass, and returns the claim_use
// fact of the tool named name.
func mediatedClaimUse(t *testing.T, files map[string]string, name string) string {
	t.Helper()
	var parsed []analysis.ParsedFile
	for path, src := range files {
		parsed = append(parsed, parsePyFile(t, path, src))
	}
	tools := analysis.DiscoverToolsFromParsed(parsed)
	tools = append(tools, analysis.DiscoverPydanticAITools(parsed)...)
	analysis.ApplyClaimMediators(tools, parsed)
	for _, td := range tools {
		if td.Name == name {
			return td.Facts[models.FactClaimUse]
		}
	}
	t.Fatalf("tool %q not discovered: %+v", name, tools)
	return ""
}

const crewTool = `from crewai.tools import tool

@tool("Send email")
def send_email(to: str, claim: dict) -> str:
    """Send an email."""
    return "ok"
`

const crewToolPrinted = `from crewai.tools import tool

@tool("Send email")
def send_email(to: str, claim: dict) -> str:
    """Send an email."""
    print(claim)
    return "ok"
`

const pydTool = `from pydantic_ai import Agent

agent = Agent("test")

@agent.tool_plain
def send_email(to: str, claim: dict) -> str:
    """Send an email."""
    return "ok"
`

func TestApplyClaimMediators(t *testing.T) {
	cases := []struct {
		name  string
		files map[string]string
		tool  string
		want  string
	}{
		{"crewai, no hook: unchanged", map[string]string{"tools.py": crewTool}, "send_email", models.ClaimUseUnused},
		{"crewai @before_tool_call in another file", map[string]string{
			"tools.py": crewTool,
			"hooks.py": `from crewai.hooks import before_tool_call

@before_tool_call
def check_claim(context):
    return verify(context.tool_input.get("claim"))
`}, "send_email", models.ClaimUseWrapped},
		{"crewai register_before_tool_call_hook", map[string]string{
			"tools.py": crewToolPrinted,
			"hooks.py": `from crewai.hooks import register_before_tool_call_hook

register_before_tool_call_hook(check_claim)
`}, "send_email", models.ClaimUseWrapped},
		{"crewai @on(InterceptionPoint.PRE_TOOL_CALL)", map[string]string{
			"tools.py": crewTool,
			"hooks.py": `from crewai.hooks import on, InterceptionPoint

@on(InterceptionPoint.PRE_TOOL_CALL)
def check_claim(ctx):
    pass
`}, "send_email", models.ClaimUseWrapped},
		{"crewai @on for another point: unchanged", map[string]string{
			"tools.py": crewTool,
			"hooks.py": `from crewai.hooks import on, InterceptionPoint

@on(InterceptionPoint.POST_TOOL_CALL)
def after(ctx):
    pass
`}, "send_email", models.ClaimUseUnused},
		{"pydantic, no hook: unchanged", map[string]string{"tools.py": pydTool}, "send_email", models.ClaimUseUnused},
		// Review Focus 1: a hook filtered to other tools still counts.
		{"pydantic @hooks.on.before_tool_execute filtered to another tool", map[string]string{
			"tools.py": pydTool,
			"hooks.py": `from pydantic_ai.capabilities import Hooks

hooks = Hooks()

@hooks.on.before_tool_execute(tools=["other"])
async def check_claim(ctx, *, call, tool_def, args):
    return args
`}, "send_email", models.ClaimUseWrapped},
		{"pydantic wrap-style hooks.on.tool_execute", map[string]string{
			"tools.py": pydTool,
			"hooks.py": `from pydantic_ai.capabilities import Hooks

hooks = Hooks()

@hooks.on.tool_execute
async def check_claim(ctx, *, call, tool_def, args, handler):
    return await handler(args)
`}, "send_email", models.ClaimUseWrapped},
		{"pydantic Hooks(before_tool_execute=...)", map[string]string{
			"tools.py": pydTool,
			"hooks.py": `from pydantic_ai.capabilities import Hooks

hooks = Hooks(before_tool_execute=check_claim)
`}, "send_email", models.ClaimUseWrapped},
		{"pydantic args_validator on the tool", map[string]string{
			"tools.py": `from pydantic_ai import Agent

agent = Agent("test")

@agent.tool_plain(args_validator=check_claim)
def send_email(to: str, claim: dict) -> str:
    """Send an email."""
    return "ok"
`}, "send_email", models.ClaimUseWrapped},
		{"pydantic Tool(fn) factory, no hook: unused", map[string]string{
			"tools.py": `from pydantic_ai import Agent, Tool

def send_email(to: str, claim: dict) -> str:
    """Send an email."""
    return "ok"

agent = Agent("test", tools=[Tool(send_email)])
`}, "send_email", models.ClaimUseUnused},
		// Review Focus 5: one SDK's hook does not credit another SDK's tools.
		{"crewai hook does not credit a pydantic tool", map[string]string{
			"tools.py": pydTool,
			"hooks.py": `from crewai.hooks import before_tool_call

@before_tool_call
def check_claim(context):
    return None
`}, "send_email", models.ClaimUseUnused},
		{"a checked claim stays checked", map[string]string{
			"tools.py": `from crewai.tools import tool

@tool("Send email")
def send_email(to: str, claim: dict) -> str:
    """Send an email."""
    if verify(claim) != "allow":
        raise PermissionError("no")
    return "ok"
`,
			"hooks.py": `from crewai.hooks import before_tool_call

@before_tool_call
def check_claim(context):
    return None
`}, "send_email", models.ClaimUseChecked},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := mediatedClaimUse(t, tc.files, tc.tool); got != tc.want {
				t.Errorf("claim_use = %q, want %q", got, tc.want)
			}
		})
	}
}
