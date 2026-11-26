package rules_test

import (
	"github.com/trustabl/trustabl/internal/models"
)

// Fire and silent cases for the static batch 4 rules (AGCFG-019, 020, 021, OAI-210, OAI-211).
// The cases are appended to the shared tables in init so TestPolicyRules_AllRulesCovered sees them.

func b4Hit(callee, kwarg, value string) models.CallKwargHit {
	return models.CallKwargHit{
		FilePath: "app/client.py", Line: 7, Language: models.LanguagePython,
		Callee: callee, Kwarg: kwarg, Value: value,
	}
}

func b4Inv(hits ...models.CallKwargHit) models.RepoInventory {
	return models.RepoInventory{CallKwargs: hits}
}

func b4ShellTool() *models.ToolDef {
	return &models.ToolDef{Name: "run", Kind: models.KindShellInvocation}
}

func b4Agent(name string, guarded bool, shell bool, handoffs ...*models.AgentDef) *models.AgentDef {
	a := &models.AgentDef{
		Name: name, SDK: models.SDKOpenAIAgents, Class: "Agent", Language: models.LanguagePython,
		Location: models.Location{FilePath: "app/agents.py", Line: 10 + len(name)},
		Kwargs:   &models.KwargTree{Children: map[string]*models.KwargTree{}},
	}
	if guarded {
		a.Kwargs.Children["input_guardrails"] = &models.KwargTree{Value: &models.Expr{Kind: models.ExprList, Text: "[g]"}}
	}
	if shell {
		a.ToolRefs = []models.ToolRef{{Name: "run", Resolved: b4ShellTool()}}
	}
	for _, h := range handoffs {
		a.HandoffRefs = append(a.HandoffRefs, models.AgentRef{Name: h.Name, Resolved: h})
	}
	return a
}

