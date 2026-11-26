package rules_test

import (
	"github.com/trustabl/trustabl/internal/models"
)

// Fire and silent cases for the static batch 2 rules (MCP-204 to MCP-210, OAI-204, OAI-205,
// LC-203, CREW-203, ADK-203, PYD-203). The facts are built by analysis.DiscoverMCPCode and by
// agent discovery, which are covered in internal/analysis; these cases cover the predicates and
// the rule wiring. The cases are appended to the shared tables in init so
// TestPolicyRules_AllRulesCovered sees them.

func batch2Tool(name string, params ...string) models.ToolDef {
	return models.ToolDef{Name: name, ParamNames: params, Location: models.Location{FilePath: "app/agent.py"}}
}

func batch2Case(name, ruleID string, sdk models.SDK, want bool, tools ...models.ToolDef) policyAgentCase {
	a := models.AgentDef{SDK: sdk, Language: models.LanguagePython, Name: "a", VarName: "a",
		Location: models.Location{FilePath: "app/agent.py"}}
	switch sdk {
	case models.SDKGoogleADK:
		a.Class = "LlmAgent"
	case models.SDKLangChain:
		a.Class = "CreateAgent"
	case models.SDKPydanticAI:
		a.Class = "PydanticAgent"
	default:
		a.Class = "Agent"
	}
	for i := range tools {
		t := tools[i]
		a.ToolRefs = append(a.ToolRefs, models.ToolRef{Name: t.Name, Resolved: &t})
	}
	return policyAgentCase{name, ruleID, a, models.RepoInventory{}, want}
}

// batch2HandoffAgent builds an OpenAI agent whose handoffs entries are given; the inventory
// holds the same-file targets "worker" (a shell tool) and "reader" (no shell tool).
func batch2HandoffAgent(inputs []models.HandoffInput) models.AgentDef {
	return models.AgentDef{SDK: models.SDKOpenAIAgents, Class: "Agent", Language: models.LanguagePython,
		Name: "triage", VarName: "triage", Location: models.Location{FilePath: "app/agents.py"}, HandoffInputs: inputs}
}

func batch2HandoffInv(runFilter bool) models.RepoInventory {
	shell := models.ToolDef{Name: "run", Kind: models.KindShellInvocation}
	worker := models.AgentDef{SDK: models.SDKOpenAIAgents, Class: "Agent", Language: models.LanguagePython, Name: "worker", VarName: "worker",
		Location: models.Location{FilePath: "app/agents.py"}, ToolRefs: []models.ToolRef{{Name: "run", Resolved: &shell}}}
	reader := models.AgentDef{SDK: models.SDKOpenAIAgents, Class: "Agent", Language: models.LanguagePython, Name: "reader", VarName: "reader",
		Location: models.Location{FilePath: "app/agents.py"}}
	return models.RepoInventory{Agents: []models.AgentDef{worker, reader}, HandoffInputFilterConfigured: runFilter}
}

