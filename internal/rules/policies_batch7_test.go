package rules_test

import (
	"testing"
	"testing/fstest"

	"github.com/trustabl/trustabl/internal/models"
	"github.com/trustabl/trustabl/internal/rules"
)

// Fire and silent cases for static batch 7: MCP-218, MCP-221, MCP-222, MCP-223, RAW-003,
// CSDK-209, CSKILL-094. Appended in init so TestPolicyRules_AllRulesCovered sees them.

func repoFlow(src models.SourceKind, class string) models.RepoInventory {
	return models.RepoInventory{RepoFlows: []models.RepoFlow{{FilePath: "agent.py", Func: "f", Source: src, Param: "p", SourceLine: 3, SinkClass: class, SinkCallee: "x", SinkLine: 5}}}
}

func withSDK(inv models.RepoInventory) models.RepoInventory {
	inv.SDKsDetected = []models.SDK{models.SDKClaudeAgentSDK}
	return inv
}

func init() {
	ts := models.LanguageTypeScript
	py := models.RepoProfile{Languages: []models.Language{models.LanguagePython}}
	policyRuleCases = append(policyRuleCases,
		policyRuleCase{name: "MCP-218 fires on a table name in an f-string statement", ruleID: "MCP-218", kind: models.KindMCPTool, src: `
def describe(table_name: str) -> str:
    """Describe."""
    cur = db.execute(f"PRAGMA table_info('{table_name}')")
    return str(cur.fetchall())
`, wantFires: true},
		policyRuleCase{name: "MCP-218 silent on a bound value", ruleID: "MCP-218", kind: models.KindMCPTool, src: `
def lookup(user_id: str) -> str:
    """Lookup."""
    cur = db.execute("SELECT name FROM users WHERE id = ?", (user_id,))
    return str(cur.fetchall())
`, wantFires: false},
		policyRuleCase{name: "MCP-218 silent behind an allowlist guard", ruleID: "MCP-218", kind: models.KindMCPTool, src: `
def describe(table_name: str) -> str:
    """Describe."""
    if table_name not in TABLES:
        raise ValueError(table_name)
    return str(db.execute(f"PRAGMA table_info('{table_name}')").fetchall())
`, wantFires: false},
		policyRuleCase{name: "MCP-218 silent when execute appears in a comment", ruleID: "MCP-218", kind: models.KindMCPTool, src: `
def describe(table_name: str) -> str:
    """Describe."""
    # db.execute(f"PRAGMA {table_name}")
    return "x"
`, wantFires: false},

		policyRuleCase{name: "MCP-221 fires on a returned secret env value", ruleID: "MCP-221", kind: models.KindMCPTool, src: `
def whoami() -> str:
    """Account."""
    key = os.environ.get("SERVICE_API_KEY", "")
    return f"Using key {key}"
`, wantFires: true},
		policyRuleCase{name: "MCP-221 silent on a masked value", ruleID: "MCP-221", kind: models.KindMCPTool, src: `
def whoami() -> str:
    """Account."""
    key = os.environ["SERVICE_API_KEY"]
    return "ends " + key[-4:]
`, wantFires: false},
		policyRuleCase{name: "MCP-221 silent on a set-or-missing answer", ruleID: "MCP-221", kind: models.KindMCPTool, src: `
def whoami() -> str:
    """Account."""
    key = os.environ.get("SERVICE_API_KEY", "")
    return "configured" if key else "missing"
`, wantFires: false},
		policyRuleCase{name: "MCP-221 silent on a non-secret name", ruleID: "MCP-221", kind: models.KindMCPTool, src: `
def home() -> str:
    """Home."""
    return os.environ.get("HOME", "")
`, wantFires: false},
		policyRuleCase{name: "MCP-221 silent when the name is only in a comment", ruleID: "MCP-221", kind: models.KindMCPTool, src: `
def whoami() -> str:
    """Account."""
    # return os.environ["API_KEY"]
    return "x"
`, wantFires: false},

		policyRuleCase{name: "MCP-222 fires on a returned process.env token", ruleID: "MCP-222", kind: models.KindMCPTool, lang: ts, src: tsMCPHead + `
server.tool('cfg', 'Config.', { n: z.string() }, async ({ n }) => {
  const token = process.env.SERVICE_AUTH_TOKEN;
  return { content: [{ type: 'text', text: ` + "`token=${token}`" + ` }] };
});
`, wantFires: true},
		policyRuleCase{name: "MCP-222 silent on a sliced value", ruleID: "MCP-222", kind: models.KindMCPTool, lang: ts, src: tsMCPHead + `
server.tool('cfg', 'Config.', { n: z.string() }, async ({ n }) => {
  const token = process.env.SERVICE_AUTH_TOKEN;
  return token.slice(-4);
});
`, wantFires: false},
		policyRuleCase{name: "MCP-222 silent on a non-secret name", ruleID: "MCP-222", kind: models.KindMCPTool, lang: ts, src: tsMCPHead + `
server.tool('cfg', 'Config.', { n: z.string() }, async ({ n }) => process.env.NODE_ENV);
`, wantFires: false},

		policyRuleCase{name: "MCP-223 fires on a password written to a memory client", ruleID: "MCP-223", kind: models.KindMCPTool, src: `
memory = Memory()

def remember(user: str, password: str) -> str:
    """Remember a login."""
    memory.add(f"login for {user}: {password}", user_id="shared")
    return "saved"
`, wantFires: true},
		policyRuleCase{name: "MCP-223 fires on a token written to a store", ruleID: "MCP-223", kind: models.KindMCPTool, src: `
def remember(token: str, store) -> str:
    """Remember."""
    store.put(("users",), "k", {"t": token})
    return "saved"
`, wantFires: true},
		policyRuleCase{name: "MCP-223 silent on a note parameter", ruleID: "MCP-223", kind: models.KindMCPTool, src: `
memory = Memory()

def remember(user: str, note: str) -> str:
    """Remember a note."""
    memory.add(f"note from {user}: {note}", user_id=user)
    return "saved"
`, wantFires: false},
		policyRuleCase{name: "MCP-223 silent when the password is not written", ruleID: "MCP-223", kind: models.KindMCPTool, src: `
memory = Memory()

def login(user: str, password: str) -> str:
    """Login."""
    memory.add(f"{user} logged in", user_id=user)
    return authenticate(user, password)
`, wantFires: false},
	)
	policyRepoRuleCases = append(policyRepoRuleCases,
		policyRepoCase{name: "RAW-003 fires on model output reaching a shell call", ruleID: "RAW-003", profile: py, inv: repoFlow(models.SourceLLMOutput, models.SinkShell), wantFires: true},
		policyRepoCase{name: "RAW-003 fires on model output reaching eval", ruleID: "RAW-003", profile: py, inv: repoFlow(models.SourceLLMOutput, models.SinkCodeExec), wantFires: true},
		policyRepoCase{name: "RAW-003 silent on a parameter reaching resume", ruleID: "RAW-003", profile: py, inv: repoFlow(models.SourceHandlerParam, models.SinkClaudeResume), wantFires: false},
		policyRepoCase{name: "RAW-003 silent with no flow", ruleID: "RAW-003", profile: py, inv: models.RepoInventory{}, wantFires: false},
		policyRepoCase{name: "CSDK-209 fires on a parameter reaching resume", ruleID: "CSDK-209", profile: py, inv: withSDK(repoFlow(models.SourceHandlerParam, models.SinkClaudeResume)), wantFires: true},
		policyRepoCase{name: "CSDK-209 silent on model output reaching a shell", ruleID: "CSDK-209", profile: py, inv: repoFlow(models.SourceLLMOutput, models.SinkShell), wantFires: false},
		policyRepoCase{name: "CSDK-209 silent with no flow", ruleID: "CSDK-209", profile: py, inv: models.RepoInventory{}, wantFires: false},
	)
	policySkillRuleCases = append(policySkillRuleCases,
		policySkillCase{name: "CSKILL-094 fires on a script that prints a secret", ruleID: "CSKILL-094",
			skill: models.SkillDef{Name: "diag", Location: models.Location{FilePath: ".claude/skills/diag/SKILL.md"},
				BundledFiles: []models.BundledFile{{Path: ".claude/skills/diag/scripts/r.py", Kind: "script", PrintsSecret: true, PrintsSecretLine: 5}}}, wantFires: true},
		policySkillCase{name: "CSKILL-094 silent on a clean script", ruleID: "CSKILL-094",
			skill: models.SkillDef{Name: "diag", Location: models.Location{FilePath: ".claude/skills/diag/SKILL.md"},
				BundledFiles: []models.BundledFile{{Path: ".claude/skills/diag/scripts/r.py", Kind: "script"}}}, wantFires: false},
	)
}

