package scanner_test

import "testing"

// TestScan_ToolCallClaims runs the tool-call-claims ladder (OAI-034..036,
// MCP-033..035, LC-028..030, CREW-016..018, PYD-016..018) through real
// discovery, so a change that stops
// discovery from stamping claim_use fails here rather than silently killing
// the rules. The per-rule table in policies_test.go sets the fact by hand.
func TestScan_ToolCallClaims(t *testing.T) {
	type sdk struct {
		manifest, header, decorator string
		absent, unused, passthrough string
	}
	sdks := map[string]sdk{
		"openai": {
			manifest:  "[project]\nname = \"f\"\ndependencies = [\"openai-agents\"]\n",
			header:    "from agents import function_tool\n",
			decorator: "@function_tool",
			absent:    "OAI-034", unused: "OAI-035", passthrough: "OAI-036",
		},
		"mcp": {
			manifest:  "[project]\nname = \"f\"\ndependencies = [\"mcp\"]\n",
			header:    "from mcp.server.fastmcp import FastMCP\nmcp = FastMCP(\"x\")\n",
			decorator: "@mcp.tool()",
			absent:    "MCP-033", unused: "MCP-034", passthrough: "MCP-035",
		},
		"langchain": {
			manifest:  "[project]\nname = \"f\"\ndependencies = [\"langchain-core\"]\n",
			header:    "from langchain_core.tools import tool\n",
			decorator: "@tool",
			absent:    "LC-028", unused: "LC-029", passthrough: "LC-030",
		},
		"crewai": {
			manifest:  "[project]\nname = \"f\"\ndependencies = [\"crewai\"]\n",
			header:    "from crewai.tools import tool\n",
			decorator: "@tool(\"Send email\")",
			absent:    "CREW-016", unused: "CREW-017", passthrough: "CREW-018",
		},
		"pydantic_ai": {
			manifest:  "[project]\nname = \"f\"\ndependencies = [\"pydantic-ai\"]\n",
			header:    "from pydantic_ai import Agent\nagent = Agent(\"test\")\n",
			decorator: "@agent.tool_plain",
			absent:    "PYD-016", unused: "PYD-017", passthrough: "PYD-018",
		},
	}
	bodies := map[string]string{
		"absent": `
def send_email(to: str, body: str) -> str:
    """Send an email."""
    return "ok"
`,
		"unused": `
def send_email(to: str, body: str, claim: dict) -> str:
    """Send an email."""
    return "ok"
`,
		"passthrough": `
def send_email(to: str, body: str, claim: dict) -> str:
    """Send an email."""
    print(claim)
    return "ok"
`,
		"checked": `
def send_email(to: str, body: str, claim: dict) -> str:
    """Send an email."""
    if verify_claim(claim, tool="send_email") != "allow":
        raise PermissionError("claim does not match route")
    return "ok"
`,
	}
	for name, s := range sdks {
		rungs := map[string]string{"absent": s.absent, "unused": s.unused, "passthrough": s.passthrough}
		for shape, body := range bodies {
			files := map[string]string{
				"pyproject.toml": s.manifest,
				"tools.py":       s.header + "\n" + s.decorator + body,
			}
			for rung, ruleID := range rungs {
				want := rung == shape // the ladder is exclusive: one rung per shape, none for checked
				t.Run(name+"/"+shape+"/"+ruleID, func(t *testing.T) {
					if got := scanFires(t, files, ruleID); got != want {
						t.Errorf("%s on a %s claim: fired=%v, want %v", ruleID, shape, got, want)
					}
				})
			}
		}
	}
}

