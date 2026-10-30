# Changelog

All notable changes to Trustabl are documented here. The format is based on
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and the project aims
to follow Semantic Versioning once it reaches 1.0.

## [Unreleased]

Rule schema 31 → 40 (`rules.SupportedSchemaVersion`). The rules fixture goes
from 355 to 387 rules. OAI-034 / MCP-033 / LC-028 / CREW-016 / PYD-016 /
CSDK-026 / ADK-024 / AG2-023 fire at **info** on every side-effecting Python
OpenAI Agents, MCP, LangChain, CrewAI, Pydantic AI, Claude Agent SDK, Google
ADK or AutoGen tool that takes no `claim` argument, and OAI-037 on every
side-effecting TypeScript OpenAI Agents tool. They never fail `--strict`, and they lower the score only slightly.

### Added

- **Strands Agents discovery (Python).** The scanner now finds Strands agents
  (`Agent(...)` bound to the `strands` package, Class `StrandsAgent`), their
  `@tool` functions (`strands_tool`), the risky ready-made tools from
  `strands_tools` (`shell`, `python_repl`, `use_computer`, `load_tool`, `cron`,
  `file_write`, `editor`, `http_request`, `mcp_client`, `use_aws`,
  `environment`) as hosted tools, and MCP servers wired as `MCPClient` tool
  providers, including `tools=client.list_tools_sync()`. `strands-agents` is a
  new dependency needle. No Strands rule pack ships yet, so a Strands repo gets
  META-001 rather than a clean result.
- **Model endpoint override in project settings (rule schema 39 → 40, provisional).** New
  repo-scope predicate `repo_settings_env_redirects_model_endpoint` and rule **AGCFG-014**
  (medium, 0.5, pack file `agent_config/model_endpoint_override.yaml`) flag an
  `ANTHROPIC_BASE_URL` entry in the `env` block of a committed `.claude/settings.json` or
  `settings.local.json` whose host is neither api.anthropic.com nor a loopback address. The
  host is reduced to a class inside `modelEndpointClass` and is never stored; the finding
  names the file and the key, not the URL. A value that is empty, holds a `$` reference or
  does not parse is not judged. Not read: other base-URL variables, `.env` files, workflow
  `env`, `.codex` and `.gemini` config, user and managed settings, code-level `base_url`;
  there is no list of approved gateways. AGCFG-006 is unchanged and reads none of these
  files. Backed by `ProjectConfigFacts.ModelEndpointOverride`.
- **Hidden text in agent guidance files (rule schema 38 → 39, provisional).** New
  repo-scope predicate `repo_guidance_file_has_hidden_text` and rule **AGCFG-013**
  (medium, 0.6, pack file `agent_config/guidance_hidden_text.yaml`) apply the AGCFG-009
  character test (zero-width, Unicode Tags-block and bidirectional override characters,
  with the same exemptions) to CLAUDE.md, GEMINI.md, repo-root `.cursor/rules/**/*.mdc`,
  `.windsurfrules`, `.windsurf/rules/*.md`, `.devin/rules/*.md`,
  `.github/copilot-instructions.md` and `.github/instructions/**/*.instructions.md`.
  AGENTS.md (AGCFG-009), staged OpenCode guidance (AGCFG-010) and SKILL.md (CSKILL-040)
  are not read by it. HTML comments, instruction phrases and encoded text are not tested;
  `.cursorrules` is not read. The walk gains `ScanManifest.GuidanceFiles` and four new
  component kinds (`gemini_md`, `cursor_rule`, `windsurf_rule`, `copilot_instruction`);
  no existing component kind, rule match or manifest list changes. Backed by
  `ProjectConfigFacts.HiddenGuidanceText`.
- **MCP client config: unscoped credential and plain http remote (rule schema 37 → 38,
  provisional).** Two repo-scope predicates and two rules in
  `agent_config/mcp_client_config.yaml`. `repo_mcp_config_unscoped_credential` and
  **MCPCFG-004** (high, 0.6) flag a committed MCP client config that starts the
  `github-mcp-server` binary or image with `GITHUB_PERSONAL_ACCESS_TOKEN` and no
  `--read-only` / `GITHUB_READ_ONLY`, `--toolsets` / `GITHUB_TOOLSETS` or `--tools` /
  `GITHUB_TOOLS`; the deprecated npm `@modelcontextprotocol/server-github` with that token
  (its source has no scope option); `@supabase/mcp-server-supabase` with neither
  `--read-only` nor `--project-ref`; or any env key whose name contains `SERVICE_ROLE`.
  `GITHUB_READ_ONLY` counts as set when `strconv.ParseBool` reads it as true, as the server
  binds it. `repo_mcp_config_remote_plain_http` and **MCPCFG-005** (medium, 0.6) flag a
  `url` or `httpUrl` with the `http` scheme on a host that is not `localhost` or a loopback
  address. A remote server with no credential in the file is not flagged: Claude Code,
  Cursor, Gemini CLI and Codex each document OAuth sign-in with nothing in the file. Backed
  by `ProjectConfigFacts.UnscopedMCPCredential` and `PlainHTTPMCPServer`. Env keys are read
  by name; only the value of `GITHUB_READ_ONLY` and `GITHUB_TOOLSETS` is inspected, and it is
  reduced to a state and never stored. Remote GitHub and Supabase URLs are not judged. Schema 38
  is provisional and a later merge may renumber.
