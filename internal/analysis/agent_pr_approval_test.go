package analysis

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/trustabl/trustabl/internal/models"
)

func TestGhPRSelfApproval(t *testing.T) {
	fire := []string{
		"gh pr review 12 --approve",
		"gh pr review --approve --body ok",
		"gh pr review -a",
		"gh pr merge 5 --admin",
		"gh pr merge --squash --admin 5",
		"gh -R acme/app pr merge 5 --admin",
		"GH_TOKEN=$T gh pr review 3 --approve",
		"sudo gh pr merge --admin",
	}
	silent := []string{
		"gh pr review 12 --comment",
		"gh pr review 12 --request-changes",
		"gh pr merge 5 --squash",
		"gh pr merge 5 --auto",
		"gh pr view 5 --admin",
		"echo gh pr merge --admin",
		"gh issue review --approve",
		"git commit -m approve",
	}
	for _, s := range fire {
		if _, ok := ghPRSelfApproval(s); !ok {
			t.Errorf("want hit: %q", s)
		}
	}
	for _, s := range silent {
		if _, ok := ghPRSelfApproval(s); ok {
			t.Errorf("want no hit: %q", s)
		}
	}
}

func TestGhPRSelfApprovalInScriptSkipsComments(t *testing.T) {
	if _, _, ok := ghPRSelfApprovalInScript("# gh pr merge 1 --admin\necho hi\n"); ok {
		t.Error("comment line must not match")
	}
	if cmd, line, ok := ghPRSelfApprovalInScript("echo hi\ncd x && gh pr merge 1 --admin\n"); !ok || line != 2 || cmd != "gh pr merge --admin" {
		t.Errorf("got %q %d %v", cmd, line, ok)
	}
}

func TestGhPRSelfApprovalInMarkdownReadsFencesOnly(t *testing.T) {
	prose := "Do not run gh pr merge --admin.\n\nUse `gh pr review --approve` never.\n\n```bash\ngh pr view\n```\n"
	if _, _, ok := ghPRSelfApprovalInMarkdown(prose); ok {
		t.Error("prose and inline code must not match")
	}
	fenced := "text\n\n```sh\n$ gh pr review --approve\n```\n"
	if _, line, ok := ghPRSelfApprovalInMarkdown(fenced); !ok || line != 4 {
		t.Errorf("fenced: line %d ok %v", line, ok)
	}
	commented := "```bash\n# gh pr merge --admin\n```\n"
	if _, _, ok := ghPRSelfApprovalInMarkdown(commented); ok {
		t.Error("commented fenced line must not match")
	}
}

func TestDiscoverAgentPRSelfApprovalSurfaces(t *testing.T) {
	dir := t.TempDir()
	write := func(rel, body string) {
		p := filepath.Join(dir, filepath.FromSlash(rel))
		_ = os.MkdirAll(filepath.Dir(p), 0o755)
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write(".claude/skills/a/SKILL.md", "---\nname: a\n---\n```bash\ngh pr review --approve\n```\n")
	write(".claude/skills/a/run.sh", "gh pr merge --admin\n")
	write(".claude/commands/c.md", "```\ngh pr merge --admin\n```\n")
	write(".claude/settings.json", `{"hooks":{"PostToolUse":[{"matcher":"*","hooks":[{"type":"command","command":"gh pr review --approve"}]}]}}`)
	write(".claude/hooks/h.sh", "gh pr merge --admin\n")
	write("scripts/release.sh", "gh pr merge --admin\n")
	write(".github/workflows/w.yml", "run: gh pr merge --admin\n")
	m := models.ScanManifest{RepoRoot: dir, ServerScriptFiles: []string{".claude/hooks/h.sh", "scripts/release.sh"}}
	skills := []models.SkillDef{{Name: "a", Location: models.Location{FilePath: ".claude/skills/a/SKILL.md"},
		BundledFiles: []models.BundledFile{{Path: ".claude/skills/a/run.sh", Kind: "script"}}}}
	cmds := []models.SlashCommandDef{{Name: "c", Location: models.Location{FilePath: ".claude/commands/c.md"}}}
	settings := []models.ClaudeSettings{{Location: models.Location{FilePath: ".claude/settings.json"}}}
	hits := DiscoverAgentPRSelfApproval(m, skills, cmds, settings)
	var got []string
	for _, h := range hits {
		got = append(got, h.FilePath)
	}
	want := []string{".claude/commands/c.md", ".claude/hooks/h.sh", ".claude/settings.json", ".claude/skills/a/SKILL.md", ".claude/skills/a/run.sh"}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got %v, want %v", got, want)
		}
	}
}
