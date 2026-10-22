package scanner_test

import "testing"

// TestScan_ToolCallClaims runs the tool-call-claims ladder (OAI-034..036,
// MCP-033..035, LC-028..030) through real discovery, so a change that stops
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