- **Exec-capable pre-approved command prefix (rule schema 36 → 37, provisional).**
  New repo-scope predicate `repo_settings_allow_exec_capable_prefix` and rule
  **AGCFG-012** (medium, 0.6, pack file `agent_config/exec_capable_allow.yaml`) flag a
  `Bash` rule in `permissions.allow` of a committed `.claude/settings.json` or
  `settings.local.json` that is a bare `git`, `xargs` or `npx` command followed by a
  trailing wildcard (`Bash(git:*)`, `Bash(npx *)`). A narrower prefix, an exact command
  and `find` are not flagged. A bare `Bash`, `Bash(*)` or same-prefix deny or ask rule in
  the same `.claude/` folder cancels the finding. Backed by
  `ProjectConfigFacts.ExecCapableAllow`. A committed bare `Bash` or `Bash(*)` allow entry
  is not flagged by any rule.
- **Forge: Tool-Call Claims section (template 3 → 4).** `trustabl forge` now
  emits a `## Tool-Call Claims` section after the apply loop. It tells the
  coding agent to give every tool with an effect a typed `claim` argument (an
  EBTE `ToolInvocationExplanation`; pure reads may skip it) and to wire a mediation step that checks it
  against server-held facts before the call has any effect, with placement for
  each detected SDK. The claim never grants authority. Every skill generated
  by template 3 now reads as outdated in `trustabl forge check`; regenerate
  it.
- **Tool-call claims (EBTE, schema 32).** Nine tool-scope rules for Python
  OpenAI Agents, MCP and LangChain tools, over the new `tool_claim_use`
  predicate and `claim_use` discovery fact: OAI-034 / MCP-033 / LC-028 (info)
  flag a side-effecting tool with no `claim` argument, OAI-035 / MCP-034 /
  LC-029 (medium) a `claim` nothing reads, and OAI-036 / MCP-035 / LC-030
  (medium) a `claim` that is only printed, logged, serialized or returned. A
  second decorator, or an OpenAI tool input guardrail, counts as a possible
  mediator. The fact is stamped only on tools that declare `claim`, so scan
  output for repos that do not use claims is unchanged apart from the new
  OAI-034 / MCP-033 / LC-028 findings. **Release order:** this engine first,
  then the rules pack.
- **Tool-call claims for CrewAI and Pydantic AI (schema 33).** CREW-016..018
  and PYD-016..018 apply the same three rungs to CrewAI `@tool` functions and
  Pydantic AI `@agent.tool` / `@agent.tool_plain` / `Tool(fn)` tools. A new
  repo-level pass, `analysis.ApplyClaimMediators`, marks a claim as possibly
  mediated (`wrapped`, silent) when the repo registers the SDK's pre-tool hook:
  CrewAI `@before_tool_call`, `register_before_tool_call_hook(...)` or
  `@on(InterceptionPoint.PRE_TOOL_CALL)`; Pydantic AI
  `@<hooks>.on.before_tool_execute` / `.on.tool_execute` or
  `Hooks(before_tool_execute=...)` / `Hooks(tool_execute=...)`, or a tool's own
  `args_validator=`. The credit is per SDK across the repo, not per tool.
- **Tool-call claims for the Claude Agent SDK (schema 34).** CSDK-026..028
  apply the rungs to Python `@tool` handlers. The handler takes one `args`
  dict, so discovery records the input schema's keys in a new
  `ToolDef.InputKeys` (`input_keys` in JSON; short dict, JSON-schema
  `properties`, or a same-file TypedDict / dict variable) and reads the claim
  as `args["claim"]` / `args.get("claim")`; a whole-`args` read handed to
  another call counts as checked. A `"PreToolUse"` hooks entry, or
  `ClaudeAgentOptions(can_use_tool=...)` on a call without `allowed_tools=` or
  `bypassPermissions`, counts as a possible mediator.
- **Tool-call claims for TypeScript OpenAI Agents (schema 35).** OAI-037..039
  (`language: typescript`) apply the rungs to `tool({...})` definitions from
  `@openai/agents` whose zod `parameters` declare `claim`. The claim is read in
  the inline `execute` function as a destructured binding (`({ claim })`,
  `({ claim: c })`) or as `input.claim` / `input["claim"]`; output calls are
  `console.*`, `JSON.stringify`, `String` and logger methods. A tool with
  `inputGuardrails: [...]` counts as possibly mediated.
- **Tool-call claims for Google ADK and AutoGen (schema 36).** ADK-024..026
  over `FunctionTool(fn)` and AG2-023..025 over `register_function(fn)` and
  `register_for_llm` / `register_for_execution` tools, with the fact taken from
  the wrapped function. The AutoGen registering decorators are not counted as
  mediators; any other decorator is. An ADK `before_tool_callback` (an agent
  keyword, or a plugin method) anywhere in the repo marks ADK claims as possibly
  mediated. AutoGen has no pre-tool hook.
