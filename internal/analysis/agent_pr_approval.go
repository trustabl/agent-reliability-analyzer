package analysis

import (
	"encoding/json"
	"path"
	"strings"

	"github.com/trustabl/trustabl/internal/models"
)

// CI-006: agent-invocable code runs `gh pr review` with --approve (or -a), or `gh pr merge`
// with --admin. Surfaces read: a fenced code block in a SKILL.md or slash command file, a
// script bundled in a skill, a command in a Claude Code settings hook, and a shell file under
// .claude/. Workflow run steps and ordinary repo scripts are not read: neither is code an
// agent invokes through its own configuration. Which identity the command runs as is not judged.
//
// Secrecy contract: a hit's Reason names the surface and the flag only.

// ghPRSelfApproval returns the flag when one shell segment runs gh pr review --approve or gh pr
// merge --admin. The command word must be the first word of the segment after env assignments,
// env, exec, time or sudo, so `echo gh pr merge --admin` is not a match. Global gh flags before
// the subcommand (-R owner/repo, --repo owner/repo) are skipped.
func ghPRSelfApproval(seg string) (string, bool) {
	f := shellFields(seg)
	i := 0
	for i < len(f) && ((strings.Contains(f[i], "=") && !strings.HasPrefix(f[i], "-")) ||
		f[i] == "env" || f[i] == "exec" || f[i] == "time" || f[i] == "sudo") {
		i++
	}
	if i >= len(f) || path.Base(f[i]) != "gh" {
		return "", false
	}
	i++
	for i < len(f) && strings.HasPrefix(f[i], "-") {
		if (f[i] == "-R" || f[i] == "--repo") && i+1 < len(f) {
			i++
		}
		i++
	}
	if i+1 >= len(f) || f[i] != "pr" {
		return "", false
	}
	sub, rest := f[i+1], f[i+2:]
	has := func(flags ...string) bool {
		for _, r := range rest {
			for _, fl := range flags {
				if r == fl {
					return true
				}
			}
		}
		return false
	}
	switch sub {
	case "review":
		if has("--approve", "-a") {
			return "gh pr review --approve", true
		}
	case "merge":
		if has("--admin") {
			return "gh pr merge --admin", true
		}
	}
	return "", false
}

func ghPRSelfApprovalInScript(text string) (string, int, bool) {
	text = strings.ReplaceAll(text, "\\\n", " ")
	for n, line := range strings.Split(text, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "#") {
			continue
		}
		for _, seg := range ciSegmentSplitRe.Split(line, -1) {
			if cmd, ok := ghPRSelfApproval(seg); ok {
				return cmd, n + 1, true
			}
		}
	}
	return "", 0, false
}

// fencedBlockLines returns the lines inside ``` fenced blocks of a markdown body, with their
// 1-based line numbers, and drops a leading "$ " prompt.
func fencedBlockLines(md string) (lines []string, nums []int) {
	in := false
	for n, line := range strings.Split(md, "\n") {
		t := strings.TrimSpace(line)
		if strings.HasPrefix(t, "```") || strings.HasPrefix(t, "~~~") {
			in = !in
			continue
		}
		if in {
			lines = append(lines, strings.TrimPrefix(t, "$ "))
			nums = append(nums, n+1)
		}
	}
	return
}

func ghPRSelfApprovalInMarkdown(md string) (string, int, bool) {
	lines, nums := fencedBlockLines(md)
	for i, l := range lines {
		if strings.HasPrefix(l, "#") {
			continue
		}
		for _, seg := range ciSegmentSplitRe.Split(l, -1) {
			if cmd, ok := ghPRSelfApproval(seg); ok {
				return cmd, nums[i], true
			}
		}
	}
	return "", 0, false
}

// hookCommands collects every "command" string under the hooks key of a settings document.
func hookCommands(v any, out *[]string) {
	switch t := v.(type) {
	case map[string]any:
		for k, x := range t {
			if k == "command" {
				if s, ok := x.(string); ok {
					*out = append(*out, s)
				}
				continue
			}
			hookCommands(x, out)
		}
	case []any:
		for _, x := range t {
			hookCommands(x, out)
		}
	}
}

// DiscoverAgentPRSelfApproval returns one hit per offending file for CI-006.
func DiscoverAgentPRSelfApproval(manifest models.ScanManifest, skills []models.SkillDef, cmds []models.SlashCommandDef, settings []models.ClaudeSettings) []models.ConfigHit {
	root := manifest.RepoRoot
	var out []models.ConfigHit
	seen := map[string]bool{}
	add := func(rel string, line int, surface, cmd string) {
		if seen[rel] {
			return
		}
		seen[rel] = true
		out = append(out, hit(rel, line, surface+" runs "+cmd))
	}
	md := func(rel, surface string) {
		if cmd, line, ok := ghPRSelfApprovalInMarkdown(string(readConfig(root, rel))); ok {
			add(rel, line, surface, cmd)
		}
	}
	script := func(rel, surface string) {
		if cmd, line, ok := ghPRSelfApprovalInScript(string(readConfig(root, rel))); ok {
			add(rel, line, surface, cmd)
		}
	}
	for _, s := range skills {
		md(s.FilePath, "skill")
		for _, b := range s.BundledFiles {
			if b.Kind == "script" {
				script(b.Path, "skill script")
			}
		}
	}
	for _, c := range cmds {
		md(c.FilePath, "slash command")
	}
	for _, st := range settings {
		raw := readConfig(root, st.FilePath)
		var doc map[string]any
		if json.Unmarshal(stripJSONC(raw), &doc) != nil {
			continue
		}
		var cmdsOut []string
		hookCommands(doc["hooks"], &cmdsOut)
		for _, c := range cmdsOut {
			if cmd, _, ok := ghPRSelfApprovalInScript(c); ok {
				add(st.FilePath, lineOfText(raw, `"hooks"`), "hook", cmd)
				break
			}
		}
	}
	for _, rel := range manifest.ServerScriptFiles {
		if strings.HasSuffix(rel, ".sh") && (strings.HasPrefix(rel, ".claude/") || strings.Contains(rel, "/.claude/")) {
			script(rel, "hook script")
		}
	}
	return sortHits(out)
}
