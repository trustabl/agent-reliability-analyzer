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

#### Go

Install: `go get go.opentelemetry.io/otel go.opentelemetry.io/otel/sdk go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp`

```go
import (
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
)

exporter, err := otlptracehttp.New(ctx)
if err != nil {
	return err
}
provider := sdktrace.NewTracerProvider(
	sdktrace.WithSyncer(exporter),
	sdktrace.WithResource(resource.Default()),
)
otel.SetTracerProvider(provider)
defer func() { _ = provider.Shutdown(ctx) }()
tracer := provider.Tracer("agent")
```

Create the provider once, at startup. For production traffic, use
`sdktrace.WithBatcher` in place of `sdktrace.WithSyncer`.

##### Google ADK

ADK for Go emits `invoke_agent` and `execute_tool` spans itself, on the global
tracer provider. `otel.SetTracerProvider(provider)` must run before the agent
does. No extra package.

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

#### Go

Install: `go get github.com/trustabl/agent-reliability-otel-labels/go`

```go
import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sync"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"

	otellabels "github.com/trustabl/agent-reliability-otel-labels/go"
)

func runAgent(ctx context.Context, prompt string) (string, error) {
	// One per run, never shared between runs.
	run := otellabels.NewRun("")
	var mu sync.Mutex
	attempts := map[string]int{}

	searchFlights := func(ctx context.Context, a searchArgs) (result searchResult, err error) {
		// Structs are refused: pass the JSON form.
		rawArgs, _ := json.Marshal(a)
		key, fpErr := otellabels.CanonicalFP(json.RawMessage(rawArgs))
		if fpErr != nil {
			key = "search_flights (no fingerprint)"
		}
		mu.Lock()
		attempts[key]++
		attempt := attempts[key]
		mu.Unlock()
		step := run.NextStep()
		span := trace.SpanFromContext(ctx) // the framework's tool span

		defer func() {
			call := otellabels.ToolCall{
				Args: json.RawMessage(rawArgs), Attempt: attempt,
				SideEffect: otellabels.Read, StepIndex: &step,
				RunID: run.RunID(), Name: "search_flights",
			}
			if err == nil {
				rawResult, _ := json.Marshal(result)
				call.Result = json.RawMessage(rawResult)
			}
			if mErr := otellabels.MarkToolSpan(span, call); mErr != nil {
				fmt.Fprintln(os.Stderr, "trustabl: could not label the tool span:", mErr)
			}
		}()
		return findFlights(a)
	}

	ctx, root := tracer.Start(ctx, "invoke_agent travel-booker")
	defer root.End()
	root.SetAttributes(
		attribute.String("gen_ai.operation.name", "invoke_agent"),
		attribute.String("gen_ai.agent.name", "travel-booker"),
	)
	run.MarkStart(root)

	answer, err := callTheAgent(ctx, prompt, searchFlights)
	reason := otellabels.FinalAnswer
	if err != nil {
		reason = otellabels.Error
	}
	// Still before the root span ends: the deferred End runs after this.
	if mErr := otellabels.MarkRunEnd(root, reason); mErr != nil {
		fmt.Fprintln(os.Stderr, "trustabl: could not mark the run's end:", mErr)
	}
	return answer, err
}
```

When the failure class is known, also set `ErrorClass`, for example
`otellabels.Timeout`; the values `4xx` and `5xx` are `otellabels.Class4xx`
and `otellabels.Class5xx`. For a delegated agent, call
`otellabels.MarkHandoff(span, handoffID)` on the child agent's span. For
signed policies, call `otellabels.BindAuthority(root, bindings)` right after
`run.MarkStart(root)`.

##### Google ADK

ADK passes each tool a context that carries its `execute_tool` span, so
`trace.SpanFromContext(toolContext)` reaches it. The run labels go on ADK's
own `invoke_agent` span through agent callbacks; no application root span is
needed.

```go
BeforeAgentCallbacks: []agent.BeforeAgentCallback{func(cc agent.CallbackContext) (*genai.Content, error) {
	run.MarkStart(trace.SpanFromContext(cc))
	return nil, nil
}},
AfterAgentCallbacks: []agent.AfterAgentCallback{func(cc agent.CallbackContext) (*genai.Content, error) {
	if mErr := otellabels.MarkRunEnd(trace.SpanFromContext(cc), otellabels.FinalAnswer); mErr != nil {
		fmt.Fprintln(os.Stderr, "trustabl: could not mark the run's end:", mErr)
	}
	return nil, nil
}},
```

- ADK runs a turn's tool calls in parallel goroutines.
- With several calls in a turn, ADK adds an `execute_tool (merged)` span
  around them. It carries no labels; skip it by span name.
- ADK skips the after-agent callback when the caller stops iterating the
  run's events, so a run that stops on its first failure carries no
  `trustabl.exit_reason`.

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

Go:

- Set the global tracer provider before the agent runs.
- Marshal arguments and results and pass `json.RawMessage`; structs are refused.
- Guard shared attempt counts with a mutex when tools can run in parallel.
- Build the agent once, but the `Run` and the attempt counts per run.

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