- **A one-time star line after the first interactive scan.** `trustabl scan`
  prints "⭐ Found this useful? Star Agent Reliability Analyzer" with the
  repository link to stderr once, after the first successful scan with human
  output in a terminal. It never appears in CI, with `--format json` or `sarif`,
  or when stdout or stderr is not a terminal. A `star-prompt-shown` marker next
  to `~/.config/trustabl/telemetry.json` keeps it from showing again.
- **AGCFG-011: a committed settings file pre-approves fetches from a host that
  serves user-published content.** A WebFetch rule in `permissions.allow` of
  `.claude/settings.json` or `settings.local.json` that covers github.com,
  gist.github.com, raw.githubusercontent.com, an arbitrary `*.github.io` site,
  huggingface.co, bitbucket.org, pastebin.com or an arbitrary `*.readthedocs.io`
  site fires a high-severity, 0.7-confidence repo-scope finding, attributed to the
  entry's line. A bare `WebFetch` and `WebFetch(domain:*)` count. Matching follows
  the documented `domain:` rules (case-insensitive, `*.` for subdomains, `*` within
  one label), and a deny or ask rule in the same `.claude/` folder cancels the
  finding. User and managed settings are not read. New predicate
  `repo_settings_preapprove_user_content_fetch`, backed by
  `ProjectConfigFacts.UserContentFetchAllow`. Rule schema 30 -> 31
  (provisional: whichever open branch merges later renumbers). The rule ships in a new pack file,
  `agent_config/fetch_permissions.yaml`.
