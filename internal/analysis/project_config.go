package analysis

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/pelletier/go-toml/v2"

	"github.com/trustabl/trustabl/internal/models"
)

// Pre-trust project config (category agent_config): harness config committed to
// a repo that runs code or grants authority the moment the folder is opened,
// before the user is asked to trust it. DiscoverProjectConfig turns the files
// into typed ConfigHit facts that the repo-scope predicates read — detectors
// never re-parse config.
//
// Secrecy contract: env files are read for KEY NAMES only. A value is never
// stored in a struct, a Reason, a log, or a test fixture. Reasons are fixed
// phrases built from key names, server names, and hook event names.

// DiscoverProjectConfig reads the pre-trust config components in the manifest
// and returns the sorted, deduped facts. claude is the already-parsed
// .claude/settings.json set (DiscoverClaudeSettings).
func DiscoverProjectConfig(manifest models.ScanManifest, claude []models.ClaudeSettings) models.ProjectConfigFacts {
	var f models.ProjectConfigFacts

	// AGCFG-004: settings that auto-approve the repo's own .mcp.json servers.
	for _, cs := range claude {
		raw := readConfig(manifest.RepoRoot, cs.FilePath)
		if cs.EnableAllProjectMcpServers != nil && *cs.EnableAllProjectMcpServers {
			f.MCPAutoEnable = append(f.MCPAutoEnable, hit(cs.FilePath, lineOfText(raw, `"enableAllProjectMcpServers"`),
				"enableAllProjectMcpServers is true: every server in the repo's .mcp.json is auto-approved"))
		}
		if len(cs.EnabledMcpjsonServers) > 0 {
			f.MCPAutoEnable = append(f.MCPAutoEnable, hit(cs.FilePath, lineOfText(raw, `"enabledMcpjsonServers"`),
				"enabledMcpjsonServers pre-approves named servers from the repo's .mcp.json"))
		}
	}

	for _, c := range manifest.Components {
		switch c.Kind {
		case models.ComponentClaudeSettings, models.ComponentGeminiSettings:
			raw := readConfig(manifest.RepoRoot, c.Path)
			f.RiskyHooks = append(f.RiskyHooks, hooksHits(c.Path, raw)...)
			if c.Kind == models.ComponentGeminiSettings {
				local, unpinned := jsonMCPHits(c.Path, raw)
				f.LocalMCPLaunch = append(f.LocalMCPLaunch, local...)
				f.UnpinnedMCPLaunch = append(f.UnpinnedMCPLaunch, unpinned...)
			}
		case models.ComponentMCPConfig:
			local, unpinned := jsonMCPHits(c.Path, readConfig(manifest.RepoRoot, c.Path))
			f.LocalMCPLaunch = append(f.LocalMCPLaunch, local...)
			f.UnpinnedMCPLaunch = append(f.UnpinnedMCPLaunch, unpinned...)
		case models.ComponentEditorSettings:
			f.EditorAutoApprove = append(f.EditorAutoApprove, editorHits(c.Path, readConfig(manifest.RepoRoot, c.Path))...)
		case models.ComponentAgentEnvFile:
			f.TrustOverrideEnv = append(f.TrustOverrideEnv, envHits(c.Path, readConfig(manifest.RepoRoot, c.Path))...)
		case models.ComponentHarnessGuidance:
			f.HiddenHarnessText = append(f.HiddenHarnessText, hiddenTextHits(c.Path, readConfig(manifest.RepoRoot, c.Path))...)
		case models.ComponentAgentsMd:
			f.HiddenAgentText = append(f.HiddenAgentText, hiddenTextHits(c.Path, readConfig(manifest.RepoRoot, c.Path))...)
		case models.ComponentCodexHooks:
			f.CodexRiskyHooks = append(f.CodexRiskyHooks, codexHooksHits(c.Path, readConfig(manifest.RepoRoot, c.Path))...)
		case models.ComponentCodexConfig:
			raw := readConfig(manifest.RepoRoot, c.Path)
			f.TrustOverrideEnv = append(f.TrustOverrideEnv, codexTrustHits(c.Path, raw)...)
			local, unpinned := codexMCPHits(c.Path, raw)
			f.LocalMCPLaunch = append(f.LocalMCPLaunch, local...)
			f.UnpinnedMCPLaunch = append(f.UnpinnedMCPLaunch, unpinned...)
		}
	}

	f.MCPAutoEnable = sortHits(f.MCPAutoEnable)
	f.RiskyHooks = sortHits(f.RiskyHooks)
	f.TrustOverrideEnv = sortHits(f.TrustOverrideEnv)
	f.EditorAutoApprove = sortHits(f.EditorAutoApprove)
	f.LocalMCPLaunch = sortHits(f.LocalMCPLaunch)
	f.UnpinnedMCPLaunch = sortHits(f.UnpinnedMCPLaunch)
	f.CodexRiskyHooks = sortHits(f.CodexRiskyHooks)
	f.HiddenAgentText = sortHits(f.HiddenAgentText)
	f.HiddenHarnessText = sortHits(f.HiddenHarnessText)
	return f
}

