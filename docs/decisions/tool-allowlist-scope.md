# Tool allow-list / access-control detection: scope across SDKs

This closes out the design blocker on detecting agents with missing or
overly-broad tool access control. Investigation split the supported SDKs
into three behavior classes, because "empty allow-list" does not mean the
same thing in each one. This doc records the LangChain decision (not
applicable), scopes and partially ships the Claude SDK / OpenAI SDK work
(Class 1), and records Google ADK, the one class that shipped as a real rule
first — see [ADK-111](../../testdata/rules-fixture/google_adk/agent_safety.yaml)
— because ADK is the one SDK where an explicit allow-list genuinely narrows an
otherwise-unbounded tool surface. Claude SDK's repo-scope half of Class 1
shipped next, as **CSDK-205** — see below.

## Class 3 — LangChain: no permission-model concept (not applicable)

LangChain agent discovery (`internal/analysis/langchain_agents.go`,
`internal/analysis/ts_langchain_agents.go`, `internal/analysis/langgraph_graph.go`)
sets fields on the shared `models.AgentDef` — `SDK`, `Class`, `Language`,
`Location`, `Kwargs`, `Opaque`, and (when resolvable) `Name`/`VarName`. A
grep of all five LangChain discovery files (`langchain_agents.go`,
`langchain_tools.go`, `langchain_hosted_tools.go`, `ts_langchain_agents.go`,
`ts_langchain_tools.go`) for `allow|deny|permission|allowed_tools|blocked`
returns zero matches. `AgentDef` itself has no allow-list, deny-list, or
permission-mode field for any SDK — it carries `ToolRefs`/`HostedToolRefs`
(what's wired in), not a policy over what's wired in.

For LangChain, `tools=` (Python `create_react_agent`/`create_agent`/
`AgentExecutor`) or `tools:` (TS) is simply the literal, complete set of
tools the agent can call — there is no separate "allow-list that narrows a
larger set" layer, no `permission_mode`, and no `disallowed_tools`
concept anywhere in the SDK's surface as discovered by this codebase. An
agent with `tools=[]` has zero tools, full stop; there is nothing else
implicitly available for an allow-list to have restricted. "Empty = no
tools" is not a security finding — it's just an inert agent.

**Decision: this class of rule does not apply to LangChain.** The
`google_adk/agent_safety.yaml` shape (fire when an allow-list-bearing
construct has no explicit allow-list) has no LangChain analogue to attach
to. `testdata/rules-fixture/langchain/agent_safety.yaml` already has the
one LangChain rule that *is* the right shape for this SDK — LC-101 flags
an agent that wires a code-execution/shell built-in tool directly (the
risk is which tools are wired, not whether a filter narrows a wider set).

## Class 2 — Google ADK: shipped (ADK-111)

Investigation initially assumed a dedicated ADK tool-allow-list field
existed on `AgentDef` to confirm. It does not: ADK's `tools=` kwarg is
captured generically via `Kwargs.Children["tools"]`, same as every other
constructor kwarg, and an absent/empty `tools=` list yields zero
`ToolRefs` — the most restrictive state, not the least. So the original
framing ("empty `tools=` is unrestricted") does not hold structurally for
plain `tools=`.

The real Class-2 mechanism is narrower and more specific:
[`MCPToolset`](https://github.com/google/adk-python) — an ADK tool that
connects an agent to a whole MCP server's tool catalog. Its `tool_filter=`
kwarg is the actual allow-list: set, it narrows the agent to the named
tools; unset, the agent gets every tool the remote server currently
exposes — a surface that isn't enumerable from the agent's source, can
grow whenever the server changes, and is outside this codebase's control.
That's a genuine "empty allow-list = unrestricted" case.

`MCPToolset` was not previously in `ADKHostedToolClasses`
(`internal/analysis/adk_hosted_tools.go`), so a `tools=[MCPToolset(...)]`
item fell through classification and became an opaque External `ToolRef`
— its kwargs, including `tool_filter`, were discarded entirely. Delivered:

- Added `MCPToolset` to `ADKHostedToolClasses` — the one discovery change
  needed. Every call already has its keyword arguments captured onto
  `Expr.CallKwargs` regardless of classification
  (`internal/analysis/agents.go:734-740`); recognizing the class just
  routes those kwargs onto a queryable `HostedToolDef.Kwargs` instead of
  discarding them.
- Rule **ADK-111** in `testdata/rules-fixture/google_adk/agent_safety.yaml`,
  built entirely from existing predicates —
  `agent_uses_hosted_tool_class: [MCPToolset]` and
  `not: agent_hosted_tool_kwarg_present: {class: MCPToolset, kwarg: tool_filter}`
  — no new predicate, schema field, or `evaluator.go` wiring was needed.
- Fire/silent cases added to `policyAgentRuleCases` in
  `internal/rules/policies_test.go`; `TestPolicyRules_AllRulesCovered`
  passes. `go build ./...` and `go test ./...` are clean.

**Known gap (per the three-repo model):** this change was initially
scoped to the engine fixture only. It has since been mirrored into
`trustabl-rules` (production) via
[trustabl-rules#51](https://github.com/trustabl/trustabl-rules/pull/51)
and given a rationale doc in `trustabl-rulebook`
(`docs/Policy/google_adk/agent_safety.md`), so ADK-111 is fully
shipped per `CLAUDE.md`'s sync obligation — engine, rules, and
rulebook are all in sync.

## Class 1 — Claude SDK / OpenAI SDK

Claude SDK repo-scope: **shipped as CSDK-205.** OpenAI Agents SDK: **shipped
as OAI-115 (Python allow-list), OAI-116 (TypeScript allow-list), and OAI-117
(TypeScript require-approval)** — see below. Claude SDK agent-scope remains
not implemented.

### Claude SDK repo-scope: shipped (CSDK-205)

`ClaudeAgentOptionsDef` (`internal/models/agent.go`) already captured every
constructor kwarg generically onto a single `Kwargs *KwargTree` — so
`disallowed_tools` was structurally present in the data whenever source set
it, and no discovery change was needed. `internal/rules/predicates.go`
previously exposed only two repo-scope readers of that struct:
`PredRepoClaudeOptionsPermissionModeIs` (value check on `permission_mode`)
and `PredRepoClaudeOptionsMaxTurnsMissing` (absence check, hardcoded to the
`max_turns` kwarg name).

Delivered:

- Generalized `PredRepoClaudeOptionsMaxTurnsMissing`'s body into a shared
  helper, `repoClaudeOptionsMissingKwarg(inv, kwarg string)`
  (`internal/rules/predicates.go`) — the same shape `agentRunCallMissingKwarg`
  uses for the agent-run-call family. `PredRepoClaudeOptionsMaxTurnsMissing`'s
  observable behavior is unchanged; `PredRepoClaudeOptionsDisallowedToolsMissing`
  is the new reader built on the same helper.
- The standard four-file schema change (`schema.go` + `predicates.go` +
  `evaluator.go` + `schema.yaml`), plus a `schema_version` bump (14 → 15) in
  the fixture and `trustabl-rules` manifests.
- Rule **CSDK-205** in `claude_sdk/repo.yaml` (fixture and production, both
  synced), severity medium / confidence 0.7:
  `repo_claude_options_permission_mode_is: [acceptEdits]` combined (`all:`)
  with `repo_claude_options_disallowed_tools_missing: true`. The mode list is
  `acceptEdits` only, deliberately excluding `bypassPermissions` — CSDK-202
  already owns that value at high/0.9, and including it here would make every
  tripping repo report twice on the same `ClaudeAgentOptions(...)` call.
  `acceptEdits` is the genuinely uncovered surface: file edits auto-approve,
  and `allowed_tools` only auto-approves rather than restricts, so with no
  `disallowed_tools` deny-list nothing bounds the rest of the tool surface.
- Fire/silent cases in `policyRepoRuleCases`
  (`internal/rules/policies_test.go`), including a case exercising the
  asymmetric Opaque-skip between the two combined predicates (permission_mode
  reads Opaque constructions; the missing-kwarg helper skips them).
- Rationale doc updated: `../trustabl-rulebook/docs/Policy/claude_sdk/repo.md`
  gained a CSDK-205 rule-by-rule defense block, and its "what this policy
  does not cover" section — which previously said `acceptEdits` was
  deliberately not flagged — was corrected to say only *bare* `acceptEdits`
  (with a deny-list present) goes unflagged.

**Known gap carried forward, not fixed:** the presence check underlying
`repoClaudeOptionsMissingKwarg` only requires `node.Value != nil`, so
`disallowed_tools=[]` (empty list) and `disallowed_tools=None` both read as
"set" and silence CSDK-205 — the same tri-state gap `max_turns=None` already
had for CSDK-204. Documented in the rulebook's confidence-gap section rather
than fixed, since tightening it would also change CSDK-204's long-shipped
behavior.

### Claude SDK agent-scope: scoped, not yet built

There are two separate places `permission_mode` / `disallowed_tools` can
appear in a Claude SDK codebase; the repo-scope one (`ClaudeAgentOptions(...)`
session config) is now covered by CSDK-205 above. The agent-scope one remains
scoped but unbuilt:

**`AgentDefinition(...)` in Python, or a `query(...)` main-agent's inline
`options` in TS.** These constructors' kwargs land on `AgentDef.Kwargs`
generically, via the same `extractCallKwargs` mechanism every other SDK's
kwargs go through (`internal/analysis/agents.go`). This is confirmed already
exercised: `testdata/rules-fixture/claude_sdk/agent_safety.yaml` already has
a rule matching
`agent_kwarg_value: {kwarg: permissionMode, value: bypassPermissions}`,
and separate rules already read `agent_grants_builtin_tool` against
`options.allowedTools` directly. For the TS `query(...)` main agent
specifically, `internal/analysis/ts_agents.go:93-94` captures the whole
options object as a nested `KwargTree` when options are inline
(`agent.Kwargs = astutil.TSObjectKwargs(root, pf.Source)`), so
`options.permissionMode` and `options.disallowedTools` are reachable via
dotted-path lookup the same way.

**This means the agent-scope rule needs no new discovery or schema field.**
It's buildable today, purely as a new rule, from the already-existing generic
predicates:

```yaml
match:
  all:
    - agent_kwarg_value: {kwarg: permissionMode, value: acceptEdits}  # not bypassPermissions — mirror the CSDK-205 reasoning: that value already has its own rule
    - agent_kwarg_missing: [disallowedTools]
```

(Python `AgentDefinition` would use `permission_mode`/`disallowed_tools`;
same combinator, different kwarg spelling.) This is a separate rule from
CSDK-205, not a mirror of it — an agent's own `AgentDefinition(...)` posture
and the session-level `ClaudeAgentOptions(...)` posture are two distinct
constructs that can disagree within the same repo.

### OpenAI Agents SDK: researched — mis-filed as Class 1, real mechanisms are Class-2-shaped

The prior session's grep of this codebase's own discovery files found only
per-tool `needs_approval` (OAI-111) and correctly flagged the OpenAI half as
genuinely unresearched rather than guessing. This session read the actual
`openai-agents-python` / `openai-agents-js` source and docs (not just how
other SDKs work) to close that gap. Verdict: **a session/run-level
permission model is confirmed absent, for an architectural reason** — but
**two real registry-wide allow-list mechanisms exist**, missed by the
earlier grep because they're keyed on `tool_filter` / `allowed_tools`, not
`permission_mode` / `disallowed_tools` / `RunConfig`. Both are closer in
shape to ADK's `MCPToolset(tool_filter=)` (Class 2) than to Claude's session
config (Class 1) — the original three-class grouping mis-filed OpenAI.

**Confirmed absent: no run-level or session-level permission model.**
`RunConfig` (`src/agents/run_config.py`, all 25 fields read directly) has no
`allowed_tools`, `disallowed_tools`, or `permission_mode` — its only
tool-adjacent fields (`tool_error_formatter`, `tool_not_found_behavior`,
`tool_name_collision_policy`, `tool_execution: ToolExecutionConfig`) are
execution/error-shaping knobs, not authorization. `Runner.run` /
`run_sync` / `run_streamed` take no permission parameter. And the
`Runner.run(..., hooks=...)` approval-hook this doc previously hypothesized
as the most likely candidate **does not exist**: `RunHooks.on_tool_start`
and `on_tool_end` (`src/agents/lifecycle.py`) both return `None` — they are
observational, with no way to veto or filter a tool call. That earlier
guess is retracted here.

The architectural reason: an OpenAI agent starts with **zero** tools, and
`Agent(tools=[...])` is the complete, enumerable surface — the same
situation this doc already reasoned through for LangChain (Class 3) and
for ADK's plain `tools=`. Claude's `allowed_tools` / `permission_mode`
exist only because a Claude session starts with a broad built-in tool set
already available to narrow. OpenAI has no such implicit default, so
"absent `tools=`" is the *most* restrictive state, not the least, and OAI's
per-tool `needs_approval` is not the tip of a session-wide iceberg — it's
the whole per-tool story.

**Real mechanism 1 — MCP server `tool_filter`.** `src/agents/mcp/util.py`
defines `ToolFilterStatic{allowed_tool_names, blocked_tool_names}`;
`ToolFilter = ToolFilterCallable | ToolFilterStatic | None`, and when
`None`, **no filtering occurs — the agent gets every tool the remote MCP
server currently exposes**, not enumerable from source, able to grow when
the server changes. `create_static_tool_filter(...)` returns `None` when
both lists are `None`. Used as
`MCPServerStdio(params={...}, tool_filter=create_static_tool_filter(
allowed_tool_names=[...]))`. TS confirms the same shape:
`MCPServerStdio({ toolFilter })` + `createMCPToolStaticFilter({ allowed,
blocked })` (`MCPToolFilterStatic`). This is structurally identical to
ADK-111's `MCPToolset(tool_filter=)` — "absent = unbounded, risky" is
unambiguous.

Discovery gap: **Python discards it.** `classifyMCPServerCall`
(`internal/analysis/mcp_servers.go:33-58`) has an explicit comment —
*"Kwargs intentionally not captured at v1"* — leaving `MCPServerDef.Kwargs`
nil even though the field exists (`internal/models/agent.go:141`). **TS
already captures it** (`ts_openai_mcp_servers.go:82`,
`def.Kwargs = astutil.TSObjectKwargs(...)`). But no predicate anywhere
reads `MCPServerDef` at all — a grep of `internal/rules/predicates.go` for
`MCPServer`/`mcp_server` returns zero hits; the existing OAI-106 only reads
the agent's generic `mcp_servers` kwarg presence, never the resolved
`MCPServerDef`s. This needs **both** a discovery change (populate Python
`MCPServerDef.Kwargs`, same move as ADK-111's `Expr.CallKwargs` → queryable
struct) **and** a new MCP-server-scoped predicate family — the standard
four-file schema change plus a `schema_version` bump in both the fixture
and `trustabl-rules`. Not a rules-only change.

**Real mechanism 2 — `HostedMCPTool.tool_config.allowed_tools`.**
`HostedMCPTool` (`src/agents/tool.py`) wraps `tool_config: Mcp`, a raw
pass-through of the Responses API `Mcp` TypedDict
(`openai/types/responses/tool_param.py`). `allowed_tools:
Optional[McpAllowedTools]` — *"List of allowed tool names or a filter
object"* — bounds which tools of the remote server the model can see, and
is orthogonal to `require_approval` (which only gates human confirmation).
Absent `allowed_tools` = the model gets the server's entire catalogue —
same "risky absence" shape as mechanism 1, but for the *hosted* MCP path.

Unlike mechanism 1, **this one is reachable in Python with zero engine
changes**: `HostedMCPTool` is already in `HostedToolClasses`
(`internal/analysis/hosted_tools.go:17`); Python dict literals already
recurse into nested `KwargTree` children
(`internal/analysis/agents.go:750`, `dictChildren`), so
`tool_config={"allowed_tools": [...]}` is already a reachable leaf; and
`HostedToolKwargExpr.Kwarg` is documented dotted-path-capable
(`internal/rules/schema.go:141`), resolved by the existing
`PredAgentHostedToolKwargPresent` (`internal/rules/predicates.go:735-745`).
A rule of the ADK-111 shape — `agent_uses_hosted_tool_class:
[HostedMCPTool]` combined with `not: agent_hosted_tool_kwarg_present:
{class: HostedMCPTool, kwarg: tool_config.allowed_tools}` — is buildable
today as a **rules-only change**, no schema/predicate/evaluator work, no
`schema_version` bump. This is the cheapest available win of the three
candidates here. **Shipped as OAI-115** (Python, confidence 0.7 — see the
absent-semantic nuance below for the discount's source).

**TS follow-up shipped as OAI-116.** The caveat this section originally
carried — that `classifyTSOpenAIHostedFactoryCall`
(`internal/analysis/ts_openai_hosted_tools.go`) never set `Kwargs`, so
`hostedMcpTool({...})`'s options were invisible on the TS path — was closed
by a discovery-only change (commit `0c330b6`, PR #204): the factory's
options-object argument is now captured into `HostedToolDef.Kwargs` via
`astutil.TSObjectKwargs`, and `ResolveEdges` materializes the
discovery-built def (precise call-site `Location` + `Kwargs`) through
`HostedToolRef.Pending` rather than synthesizing a bare one at the agent's
line. That unblocked the TS sibling rule as a second rules-only change, no
further engine work: **OAI-116**, `agent_uses_hosted_tool_class:
[hostedMcpTool]` combined with `not: agent_hosted_tool_kwarg_present:
{class: hostedMcpTool, kwarg: allowedTools}`.

**Verified: the TS `hostedMcpTool` options object is flat, not nested.**
Read directly from `hostedMcpTool`'s signature in
`openai-agents-js/packages/agents-core/src/tool.ts`:

```ts
export function hostedMcpTool<Context = UnknownContext>(
  options: {
    allowedTools?: string[] | { toolNames?: string[] };
    allowedCallers?: ToolAllowedCallers;
    deferLoading?: boolean;
    serverDescription?: string;
  } & (
    | { serverLabel: string; serverUrl?: string; authorization?: string; headers?: Record<string, string> }
    | { serverLabel: string; connectorId: string; authorization?: string; headers?: Record<string, string> }
  ) & (
    | { requireApproval?: never }
    | { requireApproval: 'never' }
    | { requireApproval: 'always' | { never?: {...}; always?: {...} }; onApproval?: ... }
  ),
): HostedMCPTool<Context>
```

`allowedTools`, `serverLabel`, and `requireApproval` sit directly on the
options object — unlike Python, there is no `toolConfig` (or `tool_config`)
wrapper key. `lookupKwargInTree` (`internal/rules/predicates.go`) splits its
dotted-path argument on `.` and walks `KwargTree.Children`, so a one-segment
path is simply a one-step walk — this required no predicate change, only the
shorter path string in the YAML (`allowedTools` vs. Python's
`tool_config.allowed_tools`). Both accepted `allowedTools` shapes —
`["a", "b"]` (a `Value` leaf) and `{ toolNames: [...] }` (a `Children`
subtree) — resolve to a non-nil lookup, so either correctly silences the
rule; confirmed by
`internal/analysis/ts_openai_hosted_tools_test.go`.

**The absent-semantic nuance (OpenAI's version of Claude's
auto-approve-vs-restrict trap):** `require_approval` on `HostedMCPTool`
diverges *by language* for an identical source-level omission:

- **TypeScript**: `hostedMcpTool()` (`packages/agents-core/src/tool.ts`)
  injects `require_approval: 'never'` when the option is omitted — stated
  directly in source and restated in the official JS MCP guide
  ("`requireApproval` … Defaults to `'never'`"). Omission is **risky**.
- **Python**: `tool_config` is passed through raw with no default
  injection by the SDK, so omission falls through to the Responses API's
  platform default, which OpenAI's API guide states in prose as
  approval-required ("By default, OpenAI will request your approval before
  any data is shared with a connector or remote MCP server"). Omission is
  **safe** — but this is a *platform* default stated in docs, not pinned by
  any quotable line of SDK source, and is therefore weaker evidence than
  the TS finding and subject to change server-side without an SDK version
  bump. Any future rule on `require_approval` must be language-gated (two
  rules, per `CLAUDE.md`'s SDK-scoped-rules discipline) — and should be
  scoped to TS first, where the default is verifiable in code.

| Construct | Absent means | Semantic |
|---|---|---|
| `Agent(tools=[...])` | zero tools | safe — no rule (same as LangChain / plain ADK `tools=`) |
| MCP `tool_filter` / `toolFilter` | server's full catalogue | risky — ADK-111 shape (mechanism 1) |
| `HostedMCPTool.tool_config.allowed_tools` | server's full catalogue | risky — ADK-111 shape (mechanism 2) |
| `HostedMCPTool.tool_config.require_approval` | TS: never-approve (**shipped, OAI-117**); Python: platform default approve — not implemented, confirmed a false-positive shape | risky in TS, safe in Python — language-gated |
| `function_tool(needs_approval=)` | `False` | risky — already shipped as OAI-111 |
| `function_tool(is_enabled=)` | `True` | not a security gate — dynamic enablement, ignore |

**Status of the three next steps originally listed here:** (1) `HostedMCPTool`
`allowed_tools` rule — **shipped, OAI-115**; (2) TS hosted-tool kwarg
capture, which unblocked both the TS `allowedTools` rule and (separately) a
TS `require_approval: 'never'` rule — **capture shipped** (commit `0c330b6`),
the `allowedTools` rule it unblocked **shipped as OAI-116**, and the
`require_approval: 'never'` rule it separately unblocked **shipped as
OAI-117** (medium/0.65 — the consequence is a missing runtime gate rather
than an unbounded catalog, and the predicate cannot distinguish negligent
omission from a deliberately-read-only server, hence the confidence
discount below OAI-115/116; fires on omission only, not on an explicit
`requireApproval: 'never'`, since the latter is a reviewable choice that
OAI-116's own fix text recommends). Re-verified against
`openai-agents-python`'s `tool.py`/`run_config.py` for this rule that
Python genuinely has no equivalent gap (no SDK-level default injection,
confirmed in source rather than assumed) — no parallel Python rule was
added. (3) MCP `tool_filter` — discovery + new predicate family + schema
bump — **still not implemented**, the next pickup point for this doc.