- **Unpinned MCP package launches (rule schema 29 -> 30, provisional).** New
  repo-scope predicate `repo_mcp_config_launches_unpinned_package` and rule
  **MCPCFG-003** (high, 0.7) flag a committed MCP client config
  (`.mcp.json`, `.cursor/mcp.json`, `.vscode/mcp.json`,
  `claude_desktop_config.json`, `.gemini/settings.json`, `.codex/config.toml`)
  that starts a server with `npx`, `bunx`, `pnpm dlx`, `uvx`, `uv tool run`,
  `pipx run` or `docker run` on a package or image with no exact version: an
  exact semver for npm, `==` (or uv's `name@version`) for uv and pipx, an
  `@sha256:` digest for docker. Git, URL, local-path and `$VAR` specs are not
  judged, and a command inside the repo stays with MCPCFG-002, so the two rules
  never judge the same server entry. Lockfile pins are not seen. The package or
  image name is never echoed. MCPCFG-002's explanation no longer implies that an
  unpinned package might be flagged by it. **Release order:** this engine first,
  then the rules pack; a rule that needs a new predicate is skipped on an older
  engine. The schema number is provisional; it follows CSDK-115 (29), and
  whichever open branch merges later renumbers.
- **Coding agents in GitHub Actions workflows (rule schema 27 -> 28, provisional).** A new
  unconditionally loaded `ci` category (CI-001..005) with five repo-scope
  predicates backed by `RepoInventory.CIWorkflows`. The walk now reads the
  repo-root `.github/workflows/*.yml|yaml` into `ScanManifest.WorkflowFiles` and
  `github_workflow` components. CI-001 and CI-002 flag an agent job on an
  untrusted trigger (issues, issue and review comments, discussions,
  `pull_request_target`) with no actor gate, CI-002 when it also holds a write
  token or extra secrets; CI-003..005 flag persisted checkout credentials,
  unrestricted shell and a missing egress restriction. `claude-code-action` and
  `codex-action` count as gated through their own write-access check unless a
  `*` bypass input is set. One finding per step that runs an offending agent (for CI-003, per
  `actions/checkout` step in an agent job). `workflow_run` is not in
  the trigger set yet.

### Changed

- **Tool-call claims reach LangChain factory tools.** A tool built with
  `StructuredTool.from_function(fn)`, `StructuredTool(func=fn)` or
  `Tool.from_function(fn)` now gets the `claim_use` fact from its wrapped
  function, so LC-029 and LC-030 apply to it. A decorator on the wrapped
  function counts as a possible mediator, since no decorator registers these
  tools. No schema change.

### Fixed

- The `Harness files:` line in the scan summary no longer says that no rule judges
  those files. It now says what is checked: Codex hooks for risky commands and
  harness guidance files for hidden text, with OpenCode plugins and agents not
  judged.

## [0.1.15] - 2026-10-20

Rule schema 23 -> 26 (`rules.SupportedSchemaVersion`). The rules fixture goes
from 340 to 343 rules (AGCFG-008, AGCFG-009 and AGCFG-010; none removed). The
exit code and CLI flags are unchanged. `ScanID` changes, as with every schema
bump, because it folds the engine's supported schema version. **Overall scores
can fall on repositories that keep skills under `.agents/`** (see Changed).
The rules pack already carries AGCFG-008, AGCFG-009 and AGCFG-010. Engines older
than 0.1.15 skip them and report a degraded scan ("rules skipped") until they are
upgraded.

### Added

- **Codex and OpenCode harness files are now inventoried.** The file
  walk enters `.agents/` and `.opencode/`, so a skill at
  `.agents/skills/<name>/SKILL.md` is found like one under `skills/` or
  `.claude/skills/`. `.codex/hooks.json` is tagged `codex_hooks`,
  `.opencode/plugin(s)/*.{ts,js,mjs}` is tagged `opencode_plugin` and
  `.opencode/agent(s)/*.md` is tagged `opencode_agent`. The scan summary gains a
  `Harness files:` line. **No rule judges these files yet**, so an empty
  findings list over them is not a pass. A bare `plugin/` or `sub_agents/`
  directory outside `.opencode/` is deliberately not labelled. No schema change.

- **Codex project hooks (rule schema 23 -> 24).** New repo-scope predicate
  `repo_codex_hook_runs_repo_command` and rule **AGCFG-008** (medium) flag a
  committed `.codex/hooks.json` hook whose `command` or `commandWindows` runs a
  repo script (including the documented `$(git rev-parse --show-toplevel)/...`
  form), a network or inline-execution primitive, or a credential path. It is a
  separate rule from AGCFG-005 because Codex skips a project hook until the user
  reviews and trusts it, so the "runs before trust" claim does not apply. Codex
  hooks never feed AGCFG-005. `mcp_tool`, `prompt` and `agent` handlers carry no
  shell command and are skipped. Plugin `hooks/hooks.json` and inline `[hooks]`
  tables in `config.toml` are not read yet. **Release order:** this engine first,
  then the rules pack; a rule that needs a new predicate is skipped on an older
  engine.

- **Hidden text in `AGENTS.md` (rule schema 24 -> 25).** New repo-scope predicate
  `repo_agents_md_has_hidden_text` and rule **AGCFG-009** (medium) flag an
  `AGENTS.md` that contains zero-width characters, Unicode Tags-block characters
  or bidirectional overrides, which can hide an instruction from a reviewer while
  the model still reads it. It reuses the same character checks as the skill
  injection markers and judges no wording. A leading byte order mark, emoji or
  non-ASCII letters joined by a zero-width joiner or non-joiner, and
  subdivision-flag emoji are not flagged; right-to-left prose that uses
  bidirectional controls is, and is a known false positive. The hidden text is
  never echoed. `CLAUDE.md` is not read yet. **Release order:** this engine
  first, then the rules pack.

- **The staged OpenCode harness layout is inventoried.** A directory holding a
  `systemprompt.md` and a `guardrails.md` side by side is recognised as a staged
  harness tree, the layout the Harness² adaptation framework writes for its
  OpenCode executor. Its `systemprompt.md`, `guardrails.md`, `LongTermMEMORY.md`
  and `tool_descriptions/*.md` are tagged `harness_guidance`, `plugin/*.{ts,js,mjs}`
  is tagged `opencode_plugin` and `sub_agents/<name>/AGENT.md` is tagged
  `opencode_agent`, and the `Harness files:` line counts them. The pair is the
  signature: a lone `guardrails.md` or a bare `plugin/` directory is not labelled.
  **No rule judges these files yet**, so a clean result over them is not a pass.
  No schema change.

- **Hidden text in staged harness guidance files (rule schema 25 -> 26).** New
  repo-scope predicate `repo_harness_guidance_has_hidden_text` and rule
  **AGCFG-010** (medium) apply the AGCFG-009 character test (zero-width
  characters, Unicode Tags-block characters, bidirectional overrides, with the same
  exemptions) to the `harness_guidance` files of a staged OpenCode harness tree.
  It is a separate predicate and rule, so AGCFG-009 keeps its AGENTS.md-only
  meaning. The hidden text is never echoed. **Release order:** this engine first,
  then the rules pack.

### Changed

- **Scores can fall on repositories that keep skills under `.agents/`.** Those
  skills were never read before, so they now add findings and are scored. On
  one large open-source repository with 18 such skills, 54 findings were added
  (none removed) and the overall score fell by about six points, from 95.9% to
  89.7%. Tool and agent counts did not change. `ScanID` changes, as with any
  engine change. Repositories without `.agents/` or `.opencode/` are unaffected.

## [0.1.14] - 2026-10-16

Rule schema 18 → 23 (`rules.SupportedSchemaVersion`). The rules fixture goes
from 298 to 339 rules. Nothing in this release changes scoring, the exit code,
or an existing CLI flag. `ScanID` changes, as with every schema bump, because
it folds the engine's supported schema version.

### Added

- **Pre-trust agent config (schema 23).** A new unconditionally loaded
  `agent_config` category (AGCFG-004..007, MCPCFG-002) with five repo-scope
  predicates backed by `RepoInventory.ProjectConfig`. The walk now enters
  `.gemini/`, `.vscode/`, `.cursor/` and `.codex/`. Env files are read for key
  names only; no value is stored, logged or printed.
- **In-memory LangGraph checkpointer on a server path (schema 22).** LC-116
  flags a graph compiled with `MemorySaver` / `InMemorySaver` and run from a
  FastAPI, Flask, Celery or Dramatiq entrypoint. New predicates
  `agent_checkpointer_in_memory` and `agent_server_reachable`; run-call
  discovery gains a LangGraph anchor.
- **Server entrypoint discovery.** `RepoInventory.Entrypoints` (also
  `ScanResult.entrypoints`) records Python FastAPI / Flask routes and Celery /
  Dramatiq tasks, and stamps run calls reachable from one.
- **agent-reliability-otel-labels checks (schema 21).** OBS-006..008, a ladder
  over whether an instrumented agent uses the labels library, gated on
  `repo_observability_initialized`. Import aliases are followed and
  hand-written `trustabl.*` label keys are credited.
- **HTTPS-only fetch checks (schema 20).** 14 rules (CSDK-024/025, OAI-032/033,
  MCP-031/032, ADK-022/023, LC-026/027, CREW-015, PYD-015, AG2-022, VAI-021)
  over the new `has_unpinned_scheme_url_call` predicate, one per SSRF rule.
- **Reliability gaps (schema 19).** LC-114 (StateGraph with no checkpointer),
  LC-115 (side-effecting `add_node` function with no `interrupt(`), OAI-120 /
  PYD-108 (no wall-clock timeout on the run call), and the new `raw_llm_sdk`
  category, RAW-001 / RAW-002 (tool output fed into a dynamic system prompt in a
  raw `anthropic` / `openai` tool loop). ADK-113 / AG2-021 extend the
  wall-clock-timeout check to Google ADK and AutoGen.
- **More agent and side-effect rules.** CSDK-114, CREW-111, LC-113, VAI-020,
  and side-effect bounds Batch 2 (CREW-014, ADK-020/021, AG2-020, VAI-019,
  CSDK-023). The SSRF rules now credit a visible allow-list.
- **`trustabl forge` Runtime Tracing section.** The generated SKILL.md tells the
  coding model how to emit OpenTelemetry spans and add agent-reliability-otel-labels
  labels, with recipes for ten frameworks across Python, TypeScript and Go. New
  `--lang` flag. `TemplateVersion` is 3, so `forge check` reports older
  generated skills as outdated.
- **Distribution.** A scoped skill payload for the wshobson/agents marketplace
  (skill, manifest, disclosure README, licence; no hooks, MCP manifest or
  scripts), and catalogue metadata, a skill card and evals for `trustabl-scan`.

### Fixed

- Process-wide instrumentation calls (`logfire.instrument_pydantic_ai()`,
  `Agent.instrument_all()`, `LangChainInstrumentor().instrument()`) now count as
  observability initialization, so PYD-107 / LC-112 stop firing on a repo that
  followed their own fix text. Opik is recognized as a vendor.
- VAI-020 credits the Vercel AI native `timeout` option, and the wall-clock
  timeout check credits `abort_signal` / `cancellation_token`.
- `forge` filters rule sections by the languages each SDK is used in, and the
  ADK Go tracing recipe no longer shares one run across runs.

## [0.1.13] - 2026-09-30

The entries below shipped across 0.1.3 through 0.1.13; this section was not
split per release at the time.

### Changed

- **Test-path findings are excluded from scoring and the exit-code gate by
  default.** A finding whose file matches a test-path pattern (`tests/`,
  `__tests__/`, `testdata/`, `test_*.py`, `*_test.go`, `*.spec.ts`, …; see
  `internal/pathclass`) is still reported in every format but no longer counts
  toward `OverallScore`/`Surfaces` or fails the build — closing the reported
  gap where a `WebSearchTool` declared in a test fixture scored identically to
  one shipped in production. **This can change `overall_score` and the exit
  code** for a repo whose only agent/tool surface lives under a test path; pass
  `--include-test-paths` to restore the previous behavior exactly.
- **`ScanID` now folds the rules origin.** A provenance tag
  (`signed:<channel>` / `unsigned:custom` / `unsigned:default`) is folded into
  `ScanID` so two scans of the same code with rules of different provenance get
  distinct IDs. **This is a one-time change to every `ScanID`**: an otherwise
  unchanged scan produces a new ID after upgrading to this release. Provenance
  is now part of a scan's identity by design; baselines pinned to old IDs must
  be re-captured once.

- **The reported SDK list now describes the repository, not its test data.**
  `ScanResult.SDKs` (the `sdks` field, the `SDKs:` summary line, and the
  `sdks_detected` telemetry property) is derived from production surfaces only.
  Scanning a repo that vendors sample agent code under `testdata/` previously
  reported that sample code's SDKs as the repo's own — a Go repo reporting
  `google_adk`, `langchain` and `openai_agents` off its own fixtures. **This
  narrows `sdks` for any repo with vendored samples**, and shifts the
  `sdks_detected` telemetry series accordingly; pass `--include-test-paths` to
  restore the previous list. Findings, tools and agents are unaffected and stay
  complete. Rule-pack selection is unaffected: `SDKsDetected` is still derived
  from everything discovered, so test-path findings are still produced and
  reported.

### Fixed

- **`trustabl enrich` no longer rejects every generated Python fix on Windows.**
  Windows 11 ships App Execution Alias stubs, so `python3` resolves and runs
  even where no python3 is installed, printing "Python was not found" and
  exiting non-zero. The interpreter lookup trusted that resolution, never tried
  the `python` fallback, and reported the stub's failure as an authoritative
  "not valid Python" verdict instead of falling back to the tree-sitter check.
  Candidates are now probed with an empty program before use.

- **Attestation now works with cosign v3.** cosign v3 removed the
  `--tlog-upload` flag (it defaults `--use-signing-config=true`), which broke the
  offline `--no-tlog` signing path (`--tlog-upload=false is not supported with
  --signing-config`). Trustabl now detects the cosign major version and, on v3+,
  signs against a generated no-Rekor `--signing-config`; v2 keeps
  `--tlog-upload=false`. Verification is unchanged. CI runs the attestation e2e
  against both cosign v2 and v3 so this cannot regress.

### Added

- **`--include-test-paths` flag and `Finding.Origin`.** New `internal/pathclass`
  package classifies a finding's `FilePath` as production or test code by path
  shape. `Finding` gains an additive `origin` JSON field (`omitempty`, so
  production findings are wire-identical to before); the SARIF output gains a
  matching `properties.origin` plus a `result.suppressions` entry (GitHub code
  scanning excludes it from the default alert view without dropping it from
  the document); the human output gains a trailing "Test-path findings (not
  scored)" section. `--include-test-paths` opts these findings back into
  scoring and the exit code. See "Changed" below for the default-behavior
  impact.
