package analysis_test

import (
	"testing"

	"github.com/trustabl/trustabl/internal/analysis"
	"github.com/trustabl/trustabl/internal/models"
)

func TestDiscoverAgentRunCalls_OpenAI_CapturesMaxTurns(t *testing.T) {
	src := `from agents import Agent, Runner

agent = Agent(name="x")

async def main():
    result = await Runner.run(agent, "hi", max_turns=5)
`
	calls := analysis.DiscoverAgentRunCalls([]analysis.ParsedFile{parsePyFile(t, "main.py", src)})
	if len(calls) != 1 {
		t.Fatalf("expected 1 run call, got %d: %+v", len(calls), calls)
	}
	rc := calls[0]
	if rc.SDK != models.SDKOpenAIAgents {
		t.Errorf("SDK = %q, want %q", rc.SDK, models.SDKOpenAIAgents)
	}
	if rc.AgentVarName != "agent" {
		t.Errorf("AgentVarName = %q, want %q", rc.AgentVarName, "agent")
	}
	if rc.Kwargs == nil || rc.Kwargs.Children["max_turns"] == nil {
		t.Fatalf("max_turns kwarg not captured: %+v", rc)
	}
	if rc.FilePath != "main.py" || rc.Line == 0 {
		t.Errorf("unexpected location: %+v", rc.Location)
	}
}

func TestDiscoverAgentRunCalls_OpenAI_SilentWhenMaxTurnsAbsent(t *testing.T) {
	src := `from agents import Agent, Runner

agent = Agent(name="x")

async def main():
    result = await Runner.run(agent, "hi")
`
	calls := analysis.DiscoverAgentRunCalls([]analysis.ParsedFile{parsePyFile(t, "main.py", src)})
	if len(calls) != 1 {
		t.Fatalf("expected 1 run call, got %d", len(calls))
	}
	if calls[0].Kwargs != nil && calls[0].Kwargs.Children["max_turns"] != nil {
		t.Errorf("expected no max_turns kwarg, got %+v", calls[0].Kwargs)
	}
}

func TestDiscoverAgentRunCalls_OpenAI_RunSyncAndRunStreamed(t *testing.T) {
	src := `from agents import Agent, Runner

agent = Agent(name="x")
result = Runner.run_sync(agent, "hi", max_turns=3)
streamed = Runner.run_streamed(agent, "hi")
`
	calls := analysis.DiscoverAgentRunCalls([]analysis.ParsedFile{parsePyFile(t, "main.py", src)})
	if len(calls) != 2 {
		t.Fatalf("expected 2 run calls, got %d: %+v", len(calls), calls)
	}
}

func TestDiscoverAgentRunCalls_OpenAI_UnrelatedRunnerClassNotMatched(t *testing.T) {
	// TaskRunner.run(...) must not match: the object segment is "TaskRunner",
	// not "Runner" — a suffix check on the whole callee text would wrongly
	// match this (isRunnerObject guards against exactly this).
	src := `from agents import Agent

agent = Agent(name="x")

class TaskRunner:
    pass

TaskRunner.run(agent, "hi", max_turns=1)
`
	calls := analysis.DiscoverAgentRunCalls([]analysis.ParsedFile{parsePyFile(t, "main.py", src)})
	if len(calls) != 0 {
		t.Fatalf("expected 0 run calls (TaskRunner is not Runner), got %d: %+v", len(calls), calls)
	}
}

func TestDiscoverAgentRunCalls_OpenAI_SilentWhenFirstArgNotIdentifier(t *testing.T) {
	src := `from agents import Agent, Runner

async def main():
    result = await Runner.run(get_agent(), "hi", max_turns=5)
`
	calls := analysis.DiscoverAgentRunCalls([]analysis.ParsedFile{parsePyFile(t, "main.py", src)})
	if len(calls) != 0 {
		t.Fatalf("expected 0 run calls (non-identifier first arg), got %d: %+v", len(calls), calls)
	}
}

func TestDiscoverAgentRunCalls_PydanticAI_CapturesUsageLimits(t *testing.T) {
	src := `from pydantic_ai import Agent
from pydantic_ai.usage import UsageLimits

agent = Agent("openai:gpt-4o")

async def main():
    result = await agent.run("hi", usage_limits=UsageLimits(request_limit=5))
`
	calls := analysis.DiscoverAgentRunCalls([]analysis.ParsedFile{parsePyFile(t, "main.py", src)})
	if len(calls) != 1 {
		t.Fatalf("expected 1 run call, got %d: %+v", len(calls), calls)
	}
	rc := calls[0]
	if rc.SDK != models.SDKPydanticAI {
		t.Errorf("SDK = %q, want %q", rc.SDK, models.SDKPydanticAI)
	}
	if rc.AgentVarName != "agent" {
		t.Errorf("AgentVarName = %q, want %q", rc.AgentVarName, "agent")
	}
	if rc.Kwargs == nil || rc.Kwargs.Children["usage_limits"] == nil {
		t.Fatalf("usage_limits kwarg not captured: %+v", rc)
	}
	ul := rc.Kwargs.Children["usage_limits"]
	if ul.Value == nil || ul.Value.Kind != models.ExprCall {
		t.Fatalf("usage_limits value not a call expr: %+v", ul.Value)
	}
}

