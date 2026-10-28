package analysis_test

import (
	"reflect"
	"testing"

	"github.com/trustabl/trustabl/internal/analysis"
	"github.com/trustabl/trustabl/internal/models"
)

// claudeTool discovers the single Claude Agent SDK tool in src.
func claudeTool(t *testing.T, src string) models.ToolDef {
	t.Helper()
	tools := analysis.DiscoverToolsFromParsed([]analysis.ParsedFile{parsePyFile(t, "tools.py", src)})
	if len(tools) != 1 || tools[0].Kind != models.KindClaudeSDKTool {
		t.Fatalf("expected 1 Claude tool, got %+v", tools)
	}
	return tools[0]
}

func TestClaudeInputKeys(t *testing.T) {
	cases := []struct {
		name string
		src  string
		want []string
	}{
		{"short dict form", `from claude_agent_sdk import tool

@tool("send_email", "Send an email", {"to": str, "claim": dict})
async def send_email(args):
    return {}
`, []string{"to", "claim"}},
		// Review Focus 3: the JSON-schema form names its keys under properties.
		{"JSON-schema form", `from claude_agent_sdk import tool

@tool("send_email", "Send an email", {
    "type": "object",
    "properties": {"to": {"type": "string"}, "claim": {"type": "object"}},
    "required": ["to", "claim"],
})
async def send_email(args):
    return {}
`, []string{"to", "claim"}},
		{"TypedDict class", `from typing import TypedDict
from claude_agent_sdk import tool

class SendArgs(TypedDict):
    to: str
    claim: dict

@tool("send_email", "Send an email", SendArgs)
async def send_email(args):
    return {}
`, []string{"to", "claim"}},
		{"module-level schema variable", `from claude_agent_sdk import tool

SCHEMA = {"to": str, "claim": dict}

@tool("send_email", "Send an email", SCHEMA)
async def send_email(args):
    return {}
`, []string{"to", "claim"}},
		{"input_schema keyword", `from claude_agent_sdk import tool

@tool("send_email", "Send an email", input_schema={"to": str})
async def send_email(args):
    return {}
`, []string{"to"}},
		{"unresolvable schema", `from claude_agent_sdk import tool
from schemas import SendArgs

@tool("send_email", "Send an email", SendArgs)
async def send_email(args):
    return {}
`, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := claudeTool(t, tc.src).InputKeys; !reflect.DeepEqual(got, tc.want) {
				t.Errorf("InputKeys = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestClaimUse_Claude(t *testing.T) {
	const head = "from claude_agent_sdk import tool\n\n"
	const deco = `@tool("send_email", "Send an email", {"to": str, "claim": dict})` + "\n"
	cases := []struct {
		name string
		src  string
		want string
	}{
		{"claim in schema, never read", head + deco + `async def send_email(args):
    return {"content": [{"type": "text", "text": args["to"]}]}
`, models.ClaimUseUnused},
		{"args[\"claim\"] only printed", head + deco + `async def send_email(args):
    print(args["claim"])
    return {}
`, models.ClaimUsePassthrough},
		{"args.get(\"claim\") passed to a verifier", head + deco + `async def send_email(args):
    if verify(args.get("claim")) != "allow":
        return {"is_error": True}
    return {}
`, models.ClaimUseChecked},
		{"claim read through a typed handler parameter", head + deco + `async def send_email(args: dict[str, Any]) -> dict[str, Any]:
    logger.info("claim %s", args["claim"])
    return {}
`, models.ClaimUsePassthrough},
		// The whole args dict handed to another function may be checked there.
		{"whole args passed to a call", head + deco + `async def send_email(args):
    mediate(args)
    return {}
`, models.ClaimUseChecked},
		{"whole args only logged", head + deco + `async def send_email(args):
    logger.debug(args)
    return {}
`, models.ClaimUsePassthrough},
		{"second decorator", head + `@audited
` + deco + `async def send_email(args):
    return {}
`, models.ClaimUseWrapped},
		{"no claim key: fact not stamped", head + `@tool("send_email", "Send an email", {"to": str})
async def send_email(args):
    return {}
`, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := claudeTool(t, tc.src).Facts[models.FactClaimUse]; got != tc.want {
				t.Errorf("claim_use = %q, want %q", got, tc.want)
			}
		})
	}
}

const claudeToolSrc = `from claude_agent_sdk import tool

@tool("send_email", "Send an email", {"to": str, "claim": dict})
async def send_email(args):
    return {}
`

func TestApplyClaimMediators_Claude(t *testing.T) {
	cases := []struct {
		name    string
		options string
		want    string
	}{
		{"no options: unchanged", "", models.ClaimUseUnused},
		// Review Focus 1: a matcher for another tool still counts.
		{"PreToolUse hook, matcher for another tool", `from claude_agent_sdk import ClaudeAgentOptions, HookMatcher

options = ClaudeAgentOptions(hooks={"PreToolUse": [HookMatcher(matcher="Bash", hooks=[check])]})
`, models.ClaimUseWrapped},
		{"PreToolUse hook in a dict variable", `from claude_agent_sdk import ClaudeAgentOptions, HookMatcher

HOOKS = {"PreToolUse": [HookMatcher(hooks=[check])]}
options = ClaudeAgentOptions(hooks=HOOKS)
`, models.ClaimUseWrapped},
		{"PostToolUse only: unchanged", `from claude_agent_sdk import ClaudeAgentOptions, HookMatcher

options = ClaudeAgentOptions(hooks={"PostToolUse": [HookMatcher(hooks=[log])]})
`, models.ClaimUseUnused},
		{"can_use_tool", `from claude_agent_sdk import ClaudeAgentOptions

options = ClaudeAgentOptions(can_use_tool=check)
`, models.ClaimUseWrapped},
		// allowed_tools auto-approves its tools, so can_use_tool never runs
		// for them.
		{"can_use_tool with allowed_tools: unchanged", `from claude_agent_sdk import ClaudeAgentOptions

options = ClaudeAgentOptions(can_use_tool=check, allowed_tools=["mcp__tools__send_email"])
`, models.ClaimUseUnused},
		{"can_use_tool with bypassPermissions: unchanged", `from claude_agent_sdk import ClaudeAgentOptions

options = ClaudeAgentOptions(can_use_tool=check, permission_mode="bypassPermissions")
`, models.ClaimUseUnused},
		{"PreToolUse key in a file without the SDK: unchanged", `HOOKS = {"PreToolUse": [check]}
`, models.ClaimUseUnused},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			files := map[string]string{"tools.py": claudeToolSrc}
			if tc.options != "" {
				files["agent.py"] = tc.options
			}
			if got := mediatedClaimUse(t, files, "send_email"); got != tc.want {
				t.Errorf("claim_use = %q, want %q", got, tc.want)
			}
		})
	}
}