- **`custom` LLM provider for self-hosted / local model servers.** `trustabl
  enrich` can now target any OpenAI-compatible endpoint (LiteLLM, vLLM, a local
  model server, etc.) instead of a hosted provider. Set `OPENAI_BASE_URL` to
  activate it — it takes priority over a plain `OPENAI_API_KEY`, and unlike
  every other provider, `custom` does not require a key at all, since
  self-hosted endpoints often don't check one. `TRUSTABL_LLM_MODEL` is
  required (`custom` has no default model).
- **Scan attestation (opt-in).** New `internal/attest` package plus `trustabl
  attest` / `trustabl verify` subcommands and a `scan --attest` flag. Trustabl
  renders a deterministic in-toto predicate (type
  `https://trustabl.dev/attestation/scan/v1`) from a `ScanResult` and shells out
  to the **cosign** CLI to sign the JSON report (the attestation subject) and to
  verify it. Signing is **keyless by default** (ambient CI OIDC; the event is
  logged to the public Rekor transparency log) with a `--key` escape hatch for
  offline/private signing (`--no-tlog`). Verification is consumer-side and pins
  the signer identity + OIDC issuer. Requires the cosign CLI on PATH; a plain
  `scan` is unchanged and free of any new dependency. The predicate is **not**
  folded into `ScanID`.