func hit(path string, line int, reason string) models.ConfigHit {
	if line < 1 {
		line = 1
	}
	return models.ConfigHit{
		Location: models.Location{FilePath: filepath.ToSlash(path), Line: line, EndLine: line},
		Reason:   reason,
	}
}

func sortHits(h []models.ConfigHit) []models.ConfigHit {
	if len(h) == 0 {
		return nil
	}
	sort.Slice(h, func(i, j int) bool {
		a, b := h[i], h[j]
		if a.FilePath != b.FilePath {
			return a.FilePath < b.FilePath
		}
		if a.Line != b.Line {
			return a.Line < b.Line
		}
		return a.Reason < b.Reason
	})
	out := h[:1]
	for _, x := range h[1:] {
		if x != out[len(out)-1] {
			out = append(out, x)
		}
	}
	return out
}

func readConfig(root, rel string) []byte {
	b, err := os.ReadFile(filepath.Join(root, rel))
	if err != nil {
		return nil
	}
	return b
}

// lineOfText returns the 1-indexed line of the first occurrence of needle, or 1.
func lineOfText(raw []byte, needle string) int {
	i := bytes.Index(raw, []byte(needle))
	if i < 0 {
		return 1
	}
	return bytes.Count(raw[:i], []byte{'\n'}) + 1
}

// stripJSONC removes // and /* */ comments and trailing commas so settings
// files that editors write as JSONC (.vscode/settings.json) parse with
// encoding/json. String contents are left untouched.
func stripJSONC(in []byte) []byte {
	var out bytes.Buffer
	inStr, esc := false, false
	for i := 0; i < len(in); i++ {
		c := in[i]
		if inStr {
			out.WriteByte(c)
			switch {
			case esc:
				esc = false
			case c == '\\':
				esc = true
			case c == '"':
				inStr = false
			}
			continue
		}
		switch {
		case c == '"':
			inStr = true
			out.WriteByte(c)
		case c == '/' && i+1 < len(in) && in[i+1] == '/':
			for i < len(in) && in[i] != '\n' {
				i++
			}
			if i < len(in) {
				out.WriteByte('\n')
			}
		case c == '/' && i+1 < len(in) && in[i+1] == '*':
			i += 2
			for i+1 < len(in) && !(in[i] == '*' && in[i+1] == '/') {
				if in[i] == '\n' {
					out.WriteByte('\n')
				}
				i++
			}
			i++
		default:
			out.WriteByte(c)
		}
	}
	// Trailing commas: a comma followed (after whitespace) by } or ].
	b := out.Bytes()
	res := make([]byte, 0, len(b))
	inStr, esc = false, false
	for i := 0; i < len(b); i++ {
		c := b[i]
		if inStr {
			res = append(res, c)
			switch {
			case esc:
				esc = false
			case c == '\\':
				esc = true
			case c == '"':
				inStr = false
			}
			continue
		}
		if c == '"' {
			inStr = true
		}
		if c == ',' {
			j := i + 1
			for j < len(b) && (b[j] == ' ' || b[j] == '\t' || b[j] == '\n' || b[j] == '\r') {
				j++
			}
			if j < len(b) && (b[j] == '}' || b[j] == ']') {
				continue
			}
		}
		res = append(res, c)
	}
	return res
}

func parseJSONC(raw []byte, v any) bool {
	return len(raw) > 0 && json.Unmarshal(stripJSONC(raw), v) == nil
}

// ── AGCFG-005: hooks ────────────────────────────────────────────────────────

