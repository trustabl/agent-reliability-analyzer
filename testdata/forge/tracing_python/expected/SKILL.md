---
name: trustabl-pre-coding
description: >-
  Pre-coding reliability constraints, used for writing and reviewing agent definitions with: claude_sdk, openai_sdk
allowed-tools: Read
disable-model-invocation: false
---

# Trustabl Pre-Coding Reliability Constraints

<!-- generated: 2026-01-01 | rules: abc1234 | schema: 13 | sdks: claude_sdk, openai_sdk | template: 4 -->

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

## Tool-Call Claims

This section binds every tool call to a typed action claim and a check that
runs before the call has any effect. It follows Explanation-Bound Tool
Execution (EBTE, arXiv 2607.25364): the decision-relevant part of the model's
reasoning becomes a small typed object, and code outside the model compares
that object with facts the model cannot change.

A claim is untrusted input. It never grants authority, never widens what the
agent is already allowed to do, and never replaces an existing control. The
deterministic controls (allow and deny lists, schema validation, sandbox and
network policy, approval gates) stay the floor; the claim check can only add a
reason to stop.

### The claim

Every tool the agent can call takes one extra argument, `claim`, typed as
`ToolInvocationExplanation`. Keep the enforced fields small, typed and
categorical. Free text is advisory only and no check reads it.

```
ToolInvocationExplanation
  schema_version     "ebte-0.1"
  intent_class       one value from a closed enum the application defines
                     (for example: read, search, create, update, delete, send)
  selected_tool      the tool's registered name
  expected_effect
    operation        one value from a closed enum (read, create, update, delete, send, execute)
    resource_type    a closed enum value, or null
    resource_ids     bounded list of identifiers, or empty
    max_items        integer upper bound on items touched
    destination      allowed destination identifier, or null
  risk_tier          low | medium | high
  uncertainty        low | medium | high
  untrusted_context  list of { source, digest } for each untrusted input the
                     call depends on (tool output, fetched page, file, email)
  evidence_refs      list of { type: intent | policy | payload, digest }
  summary            optional short text; advisory, never checked
```

Rules for the schema:

- Use enums and bounded lists, not prose. A field a check cannot compare is
  not an enforced field.
- `selected_tool` must equal the tool actually being called.
- `untrusted_context` must list every untrusted input the call depends on. An
  empty list is a claim too, and it is checked.
- Python uses the names above; TypeScript and Go may use camelCase, as long as
  one spelling is used across the application.

### The mediation step

Put one mediation step between the model's tool call and the tool's effect.
It is a function the model cannot call or skip, and it runs on every call.

1. Take `claim` out of the arguments, so the tool body never sees it.
2. Validate the claim against the schema. A missing or malformed claim is a
   deny.
3. Resolve the facts from the server side, never from the claim or the
   conversation: the tool registry entry, the task's authorized intent, the
   current policy, the route the call is allowed to take, and the context-risk
   state for this run.
4. Compare. Each check gives allow, review or deny:
   - `selected_tool` differs from the called tool, or the tool is not on the
     authorized route → deny.
   - `intent_class` or `expected_effect.operation` is outside the authorized
     intent → deny.
   - The arguments go beyond `expected_effect` (more items, another
     destination, other resources), or the claim goes beyond the intent → deny.
     Required order: arguments ≤ claim ≤ authorized intent.
   - `untrusted_context` leaves out an untrusted input the run did use → deny.
   - A digest in `evidence_refs` is stale or does not match → deny.
   - A fact cannot be resolved → review. Unknown is never a match.
   - `uncertainty` is high, or `risk_tier` is higher than the server's risk
     for the tool → review. A declared risk can raise the server's risk, never
     lower it.
   - The tool is high risk on the server side → review, even when every
     check matches.
5. Combine: the result is the most restrictive outcome of every check, then
   the most restrictive of that result and the existing authorization and
   approval controls. Deny beats review, review beats allow.
6. Act on it. Only allow runs the tool. Review runs nothing: it returns a
   draft or a confirmation request through the SDK's approval mechanism, and
   an approval is a new decision that re-resolves the facts and runs the
   checks again. Deny returns an error to the model.
