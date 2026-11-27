package rules_test

import (
	"github.com/trustabl/trustabl/internal/models"
)

// Fire and silent cases for the static batch 8 rules (OAI-212, ADK-204, LC-206, PYD-204, CI-006,
// CI-007). The effect and gate facts come from rules.AttachToolEffects and the CI facts from
// the analysis package, which have their own tests; these cases cover the predicate and the
// rule wiring through the shared tables.

func b8Agent(sdk models.SDK, class string, kwargs map[string]*models.KwargTree, tools ...models.ToolDef) models.AgentDef {
	a := models.AgentDef{SDK: sdk, Class: class, Language: models.LanguagePython, Name: "a", VarName: "a",
		Location: models.Location{FilePath: "app/agent.py"}}
	if kwargs != nil {
		a.Kwargs = &models.KwargTree{Children: kwargs}
	}
	for i := range tools {
		t := tools[i]
		a.ToolRefs = append(a.ToolRefs, models.ToolRef{Name: t.Name, Resolved: &t})
	}
	return a
}

func b8Tool(name string, effects []string, config map[string]string, gates ...string) models.ToolDef {
	return models.ToolDef{Name: name, Effects: effects, Config: config, BodyGates: gates, Location: models.Location{FilePath: "app/agent.py"}}
}

func b8Kw(text string) map[string]*models.KwargTree {
	return map[string]*models.KwargTree{"x": {Value: &models.Expr{Kind: models.ExprNameRef, Text: text}}}
}

func b8Case(name, id string, a models.AgentDef, want bool) policyAgentCase {
	return policyAgentCase{name, id, a, models.RepoInventory{}, want}
}

