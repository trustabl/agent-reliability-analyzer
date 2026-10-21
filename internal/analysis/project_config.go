package analysis

import (
	"bytes"
	"encoding/json"
	"os"
	"path"
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

	// AGCFG-011: WebFetch allow rules that cover a user-content host.
	f.UserContentFetchAllow = userContentFetchHits(manifest.RepoRoot, claude)

	for _, c := range manifest.Components {
		switch c.Kind {
		case models.ComponentClaudeSettings, models.ComponentGeminiSettings:
			raw := readConfig(manifest.RepoRoot, c.Path)
			f.RiskyHooks = append(f.RiskyHooks, hooksHits(c.Path, raw)...)
			if c.Kind == models.ComponentGeminiSettings {
				f.LocalMCPLaunch = append(f.LocalMCPLaunch, jsonMCPHits(c.Path, raw)...)
			}
		case models.ComponentMCPConfig:
			f.LocalMCPLaunch = append(f.LocalMCPLaunch, jsonMCPHits(c.Path, readConfig(manifest.RepoRoot, c.Path))...)
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
			f.LocalMCPLaunch = append(f.LocalMCPLaunch, codexMCPHits(c.Path, raw)...)
		}
	}

	f.MCPAutoEnable = sortHits(f.MCPAutoEnable)
	f.RiskyHooks = sortHits(f.RiskyHooks)
	f.TrustOverrideEnv = sortHits(f.TrustOverrideEnv)
	f.EditorAutoApprove = sortHits(f.EditorAutoApprove)
	f.LocalMCPLaunch = sortHits(f.LocalMCPLaunch)
	f.CodexRiskyHooks = sortHits(f.CodexRiskyHooks)
	f.HiddenAgentText = sortHits(f.HiddenAgentText)
	f.HiddenHarnessText = sortHits(f.HiddenHarnessText)
	f.UserContentFetchAllow = sortHits(f.UserContentFetchAllow)
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

func jsonMCPHits(path string, raw []byte) []models.ConfigHit {
	var doc struct {
		McpServers map[string]mcpServer `json:"mcpServers"`
		Servers    map[string]mcpServer `json:"servers"`
	}
	if !parseJSONC(raw, &doc) {
		return nil
	}
	var out []models.ConfigHit
	for _, m := range []map[string]mcpServer{doc.McpServers, doc.Servers} {
		for name, s := range m {
			if why := classifyMCPLaunch(s); why != "" {
				out = append(out, hit(path, lineOfText(raw, `"`+name+`"`), "MCP server "+name+" "+why))
			}
		}
	}
	return out
}

func codexMCPHits(path string, raw []byte) []models.ConfigHit {
	var doc struct {
		McpServers map[string]mcpServer `toml:"mcp_servers"`
	}
	if len(raw) == 0 || toml.Unmarshal(raw, &doc) != nil {
		return nil
	}
	var out []models.ConfigHit
	for name, s := range doc.McpServers {
		if why := classifyMCPLaunch(s); why != "" {
			out = append(out, hit(path, lineOfText(raw, "mcp_servers."+name), "MCP server "+name+" "+why))
		}
	}
	return out
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

// ── AGCFG-011: pre-approved fetches from user-content hosts ────────────────

// userContentFetchProbes are the hosts AGCFG-011 treats as serving content any
// account can publish, each sourced in the rulebook doc. A "*."-labelled entry
// stands for an arbitrary account's site on that suffix, so a rule must cover
// a subdomain no one account owns: WebFetch(domain:alice.github.io) is silent.
var userContentFetchProbes = []struct{ host, label string }{
	{"github.com", "github.com"},
	{"gist.github.com", "gist.github.com"},
	{"raw.githubusercontent.com", "raw.githubusercontent.com"},
	{"any-account.github.io", "*.github.io"},
	{"huggingface.co", "huggingface.co"},
	{"bitbucket.org", "bitbucket.org"},
	{"pastebin.com", "pastebin.com"},
	{"any-project.readthedocs.io", "*.readthedocs.io"},
}

// webFetchCovers reports whether a permission rule matches a fetch to host,
// per Claude Code's documented domain: matching: case-insensitive, trailing
// "." ignored, bare "*" matches any host, a leading "*." matches subdomains at
// any depth but not the apex, any other "*" stays within one label. The bare
// tool name matches every fetch. WebFetch() and non-domain: specifiers are not
// judged (their behavior is not documented).
func webFetchCovers(r models.PermissionRule, host string) bool {
	if r.Tool != "WebFetch" {
		return false
	}
	if r.Raw == "WebFetch" {
		return true
	}
	p, ok := strings.CutPrefix(strings.TrimSpace(r.Pattern), "domain:")
	if !ok {
		return false
	}
	p = strings.TrimSuffix(strings.ToLower(p), ".")
	host = strings.TrimSuffix(strings.ToLower(host), ".")
	if p == "*" {
		return true
	}
	hl := strings.Split(host, ".")
	if rest, sub := strings.CutPrefix(p, "*."); sub {
		pl := strings.Split(rest, ".")
		return len(hl) > len(pl) && labelsMatch(pl, hl[len(hl)-len(pl):])
	}
	return labelsMatch(strings.Split(p, "."), hl)
}

// labelsMatch compares domain labels pairwise; "*" inside a label matches any
// run of characters within that label.
func labelsMatch(pat, host []string) bool {
	if len(pat) != len(host) {
		return false
	}
	for i := range pat {
		re := "^" + strings.ReplaceAll(regexp.QuoteMeta(pat[i]), `\*`, `[^.]*`) + "$"
		if ok, _ := regexp.MatchString(re, host[i]); !ok {
			return false
		}
	}
	return true
}

// userContentFetchHits emits one hit per allow entry that covers a probe host
// left uncovered by every deny and ask rule in the same .claude/ folder.
// Claude Code merges settings.json and settings.local.json, and deny then ask
// are evaluated before allow, so a restricting rule in either file wins.
func userContentFetchHits(root string, claude []models.ClaudeSettings) []models.ConfigHit {
	restrict := map[string][]models.PermissionRule{}
	for _, cs := range claude {
		dir := path.Dir(cs.FilePath)
		restrict[dir] = append(restrict[dir], cs.Permissions.Deny...)
		restrict[dir] = append(restrict[dir], cs.Permissions.Ask...)
	}
	var out []models.ConfigHit
	for _, cs := range claude {
		for _, r := range cs.Permissions.Allow {
			for _, pr := range userContentFetchProbes {
				if !webFetchCovers(r, pr.host) || anyCovers(restrict[path.Dir(cs.FilePath)], pr.host) {
					continue
				}
				line := r.Line
				if line < 1 {
					enc, _ := json.Marshal(r.Raw)
					line = lineOfText(readConfig(root, cs.FilePath), string(enc))
				}
				out = append(out, hit(cs.FilePath, line, "permissions.allow entry "+r.Raw+" pre-approves fetches from "+pr.label))
				break
			}
		}
	}
	return out
}

func anyCovers(rules []models.PermissionRule, host string) bool {
	for _, r := range rules {
		if webFetchCovers(r, host) {
			return true
		}
	}
	return false
}
