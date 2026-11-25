package analysis

import (
	"encoding/json"
	"path"
	"regexp"
	"strings"

	"github.com/trustabl/trustabl/internal/models"
)

// ── AGCFG-017: interpreter prefix or unscoped tool in permissions.allow ─────

// unscopedBashWords are the bare command names AGCFG-017 judges in a Bash
// prefix rule, each with the fixed clause the hit's Reason uses. Claude Code's
// permissions page says a Bash rule matches the command text and that
// "sh -c" and "bash -c" forms run whatever follows; a prefix on an interpreter
// or on curl or wget therefore approves whatever program text or URL follows.
var unscopedBashWords = map[string]string{
	"python":  "a python prefix, and python runs the code it is given",
	"python3": "a python3 prefix, and python3 runs the code it is given",
	"node":    "a node prefix, and node runs the code it is given",
	"bash":    "a bash prefix, and bash runs the command it is given",
	"sh":      "an sh prefix, and sh runs the command it is given",
	"curl":    "a curl prefix, and curl fetches any URL it is given",
	"wget":    "a wget prefix, and wget fetches any URL it is given",
}

// bareWildcardWord returns the command a Bash rule pattern covers when the
// pattern is that bare command followed by a trailing wildcard (":*" or " *").
func bareWildcardWord(pattern string) (string, bool) {
	p := strings.TrimSpace(pattern)
	switch {
	case strings.HasSuffix(p, ":*"):
		p = strings.TrimSuffix(p, ":*")
	case strings.HasSuffix(p, " *"):
		p = strings.TrimSuffix(p, " *")
	default:
		return "", false
	}
	if p == "" || strings.ContainsAny(p, " \t*") {
		return "", false
	}
	return p, true
}

// wholeMCPServer returns the server name when an mcp__ rule names every tool of
// one server: mcp__server or mcp__server__*.
func wholeMCPServer(r models.PermissionRule) (string, bool) {
	if r.Tool != "MCP" {
		return "", false
	}
	p := strings.TrimSuffix(r.Pattern, "__*")
	if p == "" || strings.Contains(p, "__") || strings.Contains(p, "*") {
		return "", false
	}
	return p, true
}

// unscopedKind classifies one rule as a judged unscoped grant. It returns a key
// that a deny or ask rule of the same shape cancels, and the Reason clause.
func unscopedKind(r models.PermissionRule) (key, why string, ok bool) {
	pat := strings.TrimSpace(r.Pattern)
	switch r.Tool {
	case "Bash":
		if w, ok := bareWildcardWord(r.Pattern); ok {
			if clause, judged := unscopedBashWords[w]; judged {
				return "Bash:" + w, clause, true
			}
		}
	case "WebFetch":
		if pat == "" || pat == "*" {
			return "WebFetch", "WebFetch with no domain, which matches all web fetch requests", true
		}
	case "Edit", "Write":
		if pat == "" || pat == "*" {
			return r.Tool, r.Tool + " with no path, which approves every file edit", true
		}
	case "MCP":
		if srv, ok := wholeMCPServer(r); ok {
			return "MCP:" + srv, "every tool of the " + srv + " MCP server", true
		}
	}
	return "", "", false
}

// unscopedAllowHits flags the entries unscopedKind judges. A deny or ask rule
// in a settings file of the same .claude/ folder that has the same shape
// cancels it; a narrower deny does not, since the allow still covers the rest.
func unscopedAllowHits(root string, claude []models.ClaudeSettings) []models.ConfigHit {
	restrict := map[string]map[string]bool{}
	for _, cs := range claude {
		dir := path.Dir(cs.FilePath)
		for _, r := range append(append([]models.PermissionRule{}, cs.Permissions.Deny...), cs.Permissions.Ask...) {
			if r.Tool == "Bash" && (r.Pattern == "" || strings.TrimSpace(r.Pattern) == "*") {
				// A bare Bash deny or ask covers every Bash prefix.
				for w := range unscopedBashWords {
					mark(restrict, dir, "Bash:"+w)
				}
				continue
			}
			if key, _, ok := unscopedKind(r); ok {
				mark(restrict, dir, key)
				continue
			}
			// A bare tool deny or ask also covers the "(*)" forms and mcp__ shapes.
			switch r.Tool {
			case "WebFetch", "Edit", "Write":
				if strings.TrimSpace(r.Pattern) == "" || strings.TrimSpace(r.Pattern) == "*" {
					mark(restrict, dir, r.Tool)
				}
			}
		}
	}
	var out []models.ConfigHit
	for _, cs := range claude {
		for _, r := range cs.Permissions.Allow {
			key, why, ok := unscopedKind(r)
			if !ok || restrict[path.Dir(cs.FilePath)][key] {
				continue
			}
			line := r.Line
			if line < 1 {
				enc, _ := json.Marshal(r.Raw)
				line = lineOfText(readConfig(root, cs.FilePath), string(enc))
			}
			out = append(out, hit(cs.FilePath, line, "permissions.allow entry "+strings.TrimSpace(r.Raw)+" pre-approves "+why))
		}
	}
	return out
}