// An OpenAI tool whose claim is read by an attached tool input guardrail, the
// SDK's own pre-tool hook, is not an unchecked claim.
func TestScan_ToolCallClaims_OpenAIInputGuardrailSilencesUnused(t *testing.T) {
	files := map[string]string{
		"pyproject.toml": "[project]\nname = \"f\"\ndependencies = [\"openai-agents\"]\n",
		"tools.py": `from agents import function_tool, tool_input_guardrail

@function_tool
def send_email(to: str, body: str, claim: dict) -> str:
    """Send an email."""
    return "ok"

send_email.tool_input_guardrails = [check_claim]
`,
	}
	for _, id := range []string{"OAI-034", "OAI-035", "OAI-036"} {
		if scanFires(t, files, id) {
			t.Errorf("%s fired on a tool whose claim is mediated by a tool input guardrail", id)
		}
	}
}

// A LangChain tool built with StructuredTool.from_function gets the claim_use
// fact from its wrapped function, so the unused and passthrough rungs reach it.
func TestScan_ToolCallClaims_LangChainFactory(t *testing.T) {
	manifest := "[project]\nname = \"f\"\ndependencies = [\"langchain-core\"]\n"
	tool := func(body string) map[string]string {
		return map[string]string{
			"pyproject.toml": manifest,
			"tools.py": `from langchain_core.tools import StructuredTool

def send_email(to: str, body: str, claim: dict) -> str:
    """Send an email."""
` + body + `    return "ok"

send_email_tool = StructuredTool.from_function(send_email)
`,
		}
	}
	cases := []struct {
		name  string
		files map[string]string
		fires map[string]bool
	}{
		{"unused", tool(""), map[string]bool{"LC-028": false, "LC-029": true, "LC-030": false}},
		{"passthrough", tool("    print(claim)\n"), map[string]bool{"LC-028": false, "LC-029": false, "LC-030": true}},
		{"checked", tool("    if verify_claim(claim) != \"allow\":\n        raise PermissionError(\"no\")\n"),
			map[string]bool{"LC-028": false, "LC-029": false, "LC-030": false}},
	}
	for _, tc := range cases {
		for id, want := range tc.fires {
			t.Run(tc.name+"/"+id, func(t *testing.T) {
				if got := scanFires(t, tc.files, id); got != want {
					t.Errorf("%s on a %s claim: fired=%v, want %v", id, tc.name, got, want)
				}
			})
		}
	}
}

// A CrewAI tool whose claim is read by a before_tool_call hook elsewhere in
// the repo is not an unchecked claim.
func TestScan_ToolCallClaims_CrewAIHookSilencesUnused(t *testing.T) {
	files := map[string]string{
		"pyproject.toml": "[project]\nname = \"f\"\ndependencies = [\"crewai\"]\n",
		"tools.py": `from crewai.tools import tool

@tool("Send email")
def send_email(to: str, body: str, claim: dict) -> str:
    """Send an email."""
    return "ok"
`,
		"hooks.py": `from crewai.hooks import before_tool_call

@before_tool_call
def check_claim(context):
    return verify(context.tool_input.get("claim"))
`,
	}
	for _, id := range []string{"CREW-016", "CREW-017", "CREW-018"} {
		if scanFires(t, files, id) {
			t.Errorf("%s fired on a tool whose claim is mediated by a before_tool_call hook", id)
		}
	}
}