func TestDiscoverAgentRunCalls_PydanticAI_SilentWhenUsageLimitsAbsent(t *testing.T) {
	src := `from pydantic_ai import Agent

agent = Agent("openai:gpt-4o")

async def main():
    result = await agent.run("hi")
`
	calls := analysis.DiscoverAgentRunCalls([]analysis.ParsedFile{parsePyFile(t, "main.py", src)})
	if len(calls) != 1 {
		t.Fatalf("expected 1 run call, got %d", len(calls))
	}
	if calls[0].Kwargs != nil && calls[0].Kwargs.Children["usage_limits"] != nil {
		t.Errorf("expected no usage_limits kwarg, got %+v", calls[0].Kwargs)
	}
}

func TestDiscoverAgentRunCalls_PydanticAI_RunSyncAndRunStream(t *testing.T) {
	src := `from pydantic_ai import Agent

agent = Agent("openai:gpt-4o")
result = agent.run_sync("hi")
with agent.run_stream("hi") as stream:
    pass
`
	calls := analysis.DiscoverAgentRunCalls([]analysis.ParsedFile{parsePyFile(t, "main.py", src)})
	if len(calls) != 2 {
		t.Fatalf("expected 2 run calls, got %d: %+v", len(calls), calls)
	}
}

func TestDiscoverAgentRunCalls_PydanticAI_SilentWhenReceiverNotIdentifier(t *testing.T) {
	src := `from pydantic_ai import Agent

class Service:
    def __init__(self):
        self.agent = Agent("openai:gpt-4o")

    async def go(self):
        return await self.agent.run("hi")
`
	calls := analysis.DiscoverAgentRunCalls([]analysis.ParsedFile{parsePyFile(t, "main.py", src)})
	if len(calls) != 0 {
		t.Fatalf("expected 0 run calls (self.agent receiver is not a plain identifier), got %d: %+v", len(calls), calls)
	}
}

func TestDiscoverAgentRunCalls_SilentWhenNeitherSDKImported(t *testing.T) {
	src := `class Db:
    def run(self, query):
        return query

db = Db()
db.run("select 1")
`
	calls := analysis.DiscoverAgentRunCalls([]analysis.ParsedFile{parsePyFile(t, "main.py", src)})
	if len(calls) != 0 {
		t.Fatalf("expected 0 run calls (file imports neither SDK), got %d: %+v", len(calls), calls)
	}
}

// ─── WallClockTimeoutWrapped (structural ancestor walk) ─────────────────────

func wallClockOf(t *testing.T, src string) bool {
	t.Helper()
	calls := analysis.DiscoverAgentRunCalls([]analysis.ParsedFile{parsePyFile(t, "main.py", src)})
	if len(calls) != 1 {
		t.Fatalf("expected 1 run call, got %d: %+v", len(calls), calls)
	}
	return calls[0].WallClockTimeoutWrapped
}

func TestDiscoverAgentRunCalls_WallClock_WaitForWrap(t *testing.T) {
	src := `import asyncio
from agents import Agent, Runner

agent = Agent(name="x")

async def main():
    return await asyncio.wait_for(Runner.run(agent, "hi"), timeout=30)
`
	if !wallClockOf(t, src) {
		t.Error("asyncio.wait_for(Runner.run(...)) should be wall-clock wrapped")
	}
}

func TestDiscoverAgentRunCalls_WallClock_AsyncioTimeoutContext(t *testing.T) {
	src := `import asyncio
from agents import Agent, Runner

agent = Agent(name="x")

async def main():
    async with asyncio.timeout(30):
        return await Runner.run(agent, "hi")
`
	if !wallClockOf(t, src) {
		t.Error("run call inside `async with asyncio.timeout(...)` should be wrapped")
	}
}

func TestDiscoverAgentRunCalls_WallClock_AnyioMoveOnAfterAs(t *testing.T) {
	src := `import anyio
from agents import Agent, Runner

agent = Agent(name="x")

async def main():
    with anyio.move_on_after(30) as scope:
        return await Runner.run(agent, "hi")
`
	if !wallClockOf(t, src) {
		t.Error("run call inside `with anyio.move_on_after(...) as scope` should be wrapped")
	}
}

