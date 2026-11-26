package analysis

import (
	"net/url"
	"regexp"
	"strings"

	"github.com/trustabl/trustabl/internal/models"
)

// Agent guidance files as text (AGCFG-022, AGCFG-023). The files an agent reads
// as instructions (AGENTS.md, CLAUDE.md, GEMINI.md, Cursor .mdc rules, Windsurf
// and Devin rules, the GitHub Copilot instruction files) are split into
// sentences once, here, and the rule predicates read that list. A sentence ends at
// a line break or at ".", "!" or "?" followed by white space, so a dot inside a
// URL or a file name does not end it, and a command line in a code block is one
// sentence.

const (
	guidanceMaxSentences   = 20000
	guidanceMaxSentenceLen = 1000
)

var guidanceSentenceEndRe = regexp.MustCompile(`[.!?](?:\s+|$)`)

// guidanceListMarkerRe strips a markdown list marker, heading marker or quote
// marker from the start of a line.
var guidanceListMarkerRe = regexp.MustCompile(`^\s*(?:[-*+]\s+|#{1,6}\s+|>\s*|\d+[.)]\s+)`)

// guidanceSentences splits the guidance files in the manifest into sentences.
func guidanceSentences(manifest models.ScanManifest) []models.GuidanceSentence {
	var out []models.GuidanceSentence
	for _, c := range manifest.Components {
		switch c.Kind {
		case models.ComponentAgentsMd, models.ComponentClaudeMd, models.ComponentGeminiMd,
			models.ComponentCursorRule, models.ComponentWindsurfRule, models.ComponentCopilotInstruction:
		default:
			continue
		}
		raw := readConfig(manifest.RepoRoot, c.Path)
		for i, line := range strings.Split(string(raw), "\n") {
			line = guidanceListMarkerRe.ReplaceAllString(strings.TrimRight(line, "\r"), "")
			if strings.TrimSpace(line) == "" {
				continue
			}
			start := 0
			parts := guidanceSentenceEndRe.FindAllStringIndex(line, -1)
			parts = append(parts, []int{len(line), len(line)})
			for _, p := range parts {
				seg := strings.TrimSpace(line[start:p[0]])
				start = p[1]
				if seg == "" || len(seg) > guidanceMaxSentenceLen {
					continue
				}
				out = append(out, models.GuidanceSentence{File: c.Path, Line: i + 1, Text: strings.ToLower(seg), Raw: seg})
				if len(out) >= guidanceMaxSentences {
					return out
				}
			}
		}
	}
	return out
}

// guidanceNegations mark a sentence that forbids what it names. A sentence that
// says "do not pipe curl into sh" is not an instruction to do it.
var guidanceNegations = []string{
	"do not", "don't", "dont ", "never", "avoid", "must not", "should not", "shouldn't",
	"not allowed", "forbidden", "prohibited", "refuse",
}

// GuidanceNegated reports whether a lowercased sentence forbids what it names.
func GuidanceNegated(lowered string) bool {
	for _, n := range guidanceNegations {
		if strings.Contains(lowered, n) {
			return true
		}
	}
	return false
}

// ── AGCFG-023 ───────────────────────────────────────────────────────────────

