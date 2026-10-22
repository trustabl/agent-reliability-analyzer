package analysis_test

import (
	"testing"

	"github.com/trustabl/trustabl/internal/analysis"
	"github.com/trustabl/trustabl/internal/models"
)

// claimUseOf discovers the single tool in src and returns its claim_use fact
// ("" when the fact is not stamped).
func claimUseOf(t *testing.T, src string) string {
	t.Helper()
	tools := analysis.DiscoverToolsFromParsed([]analysis.ParsedFile{parsePyFile(t, "tools.py", src)})
	if len(tools) != 1 {
		t.Fatalf("expected 1 tool, got %d: %+v", len(tools), tools)
	}
	return tools[0].Facts[models.FactClaimUse]
}

func TestClaimUse(t *testing.T) {
	cases := []struct {
		name string
		src  string
		want string
	}{
		{
			name: "no claim param: fact not stamped",
			src: `
from agents import function_tool

@function_tool
def send_email(to: str, body: str) -> str:
    return "ok"
`,
			want: "",
		},
		{
			name: "claim declared, never referenced",
			src: `
from agents import function_tool

@function_tool
def send_email(to: str, body: str, claim: dict) -> str:
    return "ok"
`,
			want: models.ClaimUseUnused,
		},
		{
			name: "claim only printed",
			src: `
from agents import function_tool

@function_tool
def send_email(to: str, body: str, claim: dict) -> str:
    print(claim)
    return "ok"
`,
			want: models.ClaimUsePassthrough,
		},
		{
			name: "claim only logged and returned",
			src: `
import json, logging
from agents import function_tool
logger = logging.getLogger(__name__)

@function_tool
def send_email(to: str, body: str, claim: dict) -> str:
    logger.info("claim %s", json.dumps(claim))
    logging.debug(f"claim={claim}")
    return f"sent with {claim}"
`,
			want: models.ClaimUsePassthrough,
		},
		{
			name: "claim passed to a verifier",
			src: `
from agents import function_tool

@function_tool
def send_email(to: str, body: str, claim: dict) -> str:
    decision = verify_claim(claim, tool="send_email", args={"to": to})
    if decision != "allow":
        raise PermissionError("claim does not match route")
    return "ok"
`,
			want: models.ClaimUseChecked,
		},
		{
			name: "claim field gates the call",
			src: `
from agents import function_tool

@function_tool
def send_email(to: str, body: str, claim: dict) -> str:
    if claim["selected_tool"] != "send_email":
        raise PermissionError("claim does not match route")
    return "ok"
`,
			want: models.ClaimUseChecked,
		},
		{
			name: "verifier result logged: the inner call decides",
			src: `
import logging
from agents import function_tool

@function_tool
def send_email(to: str, body: str, claim: dict) -> str:
    logging.info("decision %s", mediate(claim))
    return "ok"
`,
			want: models.ClaimUseChecked,
		},
		{
			name: "extra decorator: a wrapper may mediate",
			src: `
from agents import function_tool

@function_tool
@mediated
def send_email(to: str, body: str, claim: dict) -> str:
    return "ok"
`,
			want: models.ClaimUseWrapped,
		},
		{
			name: "OpenAI tool input guardrail attached by assignment",
			src: `
from agents import function_tool, tool_input_guardrail

@function_tool
def send_email(to: str, claim: dict) -> str:
    return "ok"

send_email.tool_input_guardrails = [check_claim]
`,
			want: models.ClaimUseWrapped,
		},
		{
			name: "OpenAI tool input guardrail attached by decorator kwarg",
			src: `
from agents import function_tool

@function_tool(tool_input_guardrails=[check_claim])
def send_email(to: str, claim: dict) -> str:
    print(claim)
    return "ok"
`,
			want: models.ClaimUseWrapped,
		},
		{
			name: "an output guardrail does not mediate the call",
			src: `
from agents import function_tool

@function_tool
def send_email(to: str, claim: dict) -> str:
    return "ok"

send_email.tool_output_guardrails = [check_output]
`,
			want: models.ClaimUseUnused,
		},
		{
			name: "MCP tool, claim unused",
			src: `
from mcp.server.fastmcp import FastMCP
mcp = FastMCP("x")

@mcp.tool()
def delete_file(path: str, claim: dict) -> str:
    return "ok"
`,
			want: models.ClaimUseUnused,
		},
		{
			name: "a local named claim_id is not a reference",
			src: `
from agents import function_tool

@function_tool
def send_email(to: str, claim: dict) -> str:
    claim_id = "x"
    return claim_id
`,
			want: models.ClaimUseUnused,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := claimUseOf(t, tc.src); got != tc.want {
				t.Errorf("claim_use = %q, want %q", got, tc.want)
			}
		})
	}
}

// claimUseOfLangChainFactory discovers the single factory-built LangChain tool
// in src and returns its claim_use fact ("" when the fact is not stamped).
func claimUseOfLangChainFactory(t *testing.T, src string) string {
	t.Helper()
	tools := analysis.DiscoverLangChainTools([]analysis.ParsedFile{parsePyFile(t, "tools.py", src)})
	if len(tools) != 1 {
		t.Fatalf("expected 1 tool, got %d: %+v", len(tools), tools)
	}
	return tools[0].Facts[models.FactClaimUse]
}

func TestClaimUse_LangChainFactory(t *testing.T) {
	cases := []struct {
		name string
		src  string
		want string
	}{
		{
			name: "from_function, claim never read",
			src: `
from langchain_core.tools import StructuredTool

def send_email(to: str, claim: dict) -> str:
    """Send an email."""
    return "ok"

send_email_tool = StructuredTool.from_function(send_email)
`,
			want: models.ClaimUseUnused,
		},
		{
			name: "from_function, claim only printed",
			src: `
from langchain_core.tools import StructuredTool

def send_email(to: str, claim: dict) -> str:
    """Send an email."""
    print(claim)
    return "ok"

send_email_tool = StructuredTool.from_function(send_email)
`,
			want: models.ClaimUsePassthrough,
		},
		{
			name: "from_function, claim checked",
			src: `
from langchain_core.tools import StructuredTool

def send_email(to: str, claim: dict) -> str:
    """Send an email."""
    if verify_claim(claim) != "allow":
        raise PermissionError("claim does not match route")
    return "ok"

send_email_tool = StructuredTool.from_function(send_email)
`,
			want: models.ClaimUseChecked,
		},
		{
			name: "constructor with func=, claim never read",
			src: `
from langchain_core.tools import StructuredTool

def send_email(to: str, claim: dict) -> str:
    """Send an email."""
    return "ok"

send_email_tool = StructuredTool(func=send_email, name="send_email", description="Send an email.")
`,
			want: models.ClaimUseUnused,
		},
		{
			// No decorator registers a factory-built tool, so any decorator on
			// the wrapped function may check the claim.
			name: "from_function over a decorated function: wrapped",
			src: `
from langchain_core.tools import StructuredTool

@mediated
def send_email(to: str, claim: dict) -> str:
    """Send an email."""
    return "ok"

send_email_tool = StructuredTool.from_function(send_email)
`,
			want: models.ClaimUseWrapped,
		},
		{
			name: "from_function, no claim param: fact not stamped",
			src: `
from langchain_core.tools import StructuredTool

def send_email(to: str) -> str:
    """Send an email."""
    return "ok"

send_email_tool = StructuredTool.from_function(send_email)
`,
			want: "",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := claimUseOfLangChainFactory(t, tc.src); got != tc.want {
				t.Errorf("claim_use = %q, want %q", got, tc.want)
			}
		})
	}
}
