---
name: trustabl-pre-coding
description: >-
  Pre-coding reliability constraints, used for writing and reviewing agent definitions with: claude_sdk, openai_sdk
allowed-tools: Read
disable-model-invocation: false
---

# Trustabl Pre-Coding Reliability Constraints

<!-- generated: 2026-01-01 | rules: abc1234 | schema: 13 | sdks: claude_sdk, openai_sdk | template: 3 -->

Before writing any agent code, apply every constraint below. Rules are
ordered by severity. A violation here will fire the corresponding finding
in post-build scan — prevent it now.

## How to Apply These Constraints

Work against this document; do not assume a definition is correct because it
looks right. After writing or changing any tool, agent, subagent, or skill
definition, run this loop before moving on.

1. CHECK YOUR WORK
   Re-read the definition you just wrote against every constraint in this
   document whose "When this applies" matches it. Check explicitly — do not
   assume the constraint was satisfied.

2. NAME THE VIOLATION
   State the specific rule ID, not "this looks wrong". "CSDK-005 — this tool
   raises without a structured error contract" is actionable; "error handling
   needs work" is not. If nothing matches, the definition passes; move on.

3. MATCH THE REPAIR TO THE VIOLATION
   Apply that rule's own Directive. Where the repair goes is set by scope,
   and how to proceed is set by severity:

     tool   → change the tool definition
     agent  → change the agent constructor call
     repo   → change project configuration, not code

     critical / high  → make the change, then state which rule required it
     medium / low     → apply the directive directly

   If the Directive cannot be applied as written — it conflicts with another
   constraint here, or the fix is outside the file you are editing — stop and
   say so rather than approximating it.

4. KEEP A TRAIL
   Note the rule ID and the change that cleared it. Do not reintroduce a
   pattern you already repaired in this session, and do not re-apply a repair
   that did not clear the violation — report it instead.

Scope: this loop applies to agent, tool, subagent, and skill definitions —
the surfaces the constraints below govern. It is not a general code-review
procedure.

## Runtime Tracing

This section makes the agent observable at run time. It has two layers: the
agent emits OpenTelemetry spans (Step 1), and `agent-reliability-otel-labels`
adds labels to those spans (Step 2). `agent-reliability-otel-labels` never
creates a span, so Step 1 comes first. Work through the steps in order once the
agent and its tools pass the loop above.

### Step 1 — Emit spans (OpenTelemetry)

Before adding anything, look for tracing that is already set up in this repo.
If a tracer provider is already created, reuse it; never create a second one.
Still turn on the framework's instrumentation below if it is not on yet, and
run the confirm step either way.

Each language block below is followed by framework sections where a verified
setup exists. For a framework with no section, turn on its own OpenTelemetry
instrumentation, or add an OpenTelemetry-based instrumentor for it, then run
the confirm step.

#### TypeScript

Install: `npm install @opentelemetry/api @opentelemetry/sdk-trace-base @opentelemetry/context-async-hooks @opentelemetry/exporter-trace-otlp-http`

```typescript
import { context } from "@opentelemetry/api";
import { AsyncLocalStorageContextManager } from "@opentelemetry/context-async-hooks";
import { BasicTracerProvider, SimpleSpanProcessor } from "@opentelemetry/sdk-trace-base";
import { OTLPTraceExporter } from "@opentelemetry/exporter-trace-otlp-http";

context.setGlobalContextManager(new AsyncLocalStorageContextManager().enable());

const provider = new BasicTracerProvider({
  spanProcessors: [new SimpleSpanProcessor(new OTLPTraceExporter())],
});
const tracer = provider.getTracer("agent");
```

Create the provider once, at startup. For production traffic, use
`BatchSpanProcessor` in place of `SimpleSpanProcessor`.

##### OpenAI Agents SDK

Install: `npm install @arizeai/openinference-instrumentation-openai-agents`

```typescript
import { OpenInferenceTracingProcessor } from "@arizeai/openinference-instrumentation-openai-agents";
import { addTraceProcessor } from "@openai/agents";

addTraceProcessor(new OpenInferenceTracingProcessor({ tracerProvider: provider }));
```

#### Confirm

Before Step 2, run the agent once and check that it emits one tool span per
tool call, usually under an agent span. How to recognise them depends on the
instrumentation:

- `gen_ai` conventions (Google ADK, Pydantic AI, Vercel AI SDK, OpenLLMetry
  for LangChain): spans named `invoke_agent` and `execute_tool`, with
  `gen_ai.operation.name` set to match.
- OpenInference instrumentors (OpenAI Agents SDK, LangChain, CrewAI, AutoGen):
  `openinference.span.kind` is `AGENT` or `TOOL`, and the span is named after
  the agent or the tool.
- OpenLLMetry's MCP instrumentor: `traceloop.span.kind` is `tool`, with no
  agent span.

If no tool spans appear, fix that first; the labels in Step 2 have nothing to
attach to. To send spans to a collector, set `OTEL_EXPORTER_OTLP_ENDPOINT`;
the exporter reads it from the environment.

### Step 2 — Add the Trustabl labels (agent-reliability-otel-labels)

| Span | Labels |
|---|---|
| The agent's root span (`invoke_agent`, or the instrumentor's agent span) | `trustabl.run_id`, `trustabl.exit_reason`, one `trustabl.policy.binding` event per policy |
| Each tool span (`execute_tool`, or a span whose kind is tool) | `trustabl.tool.input_fp`, `trustabl.tool.output_fp`, `trustabl.tool.attempt`, `trustabl.tool.side_effect`, `trustabl.tool.error_class`, `trustabl.step_index`, `trustabl.run_id` |
| A delegated agent's span | `trustabl.handoff_id` |