func init() {
	policyRepoRuleCases = append(policyRepoRuleCases,
		policyRepoCase{"MCP-204 fires when discovery recorded the pattern", "MCP-204",
			models.RepoProfile{Languages: []models.Language{models.LanguageTypeScript}},
			models.RepoInventory{SDKsDetected: []models.SDK{models.SDKMCP},
				MCPCode: []models.MCPCodeFact{{Kind: models.MCPCodePrefixContainment, FilePath: "fs.ts", Line: 3, Language: models.LanguageTypeScript}}},
			true},
		policyRepoCase{"MCP-204 silent when no fact was recorded", "MCP-204",
			models.RepoProfile{Languages: []models.Language{models.LanguageTypeScript}},
			models.RepoInventory{SDKsDetected: []models.SDK{models.SDKMCP}},
			false},
		policyRepoCase{"MCP-204 silent when only another kind was recorded", "MCP-204",
			models.RepoProfile{Languages: []models.Language{models.LanguageTypeScript}},
			models.RepoInventory{SDKsDetected: []models.SDK{models.SDKMCP},
				MCPCode: []models.MCPCodeFact{{Kind: "other_kind", FilePath: "fs.ts", Line: 3, Language: models.LanguageTypeScript}}},
			false},
		policyRepoCase{"MCP-205 fires when discovery recorded the pattern", "MCP-205",
			models.RepoProfile{Languages: []models.Language{models.LanguagePython}},
			models.RepoInventory{SDKsDetected: []models.SDK{models.SDKMCP},
				MCPCode: []models.MCPCodeFact{{Kind: models.MCPCodeStdioCommand, FilePath: "client.py", Line: 3, Language: models.LanguagePython}}},
			true},
		policyRepoCase{"MCP-205 silent when no fact was recorded", "MCP-205",
			models.RepoProfile{Languages: []models.Language{models.LanguagePython}},
			models.RepoInventory{SDKsDetected: []models.SDK{models.SDKMCP}},
			false},
		policyRepoCase{"MCP-205 silent when only another kind was recorded", "MCP-205",
			models.RepoProfile{Languages: []models.Language{models.LanguagePython}},
			models.RepoInventory{SDKsDetected: []models.SDK{models.SDKMCP},
				MCPCode: []models.MCPCodeFact{{Kind: "other_kind", FilePath: "client.py", Line: 3, Language: models.LanguagePython}}},
			false},
		policyRepoCase{"MCP-206 fires when discovery recorded the pattern", "MCP-206",
			models.RepoProfile{Languages: []models.Language{models.LanguagePython}},
			models.RepoInventory{SDKsDetected: []models.SDK{models.SDKMCP},
				MCPCode: []models.MCPCodeFact{{Kind: models.MCPCodeBearerNoAudience, FilePath: "auth.py", Line: 3, Language: models.LanguagePython}}},
			true},
		policyRepoCase{"MCP-206 silent when no fact was recorded", "MCP-206",
			models.RepoProfile{Languages: []models.Language{models.LanguagePython}},
			models.RepoInventory{SDKsDetected: []models.SDK{models.SDKMCP}},
			false},
		policyRepoCase{"MCP-206 silent when only another kind was recorded", "MCP-206",
			models.RepoProfile{Languages: []models.Language{models.LanguagePython}},
			models.RepoInventory{SDKsDetected: []models.SDK{models.SDKMCP},
				MCPCode: []models.MCPCodeFact{{Kind: "other_kind", FilePath: "auth.py", Line: 3, Language: models.LanguagePython}}},
			false},
		policyRepoCase{"MCP-207 fires when discovery recorded the pattern", "MCP-207",
			models.RepoProfile{Languages: []models.Language{models.LanguagePython}},
			models.RepoInventory{SDKsDetected: []models.SDK{models.SDKMCP},
				MCPCode: []models.MCPCodeFact{{Kind: models.MCPCodeClientOpensURL, FilePath: "client.py", Line: 3, Language: models.LanguagePython}}},
			true},
		policyRepoCase{"MCP-207 silent when no fact was recorded", "MCP-207",
			models.RepoProfile{Languages: []models.Language{models.LanguagePython}},
			models.RepoInventory{SDKsDetected: []models.SDK{models.SDKMCP}},
			false},
		policyRepoCase{"MCP-207 silent when only another kind was recorded", "MCP-207",
			models.RepoProfile{Languages: []models.Language{models.LanguagePython}},
			models.RepoInventory{SDKsDetected: []models.SDK{models.SDKMCP},
				MCPCode: []models.MCPCodeFact{{Kind: "other_kind", FilePath: "client.py", Line: 3, Language: models.LanguagePython}}},
			false},
		policyRepoCase{"MCP-208 fires when discovery recorded the pattern", "MCP-208",
			models.RepoProfile{Languages: []models.Language{models.LanguageTypeScript}},
			models.RepoInventory{SDKsDetected: []models.SDK{models.SDKMCP},
				MCPCode: []models.MCPCodeFact{{Kind: models.MCPCodeSharedTransport, FilePath: "shared.ts", Line: 3, Language: models.LanguageTypeScript}}},
			true},
		policyRepoCase{"MCP-208 silent when no fact was recorded", "MCP-208",
			models.RepoProfile{Languages: []models.Language{models.LanguageTypeScript}},
			models.RepoInventory{SDKsDetected: []models.SDK{models.SDKMCP}},
			false},
		policyRepoCase{"MCP-208 silent when only another kind was recorded", "MCP-208",
			models.RepoProfile{Languages: []models.Language{models.LanguageTypeScript}},
			models.RepoInventory{SDKsDetected: []models.SDK{models.SDKMCP},
				MCPCode: []models.MCPCodeFact{{Kind: "other_kind", FilePath: "shared.ts", Line: 3, Language: models.LanguageTypeScript}}},
			false},
		policyRepoCase{"MCP-209 fires when discovery recorded the pattern", "MCP-209",
			models.RepoProfile{Languages: []models.Language{models.LanguagePython}},
			models.RepoInventory{SDKsDetected: []models.SDK{models.SDKMCP},
				MCPCode: []models.MCPCodeFact{{Kind: models.MCPCodeDNSRebindingOff, FilePath: "rebind.py", Line: 3, Language: models.LanguagePython}}},
			true},
		policyRepoCase{"MCP-209 silent when no fact was recorded", "MCP-209",
			models.RepoProfile{Languages: []models.Language{models.LanguagePython}},
			models.RepoInventory{SDKsDetected: []models.SDK{models.SDKMCP}},
			false},
		policyRepoCase{"MCP-209 silent when only another kind was recorded", "MCP-209",
			models.RepoProfile{Languages: []models.Language{models.LanguagePython}},
			models.RepoInventory{SDKsDetected: []models.SDK{models.SDKMCP},
				MCPCode: []models.MCPCodeFact{{Kind: "other_kind", FilePath: "rebind.py", Line: 3, Language: models.LanguagePython}}},
			false},
		policyRepoCase{"MCP-210 fires when discovery recorded the pattern", "MCP-210",
			models.RepoProfile{Languages: []models.Language{models.LanguageTypeScript}},
			models.RepoInventory{SDKsDetected: []models.SDK{models.SDKMCP},
				MCPCode: []models.MCPCodeFact{{Kind: models.MCPCodePkgInstallSpec, FilePath: "install.ts", Line: 3, Language: models.LanguageTypeScript}}},
			true},
		policyRepoCase{"MCP-210 silent when no fact was recorded", "MCP-210",
			models.RepoProfile{Languages: []models.Language{models.LanguageTypeScript}},
			models.RepoInventory{SDKsDetected: []models.SDK{models.SDKMCP}},
			false},
		policyRepoCase{"MCP-210 silent when only another kind was recorded", "MCP-210",
			models.RepoProfile{Languages: []models.Language{models.LanguageTypeScript}},
			models.RepoInventory{SDKsDetected: []models.SDK{models.SDKMCP},
				MCPCode: []models.MCPCodeFact{{Kind: "other_kind", FilePath: "install.ts", Line: 3, Language: models.LanguageTypeScript}}},
			false},
	)

	policyAgentRuleCases = append(policyAgentRuleCases,
		// OAI-204
		policyAgentCase{"OAI-204 fires on a bare handoff to a shell-tool agent", "OAI-204",
			batch2HandoffAgent([]models.HandoffInput{{Target: "worker"}}), batch2HandoffInv(false), true},
		policyAgentCase{"OAI-204 fires on a handoff() entry with no filter", "OAI-204",
			batch2HandoffAgent([]models.HandoffInput{{Target: "worker", HasInputFilter: false}}), batch2HandoffInv(false), true},
		policyAgentCase{"OAI-204 silent when the entry sets an input filter", "OAI-204",
			batch2HandoffAgent([]models.HandoffInput{{Target: "worker", HasInputFilter: true}}), batch2HandoffInv(false), false},
		policyAgentCase{"OAI-204 silent when a RunConfig sets handoff_input_filter", "OAI-204",
			batch2HandoffAgent([]models.HandoffInput{{Target: "worker"}}), batch2HandoffInv(true), false},
		policyAgentCase{"OAI-204 silent when the target holds no shell tool", "OAI-204",
			batch2HandoffAgent([]models.HandoffInput{{Target: "reader"}}), batch2HandoffInv(false), false},
		policyAgentCase{"OAI-204 silent when the target is not defined in the file", "OAI-204",
			batch2HandoffAgent([]models.HandoffInput{{Target: "elsewhere"}}), batch2HandoffInv(false), false},
		policyAgentCase{"OAI-204 silent with no handoffs", "OAI-204",
			batch2HandoffAgent(nil), batch2HandoffInv(false), false},

		batch2Case("OAI-205 fires on authorized_by plus request_approval", "OAI-205", models.SDKOpenAIAgents, true,
			batch2Tool("export_document", "doc_id", "authorized_by"), batch2Tool("request_approval", "reason")),
		batch2Case("OAI-205 fires on a camel-case approval tool name", "OAI-205", models.SDKOpenAIAgents, true,
			batch2Tool("exportDocument", "docId", "Authorized_By"), batch2Tool("requestApprovalToken", "reason")),
		batch2Case("OAI-205 silent without the approval-requesting tool", "OAI-205", models.SDKOpenAIAgents, false,
			batch2Tool("export_document", "doc_id", "authorized_by"), batch2Tool("list_documents")),
		batch2Case("OAI-205 silent without an authorized_by parameter", "OAI-205", models.SDKOpenAIAgents, false,
			batch2Tool("export_document", "doc_id"), batch2Tool("request_approval", "reason")),
		batch2Case("OAI-205 silent when one tool both takes the parameter and has the name", "OAI-205", models.SDKOpenAIAgents, false,
			batch2Tool("request_approval", "authorized_by")),

		batch2Case("LC-203 fires on authorized_by plus request_approval", "LC-203", models.SDKLangChain, true,
			batch2Tool("export_document", "doc_id", "authorized_by"), batch2Tool("request_approval", "reason")),
		batch2Case("LC-203 fires on a camel-case approval tool name", "LC-203", models.SDKLangChain, true,
			batch2Tool("exportDocument", "docId", "Authorized_By"), batch2Tool("requestApprovalToken", "reason")),
		batch2Case("LC-203 silent without the approval-requesting tool", "LC-203", models.SDKLangChain, false,
			batch2Tool("export_document", "doc_id", "authorized_by"), batch2Tool("list_documents")),
		batch2Case("LC-203 silent without an authorized_by parameter", "LC-203", models.SDKLangChain, false,
			batch2Tool("export_document", "doc_id"), batch2Tool("request_approval", "reason")),
		batch2Case("LC-203 silent when one tool both takes the parameter and has the name", "LC-203", models.SDKLangChain, false,
			batch2Tool("request_approval", "authorized_by")),

		batch2Case("CREW-203 fires on authorized_by plus request_approval", "CREW-203", models.SDKCrewAI, true,
			batch2Tool("export_document", "doc_id", "authorized_by"), batch2Tool("request_approval", "reason")),
		batch2Case("CREW-203 fires on a camel-case approval tool name", "CREW-203", models.SDKCrewAI, true,
			batch2Tool("exportDocument", "docId", "Authorized_By"), batch2Tool("requestApprovalToken", "reason")),
		batch2Case("CREW-203 silent without the approval-requesting tool", "CREW-203", models.SDKCrewAI, false,
			batch2Tool("export_document", "doc_id", "authorized_by"), batch2Tool("list_documents")),
		batch2Case("CREW-203 silent without an authorized_by parameter", "CREW-203", models.SDKCrewAI, false,
			batch2Tool("export_document", "doc_id"), batch2Tool("request_approval", "reason")),
		batch2Case("CREW-203 silent when one tool both takes the parameter and has the name", "CREW-203", models.SDKCrewAI, false,
			batch2Tool("request_approval", "authorized_by")),

		batch2Case("ADK-203 fires on authorized_by plus request_approval", "ADK-203", models.SDKGoogleADK, true,
			batch2Tool("export_document", "doc_id", "authorized_by"), batch2Tool("request_approval", "reason")),
		batch2Case("ADK-203 fires on a camel-case approval tool name", "ADK-203", models.SDKGoogleADK, true,
			batch2Tool("exportDocument", "docId", "Authorized_By"), batch2Tool("requestApprovalToken", "reason")),
		batch2Case("ADK-203 silent without the approval-requesting tool", "ADK-203", models.SDKGoogleADK, false,
			batch2Tool("export_document", "doc_id", "authorized_by"), batch2Tool("list_documents")),
		batch2Case("ADK-203 silent without an authorized_by parameter", "ADK-203", models.SDKGoogleADK, false,
			batch2Tool("export_document", "doc_id"), batch2Tool("request_approval", "reason")),
		batch2Case("ADK-203 silent when one tool both takes the parameter and has the name", "ADK-203", models.SDKGoogleADK, false,
			batch2Tool("request_approval", "authorized_by")),

		batch2Case("PYD-203 fires on authorized_by plus request_approval", "PYD-203", models.SDKPydanticAI, true,
			batch2Tool("export_document", "doc_id", "authorized_by"), batch2Tool("request_approval", "reason")),
		batch2Case("PYD-203 fires on a camel-case approval tool name", "PYD-203", models.SDKPydanticAI, true,
			batch2Tool("exportDocument", "docId", "Authorized_By"), batch2Tool("requestApprovalToken", "reason")),
		batch2Case("PYD-203 silent without the approval-requesting tool", "PYD-203", models.SDKPydanticAI, false,
			batch2Tool("export_document", "doc_id", "authorized_by"), batch2Tool("list_documents")),
		batch2Case("PYD-203 silent without an authorized_by parameter", "PYD-203", models.SDKPydanticAI, false,
			batch2Tool("export_document", "doc_id"), batch2Tool("request_approval", "reason")),
		batch2Case("PYD-203 silent when one tool both takes the parameter and has the name", "PYD-203", models.SDKPydanticAI, false,
			batch2Tool("request_approval", "authorized_by")),
	)
}