func init() {
	named := []string{"named"}
	shell := []string{"shell"}
	none := map[string]*models.KwargTree(nil)
	policyAgentRuleCases = append(policyAgentRuleCases,
		b8Case("OAI-212 fires on a named tool with no gate", "OAI-212",
			b8Agent(models.SDKOpenAIAgents, "Agent", none, b8Tool("send_email", named, nil)), true),
		b8Case("OAI-212 silent with needs_approval=True", "OAI-212",
			b8Agent(models.SDKOpenAIAgents, "Agent", none, b8Tool("send_email", named, map[string]string{"needs_approval": "True"})), false),
		b8Case("OAI-212 fires with needs_approval=False", "OAI-212",
			b8Agent(models.SDKOpenAIAgents, "Agent", none, b8Tool("send_email", named, map[string]string{"needs_approval": "False"})), true),
		b8Case("OAI-212 silent with a tool input guardrail", "OAI-212",
			b8Agent(models.SDKOpenAIAgents, "Agent", none, b8Tool("send_email", named, map[string]string{"tool_input_guardrails": "[g]"})), false),
		b8Case("OAI-212 silent on a tool with no effect", "OAI-212",
			b8Agent(models.SDKOpenAIAgents, "Agent", none, b8Tool("lookup", nil, nil)), false),
		b8Case("OAI-212 silent when only the body shows a shell call", "OAI-212",
			b8Agent(models.SDKOpenAIAgents, "Agent", none, b8Tool("run", shell, nil)), false),
		b8Case("OAI-212 silent on an opaque agent", "OAI-212", func() models.AgentDef {
			a := b8Agent(models.SDKOpenAIAgents, "Agent", none, b8Tool("send_email", named, nil))
			a.Opaque = true
			return a
		}(), false),
		b8Case("ADK-204 fires on a shell tool with no gate", "ADK-204",
			b8Agent(models.SDKGoogleADK, "LlmAgent", none, b8Tool("run", shell, nil)), true),
		b8Case("ADK-204 silent with require_confirmation", "ADK-204",
			b8Agent(models.SDKGoogleADK, "LlmAgent", none, b8Tool("send_invoice", named, map[string]string{"require_confirmation": "True"})), false),
		b8Case("ADK-204 silent with a request_confirmation call", "ADK-204",
			b8Agent(models.SDKGoogleADK, "LlmAgent", none, b8Tool("send_invoice", named, nil, "request_confirmation")), false),
		b8Case("ADK-204 silent with a before_tool_callback", "ADK-204",
			b8Agent(models.SDKGoogleADK, "LlmAgent", map[string]*models.KwargTree{"before_tool_callback": {Value: &models.Expr{Kind: models.ExprNameRef, Text: "check"}}}, b8Tool("send_invoice", named, nil)), false),
		b8Case("ADK-204 fires when before_tool_callback is None", "ADK-204",
			b8Agent(models.SDKGoogleADK, "LlmAgent", map[string]*models.KwargTree{"before_tool_callback": {Value: &models.Expr{Kind: models.ExprLiteralNone, Text: "None"}}}, b8Tool("send_invoice", named, nil)), true),
		b8Case("LC-206 fires on a file-writing tool with no gate", "LC-206",
			b8Agent(models.SDKLangChain, "CreateAgent", none, b8Tool("save", []string{"file_write"}, nil)), true),
		b8Case("LC-206 silent with an interrupt call in the tool", "LC-206",
			b8Agent(models.SDKLangChain, "CreateAgent", none, b8Tool("transfer_funds", named, nil, "interrupt")), false),
		b8Case("LC-206 silent with interrupt_on", "LC-206",
			b8Agent(models.SDKLangChain, "CreateAgent", map[string]*models.KwargTree{"interrupt_on": {Value: &models.Expr{Kind: models.ExprNameRef, Text: "{}"}}}, b8Tool("transfer_funds", named, nil)), false),
		b8Case("LC-206 silent with HumanInTheLoopMiddleware in middleware", "LC-206",
			b8Agent(models.SDKLangChain, "CreateAgent", map[string]*models.KwargTree{"middleware": {Value: &models.Expr{Kind: models.ExprList, Text: "[...]", List: []models.Expr{{Kind: models.ExprCall, Text: "HumanInTheLoopMiddleware(interrupt_on={})"}}}}}, b8Tool("transfer_funds", named, nil)), false),
		b8Case("LC-206 fires with another middleware", "LC-206",
			b8Agent(models.SDKLangChain, "CreateAgent", map[string]*models.KwargTree{"middleware": {Value: &models.Expr{Kind: models.ExprList, Text: "[...]", List: []models.Expr{{Kind: models.ExprCall, Text: "SummarizationMiddleware()"}}}}}, b8Tool("transfer_funds", named, nil)), true),
		b8Case("PYD-204 fires on a named tool with no gate", "PYD-204",
			b8Agent(models.SDKPydanticAI, "PydanticAgent", none, b8Tool("send_sms", named, nil)), true),
		b8Case("PYD-204 silent with requires_approval", "PYD-204",
			b8Agent(models.SDKPydanticAI, "PydanticAgent", none, b8Tool("send_sms", named, map[string]string{"requires_approval": "True"})), false),
		b8Case("PYD-204 silent with an ApprovalRequired raise", "PYD-204",
			b8Agent(models.SDKPydanticAI, "PydanticAgent", none, b8Tool("send_sms", named, nil, "ApprovalRequired")), false),
		b8Case("PYD-204 silent with a toolset that sets requires_approval", "PYD-204",
			b8Agent(models.SDKPydanticAI, "PydanticAgent", map[string]*models.KwargTree{"toolsets": {Value: &models.Expr{Kind: models.ExprCall, Text: "FunctionToolset([f], requires_approval=True)"}}}, b8Tool("send_sms", named, nil)), false),
	)
	hit := models.ConfigHit{Location: models.Location{FilePath: ".claude/hooks/merge.sh", Line: 2, EndLine: 2}, Reason: "hook script runs gh pr merge --admin"}
	policyRepoRuleCases = append(policyRepoRuleCases,
		policyRepoCase{"CI-006 fires when a hit was recorded", "CI-006", models.RepoProfile{},
			models.RepoInventory{ProjectConfig: models.ProjectConfigFacts{AgentPRSelfApproval: []models.ConfigHit{hit}}}, true},
		policyRepoCase{"CI-006 silent when no hit was recorded", "CI-006", models.RepoProfile{}, models.RepoInventory{}, false},
		policyRepoCase{"CI-007 fires when a gap was recorded", "CI-007", models.RepoProfile{},
			models.RepoInventory{CIWorkflows: models.CIWorkflowFacts{CodeownersGap: []models.CIHit{{Location: models.Location{FilePath: ".github/workflows/a.yml", Line: 1, EndLine: 1}, Reason: "x"}}}}, true},
		policyRepoCase{"CI-007 silent when no gap was recorded", "CI-007", models.RepoProfile{}, models.RepoInventory{}, false},
	)
}