- **Signed rules distribution — trust core (opt-in).** New `internal/rulesign`
  package: a reproducible bundle digest (sha256 over a normalized tar), an
  embedded Ed25519 trust keyring (verify-by-key-ID with validity windows,
  fail-closed), and signed channel statements that bind a channel to one bundle
  by digest with freshness and anti-rollback checks. A new `releaseSource`
  (behind the new `rulesource.Source` interface) resolves rules from a signed
  release channel — verify the statement, fetch the bundle by digest, re-derive
  and match the digest, install to a content-addressed cache — with an offline
  fallback that serves (and watermarks as stale) the last verified bundle.
  Reachable via the new opt-in **`--channel <name>`** flag; the **default scan
  is unchanged** (still the git source). Until signing keys are published a
  `--channel` scan refuses with exit `2` rather than running unverified rules.
  A scan that did not use blessed production rules — a pre-release channel or an
  unsigned `--rules-repo` source — is now **watermarked** in the human report
  and in the JSON `rules_origin` field.

- **`--verbose` / `--debug` diagnostics.** New global (persistent) flags on the
  root command, valid on every subcommand and placeable before or after it
  (`-v`/`--verbose`, `--debug`; `--debug` implies `--verbose`). `--verbose`
  narrates the scan on stderr — rule provenance (repo, ref, resolved SHA, cache
  fallback), per-phase discovery counts (languages, tools, agents, detected and
  unaudited SDKs, loaded detectors), output destinations, and a result summary
  (scan ID, score, findings by severity, exit code). `--debug` adds per-phase
  timing and capped per-entity / per-finding detail. Backed by a new leaf
  package `internal/logx` (nil-safe leveled logger). All diagnostics are
  **stderr-only**, so the report on stdout and the JSON/SARIF byte-stability
  contract are unaffected (`--format json --debug` still emits a clean
  document). Diagnostic color follows the report's rules (off under
  `--no-color`, `NO_COLOR`, or a non-terminal stderr). Because an animated
  progress panel and interleaved log lines would corrupt each other,
  `--verbose`/`--debug` render progress as plain `[phase]` lines.