func TestDiscoverAgentRunCalls_WallClock_Unwrapped(t *testing.T) {
	src := `from agents import Agent, Runner

agent = Agent(name="x")

async def main():
    return await Runner.run(agent, "hi")
`
	if wallClockOf(t, src) {
		t.Error("an unwrapped run call must not be credited")
	}
}

func TestDiscoverAgentRunCalls_WallClock_UnrelatedTimeoutNotCredited(t *testing.T) {
	// A timeout block and a wait_for exist in the file but neither ENCLOSES the
	// run call — a same-file text search would wrongly credit it.
	src := `import asyncio
from agents import Agent, Runner

agent = Agent(name="x")

async def other():
    async with asyncio.timeout(5):
        await asyncio.sleep(1)
    await asyncio.wait_for(asyncio.sleep(1), timeout=2)

async def main():
    return await Runner.run(agent, "hi")
`
	if wallClockOf(t, src) {
		t.Error("an unrelated timeout elsewhere in the file must not be credited")
	}
}

func TestDiscoverAgentRunCalls_WallClock_NotCreditedWhenOnlyInWithHeader(t *testing.T) {
	// The run call is in the with-item expression, not the bounded body.
	src := `import asyncio
from agents import Agent, Runner

agent = Agent(name="x")

async def main():
    async with asyncio.timeout(Runner.run(agent, "hi")):
        pass
`
	if wallClockOf(t, src) {
		t.Error("a call in the context-manager expression is not bounded by that manager")
	}
}

func TestDiscoverAgentRunCalls_WallClock_Pydantic(t *testing.T) {
	wrapped := `import asyncio
from pydantic_ai import Agent

agent = Agent("openai:gpt-4o")

async def main():
    return await asyncio.wait_for(agent.run("hi"), timeout=30)
`
	bare := `from pydantic_ai import Agent

agent = Agent("openai:gpt-4o")

async def main():
    return await agent.run("hi")
`
	if !wallClockOf(t, wrapped) {
		t.Error("Pydantic agent.run inside asyncio.wait_for should be wrapped")
	}
	if wallClockOf(t, bare) {
		t.Error("bare Pydantic agent.run must not be credited")
	}
}

// ─── Google ADK / AutoGen run-call discovery ──────────────────────────────

func adkRunCalls(t *testing.T, src string) []models.AgentRunCallDef {
	t.Helper()
	return analysis.DiscoverAgentRunCalls([]analysis.ParsedFile{parsePyFile(t, "main.py", src)})
}

func TestDiscoverAgentRunCalls_ADK_RunnerKwargResolvesAgent(t *testing.T) {
	calls := adkRunCalls(t, `from google.adk.agents import LlmAgent
from google.adk.runners import Runner

root = LlmAgent(name="r", model="gemini-2.0-flash")
runner = Runner(agent=root, app_name="a", session_service=svc)

async def main():
    async for ev in runner.run_async(user_id="u", session_id="s", new_message=msg):
        pass
`)
	if len(calls) != 1 {
		t.Fatalf("expected 1 run call, got %d: %+v", len(calls), calls)
	}
	if calls[0].SDK != models.SDKGoogleADK || calls[0].AgentVarName != "root" {
		t.Errorf("got %+v, want ADK call for agent root", calls[0])
	}
	if calls[0].WallClockTimeoutWrapped {
		t.Error("unwrapped run_async must not be wall-clock wrapped")
	}
}

func TestDiscoverAgentRunCalls_ADK_InMemoryRunnerPositionalAndSyncRun(t *testing.T) {
	calls := adkRunCalls(t, `from google.adk.agents import Agent
from google.adk.runners import InMemoryRunner

root = Agent(name="r", model="m")
runner = InMemoryRunner(root)
for ev in runner.run(user_id="u", session_id="s", new_message=msg):
    pass
`)
	if len(calls) != 1 || calls[0].AgentVarName != "root" {
		t.Fatalf("expected 1 call for root, got %+v", calls)
	}
}

func TestDiscoverAgentRunCalls_ADK_InlineRunnerReceiver(t *testing.T) {
	calls := adkRunCalls(t, `from google.adk.agents import LlmAgent
from google.adk.runners import Runner

root = LlmAgent(name="r", model="m")

async def main():
    async for ev in Runner(agent=root, app_name="a", session_service=s).run_async(user_id="u", session_id="s"):
        pass
`)
	if len(calls) != 1 || calls[0].AgentVarName != "root" {
		t.Fatalf("expected 1 call for root, got %+v", calls)
	}
}

