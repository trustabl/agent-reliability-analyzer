package analysis

import (
	"regexp"
	"sort"
	"strings"

	"github.com/trustabl/trustabl/internal/models"
)

// CI-007: a repo that runs a coding agent in a workflow has no CODEOWNERS owner for that
// workflow file or for the agent-config files it holds. Only the repo is read; branch
// protection and the "require review from Code Owners" setting live in GitHub and are not
// seen.

// codeownersRule is one CODEOWNERS line: a pattern compiled to a regular expression and its
// owners. A line with no owners clears ownership for the paths it matches.
type codeownersRule struct {
	re     *regexp.Regexp
	owners int
}

// codeownersPatternRe compiles a CODEOWNERS pattern (the gitignore subset GitHub documents:
// leading / anchors to the root, a pattern with no / inside matches at any depth, a trailing
// / matches everything below, * does not cross /, ** does, ? is one non-/ character).
func codeownersPatternRe(p string) *regexp.Regexp {
	anchored := strings.HasPrefix(p, "/")
	p = strings.TrimPrefix(p, "/")
	p = strings.TrimSuffix(p, "/")
	if !anchored && !strings.Contains(p, "/") {
		p = "**/" + p
	}
	var b strings.Builder
	b.WriteString("^")
	for i := 0; i < len(p); i++ {
		switch c := p[i]; c {
		case '*':
			if i+1 < len(p) && p[i+1] == '*' {
				i++
				if i+1 < len(p) && p[i+1] == '/' {
					i++
					b.WriteString("(?:.*/)?")
				} else {
					b.WriteString(".*")
				}
			} else {
				b.WriteString("[^/]*")
			}
		case '?':
			b.WriteString("[^/]")
		case '\\':
			if i+1 < len(p) {
				i++
				b.WriteString(regexp.QuoteMeta(string(p[i])))
			}
		default:
			b.WriteString(regexp.QuoteMeta(string(c)))
		}
	}
	// A pattern names the path itself or any directory above the path.
	b.WriteString("(?:/.*)?$")
	re, err := regexp.Compile(b.String())
	if err != nil {
		return nil
	}
	return re
}

func parseCodeowners(text string) []codeownersRule {
	var out []codeownersRule
	for _, line := range strings.Split(text, "\n") {
		if i := strings.Index(line, " #"); i >= 0 {
			line = line[:i]
		}
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, "[") || strings.HasPrefix(line, "^[") {
			continue
		}
		f := strings.Fields(line)
		re := codeownersPatternRe(f[0])
		if re == nil {
			continue
		}
		out = append(out, codeownersRule{re: re, owners: len(f) - 1})
	}
	return out
}

// codeownersOwned reports whether the last matching rule gives rel at least one owner.
func codeownersOwned(rules []codeownersRule, rel string) bool {
	owned := false
	for _, r := range rules {
		if r.re.MatchString(rel) {
			owned = r.owners > 0
		}
	}
	return owned
}

// codeownersFile returns the CODEOWNERS file GitHub uses: .github/ first, then the root,
// then docs/.
func codeownersFile(files []string) string {
	for _, want := range []string{".github/CODEOWNERS", "CODEOWNERS", "docs/CODEOWNERS"} {
		for _, f := range files {
			if f == want {
				return f
			}
		}
	}
	return ""
}

var agentConfigPaths = []string{"CLAUDE.md", "AGENTS.md", "GEMINI.md", ".mcp.json", ".claude/settings.json"}

// DiscoverCodeownersGap returns one hit for CI-007, or nil. facts must come from
// DiscoverCIWorkflows (AgentWorkflowFiles).
func DiscoverCodeownersGap(manifest models.ScanManifest, facts models.CIWorkflowFacts) []models.CIHit {
	if len(facts.AgentWorkflowFiles) == 0 {
		return nil
	}
	mk := func(rel, reason string) []models.CIHit {
		return []models.CIHit{{Location: models.Location{FilePath: rel, Line: 1, EndLine: 1}, Reason: reason}}
	}
	first := facts.AgentWorkflowFiles[0]
	file := codeownersFile(manifest.CodeownersFiles)
	if file == "" {
		return mk(first, "the repo has no CODEOWNERS file and a workflow runs a coding agent")
	}
	rules := parseCodeowners(string(readConfig(manifest.RepoRoot, file)))
	for _, wf := range facts.AgentWorkflowFiles {
		if !codeownersOwned(rules, wf) {
			return mk(wf, "CODEOWNERS names no owner for a workflow that runs a coding agent")
		}
	}
	present := map[string]bool{}
	for _, l := range [][]string{manifest.MarkdownFiles, manifest.JSONFiles} {
		for _, f := range l {
			present[f] = true
		}
	}
	var cfg []string
	for _, c := range agentConfigPaths {
		if present[c] {
			cfg = append(cfg, c)
		}
	}
	sort.Strings(cfg)
	for _, c := range cfg {
		if !codeownersOwned(rules, c) {
			return mk(c, "CODEOWNERS names no owner for an agent-config file in a repo that runs a coding agent in a workflow")
		}
	}
	return nil
}