// A Claude Agent SDK handler takes one args dict, so its claim is a key of the
// @tool input schema, read as args["claim"]. The three rungs follow the schema
// and those reads, and a PreToolUse hook silences the unused rung.
func TestScan_ToolCallClaims_Claude(t *testing.T) {
	manifest := "[project]\nname = \"f\"\ndependencies = [\"claude-agent-sdk\"]\n"
	tool := func(schema, body string) map[string]string {
		return map[string]string{
			"pyproject.toml": manifest,
			"tools.py": `from claude_agent_sdk import tool

@tool("send_email", "Send an email", ` + schema + `)
async def send_email(args):
` + body + `    return {"content": [{"type": "text", "text": mailer.send(args["to"])}]}
`,
		}
	}
	const withClaim = `{"to": str, "claim": dict}`
	cases := []struct {
		name  string
		files map[string]string
		fires map[string]bool
	}{
		{"absent", tool(`{"to": str}`, ""), map[string]bool{"CSDK-026": true, "CSDK-027": false, "CSDK-028": false}},
		{"unused", tool(withClaim, ""), map[string]bool{"CSDK-026": false, "CSDK-027": true, "CSDK-028": false}},
		{"JSON-schema form, unused", tool(`{"type": "object", "properties": {"to": {"type": "string"}, "claim": {"type": "object"}}}`, ""),
			map[string]bool{"CSDK-026": false, "CSDK-027": true, "CSDK-028": false}},
		{"passthrough", tool(withClaim, "    print(args[\"claim\"])\n"), map[string]bool{"CSDK-026": false, "CSDK-027": false, "CSDK-028": true}},
		{"checked", tool(withClaim, "    if verify_claim(args.get(\"claim\")) != \"allow\":\n        return {\"is_error\": True}\n"),
			map[string]bool{"CSDK-026": false, "CSDK-027": false, "CSDK-028": false}},
	}
	hooked := tool(withClaim, "")
	hooked["agent.py"] = `from claude_agent_sdk import ClaudeAgentOptions, HookMatcher

options = ClaudeAgentOptions(hooks={"PreToolUse": [HookMatcher(matcher="mcp__tools__send_email", hooks=[check_claim])]})
`
	cases = append(cases, struct {
		name  string
		files map[string]string
		fires map[string]bool
	}{"PreToolUse hook", hooked, map[string]bool{"CSDK-026": false, "CSDK-027": false, "CSDK-028": false}})
	for _, tc := range cases {
		for id, want := range tc.fires {
			t.Run(tc.name+"/"+id, func(t *testing.T) {
				if got := scanFires(t, tc.files, id); got != want {
					t.Errorf("%s: fired=%v, want %v", id, got, want)
				}
			})
		}
	}
}

// A TypeScript OpenAI Agents tool declares its claim as a parameters key and
// reads it in execute; an attached tool input guardrail silences the unused
// rung.
func TestScan_ToolCallClaims_TSOpenAI(t *testing.T) {
	manifest := `{"name": "f", "dependencies": {"@openai/agents": "^0.3.0", "zod": "^3.25.0"}}`
	tool := func(params, extra, execute string) map[string]string {
		return map[string]string{
			"package.json": manifest,
			"src/tools.ts": `import { tool } from "@openai/agents";
import { z } from "zod";

export const sendEmail = tool({
  name: "send_email",
  description: "Send an email.",
  parameters: z.object({ ` + params + ` }),` + extra + `
  execute: ` + execute + `,
});
`,
		}
	}
	const withClaim = `to: z.string(), claim: z.any()`
	cases := []struct {
		name  string
		files map[string]string
		fires map[string]bool
	}{
		{"absent", tool(`to: z.string()`, "", `async ({ to }) => mailer.send(to)`),
			map[string]bool{"OAI-037": true, "OAI-038": false, "OAI-039": false}},
		{"unused", tool(withClaim, "", `async ({ to, claim }) => mailer.send(to)`),
			map[string]bool{"OAI-037": false, "OAI-038": true, "OAI-039": false}},
		{"passthrough", tool(withClaim, "", `async ({ to, claim }) => { console.log(claim); return mailer.send(to); }`),
			map[string]bool{"OAI-037": false, "OAI-038": false, "OAI-039": true}},
		{"checked", tool(withClaim, "", `async ({ to, claim: c }) => { if (!verify(c)) throw new Error("no"); return mailer.send(to); }`),
			map[string]bool{"OAI-037": false, "OAI-038": false, "OAI-039": false}},
		{"input guardrail", tool(withClaim, "\n  inputGuardrails: [checkClaim],", `async ({ to, claim }) => mailer.send(to)`),
			map[string]bool{"OAI-037": false, "OAI-038": false, "OAI-039": false}},
	}
	for _, tc := range cases {
		for id, want := range tc.fires {
			t.Run(tc.name+"/"+id, func(t *testing.T) {
				if got := scanFires(t, tc.files, id); got != want {
					t.Errorf("%s: fired=%v, want %v", id, got, want)
				}
			})
		}
	}
}