func TestDiscoverAgentRunCalls_ADK_WrappedInAsyncioTimeout(t *testing.T) {
	calls := adkRunCalls(t, `import asyncio
from google.adk.agents import LlmAgent
from google.adk.runners import Runner

root = LlmAgent(name="r", model="m")
runner = Runner(agent=root, app_name="a", session_service=s)

async def main():
    async with asyncio.timeout(30):
        async for ev in runner.run_async(user_id="u", session_id="s"):
            pass
`)
	if len(calls) != 1 || !calls[0].WallClockTimeoutWrapped {
		t.Fatalf("run_async inside asyncio.timeout should be wrapped, got %+v", calls)
	}
}

func TestDiscoverAgentRunCalls_ADK_CapturesAbortSignal(t *testing.T) {
	calls := adkRunCalls(t, `from google.adk.agents import LlmAgent
from google.adk.runners import Runner

root = LlmAgent(name="r", model="m")
runner = Runner(agent=root, app_name="a", session_service=s)

async def main():
    async for ev in runner.run_async(user_id="u", session_id="s", abort_signal=stop):
        pass
`)
	if len(calls) != 1 || calls[0].Kwargs == nil || calls[0].Kwargs.Children["abort_signal"] == nil {
		t.Fatalf("abort_signal kwarg not captured: %+v", calls)
	}
}

func TestDiscoverAgentRunCalls_ADK_SilentCases(t *testing.T) {
	cases := map[string]string{
		"app-based runner is unresolved": `from google.adk.agents import LlmAgent
from google.adk.runners import Runner
runner = Runner(app=my_app, session_service=s)
async def main():
    async for ev in runner.run_async(user_id="u", session_id="s"):
        pass
`,
		"run_live is not a run call": `from google.adk.agents import LlmAgent
from google.adk.runners import Runner
root = LlmAgent(name="r", model="m")
runner = Runner(agent=root, app_name="a", session_service=s)
async def main():
    async for ev in runner.run_live(live_request_queue=q):
        pass
`,
		"file does not import ADK": `from other import Runner
root = make()
runner = Runner(agent=root)
runner.run(x=1)
`,
	}
	for name, src := range cases {
		if calls := adkRunCalls(t, src); len(calls) != 0 {
			t.Errorf("%s: expected no run calls, got %+v", name, calls)
		}
	}
}

func TestDiscoverAgentRunCalls_AutoGen_InitiateChatYieldsReceiverAndRecipient(t *testing.T) {
	calls := adkRunCalls(t, `from autogen import ConversableAgent

assistant = ConversableAgent(name="a")
user_proxy = ConversableAgent(name="u")
user_proxy.initiate_chat(assistant, message="hi", max_turns=2)
`)
	if len(calls) != 2 {
		t.Fatalf("expected 2 records (receiver + recipient), got %d: %+v", len(calls), calls)
	}
	got := map[string]bool{}
	for _, c := range calls {
		if c.SDK != models.SDKAutoGen {
			t.Errorf("SDK = %q, want autogen", c.SDK)
		}
		got[c.AgentVarName] = true
	}
	if !got["assistant"] || !got["user_proxy"] {
		t.Errorf("want records for assistant and user_proxy, got %v", got)
	}
}

func TestDiscoverAgentRunCalls_AutoGen_RecipientKwargAndASelfChat(t *testing.T) {
	calls := adkRunCalls(t, `from autogen import ConversableAgent
a = ConversableAgent(name="a")
b = ConversableAgent(name="b")
a.initiate_chat(recipient=b, message="x")
a.initiate_chat(a, message="y")
`)
	if len(calls) != 3 { // a+b for the first call, a once for the self-chat
		t.Fatalf("expected 3 records, got %d: %+v", len(calls), calls)
	}
}

func TestDiscoverAgentRunCalls_AutoGen_V04RunAndWait(t *testing.T) {
	calls := adkRunCalls(t, `import asyncio
from autogen_agentchat.agents import AssistantAgent

agent = AssistantAgent("a", model_client=mc)

async def main():
    r1 = await agent.run(task="t")
    r2 = await asyncio.wait_for(Console(agent.run_stream(task="t")), timeout=30)
`)
	if len(calls) != 2 {
		t.Fatalf("expected 2 run calls, got %d: %+v", len(calls), calls)
	}
	if calls[0].WallClockTimeoutWrapped || !calls[1].WallClockTimeoutWrapped {
		t.Errorf("want [unwrapped, wrapped], got [%v, %v]", calls[0].WallClockTimeoutWrapped, calls[1].WallClockTimeoutWrapped)
	}
}

func TestDiscoverAgentRunCalls_AutoGen_SilentWithoutImport(t *testing.T) {
	if calls := adkRunCalls(t, "def f(agent, other):\n    agent.initiate_chat(other, message='x')\n"); len(calls) != 0 {
		t.Errorf("expected no run calls without an autogen import, got %+v", calls)
	}
}