func TestBatch7_LoaderChecksValues(t *testing.T) {
	load := func(match string) error {
		_, err := rules.Load(fstest.MapFS{"mcp/x.yaml": &fstest.MapFile{Data: []byte("policy:\n  id: p\n  name: P\n  category: mcp\n  description: d\nrules:\n  - id: X-001\n    title: t\n    scope: tool\n    severity: low\n    confidence: 0.5\n    applies_to: [mcp_tool]\n    match:\n" + match + "    explanation: e\n    fix: f\n")}})
		return err
	}
	if err := load("      tool_flow_reaches_sink:\n        sources: [secret_env]\n        sinks: [return_value]\n"); err != nil {
		t.Errorf("valid secret rule rejected: %v", err)
	}
	if err := load("      tool_flow_reaches_sink:\n        sources: [secret_env]\n        sinks: [shell]\n"); err == nil {
		t.Errorf("unknown secret sink accepted")
	}
	if err := load("      tool_source_reaches_sink:\n        sources: [secret_env]\n        sinks: [shell]\n"); err == nil {
		t.Errorf("secret_env accepted as a tool_source_reaches_sink source")
	}
	if err := load("      tool_source_reaches_sink:\n        sources: [param]\n        sinks: [memory_write]\n"); err == nil {
		t.Errorf("memory_write accepted by tool_source_reaches_sink")
	}
	if err := load("      tool_flow_reaches_sink:\n        sources: [param]\n        sinks: [memory_write]\n"); err != nil {
		t.Errorf("memory_write rejected by tool_flow_reaches_sink: %v", err)
	}
}