var (
	hookExecRe = regexp.MustCompile(`(?i)\b(curl|wget|nc|ncat)\b|\b(bash|sh|zsh)\s+-c\b|\bpython3?\s+-c\b|\bnode\s+-e\b|\beval\b`)
	hookSecRe  = regexp.MustCompile(`\.ssh\b|\.aws\b|\.env\b`)
	scriptExt  = regexp.MustCompile(`(?i)\.(sh|bash|py|js|mjs|cjs|ts|rb|ps1|bat|cmd)$`)
)

// isRepoRelativePath reports whether tok names a file inside the repo: an
// explicit ./ ../ or project-dir variable prefix, or a relative path with a
// directory part or a script extension. Absolute, home-relative, other-variable
// and flag tokens are not repo-relative.
func isRepoRelativePath(tok string) bool {
	tok = strings.Trim(tok, `"'`)
	switch {
	case tok == "" || strings.HasPrefix(tok, "-"):
		return false
	case strings.HasPrefix(tok, "./"), strings.HasPrefix(tok, "../"),
		strings.HasPrefix(tok, "$CLAUDE_PROJECT_DIR"), strings.HasPrefix(tok, "${CLAUDE_PROJECT_DIR}"),
		strings.HasPrefix(tok, "$GEMINI_PROJECT_DIR"), strings.HasPrefix(tok, "${GEMINI_PROJECT_DIR}"),
		strings.HasPrefix(tok, "$PWD"), strings.HasPrefix(tok, "${PWD}"):
		return true
	case strings.HasPrefix(tok, "/"), strings.HasPrefix(tok, "~"), strings.HasPrefix(tok, "$"),
		strings.Contains(tok, "://"), strings.HasPrefix(tok, "@"):
		return false
	}
	return strings.Contains(tok, "/") || scriptExt.MatchString(tok)
}

// classifyHookCommand returns fixed-phrase reasons a hook command is
// repo-controlled or exfiltration-capable; empty when it looks benign.
func classifyHookCommand(cmd string) []string {
	var r []string
	for _, tok := range strings.Fields(cmd) {
		if isRepoRelativePath(tok) {
			r = append(r, "runs a repo-relative script")
			break
		}
	}
	if hookExecRe.MatchString(cmd) {
		r = append(r, "uses a network or inline-exec primitive (curl/wget/nc, bash -c, python -c)")
	}
	if hookSecRe.MatchString(cmd) {
		r = append(r, "references .ssh, .aws, or .env")
	}
	return r
}

type hookEntry struct {
	Type           string      `json:"type"`
	Command        string      `json:"command"`
	CommandWindows string      `json:"commandWindows"`
	Hooks          []hookEntry `json:"hooks"`
}

// codexRepoRootRe matches the repo-root form OpenAI documents for Codex hook
// commands, "$(git rev-parse --show-toplevel)/.codex/hooks/x.py". The shared
// repo-relative check ignores tokens that start with $, so without this the
// documented idiom would produce no hit.
var codexRepoRootRe = regexp.MustCompile(`\$\(\s*git\s+rev-parse\s+--show-toplevel\s*\)["']?/`)

// classifyCodexHookCommand is classifyHookCommand plus the documented
// repo-root form.
func classifyCodexHookCommand(cmd string) []string {
	r := classifyHookCommand(cmd)
	if codexRepoRootRe.MatchString(cmd) {
		for _, why := range r {
			if why == "runs a repo-relative script" {
				return r
			}
		}
		r = append([]string{"runs a repo-relative script"}, r...)
	}
	return r
}

// codexHooksHits reads .codex/hooks.json (hooks.<Event>[].hooks[]). Only
// "command" handlers (or an untyped one) carry a shell command; mcp_tool,
// prompt and agent handlers are skipped. Both command and commandWindows are
// judged, since either can run on the user's machine.
func codexHooksHits(path string, raw []byte) []models.ConfigHit {
	var doc struct {
		Hooks map[string]json.RawMessage `json:"hooks"`
	}
	if !parseJSONC(raw, &doc) {
		return nil
	}
	var out []models.ConfigHit
	for event, msg := range doc.Hooks {
		var entries []hookEntry
		if json.Unmarshal(msg, &entries) != nil {
			continue
		}
		var walk func(es []hookEntry)
		walk = func(es []hookEntry) {
			for _, e := range es {
				if e.Type != "" && e.Type != "command" {
					continue
				}
				for _, cmd := range []string{e.Command, e.CommandWindows} {
					if cmd == "" {
						continue
					}
					enc, _ := json.Marshal(cmd)
					for _, why := range classifyCodexHookCommand(cmd) {
						out = append(out, hit(path, lineOfText(raw, string(enc)), "hook "+event+" "+why))
					}
				}
				walk(e.Hooks)
			}
		}
		walk(entries)
	}
	return out
}

