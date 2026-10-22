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

1. Read `claim` from the call's arguments. When the step runs in a wrapper
   that can change the arguments, remove `claim` before the tool function is
   called. A hook that can only allow or reject leaves it in place. Either
   way, nothing after the step acts on the claim.
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

- Every tool takes a `claim` argument, and the mediation step reads it before
  the tool has any effect. Nothing after the step acts on the claim.
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

