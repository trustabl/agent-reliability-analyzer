package analysis_test

import (
	"testing"

	"github.com/trustabl/trustabl/internal/models"
)

const adkTool = `from google.adk.tools import FunctionTool

def send_email(to: str, claim: dict) -> str:
    """Send an email."""
    return "ok"

send_email_tool = FunctionTool(send_email)
`

const autogenRegistered = `from autogen import register_function

def send_email(to: str, claim: dict) -> str:
    """Send an email."""
    print(claim)
    return "ok"

register_function(send_email, caller=assistant, executor=user_proxy, description="Send an email.")
`

func TestClaimUse_ADKAndAutoGen(t *testing.T) {
	cases := []struct {
		name  string
		files map[string]string
		want  string
	}{
		{"ADK FunctionTool, claim never read", map[string]string{"tools.py": adkTool}, models.ClaimUseUnused},
		{"ADK FunctionTool, claim checked", map[string]string{"tools.py": `from google.adk.tools import FunctionTool

def send_email(to: str, claim: dict) -> str:
    """Send an email."""
    if verify(claim) != "allow":
        raise PermissionError("no")
    return "ok"

send_email_tool = FunctionTool(send_email)
`}, models.ClaimUseChecked},
		{"AutoGen register_function, claim only printed", map[string]string{"tools.py": autogenRegistered}, models.ClaimUsePassthrough},
		// The two registering decorators are not mediators.
		{"AutoGen stacked registering decorators, claim never read", map[string]string{"tools.py": `from autogen import ConversableAgent

@user_proxy.register_for_execution()
@assistant.register_for_llm(description="Send an email.")
def send_email(to: str, claim: dict) -> str:
    return "ok"
`}, models.ClaimUseUnused},
		{"AutoGen registering decorator plus another decorator", map[string]string{"tools.py": `from autogen import ConversableAgent

@user_proxy.register_for_execution()
@assistant.register_for_llm(description="Send an email.")
@check_claim
def send_email(to: str, claim: dict) -> str:
    return "ok"
`}, models.ClaimUseWrapped},
		{"ADK before_tool_callback on an agent", map[string]string{
			"tools.py": adkTool,
			"agent.py": `from google.adk.agents import LlmAgent

root_agent = LlmAgent(name="mailer", model="gemini-2.5-flash", tools=[send_email_tool], before_tool_callback=check_claim)
`}, models.ClaimUseWrapped},
		{"ADK plugin before_tool_callback", map[string]string{
			"tools.py": adkTool,
			"plugins.py": `from google.adk.plugins.base_plugin import BasePlugin

class ClaimPlugin(BasePlugin):
    async def before_tool_callback(self, *, tool, tool_args, tool_context):
        return None
`}, models.ClaimUseWrapped},
		{"ADK agent with only after_tool_callback: unchanged", map[string]string{
			"tools.py": adkTool,
			"agent.py": `from google.adk.agents import Agent

root_agent = Agent(name="mailer", tools=[send_email_tool], after_tool_callback=log_result)
`}, models.ClaimUseUnused},
		// An ADK hook does not reach AutoGen tools (Review Focus 5).
		{"ADK hook does not mediate an AutoGen tool", map[string]string{
			"tools.py": autogenRegistered,
			"agent.py": `from google.adk.agents import LlmAgent

root_agent = LlmAgent(name="x", before_tool_callback=check_claim)
`}, models.ClaimUsePassthrough},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := mediatedClaimUse(t, tc.files, "send_email"); got != tc.want {
				t.Errorf("claim_use = %q, want %q", got, tc.want)
			}
		})
	}
}