func hooksHits(path string, raw []byte) []models.ConfigHit {
	var doc struct {
		Hooks map[string]json.RawMessage `json:"hooks"`
	}
	if !parseJSONC(raw, &doc) {
		return nil
	}
	var out []models.ConfigHit
	for event, msg := range doc.Hooks {
		var entries []hookEntry
		if json.Unmarshal(msg, &entries) != nil {
			continue
		}
		var walk func(es []hookEntry)
		walk = func(es []hookEntry) {
			for _, e := range es {
				if e.Command != "" {
					enc, _ := json.Marshal(e.Command)
					for _, why := range classifyHookCommand(e.Command) {
						out = append(out, hit(path, lineOfText(raw, string(enc)), "hook "+event+" "+why))
					}
				}
				walk(e.Hooks)
			}
		}
		walk(entries)
	}
	return out
}

// ── MCPCFG-002: local-launch MCP servers ────────────────────────────────────

type mcpServer struct {
	Command string   `json:"command"`
	Args    []string `json:"args"`
	URL     string   `json:"url"`
	HTTPURL string   `json:"httpUrl"`
}

var launcherShells = map[string]bool{
	"bash": true, "sh": true, "zsh": true, "cmd": true,
	"powershell": true, "pwsh": true, "python": true, "python3": true, "node": true,
}

var inlineFlags = map[string]bool{
	"-c": true, "-e": true, "--eval": true, "/c": true, "-command": true, "-encodedcommand": true,
}