func mark(m map[string]map[string]bool, dir, key string) {
	if m[dir] == nil {
		m[dir] = map[string]bool{}
	}
	m[dir][key] = true
}

// ── AGCFG-018: settings.local.json present and not git-ignored ──────────────

// localSettingsCommittedHits flags a .claude/settings.local.json in the scanned
// tree that no .gitignore in the tree ignores. Claude Code's settings page
// says the file is kept out of git when Claude Code creates it, and that a
// hand-made file needs an entry in .gitignore.
func localSettingsCommittedHits(manifest models.ScanManifest) []models.ConfigHit {
	var out []models.ConfigHit
	for _, c := range manifest.Components {
		if c.Kind != models.ComponentClaudeSettings || path.Base(c.Path) != "settings.local.json" {
			continue
		}
		if gitignored(manifest.RepoRoot, c.Path) {
			continue
		}
		out = append(out, hit(c.Path, 1, ".claude/settings.local.json is in the scanned tree and no .gitignore in the tree ignores it"))
	}
	return out
}

// gitignored reports whether the .gitignore files from the scan root down to
// the file's folder ignore rel. Patterns are read in order from the outermost
// file inward and the last match wins, as git does. A pattern that ignores a
// parent folder ignores the file. Global excludes and .git/info/exclude are
// outside the tree and not read.
func gitignored(root, rel string) bool {
	dirs := []string{""}
	parts := strings.Split(path.Dir(rel), "/")
	for i := range parts {
		if parts[i] == "." {
			continue
		}
		dirs = append(dirs, strings.Join(parts[:i+1], "/"))
	}
	ignored := false
	for _, d := range dirs {
		raw := readConfig(root, path.Join(d, ".gitignore"))
		if raw == nil {
			continue
		}
		sub := rel
		if d != "" {
			sub = strings.TrimPrefix(rel, d+"/")
		}
		for _, line := range strings.Split(string(raw), "\n") {
			line = strings.TrimRight(strings.TrimSuffix(line, "\r"), " \t")
			if line == "" || strings.HasPrefix(line, "#") {
				continue
			}
			neg := strings.HasPrefix(line, "!")
			line = strings.TrimPrefix(line, "!")
			if gitignorePatternMatches(line, sub) {
				ignored = !neg
			}
		}
	}
	return ignored
}

func gitignorePatternMatches(pat, rel string) bool {
	dirOnly := strings.HasSuffix(pat, "/")
	pat = strings.TrimSuffix(pat, "/")
	anchored := strings.Contains(pat, "/")
	pat = strings.TrimPrefix(pat, "/")
	if pat == "" {
		return false
	}
	var b strings.Builder
	b.WriteString("^")
	if !anchored {
		b.WriteString(`(?:.*/)?`)
	}
	for i := 0; i < len(pat); i++ {
		switch c := pat[i]; {
		case c == '*' && strings.HasPrefix(pat[i:], "**/"):
			b.WriteString(`(?:.*/)?`)
			i += 2
		case c == '*' && strings.HasPrefix(pat[i:], "**"):
			b.WriteString(`.*`)
			i++
		case c == '*':
			b.WriteString(`[^/]*`)
		case c == '?':
			b.WriteString(`[^/]`)
		default:
			b.WriteString(regexp.QuoteMeta(string(c)))
		}
	}
	b.WriteString("$")
	re, err := regexp.Compile(b.String())
	if err != nil {
		return false
	}
	segs := strings.Split(rel, "/")
	for i := range segs {
		isFile := i == len(segs)-1
		if dirOnly && isFile {
			continue
		}
		if re.MatchString(strings.Join(segs[:i+1], "/")) {
			return true
		}
	}
	return false
}