var (
	// pipInstallRe and jsInstallRe capture the argument text after an install
	// command, up to a shell separator.
	pipInstallRe = regexp.MustCompile("(?i)(?:\\bpip3?|\\bpython3?\\s+-m\\s+pip|\\buv\\s+pip)\\s+install\\s+([^;&|`\"')\\n]*)")
	pyAddRe      = regexp.MustCompile("(?i)\\b(?:uv|poetry|pdm)\\s+add\\s+([^;&|`\"')\\n]*)")
	jsInstallRe  = regexp.MustCompile("(?i)\\b(?:npm|pnpm|yarn|bun)\\s+(?:install|add|i)\\s+([^;&|`\"')\\n]*)")

	indexURLRe = regexp.MustCompile(`(?i)(?:--(?:extra-)?index-url|--registry|npm\s+config\s+set\s+registry|\bregistry)(?:\s*=\s*|\s+)(\S+)`)
	shortIdxRe = regexp.MustCompile(`(?i)\bpip3?\s+install\b[^;&|\n]*\s-i\s+(\S+)`)

	pypiNameRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*$`)
	npmNameRe  = regexp.MustCompile(`^(?:@[a-z0-9~-][a-z0-9._~-]*/)?[a-z0-9~-][a-z0-9._~-]*$`)
)

// installProseStop are English words that end a package list: the argument text
// of an install command in prose runs on into the rest of the sentence.
var installProseStop = map[string]bool{
	"and": true, "then": true, "or": true, "to": true, "before": true, "after": true, "first": true,
	"for": true, "with": true, "so": true, "when": true, "if": true, "in": true, "on": true, "the": true,
	"a": true, "an": true, "using": true, "via": true, "as": true, "that": true, "which": true,
	"but": true, "once": true, "because": true, "you": true, "it": true, "is": true, "are": true,
	"will": true, "run": true, "needed": true, "required": true, "package": true, "packages": true,
	"name": true, "pkg": true, "dependencies": true, "deps": true, "requirements": true, "install": true, "list": true, "most": true, "use": true, "should": true, "can": true, "from": true, "at": true,
}

// flagsWithValue are install flags whose next token is a value, not a package.
var flagsWithValue = map[string]bool{
	"-r": true, "--requirement": true, "-c": true, "--constraint": true, "-e": true, "--editable": true,
	"-i": true, "--index-url": true, "--extra-index-url": true, "-f": true, "--find-links": true,
	"--target": true, "-t": true, "--prefix": true, "--python": true, "--registry": true,
	"--proxy": true, "--cert": true, "--group": true, "--with": true, "--extra": true,
}

// defaultIndexHosts are package index hosts that are not another index: the
// ecosystem defaults, and the PyTorch wheel host that install guides name for
// GPU wheels.
var defaultIndexHosts = map[string]bool{
	"pypi.org":               true,
	"files.pythonhosted.org": true,
	"registry.npmjs.org":     true,
	"registry.yarnpkg.com":   true,
	"download.pytorch.org":   true,
}

func normPyName(s string) string {
	s = strings.ToLower(s)
	return strings.NewReplacer("_", "-", ".", "-").Replace(s)
}

// guidanceInstallArgs returns the package names an install command's argument text asks
// for. Flags, paths, URLs, requirement files and placeholders are not packages.
func guidanceInstallArgs(args string, npm bool) []string {
	toks := strings.Fields(args)
	var out []string
	for i := 0; i < len(toks); i++ {
		t := strings.Trim(toks[i], "\\")
		switch {
		case t == "":
			continue
		case strings.HasPrefix(t, "#"):
			return out // a shell comment follows the command
		case installProseStop[strings.ToLower(strings.Trim(t, ",:"))]:
			return out // prose follows the package list
		case t == "-g" || t == "--global" || t == "--location=global":
			return nil // a global tool install is not a project dependency
		case strings.HasPrefix(t, "-"):
			if flagsWithValue[t] && i+1 < len(toks) {
				i++
			}
			continue
		}
		if strings.ContainsAny(t, "<>$*{}(") || strings.Contains(t, "://") || strings.HasPrefix(t, "git+") ||
			strings.HasPrefix(t, ".") || strings.HasPrefix(t, "~") || strings.HasPrefix(t, "/") ||
			strings.HasSuffix(t, ".txt") || strings.HasSuffix(t, ".whl") || strings.HasSuffix(t, ".tgz") ||
			strings.HasSuffix(t, ".zip") || t == "..." || t == "etc" {
			continue
		}
		name := t
		if npm {
			if at := strings.LastIndex(name, "@"); at > 0 {
				name = name[:at]
			}
			if !npmNameRe.MatchString(name) {
				continue
			}
		} else {
			if j := strings.IndexAny(name, "[=<>!~@;"); j >= 0 {
				name = name[:j]
			}
			if !pypiNameRe.MatchString(name) {
				continue
			}
		}
		out = append(out, name)
	}
	return out
}

// guidanceInstallHits reports guidance sentences that mandate an install the
// manifests do not back (AGCFG-023): an install command naming a package that no
// manifest of that ecosystem declares, or a package index other than the
// ecosystem default. An ecosystem with no manifest in the repo is not judged,
// because nothing can be compared.
func GuidanceInstallHits(sents []models.GuidanceSentence, deps []models.DepRef) []models.ConfigHit {
	if len(sents) == 0 {
		return nil
	}
	declared := map[string]map[string]bool{"pypi": {}, "npm": {}}
	for _, d := range deps {
		if m, ok := declared[d.Ecosystem]; ok {
			if d.Ecosystem == "pypi" {
				m[normPyName(d.Name)] = true
			} else {
				m[strings.ToLower(d.Name)] = true
			}
		}
	}
	var out []models.ConfigHit
	for _, s := range sents {
		if GuidanceNegated(s.Text) {
			continue
		}
		for _, host := range indexHosts(s.Raw) {
			if !defaultIndexHosts[host] {
				out = append(out, hit(s.File, s.Line, "points a package install at the index "+host+", which is not the ecosystem default"))
			}
		}
		for _, re := range []*regexp.Regexp{pipInstallRe, pyAddRe} {
			for _, loc := range re.FindAllStringSubmatchIndex(s.Raw, -1) {
				if len(declared["pypi"]) == 0 || nounUse(s.Raw, loc[0]) {
					break
				}
				for _, n := range guidanceInstallArgs(s.Raw[loc[2]:loc[3]], false) {
					if !declared["pypi"][normPyName(n)] {
						out = append(out, hit(s.File, s.Line, "tells the agent to install "+n+", which no dependency manifest declares"))
					}
				}
			}
		}
		for _, loc := range jsInstallRe.FindAllStringSubmatchIndex(s.Raw, -1) {
			if len(declared["npm"]) == 0 || nounUse(s.Raw, loc[0]) {
				break
			}
			for _, n := range guidanceInstallArgs(s.Raw[loc[2]:loc[3]], true) {
				if !declared["npm"][n] {
					out = append(out, hit(s.File, s.Line, "tells the agent to install "+n+", which no dependency manifest declares"))
				}
			}
		}
	}
	return sortHits(out)
}

// nounUse reports whether the word before idx is a determiner, which makes the
// install command a noun in prose ("a pip install still gets both") and not an
// instruction.
func nounUse(text string, idx int) bool {
	fields := strings.Fields(strings.ToLower(text[:idx]))
	if len(fields) == 0 {
		return false
	}
	switch strings.Trim(fields[len(fields)-1], "*_(\"'") {
	case "a", "an", "the", "no", "any", "each", "without", "plain", "that", "this", "one", "your", "its":
		return true
	}
	return false
}

// indexHosts returns the host of every package index URL literal in text.
func indexHosts(text string) []string {
	var hosts []string
	add := func(raw string) {
		raw = strings.Trim(raw, "\"'`,)")
		if strings.ContainsAny(raw, "<>${}") || !strings.Contains(raw, "://") {
			return
		}
		if u, err := url.Parse(raw); err == nil && u.Hostname() != "" {
			hosts = append(hosts, strings.ToLower(u.Hostname()))
		}
	}
	for _, m := range indexURLRe.FindAllStringSubmatch(text, -1) {
		add(m[1])
	}
	for _, m := range shortIdxRe.FindAllStringSubmatch(text, -1) {
		add(m[1])
	}
	return hosts
}