- **`trustabl llm provider` — provider switching.** New subcommand group for
  managing which LLM provider is active:
  - `trustabl llm provider set <provider>` — switch the active provider;
    auto-creates an entry with a per-provider default model if not yet
    configured (`anthropic → claude-haiku-4-5`, `openai → gpt-4.1-nano`,
    `google → gemini-2.5-flash-lite`). Prints a key hint when a new provider
    is created.
  - `trustabl llm provider list` — list all configured providers; active
    provider marked with `*`.

- **`trustabl llm` — LLM provider configuration.** New command group for
  managing LLM provider keys and models, stored at
  `~/.config/trustabl/keys.json` (mode 0600, atomic write):
  - `trustabl llm list` — table of configured providers with masked keys;
    active provider marked with `*`.
  - `trustabl llm key set [key]` — store an API key (prompts securely if
    key is omitted; validates format for `anthropic` keys).
  - `trustabl llm key get` — display the masked key for the active provider.
  - `trustabl llm key delete` — delete the key with a `y/N` confirmation prompt.
  - `trustabl llm model set <model>` — set the model for the active provider.
  Defaults: provider `anthropic`, model `claude-haiku-4-5`. Multi-provider
  shape is in place from day one. Pre-requisite for `trustabl enrich`.

## [0.1.2] - 2026-06-03

### Added

- **`AGENTS.md` as a vendor-neutral repo-hygiene signal.** The scanner now
  discovers `AGENTS.md` alongside `CLAUDE.md`; the repo-guidance rules
  (`CSDK-203` / `OAI-202` / `ADK-201`) accept either file and fire only when
  neither is present, so a repo that documents its agents in `AGENTS.md` is no
  longer flagged.

### Changed

- **Interrupt cleanup.** Ctrl-C during a scan now cancels the in-flight work and
  drains it, so the rules-clone temp directory is removed instead of being
  leaked on interrupt.
- **`--strict` floors at low severity.** `info`/`META` findings no longer fail
  the build under `--strict`; only real findings gate CI.
- **Clearer schema-incompatibility error.** When a resolved rule pack targets a
  schema the engine cannot evaluate, the CLI now explains what to do (including
  `--rules-ref` troubleshooting) instead of failing opaquely.
- Determinism hardening: tools and agents are sorted by `(FilePath, Line, Name)`
  before edge resolution, and findings carry a total-order sort with adjacent
  dedup, so the report stays byte-stable regardless of walk order.

### Fixed

- **Subagent discovery no longer reports scaffolding as live agents.** The
  flat-collection fallback now skips `TEMPLATE.md` and `*-template.md` files,
  whose subagent-shaped frontmatter is a fill-in example rather than a real
  declaration. Files placed under the canonical `.claude/agents/` path are
  unaffected.

## [0.1.1] - 2026-06-01

### Added

- **TypeScript discovery for the OpenAI Agents SDK and Google ADK.** Previously
  only the Claude TypeScript surface was understood; the scanner now discovers
  tools, agents, guardrails, sessions, MCP servers, and hosted tools in the
  OpenAI Agents (`tool({...})`, `Agent({...})` / `Agent.create(...)`) and Google
  ADK TypeScript/JavaScript shapes, resolving agent→tool/handoff/guardrail edges
  by variable name. Backed by vendored, licensed example corpora.
- **Markdown agent surface.** Discovers and parses Claude Code skills
  (`SKILL.md`), slash commands (frontmatter `allowed-tools` / `model`), and
  plugins (`.claude-plugin/plugin.json`, `marketplace.json`, including the
  object form of `plugins[].source`), plus flat-collection subagents matched by
  frontmatter shape. These surface in the scan summary, and the presence of
  subagents/skills now triggers the Claude SDK rule pack even with no SDK code.
- **Repo-scope permission-bypass detection.** `CSDK-201` flags a
  `.claude/settings.json` `defaultMode: bypassPermissions`; `CSDK-202` flags
  `ClaudeAgentOptions(permission_mode="bypassPermissions")`. (Rule
  `schema_version` advanced to 8.)
- **Hosted-tool approval and policy checks.** Hosted-tool kwargs are captured and
  new predicates evaluate them, powering rules such as `OAI-111` and `ADK-008`
  for missing approval gates / safety policies on privileged hosted tools.
- **Per-finding line attribution.** Findings now carry a real
  `file:line`–`end_line` range for MCP servers, hosted tools, subagents,
  guardrails, sessions, and individual permission rules (a `Location` type is
  embedded across the inventory; the `line` JSON field was renamed to
  `start_line`).
- New and expanded rules across all three SDKs (Tier-1 additions for Claude,
  OpenAI, and ADK; `has_code_exec_call` and `has_print_call` predicates;
  `mcp_tool` applicability on `CSDK-004/005/006`).
- An `llms.txt` index pointing at the project documentation.

### Changed