// classifyMCPLaunch returns a fixed-phrase reason when the server starts local
// repo-controlled code, or "" for a published package, a remote URL, or a
// command outside the repo.
func classifyMCPLaunch(s mcpServer) string {
	if s.URL != "" || s.HTTPURL != "" || s.Command == "" {
		return ""
	}
	base := strings.ToLower(strings.TrimSuffix(filepath.Base(strings.ReplaceAll(s.Command, `\`, "/")), ".exe"))
	if launcherShells[base] {
		for _, a := range s.Args {
			if inlineFlags[strings.ToLower(a)] {
				return "launches " + base + " with inline code"
			}
		}
		for _, a := range s.Args {
			if strings.HasPrefix(a, "-") {
				continue
			}
			if isRepoRelativePath(a) {
				return "launches " + base + " on a repo-relative script"
			}
			break
		}
		return ""
	}
	if isRepoRelativePath(s.Command) {
		return "launches a command inside the repo"
	}
	return ""
}

// mcpHit classifies one server entry: a repo-controlled launch (MCPCFG-002) or
// an unpinned package launch (MCPCFG-003). The two classifiers never accept the
// same entry, so at most one hit is returned.
func mcpHit(s mcpServer, path string, line int, name string) (local, unpinned *models.ConfigHit) {
	if why := classifyMCPLaunch(s); why != "" {
		h := hit(path, line, "MCP server "+name+" "+why)
		return &h, nil
	}
	if why := classifyMCPPin(s); why != "" {
		h := hit(path, line, "MCP server "+name+" "+why)
		return nil, &h
	}
	return nil, nil
}

func jsonMCPHits(path string, raw []byte) (local, unpinned []models.ConfigHit) {
	var doc struct {
		McpServers map[string]mcpServer `json:"mcpServers"`
		Servers    map[string]mcpServer `json:"servers"`
	}
	if !parseJSONC(raw, &doc) {
		return nil, nil
	}
	for _, m := range []map[string]mcpServer{doc.McpServers, doc.Servers} {
		for name, s := range m {
			l, u := mcpHit(s, path, lineOfText(raw, `"`+name+`"`), name)
			if l != nil {
				local = append(local, *l)
			}
			if u != nil {
				unpinned = append(unpinned, *u)
			}
		}
	}
	return local, unpinned
}

func codexMCPHits(path string, raw []byte) (local, unpinned []models.ConfigHit) {
	var doc struct {
		McpServers map[string]mcpServer `toml:"mcp_servers"`
	}
	if len(raw) == 0 || toml.Unmarshal(raw, &doc) != nil {
		return nil, nil
	}
	for name, s := range doc.McpServers {
		l, u := mcpHit(s, path, lineOfText(raw, "mcp_servers."+name), name)
		if l != nil {
			local = append(local, *l)
		}
		if u != nil {
			unpinned = append(unpinned, *u)
		}
	}
	return local, unpinned
}

// ── MCPCFG-003: unpinned package launches ───────────────────────────────────

var (
	exactSemverRe  = regexp.MustCompile(`^v?\d+\.\d+\.\d+(-[0-9A-Za-z.-]+)?(\+[0-9A-Za-z.-]+)?$`)
	pyExactPinRe   = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*(\[[^\]]*\])?\s*===?\s*[^\s,*;<>!~=]+$`)
	sha256DigestRe = regexp.MustCompile(`@sha256:[0-9a-f]{64}`)
)

// Flags that take a value, per runner. A flag outside these lists is read as
// boolean, so an unlisted value flag can make the check read the wrong argument.
var (
	npxValueFlags = map[string]bool{
		"-p": true, "--package": true, "-c": true, "--call": true, "-w": true, "--workspace": true,
		"--registry": true, "--cache": true, "--userconfig": true, "--prefix": true,
	}
	// bunx and pnpm dlx: the package flag is the only one read; -c and
	// --shell-mode only mark the command as a shell string.
	bunxValueFlags = map[string]bool{"-p": true, "--package": true}
	// uv tool run options that take a value, from the uv CLI reference.
	uvValueFlags = map[string]bool{
		"--allow-insecure-host": true, "--trusted-host": true, "--build-constraints": true,
		"--build-constraint": true, "-b": true, "--cache-dir": true, "--color": true,
		"--config-file": true, "--config-setting": true, "--config-settings": true, "-C": true,
		"--config-settings-package": true, "--constraints": true, "--constraint": true, "-c": true,
		"--default-index": true, "--directory": true, "--env-file": true, "--exclude-newer": true,
		"--exclude-newer-package": true, "--extra-index-url": true, "--find-links": true, "-f": true,
		"--fork-strategy": true, "--from": true, "--index": true, "--index-strategy": true,
		"--index-url": true, "-i": true, "--keyring-provider": true, "--link-mode": true,
		"--no-binary-package": true, "--no-build-isolation-package": true, "--no-build-package": true,
		"--no-sources-package": true, "--overrides": true, "--override": true, "--prerelease": true,
		"--prerelease-package": true, "--project": true, "--python": true, "-p": true,
		"--python-platform": true, "--refresh-package": true, "--reinstall-package": true,
		"--resolution": true, "--torch-backend": true, "--upgrade-group": true,
		"--upgrade-package": true, "-P": true, "--with": true, "-w": true,
		"--with-editable": true, "--with-requirements": true,
	}
	pipxValueFlags = map[string]bool{"--spec": true, "--python": true, "--pip-args": true, "--index-url": true, "-i": true, "--path": true}

	npmPackageFlags = map[string]bool{"-p": true, "--package": true}
	npmShellFlags   = map[string]bool{"-c": true, "--call": true}
	pnpmShellFlags  = map[string]bool{"-c": true, "--shell-mode": true}
	uvFromFlags     = map[string]bool{"--from": true}
	pipxSpecFlags   = map[string]bool{"--spec": true}
)

// runnerArgs is what scanRunnerArgs reads from a package runner's arguments.
type runnerArgs struct {
	flagSpecs  []string // values of the spec flag (--package, --from, --spec)
	positional string   // first positional argument, "" when none
	shellCmd   bool     // the command was given as a shell string (-c)
}

// scanRunnerArgs walks args up to the first positional argument, which is where
// a package runner stops reading its own flags. specFlags name the package;
// valueFlags consume the next argument; shellFlags mark a shell-string command.
func scanRunnerArgs(args []string, specFlags, valueFlags, shellFlags map[string]bool) runnerArgs {
	var r runnerArgs
	for i := 0; i < len(args); i++ {
		a := args[i]
		if a == "--" {
			if i+1 < len(args) {
				r.positional = args[i+1]
			}
			break
		}
		if !strings.HasPrefix(a, "-") || a == "-" {
			r.positional = a
			break
		}
		name, val, hasEq := strings.Cut(a, "=")
		if shellFlags[name] {
			r.shellCmd = true
		}
		if specFlags[name] {
			switch {
			case hasEq:
				r.flagSpecs = append(r.flagSpecs, val)
			case i+1 < len(args):
				r.flagSpecs = append(r.flagSpecs, args[i+1])
				i++
			}
			continue
		}
		if valueFlags[name] && !hasEq {
			i++
		}
	}
	return r
}

// npmSpecPinned judges one npm-style package spec. judged is false for a spec
// that is not a registry name (path, git, URL, alias, interpolation), which is
// neither pinned nor unpinned.
func npmSpecPinned(spec string) (pinned, judged bool) {
	spec = strings.TrimSpace(spec)
	if spec == "" || strings.Contains(spec, "$") || strings.Contains(spec, "://") {
		return false, false
	}
	for _, p := range []string{".", "/", "~", "file:", "link:", "git+", "git:", "github:", "gitlab:", "bitbucket:", "gist:", "npm:", "workspace:"} {
		if strings.HasPrefix(spec, p) {
			return false, false
		}
	}
	if !strings.HasPrefix(spec, "@") && strings.Contains(spec, "/") { // user/repo shorthand
		return false, false
	}
	body := strings.TrimPrefix(spec, "@")
	i := strings.IndexByte(body, '@')
	if i < 0 {
		return false, true
	}
	ver := body[i+1:]
	if strings.ContainsAny(ver, ":/") {
		return false, false
	}
	return exactSemverRe.MatchString(ver), true
}

// pySpecPinned judges one Python package spec. atExact credits uv's name@X
// form, which uv documents as exact-only.
func pySpecPinned(spec string, atExact bool) (pinned, judged bool) {
	spec = strings.TrimSpace(strings.SplitN(spec, ";", 2)[0])
	low := strings.ToLower(spec)
	if spec == "" || strings.ContainsAny(spec, "$/\\") || strings.Contains(spec, "://") ||
		strings.HasPrefix(low, "git+") || strings.HasPrefix(spec, ".") || strings.HasPrefix(spec, "~") ||
		strings.Contains(spec, " @ ") || strings.HasSuffix(low, ".whl") || strings.HasSuffix(low, ".tar.gz") || strings.HasSuffix(low, ".zip") {
		return false, false
	}
	if pyExactPinRe.MatchString(spec) {
		return true, true
	}
	if atExact {
		if name, ver, ok := strings.Cut(spec, "@"); ok && name != "" && ver != "" && ver != "latest" {
			return true, true
		}
	}
	return false, true
}

// anyUnpinned reports whether some judged spec is not pinned.
func anyUnpinned(specs []string, pinned func(string) (bool, bool)) bool {
	for _, sp := range specs {
		if p, judged := pinned(sp); judged && !p {
			return true
		}
	}
	return false
}

// classifyMCPPin returns a fixed-phrase reason when the server is started by a
// package runner on a registry package or image that is not pinned to an exact
// version (a digest for docker), or "" otherwise. A url server, a command
// inside the repo (MCPCFG-002's), and a spec that is a path, git source, URL or
// interpolation are not judged. The spec is never echoed into the reason.
func classifyMCPPin(s mcpServer) string {
	if s.URL != "" || s.HTTPURL != "" || s.Command == "" || isRepoRelativePath(s.Command) {
		return ""
	}
	base := strings.ToLower(filepath.Base(strings.ReplaceAll(s.Command, `\`, "/")))
	base = strings.TrimSuffix(base, ".exe")
	a := s.Args
	reason := func(label string) string {
		return "launches a " + label + " package that is not pinned to an exact version"
	}
	npm := func(label string, args []string, value, shell map[string]bool) string {
		r := scanRunnerArgs(args, npmPackageFlags, value, shell)
		specs := r.flagSpecs
		switch {
		case len(specs) > 0:
		case r.shellCmd || r.positional == "":
			return ""
		default:
			specs = []string{r.positional}
		}
		if anyUnpinned(specs, npmSpecPinned) {
			return reason(label)
		}
		return ""
	}
	py := func(label string, args []string, from, value map[string]bool, atExact bool) string {
		r := scanRunnerArgs(args, from, value, nil)
		specs := r.flagSpecs
		if len(specs) == 0 && r.positional != "" {
			specs = []string{r.positional}
		}
		if anyUnpinned(specs, func(sp string) (bool, bool) { return pySpecPinned(sp, atExact) }) {
			return reason(label)
		}
		return ""
	}
	switch {
	case base == "npx":
		return npm("npx", a, npxValueFlags, npmShellFlags)
	case base == "bunx":
		return npm("bunx", a, bunxValueFlags, npmShellFlags)
	case base == "pnpm" && len(a) > 0 && a[0] == "dlx":
		return npm("pnpm dlx", a[1:], bunxValueFlags, pnpmShellFlags)
	case base == "uvx":
		return py("uvx", a, uvFromFlags, uvValueFlags, true)
	case base == "uv" && len(a) > 1 && a[0] == "tool" && a[1] == "run":
		return py("uv tool run", a[2:], uvFromFlags, uvValueFlags, true)
	case base == "pipx" && len(a) > 0 && a[0] == "run":
		return py("pipx run", a[1:], pipxSpecFlags, pipxValueFlags, false)
	case base == "docker" && len(a) > 1 && a[0] == "run":
		for _, x := range a[1:] {
			if sha256DigestRe.MatchString(x) || strings.Contains(x, "$") {
				return ""
			}
		}
		return "runs a docker image that is not pinned by digest"
	}
	return ""
}

// ── AGCFG-007: editor auto-approve ──────────────────────────────────────────

func editorHits(path string, raw []byte) []models.ConfigHit {
	var doc map[string]any
	if !parseJSONC(raw, &doc) {
		return nil
	}
	var out []models.ConfigHit
	var walk func(prefix string, m map[string]any)
	walk = func(prefix string, m map[string]any) {
		for k, v := range m {
			full := prefix + k
			switch t := v.(type) {
			case map[string]any:
				walk(full+".", t)
			case bool:
				lk := strings.ToLower(full)
				if t && (strings.Contains(lk, "autoapprove") || strings.Contains(lk, "yolo")) {
					out = append(out, hit(path, lineOfText(raw, `"`+k+`"`), "editor setting "+full+" is true: agent tool calls run without approval"))
				}
			}
		}
	}
	walk("", doc)
	return out
}

// ── AGCFG-006: workspace env / Codex trust overrides ────────────────────────

var envKeyRe = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

// trustKey reports whether an env key can redirect a sandbox, proxy, endpoint,
// or loader. full selects the wide set (.gemini/.env, .codex/config.toml);
// the narrow set is used for a repo-root .env, where ordinary applications set
// *_BASE_URL, NODE_OPTIONS and LD_PRELOAD legitimately.
func trustKey(key string, full bool) bool {
	k := strings.ToUpper(key)
	if strings.HasPrefix(k, "GEMINI_SANDBOX") || strings.HasSuffix(k, "_PROXY_COMMAND") {
		return true
	}
	if !full {
		return false
	}
	return strings.HasSuffix(k, "_BASE_URL") || k == "NODE_OPTIONS" || k == "LD_PRELOAD"
}

// envHits reads KEY NAMES from a dotenv file. The right-hand side of every
// line is discarded unread.
func envHits(path string, raw []byte) []models.ConfigHit {
	full := path != ".env" // a repo-root .env gets the narrow key set
	var out []models.ConfigHit
	for i, line := range strings.Split(string(raw), "\n") {
		line = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(line), "export "))
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		eq := strings.IndexByte(line, '=')
		if eq <= 0 {
			continue
		}
		key := strings.TrimSpace(line[:eq])
		if !envKeyRe.MatchString(key) || !trustKey(key, full) {
			continue
		}
		out = append(out, hit(path, i+1, "env key "+key+" overrides sandbox, proxy, endpoint, or loader behavior"))
	}
	return out
}

func codexTrustHits(path string, raw []byte) []models.ConfigHit {
	var doc map[string]any
	if len(raw) == 0 || toml.Unmarshal(raw, &doc) != nil {
		return nil
	}
	var out []models.ConfigHit
	if v, _ := doc["sandbox_mode"].(string); v == "danger-full-access" {
		out = append(out, hit(path, lineOfText(raw, "sandbox_mode"), `sandbox_mode = "danger-full-access" disables the sandbox`))
	}
	if v, _ := doc["approval_policy"].(string); v == "never" {
		out = append(out, hit(path, lineOfText(raw, "approval_policy"), `approval_policy = "never" removes command approval`))
	}
	var walk func(m map[string]any)
	walk = func(m map[string]any) {
		for k, v := range m {
			if sub, ok := v.(map[string]any); ok {
				walk(sub)
				continue
			}
			if trustKey(k, true) {
				out = append(out, hit(path, lineOfText(raw, k), "key "+k+" overrides sandbox, proxy, endpoint, or loader behavior"))
			}
		}
	}
	walk(doc)
	return out
}
