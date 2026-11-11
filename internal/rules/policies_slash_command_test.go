package rules_test

import (
	"testing"

	"github.com/trustabl/trustabl/internal/analysis/detectors"
	"github.com/trustabl/trustabl/internal/models"
	"github.com/trustabl/trustabl/internal/rules"
)

type policySlashCommandCase struct {
	name      string
	ruleID    string
	cmd       models.SlashCommandDef
	wantFires bool
}

func slashCmd(name string, dmi bool, grants ...models.ToolGrant) models.SlashCommandDef {
	return models.SlashCommandDef{
		Name:                   name,
		Location:               models.Location{FilePath: ".claude/commands/" + name + ".md", Line: 1, EndLine: 4},
		ToolGrants:             grants,
		DisableModelInvocation: dmi,
	}
}

func grant(tool, pattern string) models.ToolGrant {
	return models.ToolGrant{Tool: tool, Pattern: pattern}
}

// policySlashCommandRuleCases covers slash_command-scoped rules (CCMD-*).
var policySlashCommandRuleCases = []policySlashCommandCase{
	{"CCMD-001 fires on bare Bash", "CCMD-001", slashCmd("ship", false, grant("Bash", "")), true},
	{"CCMD-001 fires on Bash(*)", "CCMD-001", slashCmd("ship2", true, grant("Bash", "*")), true},
	{"CCMD-001 fires on Bash(:*)", "CCMD-001", slashCmd("ship3", false, grant("Bash", ":*")), true},
	{"CCMD-001 silent on scoped Bash", "CCMD-001", slashCmd("safe", false, grant("Bash", "git status *")), false},
	{"CCMD-001 silent with no grants", "CCMD-001", slashCmd("plain", false), false},
	{"CCMD-001 silent on read-only tools", "CCMD-001", slashCmd("reader", false, grant("Read", ""), grant("Grep", "")), false},

	{"CCMD-002 fires on model-invocable Write", "CCMD-002", slashCmd("w", false, grant("Write", "")), true},
	{"CCMD-002 fires on model-invocable Edit", "CCMD-002", slashCmd("e", false, grant("Edit", "")), true},
	{"CCMD-002 fires on Edit(**)", "CCMD-002", slashCmd("e2", false, grant("Edit", "**")), true},
	{"CCMD-002 fires on Write with a path (path is not consulted)", "CCMD-002", slashCmd("w2", false, grant("Write", "src/**")), true},
	{"CCMD-002 silent when disable-model-invocation is set", "CCMD-002", slashCmd("manual", true, grant("Write", "")), false},
	{"CCMD-002 silent on path-scoped Edit", "CCMD-002", slashCmd("docs", false, grant("Edit", "docs/**")), false},
	{"CCMD-002 silent on read-only tools", "CCMD-002", slashCmd("r", false, grant("Read", "")), false},

	{"CCMD-003 fires on WebSearch", "CCMD-003", slashCmd("s", false, grant("WebSearch", "")), true},
	{"CCMD-003 fires on bare WebFetch", "CCMD-003", slashCmd("f", true, grant("WebFetch", "")), true},
	{"CCMD-003 silent on domain-scoped WebFetch", "CCMD-003", slashCmd("f2", false, grant("WebFetch", "domain:example.com")), false},
	{"CCMD-003 silent on read-only tools", "CCMD-003", slashCmd("r2", false, grant("Read", "")), false},
}

func loadSlashCommandRule(t *testing.T, ruleID string) detectors.SlashCommandDetector {
	t.Helper()
	policies, err := rules.Load(fixtureFS(t))
	if err != nil {
		t.Fatalf("load policies: %v", err)
	}
	for _, p := range policies {
		for _, r := range p.Rules {
			if r.ID == ruleID && r.Scope == models.ScopeSlashCommand {
				return rules.NewSlashCommandRuleDetector(r)
			}
		}
	}
	t.Fatalf("slash_command-scoped rule %s not found in shipped policies", ruleID)
	return nil
}

func TestPolicySlashCommandRules(t *testing.T) {
	for _, tc := range policySlashCommandRuleCases {
		t.Run(tc.name, func(t *testing.T) {
			d := loadSlashCommandRule(t, tc.ruleID)
			if !d.Applies(tc.cmd) {
				t.Fatalf("rule %s does not Apply to command %s", tc.ruleID, tc.cmd.Name)
			}
			var got []models.Finding
			for _, f := range d.Detect(tc.cmd, models.RepoInventory{}) {
				if f.RuleID == tc.ruleID {
					got = append(got, f)
				}
			}
			if (len(got) == 1) != tc.wantFires || len(got) > 1 {
				t.Fatalf("rule %s: %d findings, wantFires=%v", tc.ruleID, len(got), tc.wantFires)
			}
			if tc.wantFires {
				f := got[0]
				if f.Scope != models.ScopeSlashCommand || f.ToolName != tc.cmd.Name || f.FilePath != tc.cmd.FilePath {
					t.Errorf("finding attribution = scope %q name %q file %q", f.Scope, f.ToolName, f.FilePath)
				}
			}
		})
	}
}