7. Tell the model only a coarse reason (for example "claim does not match
   route"). Keep the per-check detail in the audit trail, so the reasons
   cannot be used to probe the checks.

### Where the step goes

Put the step where the SDK gives control before a tool runs. When the SDK has
no such hook, put it in one shared wrapper that every tool goes through, as
the first thing it does — never copied into each tool by hand.

#### Claude Agent SDK

Run the mediation step in a `PreToolUse` hook (or the `canUseTool` /
`can_use_tool` callback) and return a deny or ask decision from it. Map review
to ask.

#### OpenAI Agents SDK

Run the mediation step in a tool input guardrail (`@tool_input_guardrail`,
attached through the tool's `tool_input_guardrails`). It runs before the tool
with the call's arguments; reject the call from it on deny and review. Map
review to the tool's `needs_approval` gate.

### Check before moving on

Before moving on, confirm each of these. If one does not hold, say which one
and why, rather than working around it.

- Every tool takes a `claim` argument, and the mediation step removes it
  before the tool body runs.
- The mediation step runs on every call path, including retries, handoffs and
  sub-agents. A tool that can be reached without it is not covered.
- Every fact the step compares against comes from the server side. Nothing in
  the claim, the prompt or a tool result decides which facts are used.
- A missing claim is a deny, an unresolved fact is a review, and only allow
  runs the tool.
- The claim check is combined with the existing controls, and the most
  restrictive outcome wins. It never turns another control's deny or review
  into allow.
- The audit trail holds the schema version, the outcome, the check outcomes
  and digests — not raw untrusted content and not the `summary` text.

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

#### Python

Install: `pip install opentelemetry-sdk opentelemetry-exporter-otlp-proto-http`

```python
from opentelemetry import trace
from opentelemetry.sdk.trace import TracerProvider
from opentelemetry.sdk.trace.export import SimpleSpanProcessor
from opentelemetry.exporter.otlp.proto.http.trace_exporter import OTLPSpanExporter

provider = TracerProvider()
provider.add_span_processor(SimpleSpanProcessor(OTLPSpanExporter()))
trace.set_tracer_provider(provider)
tracer = provider.get_tracer("agent")
```

Create the provider once, at startup. For production traffic, use the SDK's
batching span processor in place of the simple one.

##### OpenAI Agents SDK

Install: `pip install openinference-instrumentation-openai-agents`

```python
from openinference.instrumentation.openai_agents import OpenAIAgentsInstrumentor

OpenAIAgentsInstrumentor().instrument(tracer_provider=provider)
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

#### Python

Install: `pip install agent-reliability-otel-labels`

```python
import warnings

from opentelemetry import trace
from agent_reliability_otel_labels import Run, canonical_fp, mark_run_end, mark_tool_span


def run_agent(prompt):
    run = Run()        # one per run, never shared between runs
    attempts = {}      # calls so far in this run, per arguments fingerprint

    def search_flights(args: dict) -> dict:
        """Search flights between two airports on a date."""
        try:
            key = canonical_fp(args)
        except Exception:
            key = "search_flights (no fingerprint)"
        attempts[key] = attempts.get(key, 0) + 1
        step = run.next_step()
        span = trace.get_current_span()   # the framework's tool span
        result = None
        try:
            result = find_flights(args)
            return result
        finally:
            try:
                mark_tool_span(span, args=args, result=result,
                               attempt=attempts[key], side_effect="read",
                               step_index=step, run_id=run.run_id)
            except ValueError as err:
                warnings.warn(f"trustabl: could not label the tool span: {err}")

    with tracer.start_as_current_span("invoke_agent travel-booker") as root:
        root.set_attribute("gen_ai.operation.name", "invoke_agent")
        root.set_attribute("gen_ai.agent.name", "travel-booker")
        run.mark_start(root)
        try:
            answer = call_the_agent(prompt, tools=[search_flights])
            mark_run_end(root, exit_reason="final_answer")
            return answer
        except Exception:
            mark_run_end(root, exit_reason="error")
            raise
```

When the failure class is known, also pass `error_class`, for example
`error_class="timeout"`. For a delegated agent, call
`mark_handoff(span, handoff_id=...)` on the child agent's span. For signed
policies, call `bind_authority(root, bindings=[Binding(...)])` right after
`run.mark_start(root)`.

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

Python:

- Create one `Run` per run. Never share one between concurrent runs.

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

