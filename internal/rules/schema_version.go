package rules

// SupportedSchemaVersion is the highest rule-schema version this engine build
// understands. A rule pack declares the schema version it targets in its
// manifest.yaml.
//
// Forward-compatible loading (see rules.LoadLenient): the engine NO LONGER
// rejects a pack just because its schema_version exceeds this constant. It
// loads the pack leniently — a rule that references a scope, applies_to value,
// language, or predicate this build lacks is skipped (surfaced as a stderr
// warning, the META-005 info finding, and ScanResult.RulesSkipped), and the scan
// proceeds with the rules it does understand. A pack is refused only when it has no usable manifest
// (ErrNoCompatibleRules) or when NO rule is evaluable at all
// (ErrAllRulesIncompatible). This decouples additive rule updates from binary
// upgrades: shipping a new predicate no longer locks every older binary out of
// the entire pack — older binaries skip the rules they can't evaluate and keep
// running the rest.
//
// Bumping this constant is therefore an informational signal — it advertises
// this build's support level, drives the "rules are newer than this build"
// warning, and is folded into ScanID so the ID stays honest about the effective
// ruleset — NOT a fleet-wide gate. Bump it when this build gains a new predicate
// or schema field.
//
// Make BREAKING changes by RENAMING the predicate (or field), never by silently
// redefining an existing one: an old binary then sees an unknown key and skips
// that rule (safe) instead of mis-evaluating a predicate whose meaning changed.
// That rename discipline is what keeps lenient loading safe without a hard
// version gate.
//
// Schema 28 adds the CI workflow predicates (repo_ci_agent_*), the `ci`
// category and the `github_actions` repo applies_to token.
//
// Schema 29 adds the subagent_inherits_all_tools predicate (subagent scope).
//
// Schema 30 adds the repo_mcp_config_launches_unpinned_package predicate
// (repo scope).
//
// Schema 31 adds the repo_settings_preapprove_user_content_fetch predicate
// (repo scope).
//
// Schema 32 adds the tool_claim_use predicate (tool scope) and the claim_use
// discovery fact.
//
// Schema 33 adds the claim_use fact on CrewAI and Pydantic AI tools and the
// repo-level claim mediator pass (analysis.ApplyClaimMediators).
//
// Schema 34 adds ToolDef.InputKeys, so tool_claim_use sees a claim declared in a
// Claude Agent SDK tool's input schema.
//
// Schema 35 adds the claim_use fact on TypeScript OpenAI Agents tools.
//
// Schema 36 adds the claim_use fact on Google ADK FunctionTool and AutoGen tools.
//
// Schema 37 adds the repo_settings_allow_exec_capable_prefix predicate (repo
// scope).
//
// Schema 38 adds the repo_mcp_config_unscoped_credential and
// repo_mcp_config_remote_plain_http predicates (repo scope).
//
// Schema 39 adds the repo_guidance_file_has_hidden_text predicate (repo scope).
//
// Schema 40 adds the repo_settings_env_redirects_model_endpoint predicate (repo
// scope).
//
// Schema 41 adds the repo_settings_allow_all_bash and
// repo_script_runs_agent_cli_with_bypass_flag predicates (repo scope).
//
// Schema 42 adds the repo_otel_labels_reasoning_unhashed,
// repo_otel_labels_reasoning_labelled and repo_model_reasoning_enabled predicates
// (repo scope), read by the reasoning-label rules OBS-013..014.
//
// Schema 43 adds the repo_agent_server_allows_any_origin and
// repo_agent_server_exposed_without_key predicates (repo scope, category
// agent_server).
//
// Schema 44 adds the agent_deep_agent_shell_execute_ungated predicate (agent
// scope) and the langchain_deep_agent applies_to token.
//
// Schema 45 adds the slash_command scope (applies_to claude_slash_command) and its three
// predicates, slash_command_allows_unrestricted_shell, slash_command_model_invocable and
// slash_command_allows_unrestricted_tool. A binary without the scope skips those
// rules with META-005.
//
// Schema 46 adds the tool_description_has_hidden_text predicate (tool scope) and the
// repo_mcp_server_binds_all_interfaces_unprotected predicate (repo scope). Provisional: this branch
// numbers it after main's 45.
//
// Schema 47 adds seven predicates: repo_settings_allow_unscoped_grant,
// repo_claude_settings_local_committed and repo_mcp_config_launches_deprecated_package (repo
// scope), and skill_bundled_script_decodes_and_executes,
// skill_bundled_script_destructive_command, skill_bundled_file_is_opaque and
// skill_unpinned_install (skill scope). A binary below 47 skips the rules that use them with
// META-005.
//
// Schema 48 adds nine predicates: repo_mcp_prefix_containment_unsafe,
// repo_mcp_stdio_client_nonliteral_command, repo_mcp_server_bearer_without_audience,
// repo_mcp_client_opens_server_url, repo_mcp_server_shares_transport,
// repo_mcp_server_dns_rebinding_disabled and repo_mcp_server_installs_package_from_input (repo
// scope), and agent_handoff_missing_input_filter and agent_tools_match (agent scope). A binary
// below 48 skips the rules that use them with META-005.
//
// Schema 49 adds the repo_call_kwarg_matches predicate (repo scope) and the
// transitive_capability_exceeds_direct predicate (agent scope). Batch 2 holds 48.
//
// Schema 50 adds repo_guidance_text_matches, repo_guidance_directs_unverified_install (guidance
// files), repo_judge_model_floating_alias, repo_otel_span_attrs_missing and
// repo_has_consequential_tool (repo scope). 48 is held by static batch 2 and 49 by static
// batch 4, both merged; 50 is provisional. A binary below 50 skips the rules that use them
// with META-005.
//
// Schema 51 adds the tool_source_reaches_sink predicate (tool scope) over
// ToolDef.TaintFlows, the one-function taint pass in analysis/taint*.go. A binary below 51 skips
// the rules that use it with META-005.
//
// Schema 53 adds three predicates: tool_flow_reaches_sink (tool scope),
// repo_source_reaches_sink (repo scope) and skill_bundled_script_prints_secret (skill scope),
// . They read the secret_env, llm_output and
// handler_param sources the taint pass gained in static batch 7. 52 is held by static batch 8;
// 53 is provisional. A binary below 53 skips the rules that use them with META-005.
const SupportedSchemaVersion = 53