func init() {
	// A chain entry -> mid -> worker (shell), a cycle a <-> b with no shell, and a self edge.
	worker := b4Agent("worker", false, true)
	mid := b4Agent("mid", false, false, worker)
	entry := b4Agent("entry", false, false, mid)
	guardedEntry := b4Agent("gentry", true, false, mid)
	direct := b4Agent("direct", false, false, worker)
	direct.ToolRefs = []models.ToolRef{{Name: "run", Resolved: b4ShellTool()}}
	cyA := b4Agent("cya", false, false)
	cyB := b4Agent("cyb", false, false, cyA)
	cyA.HandoffRefs = []models.AgentRef{{Name: "cyb", Resolved: cyB}}
	cyShell := b4Agent("cyshell", false, true)
	cyC := b4Agent("cyc", false, false, cyShell)
	cyShell.HandoffRefs = []models.AgentRef{{Name: "cyc", Resolved: cyC}}
	self := b4Agent("selfy", false, false)
	self.HandoffRefs = []models.AgentRef{{Name: "selfy", Resolved: self}}
	plain := b4Agent("plain", false, false, b4Agent("reader", false, false))
	external := b4Agent("ext", false, false)
	external.HandoffRefs = []models.AgentRef{{Name: "elsewhere", External: true}}
	hosted := b4Agent("hosted", false, false)
	hosted.HostedToolRefs = []models.HostedToolRef{{Class: "ShellTool"}}
	viaHosted := b4Agent("viahosted", false, false, hosted)
	shellTarget := b4Agent("st", false, false)
	shellTarget.ToolRefs = []models.ToolRef{{Name: "x", Resolved: &models.ToolDef{
		Name: "x", Kind: models.KindOpenAITool, Facts: map[string]string{"shells_out": "true"},
	}}}
	viaDecorated := b4Agent("viadec", false, false, shellTarget)
	noHandoff := b4Agent("lonely", false, false)

	policyRepoRuleCases = append(policyRepoRuleCases,
		// AGCFG-019
		policyRepoCase{"AGCFG-019 fires on StdioServerParameters env=environ", "AGCFG-019", models.RepoProfile{},
			b4Inv(b4Hit("StdioServerParameters", "env", "environ")), true},
		policyRepoCase{"AGCFG-019 fires on StdioClientTransport env=environ", "AGCFG-019", models.RepoProfile{},
			b4Inv(b4Hit("StdioClientTransport", "env", "environ")), true},
		policyRepoCase{"AGCFG-019 silent on an unrelated callee", "AGCFG-019", models.RepoProfile{},
			b4Inv(b4Hit("Popen", "env", "environ")), false},
		policyRepoCase{"AGCFG-019 silent when no call is recorded", "AGCFG-019", models.RepoProfile{}, b4Inv(), false},
		policyRepoCase{"AGCFG-019 silent on a different keyword", "AGCFG-019", models.RepoProfile{},
			b4Inv(b4Hit("StdioServerParameters", "cwd", "environ")), false},
		// AGCFG-020
		policyRepoCase{"AGCFG-020 fires on warn_only=True at an ssrf callee", "AGCFG-020", models.RepoProfile{},
			b4Inv(b4Hit("validate_url_for_ssrf", "warn_only", "true")), true},
		policyRepoCase{"AGCFG-020 fires on a mixed-case ssrf callee", "AGCFG-020", models.RepoProfile{},
			b4Inv(b4Hit("checkSSRF", "warn_only", "true")), true},
		policyRepoCase{"AGCFG-020 fires on ssrf_protection_enabled=False at any callee", "AGCFG-020", models.RepoProfile{},
			b4Inv(b4Hit("Settings", "ssrf_protection_enabled", "false")), true},
		policyRepoCase{"AGCFG-020 silent on warn_only=True at an unrelated callee", "AGCFG-020", models.RepoProfile{},
			b4Inv(b4Hit("use_deterministic_algorithms", "warn_only", "true")), false},
		policyRepoCase{"AGCFG-020 silent on ssrf_protection_enabled=True", "AGCFG-020", models.RepoProfile{},
			b4Inv(b4Hit("Settings", "ssrf_protection_enabled", "true")), false},
		policyRepoCase{"AGCFG-020 silent on warn_only=False at an ssrf callee", "AGCFG-020", models.RepoProfile{},
			b4Inv(b4Hit("validate_url_for_ssrf", "warn_only", "false")), false},
		// AGCFG-021
		policyRepoCase{"AGCFG-021 fires on from_pretrained trust_remote_code=True", "AGCFG-021", models.RepoProfile{},
			b4Inv(b4Hit("from_pretrained", "trust_remote_code", "true")), true},
		policyRepoCase{"AGCFG-021 silent on trust_remote_code=False", "AGCFG-021", models.RepoProfile{},
			b4Inv(b4Hit("from_pretrained", "trust_remote_code", "false")), false},
		policyRepoCase{"AGCFG-021 silent on another callee", "AGCFG-021", models.RepoProfile{},
			b4Inv(b4Hit("pipeline", "trust_remote_code", "true")), false},
	)

	policyAgentRuleCases = append(policyAgentRuleCases,
		// OAI-210 (unguarded entry)
		policyAgentCase{"OAI-210 fires two hops from shell", "OAI-210", *entry, models.RepoInventory{}, true},
		policyAgentCase{"OAI-210 fires one hop from shell", "OAI-210", *mid, models.RepoInventory{}, true},
		policyAgentCase{"OAI-210 silent when the agent has shell itself", "OAI-210", *direct, models.RepoInventory{}, false},
		policyAgentCase{"OAI-210 silent when the agent is guarded", "OAI-210", *guardedEntry, models.RepoInventory{}, false},
		policyAgentCase{"OAI-210 silent on a chain with no shell", "OAI-210", *plain, models.RepoInventory{}, false},
		policyAgentCase{"OAI-210 silent on an agent with no handoffs", "OAI-210", *noHandoff, models.RepoInventory{}, false},
		policyAgentCase{"OAI-210 silent on a cycle with no shell", "OAI-210", *cyA, models.RepoInventory{}, false},
		policyAgentCase{"OAI-210 fires through a cycle that reaches shell", "OAI-210", *cyC, models.RepoInventory{}, true},
		policyAgentCase{"OAI-210 silent on a self edge", "OAI-210", *self, models.RepoInventory{}, false},
		policyAgentCase{"OAI-210 silent on an external handoff", "OAI-210", *external, models.RepoInventory{}, false},
		policyAgentCase{"OAI-210 fires through a hosted ShellTool target", "OAI-210", *viaHosted, models.RepoInventory{}, true},
		policyAgentCase{"OAI-210 fires through a decorated tool that shells out", "OAI-210", *viaDecorated, models.RepoInventory{}, true},
		// OAI-211 (guarded entry)
		policyAgentCase{"OAI-211 fires on a guarded agent two hops from shell", "OAI-211", *guardedEntry, models.RepoInventory{}, true},
		policyAgentCase{"OAI-211 silent on an unguarded agent with the same chain", "OAI-211", *entry, models.RepoInventory{}, false},
		policyAgentCase{"OAI-211 silent on a guarded agent with no shell in the chain", "OAI-211", func() models.AgentDef {
			g := *b4Agent("g2", true, false, b4Agent("r", false, false))
			return g
		}(), models.RepoInventory{}, false},
	)
}
