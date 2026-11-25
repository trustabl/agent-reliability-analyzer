package rules_test

import (
	"strings"
	"testing"

	"github.com/trustabl/trustabl/internal/models"
	"github.com/trustabl/trustabl/internal/rules"
)

func TestPredSlashCommand_Shell(t *testing.T) {
	cases := []struct {
		name   string
		grants []models.ToolGrant
		want   bool
	}{
		{"bare", []models.ToolGrant{{Tool: "Bash"}}, true},
		{"star", []models.ToolGrant{{Tool: "Bash", Pattern: "*"}}, true},
		{"colon star", []models.ToolGrant{{Tool: "Bash", Pattern: ":*"}}, true},
		{"prefix", []models.ToolGrant{{Tool: "Bash", Pattern: "git status *"}}, false},
		{"other tool", []models.ToolGrant{{Tool: "Read"}}, false},
		{"none", nil, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := rules.PredSlashCommandAllowsUnrestrictedShell(models.SlashCommandDef{ToolGrants: tc.grants}); got != tc.want {
				t.Errorf("got %v want %v", got, tc.want)
			}
		})
	}
}

func TestPredSlashCommand_ModelInvocable(t *testing.T) {
	if !rules.PredSlashCommandModelInvocable(models.SlashCommandDef{}) {
		t.Error("default command should be model-invocable")
	}
	if rules.PredSlashCommandModelInvocable(models.SlashCommandDef{DisableModelInvocation: true}) {
		t.Error("disable-model-invocation: true should not be model-invocable")
	}
}

func TestPredSlashCommand_UnrestrictedTool(t *testing.T) {
	write := []string{"Write", "Edit", "MultiEdit", "NotebookEdit"}
	web := []string{"WebFetch", "WebSearch"}
	cases := []struct {
		name   string
		grants []models.ToolGrant
		tokens []string
		names  []string
		want   bool
	}{
		{"write with path counts", []models.ToolGrant{{Tool: "Write", Pattern: "src/**"}}, nil, write, true},
		{"notebook edit counts", []models.ToolGrant{{Tool: "NotebookEdit", Pattern: "a"}}, nil, write, true},
		{"edit bare", []models.ToolGrant{{Tool: "Edit"}}, nil, write, true},
		{"edit catch-all", []models.ToolGrant{{Tool: "Edit", Pattern: "**"}}, nil, write, true},
		{"edit scoped", []models.ToolGrant{{Tool: "Edit", Pattern: "docs/**"}}, nil, write, false},
		{"multiedit bare", []models.ToolGrant{{Tool: "MultiEdit"}}, nil, write, true},
		{"webfetch bare", []models.ToolGrant{{Tool: "WebFetch"}}, nil, web, true},
		{"webfetch domain", []models.ToolGrant{{Tool: "WebFetch", Pattern: "domain:example.com"}}, nil, web, false},
		{"websearch", []models.ToolGrant{{Tool: "WebSearch"}}, nil, web, true},
		{"raw token fallback", nil, []string{"WebSearch"}, web, true},
		{"unlisted tool", []models.ToolGrant{{Tool: "Read"}}, nil, write, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := models.SlashCommandDef{ToolGrants: tc.grants, AllowedTools: tc.tokens}
			if got := rules.PredSlashCommandAllowsUnrestrictedTool(c, tc.names); got != tc.want {
				t.Errorf("got %v want %v", got, tc.want)
			}
		})
	}
}

// The slash-command and skill predicates share one grant logic: for the same
// grants they must agree.
func TestPredSlashCommand_MatchesSkillPredicates(t *testing.T) {
	names := []string{"Bash", "Write", "Edit", "NotebookEdit", "WebFetch", "WebSearch"}
	grants := [][]models.ToolGrant{
		{{Tool: "Bash"}}, {{Tool: "Bash", Pattern: "*"}}, {{Tool: "Bash", Pattern: "ls *"}},
		{{Tool: "Write", Pattern: "x"}}, {{Tool: "Edit", Pattern: "docs/**"}}, {{Tool: "Edit"}},
		{{Tool: "WebFetch", Pattern: "domain:a.b"}}, {{Tool: "WebFetch"}}, {{Tool: "WebSearch"}}, nil,
	}
	for _, g := range grants {
		s := models.SkillDef{ToolGrants: g}
		c := models.SlashCommandDef{ToolGrants: g}
		if rules.PredSkillAllowsUnrestrictedShell(s) != rules.PredSlashCommandAllowsUnrestrictedShell(c) {
			t.Errorf("shell predicates disagree on %v", g)
		}
		if rules.PredSkillAllowsUnrestrictedTool(s, names) != rules.PredSlashCommandAllowsUnrestrictedTool(c, names) {
			t.Errorf("tool predicates disagree on %v", g)
		}
	}
}

const slashCommandPack = `
policy:
  id: test
  name: Test
  category: claude_skill
  description: Test policy.
rules:
  - id: TEST-001
    title: A command rule
    scope: slash_command
    severity: low
    confidence: 0.8
    applies_to: [%s]
    match:
      slash_command_model_invocable: true
    explanation: x
    fix: x
`

func TestLoad_SlashCommandScope(t *testing.T) {
	ok := makeFS(map[string]string{"c/r.yaml": strings.Replace(slashCommandPack, "%s", "claude_slash_command", 1)})
	pols, err := rules.Load(ok)
	if err != nil || len(pols) != 1 || pols[0].Rules[0].Scope != models.ScopeSlashCommand {
		t.Fatalf("slash_command rule should load: %v", err)
	}
	bad := makeFS(map[string]string{"c/r.yaml": strings.Replace(slashCommandPack, "%s", "claude_skill", 1)})
	if _, err := rules.Load(bad); err == nil {
		t.Fatal("applies_to claude_skill must be rejected on a slash_command rule")
	}
	skillPredicate := strings.Replace(strings.Replace(slashCommandPack, "%s", "claude_slash_command", 1),
		"slash_command_model_invocable: true", "skill_model_invocable: true", 1)
	if _, err := rules.Load(makeFS(map[string]string{"c/r.yaml": skillPredicate})); err == nil {
		t.Fatal("a skill predicate must be rejected on a slash_command rule")
	}
}