Arguments and results are fingerprinted, never copied onto the span.

Policy bindings: call the bind-authority function only for a signed policy
that really exists in the deployment. Never invent an id, a version or a
sha256. With no policies, leave the call out.

If the framework's own agent span cannot be reached where the run starts, wrap
the run in a root span the application creates, and set
`gen_ai.operation.name` to `invoke_agent` and `gen_ai.agent.name` on it.

#### TypeScript

Install: `npm install @trustabl/agent-reliability-otel-labels`

```typescript
import { trace } from "@opentelemetry/api";
import { Run, canonicalFp, markRunEnd, markToolSpan } from "@trustabl/agent-reliability-otel-labels";

// Built once per run, so two runs never share counters.
function buildRun() {
  const run = new Run();
  const attempts = new Map<string, number>();

  async function searchFlights(args: { from: string; to: string; date: string }) {
    let key: string;
    try {
      key = canonicalFp(args);
    } catch {
      key = "search_flights (no fingerprint)";
    }
    const attempt = (attempts.get(key) ?? 0) + 1;
    attempts.set(key, attempt);
    const stepIndex = run.nextStep();
    const span = trace.getActiveSpan(); // the framework's tool span

    let result: unknown;
    try {
      result = await findFlights(args);
      return result;
    } finally {
      try {
        markToolSpan(span, {
          args, result, attempt, sideEffect: "read",
          stepIndex, runId: run.runId, name: "search_flights",
        });
      } catch (err) {
        console.error(`trustabl: could not label the tool span: ${err}`);
      }
    }
  }

  return { run, searchFlights };
}

async function runAgent(prompt: string) {
  const { run, searchFlights } = buildRun();
  return tracer.startActiveSpan("travel-booker", async (root) => {
    root.setAttribute("gen_ai.operation.name", "invoke_agent");
    root.setAttribute("gen_ai.agent.name", "travel-booker");
    run.markStart(root);
    try {
      const answer = await callTheAgent(prompt, { searchFlights });
      markRunEnd(root, "final_answer");
      return answer;
    } catch (err) {
      markRunEnd(root, "error");
      throw err;
    } finally {
      root.end();
    }
  });
}
```

When the failure class is known, also pass `errorClass`, for example
`errorClass: "timeout"`. For a delegated agent, call
`markHandoff(span, handoffId)` on the child agent's span. For signed policies,
call `bindAuthority(root, bindings)` right after `run.markStart(root)`.

### Step 3 — Choose the values

- `trustabl.tool.attempt`: how many times this run has made the same call.
  Keep a count per run, keyed by the arguments fingerprint. Different
  arguments start again at 1.
- `trustabl.tool.side_effect`: one of `pure`, `read`, `write`, `irreversible`.
  `pure` means no I/O and the same input gives the same output. `read` means
  it reads outside state and changes nothing. `write` means it changes outside
  state in a way that can be undone. `irreversible` means it cannot be undone,
  such as a send, a payment or a delete. Leave it out when the class is not
  known; `pure` is a claim, not a default.
- `trustabl.tool.error_class`: one of `timeout`, `4xx`, `5xx`, `empty`, `schema`, `auth`.
  Set it only when the failure class is known. Leave it out rather than guess.
- `trustabl.exit_reason`: one of `final_answer`, `max_steps`, `policy_stop`, `error`.
  Use `error` when the run raised, `max_steps` when a step cap stopped a run
  that still wanted tools, `policy_stop` when a policy halted it, and
  `final_answer` otherwise.

### Step 4 — Check before moving on

- The labels are on the framework's own spans. Export one run and look for
  `trustabl.tool.input_fp` on each tool span. With no current span, each call
  is a silent no-op.
- No tool label, such as `trustabl.tool.input_fp`, appears on the root span or
  on any span that is not a tool span. If one does, the tool's span was not
  current inside the tool: say so, and do not create a span to hold the labels.
- A tool that fails is still labelled: label in a `finally` block or a `defer`.
- A labelling error never fails the tool: catch it, report it, and return the
  tool's own result.
- The run's end is marked before the root span ends; an ended span drops it.
- No argument or result text appears in any `trustabl.*` value. The
  framework's own content capture is a separate setting.

TypeScript:

- Register a context manager before anything else. Without one there is no
  current span inside a tool, and each label call does nothing.
- Build the `Run` and the attempt counts once per run, inside one function.

- OpenAI Agents SDK: its spans arrive through a bridge and may not be current
  while a tool runs. `trace.getActiveSpan()` inside the tool is then the
  application's root span, and the tool labels would land there. Check where
  they landed before relying on them.

---

## Claude Agent SDK

### Tool Rules

---

#### [CSDK-001] Claude subagent tool function has no docstring
**Severity:** medium | **Confidence:** 0.90

**Directive:** Add a docstring describing what the tool does, its parameters, and return value.

**Why:** Missing docstring means the model cannot read the tool's intent.

**When this applies:** When defining a tool.

### Agent Rules

---

#### [CSDK-101] Claude subagent is granted the Bash tool without restrictions
**Severity:** high | **Confidence:** 0.80

**Directive:** Add input guardrails or restrict tool grants to exact prefixes.

**Why:** An agent with unrestricted Bash access poses a high risk.

**When this applies:** When declaring an agent.

---

## OpenAI Agents SDK

### Tool Rules

---

#### [OAI-001] Tool function has no docstring
**Severity:** low | **Confidence:** 0.90

**Directive:** Add a docstring to every @function_tool decorated function.

**Why:** The docstring is the model-facing description; without it the model cannot use the tool correctly.

**When this applies:** When defining a tool.