- **Production-readiness hardening.** A panicking detector is now recovered and
  skipped (one malformed rule can no longer crash a scan); all network git
  operations are bounded by a timeout; rules-repo and target URLs are restricted
  to `https`/`ssh` (`file://` and `git://` are rejected); individual scanned
  files are size-capped and symlinks are skipped; SARIF results are sorted
  deterministically and file URIs no longer leak the local clone path; and parse
  **coverage** (files parsed vs. skipped) is now surfaced so an incomplete scan
  is never mistaken for a clean one.
- The risk-surfaces summary line now reports a count, examples, why it matters,
  and how to fix it — and no longer claims an OpenShell audit that does not ship.
- The example corpus moved to `testdata/corpus/`.

### Fixed

- **Rules that never fired.** `CSDK-102`; `ADK-104` (had been firing on every
  `LlmAgent`, re-pointed to `generate_content_config.safety_settings`); `ADK-105`
  (class-name mismatch).
- **Determinism.** `ScanID` now folds in every inventoried file list (not just
  Python), and TypeScript parameter names are sorted, so the ID and report stay
  byte-stable across more inputs.
- **Rule sourcing.** A local install fault (disk full, permission denied, failed
  rename, corrupt clone) is propagated instead of being masked as a cache hit.
- **Scoring.** Per-tool readiness is keyed by `(FilePath, Name)` so same-named
  tools in different files no longer collide.
- **Rule loading.** The loader rejects match predicates the rule's scope never
  evaluates, enforces the confidence upper bound (and rejects `NaN`), and applies
  the rule `language` gate at repo scope.
- AST-walk recursion is depth-bounded against adversarial nesting, and
  tree-sitter parsers are reused with parsed trees closed to bound memory.

## [0.1.0] - 2026-05-26

First tagged release. A single-binary, read-only static analyzer for agent-SDK
repositories that discovers an agent/tool inventory and reports reliability and
safety weaknesses.

### Added

- **Scan pipeline.** Flat, deterministic pipeline — recon (cheap, no AST) →
  per-language AST inventory → data-driven policy selection → analysis →
  scoring → review — exposed through a single `trustabl scan <target>` over a
  local path or a remote repo URL (cloned read-only; nothing is written into
  the scanned repo).
- **SDK and language coverage.**
  - Claude Agent SDK — Python (tools, agents, subagents at any path depth,
    `.claude/settings.json` permissions) and TypeScript (`tool()` factory,
    `query()` main-thread agents, inline/typed-const `AgentDefinition`,
    `createSdkMcpServer` + MCP config literals).
  - OpenAI Agents SDK — Python (tools, 11 hosted-tool classes, agents, 3 MCP
    server transports, guardrails, sessions).
  - Google ADK — Python (`LlmAgent`/`Sequential`/`Parallel`/`Loop`/`Langgraph`,
    `FunctionTool` wrapping, 13 built-in hosted tools, `sub_agents` edges).
  - MCP — Python tool registrations and config files.
  - OpenShell — shell-invocation risk surface (`subprocess` / `os.system` /
    `os.popen`) and `openshell/*.yaml` policy files.
- **Four-scope detection model.** Rules fire at `tool`, `agent`, `subagent`, or
  `repo` scope, each receiving a typed input; agent-scoped findings attribute to
  the specific agent (with resolved tool / handoff / guardrail edges), not the
  repo.
- **YAML-driven rule engine.** Validating loader (required fields, enum and
  scope checks, cross-file rule-ID uniqueness), a recursive conjunctive
  match-expression evaluator, and per-tool + overall reliability scoring. Ships
  the CSDK / OAI / ADK rule packs plus engine-emitted META findings (META-001..004)
  for honest "unaudited SDK" / "unused dep" / "opaque agent" / "audited but no
  applicable rule" signals.
- **External rule resolution.** Rules are not embedded in the binary; they are
  resolved at scan time from the `trustabl-rules` git repository, cached locally
  under the user cache dir, with offline cache fallback and a `schema_version`
  compatibility gate. `trustabl rules pull` fetches eagerly without scanning.
- **Output formats.** Human summary, `--format json`, and `--format sarif`
  (SARIF 2.1.0, accepted by `github/codeql-action/upload-sarif`).
- **Determinism contract.** Identical inputs produce an identical `ScanID`
  (which folds in the resolved rules SHA) and a byte-stable report; enforced by
  a determinism regression test.
- **CLI surface.** `trustabl scan` (with `--detectors`, `--format`, `--strict`,
  `--no-color`, `--no-progress`, `--rules-repo`, `--rules-ref`,
  `--no-rules-update`), `trustabl rules pull`, and `trustabl version`. Exit
  codes: `0` clean, `1` findings ≥ medium (or any finding under `--strict`),
  `2` scanner error or no usable rules.
- **Progress reporting.** Real-time recon / inventory / analysis progress on
  stderr (animated on a TTY, plain lines when piped, silent for JSON) that never
  touches stdout or the report.
- Release distribution via GitHub Releases, Homebrew tap, Scoop bucket, and a
  multi-arch `ghcr.io/trustabl/trustabl` Docker image.