// ── MCPCFG-006: launch of a package the servers README lists as archived ────

// archivedMCPPackages are the packages of the reference servers that the
// modelcontextprotocol/servers README lists under "Archived", as retrieved on
// 2026-11-25. The name of each package is the "name" in the package.json or
// pyproject.toml of that server in modelcontextprotocol/servers-archived. The
// Git server is not listed: the README does not list it as archived and it
// shows `uvx mcp-server-git` as a current command.
var archivedMCPPackages = map[string]string{
	"@modelcontextprotocol/server-aws-kb-retrieval": "AWS KB Retrieval",
	"@modelcontextprotocol/server-brave-search":     "Brave Search",
	"@modelcontextprotocol/server-everart":          "EverArt",
	"@modelcontextprotocol/server-github":           "GitHub",
	"@modelcontextprotocol/server-gitlab":           "GitLab",
	"@modelcontextprotocol/server-gdrive":           "Google Drive",
	"@modelcontextprotocol/server-google-maps":      "Google Maps",
	"@modelcontextprotocol/server-postgres":         "PostgreSQL",
	"@modelcontextprotocol/server-puppeteer":        "Puppeteer",
	"@modelcontextprotocol/server-redis":            "Redis",
	"@modelcontextprotocol/server-slack":            "Slack",
	"mcp-server-sentry":                             "Sentry",
	"mcp-server-sqlite":                             "SQLite",
}

var pyNameCutRe = regexp.MustCompile(`[=<>!~@\[;\s].*$`)

// mcpPackageName reduces a runner spec to a registry package name: the version
// of an npm spec after its last "@" (not the scope's), and a Python spec cut at
// its first specifier, extra or "@".
func mcpPackageName(spec string, npm bool) string {
	spec = strings.TrimSpace(spec)
	if npm {
		body := strings.TrimPrefix(spec, "@")
		if i := strings.IndexByte(body, '@'); i >= 0 {
			body = body[:i]
		}
		if strings.HasPrefix(spec, "@") {
			return "@" + body
		}
		return body
	}
	return strings.ToLower(pyNameCutRe.ReplaceAllString(spec, ""))
}

// classifyMCPDeprecated returns a fixed-phrase reason when the server is started
// by a package runner on a package in archivedMCPPackages, whatever its version,
// or "" otherwise. A url server and a command inside the repo are not judged.
func classifyMCPDeprecated(s mcpServer) string {
	if s.URL != "" || s.HTTPURL != "" || s.Command == "" || isRepoRelativePath(s.Command) {
		return ""
	}
	base := strings.ToLower(strings.TrimSuffix(path.Base(strings.ReplaceAll(s.Command, `\`, "/")), ".exe"))
	a := s.Args
	var specs []string
	npm := true
	pick := func(args []string, specFlags, valueFlags, shell map[string]bool) {
		r := scanRunnerArgs(args, specFlags, valueFlags, shell)
		specs = r.flagSpecs
		if len(specs) == 0 && !r.shellCmd && r.positional != "" {
			specs = []string{r.positional}
		}
	}
	switch {
	case base == "npx":
		pick(a, npmPackageFlags, npxValueFlags, npmShellFlags)
	case base == "bunx":
		pick(a, npmPackageFlags, bunxValueFlags, npmShellFlags)
	case base == "pnpm" && len(a) > 0 && a[0] == "dlx":
		pick(a[1:], npmPackageFlags, bunxValueFlags, pnpmShellFlags)
	case base == "uvx":
		npm = false
		pick(a, uvFromFlags, uvValueFlags, nil)
	case base == "uv" && len(a) > 1 && a[0] == "tool" && a[1] == "run":
		npm = false
		pick(a[2:], uvFromFlags, uvValueFlags, nil)
	case base == "pipx" && len(a) > 0 && a[0] == "run":
		npm = false
		pick(a[1:], pipxSpecFlags, pipxValueFlags, nil)
	default:
		return ""
	}
	for _, sp := range specs {
		if label, ok := archivedMCPPackages[mcpPackageName(sp, npm)]; ok {
			return "launches the " + label + " reference server package, which the modelcontextprotocol/servers README lists as archived"
		}
	}
	return ""
}
