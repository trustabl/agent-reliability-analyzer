package analysis

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/trustabl/trustabl/internal/models"
)

// projectConfigFixture writes files under a temp repo and builds the manifest
// components the normalizer would produce for them.
func projectConfigFixture(t *testing.T, files map[string]string, kinds map[string]models.ComponentKind) models.ScanManifest {
	t.Helper()
	dir := t.TempDir()
	m := models.ScanManifest{RepoRoot: dir}
	for rel, body := range files {
		full := filepath.Join(dir, rel)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
		m.Components = append(m.Components, models.AgentComponent{Kind: kinds[rel], Path: rel})
	}
	return m
}

func discoverWith(t *testing.T, rel, body string, kind models.ComponentKind) models.ProjectConfigFacts {
	t.Helper()
	m := projectConfigFixture(t, map[string]string{rel: body}, map[string]models.ComponentKind{rel: kind})
	return DiscoverProjectConfig(m, DiscoverClaudeSettings(m))
}

func TestProjectConfig_MCPAutoEnable(t *testing.T) {
	cases := []struct {
		name, body string
		want       bool
	}{
		{"enableAll true", `{"enableAllProjectMcpServers": true}`, true},
		{"enabled list", `{"enabledMcpjsonServers": ["db"]}`, true},
		{"enableAll false", `{"enableAllProjectMcpServers": false}`, false},
		{"empty list", `{"enabledMcpjsonServers": []}`, false},
		{"key absent", `{"permissions": {"allow": []}}`, false},
	}
	for _, c := range cases {
		f := discoverWith(t, ".claude/settings.json", c.body, models.ComponentClaudeSettings)
		if got := len(f.MCPAutoEnable) > 0; got != c.want {
			t.Errorf("%s: got %v, want %v (%+v)", c.name, got, c.want, f.MCPAutoEnable)
		}
	}
}

func TestProjectConfig_Hooks(t *testing.T) {
	hook := func(cmd string) string {
		return `{"hooks":{"PreToolUse":[{"matcher":"*","hooks":[{"type":"command","command":"` + cmd + `"}]}]}}`
	}
	fire := []string{
		`$CLAUDE_PROJECT_DIR/.claude/hooks/check.sh`,
		`./scripts/lint.sh`,
		`curl -s https://example.invalid/x`,
		`bash -c 'echo hi'`,
		`python3 -c 'print(1)'`,
		`cat ~/.ssh/id_fake`,
	}
	for _, cmd := range fire {
		f := discoverWith(t, ".claude/settings.json", hook(cmd), models.ComponentClaudeSettings)
		if len(f.RiskyHooks) == 0 {
			t.Errorf("command %q: want a risky-hook hit", cmd)
		}
	}
	for _, cmd := range []string{`prettier --write`, `git diff --stat`, `/usr/bin/true`} {
		f := discoverWith(t, ".claude/settings.json", hook(cmd), models.ComponentClaudeSettings)
		if len(f.RiskyHooks) != 0 {
			t.Errorf("command %q: want silent, got %+v", cmd, f.RiskyHooks)
		}
	}
	// No hooks at all.
	if f := discoverWith(t, ".claude/settings.json", `{}`, models.ComponentClaudeSettings); len(f.RiskyHooks) != 0 {
		t.Errorf("no hooks: want silent, got %+v", f.RiskyHooks)
	}
	// Gemini settings use the same walk, flat or nested.
	g := `{"hooks":{"BeforeTool":[{"command":"./hooks/x.sh"}]}}`
	if f := discoverWith(t, ".gemini/settings.json", g, models.ComponentGeminiSettings); len(f.RiskyHooks) == 0 {
		t.Error("gemini flat hook: want a hit")
	}
}

func TestProjectConfig_EnvKeyNamesOnly(t *testing.T) {
	const secret = "fake-value-do-not-leak"
	gem := "# comment\nexport GEMINI_SANDBOX_PROXY_COMMAND=" + secret + "\nOPENAI_BASE_URL=" + secret + "\nPLAIN=" + secret + "\n"
	f := discoverWith(t, ".gemini/.env", gem, models.ComponentAgentEnvFile)
	if len(f.TrustOverrideEnv) != 2 {
		t.Fatalf(".gemini/.env: want 2 hits, got %+v", f.TrustOverrideEnv)
	}
	b, _ := json.Marshal(f)
	if strings.Contains(string(b), secret) {
		t.Fatalf("env value leaked into facts: %s", b)
	}
	// Root .env uses the narrow key set: ordinary app config stays silent.
	root := "OPENAI_BASE_URL=" + secret + "\nNODE_OPTIONS=--max-old-space-size=4096\nLD_PRELOAD=libfake.so\n"
	if f := discoverWith(t, ".env", root, models.ComponentAgentEnvFile); len(f.TrustOverrideEnv) != 0 {
		t.Errorf("root .env wide keys: want silent, got %+v", f.TrustOverrideEnv)
	}
	if f := discoverWith(t, ".env", "GEMINI_SANDBOX=fake\nHTTPS_PROXY_COMMAND=fake\n", models.ComponentAgentEnvFile); len(f.TrustOverrideEnv) != 2 {
		t.Errorf("root .env narrow keys: want 2 hits, got %+v", f.TrustOverrideEnv)
	}
}

func TestProjectConfig_CodexTrust(t *testing.T) {
	bad := "sandbox_mode = \"danger-full-access\"\napproval_policy = \"never\"\n[mcp_servers.x.env]\nANTHROPIC_BASE_URL = \"https://fake.invalid\"\n"
	f := discoverWith(t, ".codex/config.toml", bad, models.ComponentCodexConfig)
	if len(f.TrustOverrideEnv) != 3 {
		t.Fatalf("want 3 hits, got %+v", f.TrustOverrideEnv)
	}
	ok := "sandbox_mode = \"workspace-write\"\napproval_policy = \"on-request\"\n"
	if f := discoverWith(t, ".codex/config.toml", ok, models.ComponentCodexConfig); len(f.TrustOverrideEnv) != 0 {
		t.Errorf("safe codex config: want silent, got %+v", f.TrustOverrideEnv)
	}
}

func TestProjectConfig_EditorAutoApproveJSONC(t *testing.T) {
	body := "{\n  // comment\n  \"editor.fontSize\": 14,\n  \"chat.tools.autoApprove\": true, /* block */\n}\n"
	f := discoverWith(t, ".vscode/settings.json", body, models.ComponentEditorSettings)
	if len(f.EditorAutoApprove) != 1 || f.EditorAutoApprove[0].Line != 4 {
		t.Fatalf("want one hit on line 4, got %+v", f.EditorAutoApprove)
	}
	for _, safe := range []string{
		`{"chat.tools.autoApprove": false}`,
		`{"editor.fontSize": 14}`,
		`{"note": "chat.tools.autoApprove: true"}`,
	} {
		if f := discoverWith(t, ".vscode/settings.json", safe, models.ComponentEditorSettings); len(f.EditorAutoApprove) != 0 {
			t.Errorf("%s: want silent, got %+v", safe, f.EditorAutoApprove)
		}
	}
	nested := `{"chat": {"tools": {"global": {"autoApprove": true}}}}`
	if f := discoverWith(t, ".cursor/settings.json", nested, models.ComponentEditorSettings); len(f.EditorAutoApprove) != 1 {
		t.Errorf("nested key: want 1 hit, got %+v", f.EditorAutoApprove)
	}
}

func TestProjectConfig_LocalMCPLaunch(t *testing.T) {
	srv := func(body string) string { return `{"mcpServers":{"s":` + body + `}}` }
	fire := []string{
		`{"command":"bash","args":["-c","run"]}`,
		`{"command":"sh","args":["./scripts/start.sh"]}`,
		`{"command":"python","args":["-c","import x"]}`,
		`{"command":"python","args":["server.py"]}`,
		`{"command":"node","args":["./mcp/server.js"]}`,
		`{"command":"./bin/server"}`,
		`{"command":"scripts/run.sh"}`,
	}
	for _, body := range fire {
		if f := discoverWith(t, ".mcp.json", srv(body), models.ComponentMCPConfig); len(f.LocalMCPLaunch) == 0 {
			t.Errorf("%s: want a hit", body)
		}
	}
	silent := []string{
		`{"command":"npx","args":["-y","@scope/pkg@1.2.3"]}`,
		`{"command":"uvx","args":["pkg==1.0.0"]}`,
		`{"url":"https://mcp.example.invalid/sse","headers":{"Authorization":"Bearer ${TOKEN}"}}`,
		`{"command":"python","args":["-m","pkg.server"]}`,
		`{"command":"node","args":["/opt/tool/server.js"]}`,
		`{"command":"docker","args":["run","img"]}`,
	}
	for _, body := range silent {
		if f := discoverWith(t, ".mcp.json", srv(body), models.ComponentMCPConfig); len(f.LocalMCPLaunch) != 0 {
			t.Errorf("%s: want silent, got %+v", body, f.LocalMCPLaunch)
		}
	}
	// VS Code uses "servers"; Codex uses TOML [mcp_servers.*].
	vs := `{"servers":{"s":{"command":"bash","args":["-c","x"]}}}`
	if f := discoverWith(t, ".vscode/mcp.json", vs, models.ComponentMCPConfig); len(f.LocalMCPLaunch) == 0 {
		t.Error(".vscode/mcp.json servers: want a hit")
	}
	toml := "[mcp_servers.s]\ncommand = \"node\"\nargs = [\"./srv.js\"]\n"
	if f := discoverWith(t, ".codex/config.toml", toml, models.ComponentCodexConfig); len(f.LocalMCPLaunch) == 0 {
		t.Error("codex mcp_servers: want a hit")
	}
}

func TestProjectConfig_UnpinnedMCPLaunch(t *testing.T) {
	srv := func(body string) string { return `{"mcpServers":{"s":` + body + `}}` }
	digest := "sha256:" + strings.Repeat("0", 64)
	fire := []string{
		`{"command":"npx","args":["-y","@example/mcp-fs","/tmp"]}`,
		`{"command":"npx","args":["-y","pkg@latest"]}`,
		`{"command":"npx","args":["pkg@next"]}`,
		`{"command":"npx","args":["pkg@^1.2.0"]}`,
		`{"command":"npx","args":["pkg@~1.2"]}`,
		`{"command":"npx","args":["pkg@1"]}`,
		`{"command":"npx","args":["--package=@example/pkg","-c","srv"]}`,
		`{"command":"npx","args":["--package","@example/pkg","-c","srv"]}`,
		`{"command":"npx","args":["--registry","https://r.example.invalid","-y","pkg"]}`,
		`{"command":"C:\\tools\\npx.exe","args":["-y","pkg"]}`,
		`{"command":"bunx","args":["pkg"]}`,
		`{"command":"pnpm","args":["dlx","pkg@latest"]}`,
		`{"command":"uvx","args":["example-git"]}`,
		`{"command":"uvx","args":["example-git@latest"]}`,
		`{"command":"uvx","args":["--from","example-git>=0.6","example-git"]}`,
		`{"command":"uvx","args":["--python","3.12","example-git"]}`,
		`{"command":"uvx","args":["--from","example-git==0.*","example-git"]}`,
		`{"command":"uv","args":["tool","run","example-fetch"]}`,
		`{"command":"pipx","args":["run","example-time"]}`,
		`{"command":"pipx","args":["run","--spec","pkg>=1","pkg"]}`,
		`{"command":"docker","args":["run","-i","--rm","ghcr.io/example/srv"]}`,
		`{"command":"docker","args":["run","-i","--rm","img:latest"]}`,
		`{"command":"docker","args":["run","-i","--rm","-e","TOKEN","img:1.2.3"]}`,
	}
	for _, body := range fire {
		f := discoverWith(t, ".mcp.json", srv(body), models.ComponentMCPConfig)
		if len(f.UnpinnedMCPLaunch) != 1 {
			t.Errorf("%s: want one unpinned hit, got %+v", body, f.UnpinnedMCPLaunch)
		}
		if len(f.LocalMCPLaunch) != 0 {
			t.Errorf("%s: MCPCFG-002 must not also fire, got %+v", body, f.LocalMCPLaunch)
		}
	}
	silent := []string{
		`{"command":"npx","args":["-y","@example/pkg@1.2.3"]}`,
		`{"command":"npx","args":["-y","pkg@1.2.3-beta.1"]}`,
		`{"command":"npx","args":["--package=@example/pkg@1.2.3","-c","srv"]}`,
		`{"command":"bunx","args":["pkg@1.2.3"]}`,
		`{"command":"pnpm","args":["dlx","pkg@1.2.3"]}`,
		`{"command":"uvx","args":["example-git@0.6.2"]}`,
		`{"command":"uvx","args":["--from","example-git==0.6.2","example-git"]}`,
		`{"command":"uvx","args":["example-git==0.6.2"]}`,
		`{"command":"pipx","args":["run","--spec","pkg==1.0.0","pkg"]}`,
		`{"command":"docker","args":["run","-i","--rm","img@` + digest + `"]}`,
		`{"command":"docker","args":["run","-i","img:1.2@` + digest + `"]}`,
		`{"url":"https://mcp.example.invalid/sse"}`,
		`{"command":"npx","args":["-y","./local-srv"]}`,
		`{"command":"npx","args":["github:user/repo"]}`,
		`{"command":"npx","args":["user/repo"]}`,
		`{"command":"npx","args":["pkg@github:user/repo"]}`,
		`{"command":"npx","args":["-y","${PKG}"]}`,
		`{"command":"uvx","args":["--from","git+https://example.invalid/r.git","srv"]}`,
		`{"command":"uvx","args":["./local-tool"]}`,
		`{"command":"docker","args":["run","-i","--rm","${IMAGE}"]}`,
		`{"command":"npx"}`,
		`{"command":"npx","args":["-c","echo hi"]}`,
		`{"command":"node","args":["/opt/srv.js"]}`,
		`{"command":"python","args":["-m","pkg.server"]}`,
		`{"command":"docker","args":["compose","up"]}`,
		`{"command":"pnpm","args":["--silent","dlx","pkg"]}`, // documented limit: a flag before the subcommand
	}
	for _, body := range silent {
		if f := discoverWith(t, ".mcp.json", srv(body), models.ComponentMCPConfig); len(f.UnpinnedMCPLaunch) != 0 {
			t.Errorf("%s: want silent, got %+v", body, f.UnpinnedMCPLaunch)
		}
	}

	// A command inside the repo is MCPCFG-002's, never MCPCFG-003's.
	f := discoverWith(t, ".mcp.json", srv(`{"command":"./node_modules/.bin/npx","args":["-y","pkg"]}`), models.ComponentMCPConfig)
	if len(f.LocalMCPLaunch) == 0 || len(f.UnpinnedMCPLaunch) != 0 {
		t.Errorf("repo-relative npx: want MCPCFG-002 only, got local=%+v unpinned=%+v", f.LocalMCPLaunch, f.UnpinnedMCPLaunch)
	}

	// The same classifier runs over every parsed config shape.
	vs := `{"servers":{"s":{"command":"bunx","args":["pkg"]}}}`
	if f := discoverWith(t, ".vscode/mcp.json", vs, models.ComponentMCPConfig); len(f.UnpinnedMCPLaunch) == 0 {
		t.Error(".vscode/mcp.json servers: want a hit")
	}
	gem := `{"mcpServers":{"s":{"command":"npx","args":["pkg"]}}}`
	if f := discoverWith(t, ".gemini/settings.json", gem, models.ComponentGeminiSettings); len(f.UnpinnedMCPLaunch) == 0 {
		t.Error(".gemini/settings.json mcpServers: want a hit")
	}
	toml := "[mcp_servers.s]\ncommand = \"uvx\"\nargs = [\"pkg\"]\n"
	if f := discoverWith(t, ".codex/config.toml", toml, models.ComponentCodexConfig); len(f.UnpinnedMCPLaunch) == 0 {
		t.Error("codex mcp_servers: want a hit")
	}

	// The reason names the server and a fixed phrase, never the package spec.
	f = discoverWith(t, ".mcp.json", srv(`{"command":"npx","args":["-y","@example/secret-name"]}`), models.ComponentMCPConfig)
	if len(f.UnpinnedMCPLaunch) != 1 || strings.Contains(f.UnpinnedMCPLaunch[0].Reason, "secret-name") {
		t.Errorf("reason must not echo the spec: %+v", f.UnpinnedMCPLaunch)
	}
}

func TestProjectConfig_Deterministic(t *testing.T) {
	body := `{"hooks":{"A":[{"command":"./a.sh"}],"B":[{"command":"./b.sh"}],"C":[{"command":"curl x"}]}}`
	first, _ := json.Marshal(discoverWith(t, ".claude/settings.json", body, models.ComponentClaudeSettings))
	for i := 0; i < 20; i++ {
		got, _ := json.Marshal(discoverWith(t, ".claude/settings.json", body, models.ComponentClaudeSettings))
		if string(got) != string(first) {
			t.Fatalf("run %d differs:\n%s\n%s", i, got, first)
		}
	}
}

func TestProjectConfig_UserContentFetchAllow(t *testing.T) {
	settings := func(allow, deny, ask string) string {
		return `{"permissions": {"allow": [` + allow + `], "deny": [` + deny + `], "ask": [` + ask + `]}}`
	}
	cases := []struct {
		name, body string
		want       int
	}{
		{"github.com", settings(`"WebFetch(domain:github.com)"`, "", ""), 1},
		{"gist", settings(`"WebFetch(domain:gist.github.com)"`, "", ""), 1},
		{"raw", settings(`"WebFetch(domain:raw.githubusercontent.com)"`, "", ""), 1},
		{"github.io wildcard", settings(`"WebFetch(domain:*.github.io)"`, "", ""), 1},
		{"huggingface", settings(`"WebFetch(domain:huggingface.co)"`, "", ""), 1},
		{"bitbucket", settings(`"WebFetch(domain:bitbucket.org)"`, "", ""), 1},
		{"pastebin", settings(`"WebFetch(domain:pastebin.com)"`, "", ""), 1},
		{"readthedocs wildcard", settings(`"WebFetch(domain:*.readthedocs.io)"`, "", ""), 1},
		{"case and trailing dot", settings(`"WebFetch(domain:GitHub.com.)"`, "", ""), 1},
		{"subdomain wildcard over githubusercontent", settings(`"WebFetch(domain:*.githubusercontent.com)"`, "", ""), 1},
		{"subdomain wildcard over github.com", settings(`"WebFetch(domain:*.github.com)"`, "", ""), 1},
		{"trailing wildcard", settings(`"WebFetch(domain:github.*)"`, "", ""), 1},
		{"domain star", settings(`"WebFetch(domain:*)"`, "", ""), 1},
		{"bare WebFetch", settings(`"WebFetch"`, "", ""), 1},
		{"domain star, deny covers one host", settings(`"WebFetch(domain:*)"`, `"WebFetch(domain:github.com)"`, ""), 1},
		{"two entries", settings(`"WebFetch(domain:github.com)", "WebFetch(domain:pastebin.com)"`, "", ""), 2},

		{"non-listed host", settings(`"WebFetch(domain:docs.example.invalid)"`, "", ""), 0},
		{"single account site", settings(`"WebFetch(domain:alice.github.io)"`, "", ""), 0},
		{"wildcard excludes apex", settings(`"WebFetch(domain:*.huggingface.co)"`, "", ""), 0},
		{"wildcard stays inside a label", settings(`"WebFetch(domain:example.*)"`, "", ""), 0},
		{"empty parens", settings(`"WebFetch()"`, "", ""), 0},
		{"url specifier", settings(`"WebFetch(url:https://github.com/x)"`, "", ""), 0},
		{"WebSearch", settings(`"WebSearch"`, "", ""), 0},
		{"Bash curl", settings(`"Bash(curl https://pastebin.com/x)"`, "", ""), 0},
		{"allow and deny same entry", settings(`"WebFetch(domain:github.com)"`, `"WebFetch(domain:github.com)"`, ""), 0},
		{"ask covers by wildcard", settings(`"WebFetch(domain:gist.github.com)"`, "", `"WebFetch(domain:*.github.com)"`), 0},
		{"bare deny cancels domain star", settings(`"WebFetch(domain:*)"`, `"WebFetch"`, ""), 0},
		{"allow list under another key", `{"other": {"allow": ["WebFetch"]}}`, 0},
		{"malformed json", `{"permissions": {"allow": ["WebFetch(domain:github.com)",]}}`, 0},
	}
	for _, c := range cases {
		f := discoverWith(t, ".claude/settings.json", c.body, models.ComponentClaudeSettings)
		if got := len(f.UserContentFetchAllow); got != c.want {
			t.Errorf("%s: got %d hits, want %d (%+v)", c.name, got, c.want, f.UserContentFetchAllow)
		}
	}
}

func TestProjectConfig_UserContentFetchAllowCancelScope(t *testing.T) {
	kinds := func(files map[string]string) map[string]models.ComponentKind {
		k := map[string]models.ComponentKind{}
		for rel := range files {
			k[rel] = models.ComponentClaudeSettings
		}
		return k
	}
	run := func(files map[string]string) models.ProjectConfigFacts {
		m := projectConfigFixture(t, files, kinds(files))
		return DiscoverProjectConfig(m, DiscoverClaudeSettings(m))
	}

	// A deny in settings.local.json cancels an allow in settings.json: same folder.
	same := run(map[string]string{
		".claude/settings.json":       `{"permissions": {"allow": ["WebFetch(domain:github.com)"]}}`,
		".claude/settings.local.json": `{"permissions": {"deny": ["WebFetch(domain:github.com)"]}}`,
	})
	if len(same.UserContentFetchAllow) != 0 {
		t.Errorf("same-folder deny did not cancel the allow: %+v", same.UserContentFetchAllow)
	}

	// A deny in a nested .claude/ folder does not cancel the root allow.
	nested := run(map[string]string{
		".claude/settings.json":     `{"permissions": {"allow": ["WebFetch(domain:github.com)"]}}`,
		"pkg/.claude/settings.json": `{"permissions": {"deny": ["WebFetch(domain:github.com)"]}}`,
	})
	if len(nested.UserContentFetchAllow) != 1 || nested.UserContentFetchAllow[0].FilePath != ".claude/settings.json" {
		t.Errorf("nested deny must not cancel the root allow: %+v", nested.UserContentFetchAllow)
	}
}

func TestProjectConfig_UserContentFetchAllowLine(t *testing.T) {
	body := "{\n  \"permissions\": {\n    \"allow\": [\n      \"Read\",\n      \"WebFetch(domain:huggingface.co)\"\n    ]\n  }\n}\n"
	f := discoverWith(t, ".claude/settings.json", body, models.ComponentClaudeSettings)
	if len(f.UserContentFetchAllow) != 1 || f.UserContentFetchAllow[0].Line != 5 {
		t.Fatalf("want one hit on line 5, got %+v", f.UserContentFetchAllow)
	}
	if !strings.Contains(f.UserContentFetchAllow[0].Reason, "huggingface.co") {
		t.Errorf("reason should name the host: %q", f.UserContentFetchAllow[0].Reason)
	}
}

// scopeSrv wraps one server body in an mcpServers document.
func scopeSrv(body string) string { return `{"mcpServers":{"s":` + body + `}}` }

func TestProjectConfig_UnscopedMCPCredential_GitHub(t *testing.T) {
	tok := `"env":{"GITHUB_PERSONAL_ACCESS_TOKEN":"${GITHUB_PAT}"`
	dockerArgs := func(extra ...string) string {
		a := `"run","-i","--rm","-e","GITHUB_PERSONAL_ACCESS_TOKEN"`
		for _, e := range extra {
			a += `,` + e
		}
		return a + `,"ghcr.io/github/github-mcp-server"`
	}
	fire := []string{
		`{"command":"github-mcp-server","args":["stdio"],` + tok + `}}`,
		`{"command":"/usr/local/bin/github-mcp-server.exe","args":["stdio"],` + tok + `}}`,
		`{"command":"docker","args":[` + dockerArgs() + `],` + tok + `}}`,
		`{"command":"docker","args":[` + dockerArgs() + `,"stdio"],` + tok + `}}`,
		`{"command":"docker","args":["run","-i","--rm","-e","GITHUB_PERSONAL_ACCESS_TOKEN=${PAT}","ghcr.io/github/github-mcp-server:v1.0.0"]}`,
		// a falsy GITHUB_READ_ONLY is not read-only; the server parses it with ParseBool
		`{"command":"github-mcp-server","args":["stdio"],` + tok + `,"GITHUB_READ_ONLY":"0"}}`,
		`{"command":"github-mcp-server","args":["stdio"],` + tok + `,"GITHUB_READ_ONLY":"yes"}}`,
		`{"command":"github-mcp-server","args":["stdio"],` + tok + `,"GITHUB_READ_ONLY":"on"}}`,
		`{"command":"github-mcp-server","args":["stdio","--read-only=false"],` + tok + `}}`,
		// toolsets that widen
		`{"command":"github-mcp-server","args":["stdio","--toolsets=all"],` + tok + `}}`,
		`{"command":"github-mcp-server","args":["stdio","--toolsets","default"],` + tok + `}}`,
		`{"command":"github-mcp-server","args":["stdio"],` + tok + `,"GITHUB_TOOLSETS":"default,repos"}}`,
		// --tools is additive once a toolset list widens
		`{"command":"github-mcp-server","args":["stdio","--toolsets=all","--tools=get_me"],` + tok + `}}`,
		// docker's own --read-only (before the image) is not the server's
		`{"command":"docker","args":[` + dockerArgs(`"--read-only"`) + `],` + tok + `}}`,
	}
	for _, body := range fire {
		f := discoverWith(t, ".mcp.json", scopeSrv(body), models.ComponentMCPConfig)
		if len(f.UnscopedMCPCredential) != 1 {
			t.Errorf("%s: want one unscoped hit, got %+v", body, f.UnscopedMCPCredential)
		}
		if len(f.LocalMCPLaunch) != 0 {
			t.Errorf("%s: MCPCFG-002 must not also fire", body)
		}
	}
	silent := []string{
		`{"command":"github-mcp-server","args":["stdio","--read-only"],` + tok + `}}`,
		`{"command":"github-mcp-server","args":["stdio","--read_only"],` + tok + `}}`,
		`{"command":"github-mcp-server","args":["stdio","--read-only=true"],` + tok + `}}`,
		`{"command":"github-mcp-server","args":["stdio"],` + tok + `,"GITHUB_READ_ONLY":"1"}}`,
		`{"command":"github-mcp-server","args":["stdio"],` + tok + `,"GITHUB_READ_ONLY":"true"}}`,
		`{"command":"github-mcp-server","args":["stdio"],` + tok + `,"GITHUB_READ_ONLY":"${RO}"}}`,
		`{"command":"docker","args":[` + dockerArgs(`"-e"`, `"GITHUB_READ_ONLY=1"`) + `],` + tok + `}}`,
		`{"command":"docker","args":[` + dockerArgs() + `,"stdio","--read-only"],` + tok + `}}`,
		// sheet case: toolsets and read-only together
		`{"command":"github-mcp-server","args":["stdio","--read-only"],` + tok + `,"GITHUB_TOOLSETS":"repos"}}`,
		`{"command":"github-mcp-server","args":["stdio","--toolsets=repos,issues"],` + tok + `}}`,
		`{"command":"github-mcp-server","args":["stdio"],` + tok + `,"GITHUB_TOOLSETS":"repos"}}`,
		`{"command":"github-mcp-server","args":["stdio"],` + tok + `,"GITHUB_TOOLSETS":"${TS}"}}`,
		`{"command":"github-mcp-server","args":["stdio","--tools=get_me,list_issues"],` + tok + `}}`,
		`{"command":"github-mcp-server","args":["stdio"],` + tok + `,"GITHUB_TOOLS":"get_me"}}`,
		// a flag that is read-only wins over a falsy env var
		`{"command":"github-mcp-server","args":["stdio","--read-only"],` + tok + `,"GITHUB_READ_ONLY":"0"}}`,
		// no token: OAuth login, nothing to scope
		`{"command":"github-mcp-server","args":["stdio"]}`,
		// GITHUB_TOKEN is not a variable the server documents
		`{"command":"github-mcp-server","args":["stdio"],"env":{"GITHUB_TOKEN":"${T}"}}`,
		// a command inside the repo is MCPCFG-002's
		`{"command":"./github-mcp-server","args":["stdio"],` + tok + `}}`,
		// a remote URL is out of scope
		`{"url":"https://api.githubcopilot.com/mcp/","headers":{"Authorization":"Bearer ${PAT}"}}`,
		`{"command":"npx","args":["-y","@example/other-server"],` + tok + `}}`,
	}
	for _, body := range silent {
		if f := discoverWith(t, ".mcp.json", scopeSrv(body), models.ComponentMCPConfig); len(f.UnscopedMCPCredential) != 0 {
			t.Errorf("%s: want silent, got %+v", body, f.UnscopedMCPCredential)
		}
	}
}

func TestProjectConfig_UnscopedMCPCredential_OldGitHubPackage(t *testing.T) {
	tok := `"env":{"GITHUB_PERSONAL_ACCESS_TOKEN":"${GITHUB_PAT}"}`
	for _, body := range []string{
		`{"command":"npx","args":["-y","@modelcontextprotocol/server-github"],` + tok + `}`,
		`{"command":"npx","args":["-y","@modelcontextprotocol/server-github@2025.4.8"],` + tok + `}`,
		`{"command":"bunx","args":["@modelcontextprotocol/server-github"],` + tok + `}`,
		`{"command":"pnpm","args":["dlx","@modelcontextprotocol/server-github"],` + tok + `}`,
	} {
		f := discoverWith(t, ".mcp.json", scopeSrv(body), models.ComponentMCPConfig)
		if len(f.UnscopedMCPCredential) != 1 {
			t.Errorf("%s: want one hit, got %+v", body, f.UnscopedMCPCredential)
		}
	}
	for _, body := range []string{
		`{"command":"npx","args":["-y","@modelcontextprotocol/server-github"]}`,
		`{"command":"npx","args":["-y","@modelcontextprotocol/server-github-extra"],` + tok + `}`,
		`{"command":"npx","args":["-y","@modelcontextprotocol/server-filesystem"],` + tok + `}`,
	} {
		if f := discoverWith(t, ".mcp.json", scopeSrv(body), models.ComponentMCPConfig); len(f.UnscopedMCPCredential) != 0 {
			t.Errorf("%s: want silent, got %+v", body, f.UnscopedMCPCredential)
		}
	}
}

func TestProjectConfig_UnscopedMCPCredential_Supabase(t *testing.T) {
	fire := []string{
		// the sheet's fail case
		`{"command":"npx","args":["-y","@supabase/mcp-server-supabase","--access-token","$TOKEN"]}`,
		`{"command":"npx","args":["-y","@supabase/mcp-server-supabase@latest"]}`,
		`{"command":"npx","args":["-y","@supabase/mcp-server-supabase@0.14.0"],"env":{"SUPABASE_ACCESS_TOKEN":"${T}"}}`,
		`{"command":"bunx","args":["@supabase/mcp-server-supabase"]}`,
		`{"command":"mcp-server-supabase","args":[]}`,
		// --read-only=true is not a form the CLI accepts
		`{"command":"npx","args":["-y","@supabase/mcp-server-supabase","--read-only=true"]}`,
		`{"command":"npx","args":["-y","@supabase/mcp-server-supabase","--features=database"]}`,
	}
	for _, body := range fire {
		f := discoverWith(t, ".mcp.json", scopeSrv(body), models.ComponentMCPConfig)
		if len(f.UnscopedMCPCredential) != 1 {
			t.Errorf("%s: want one hit, got %+v", body, f.UnscopedMCPCredential)
		}
	}
	silent := []string{
		// the sheet's pass case
		`{"command":"npx","args":["-y","@supabase/mcp-server-supabase","--read-only","--project-ref=abc"]}`,
		`{"command":"npx","args":["-y","@supabase/mcp-server-supabase","--read-only"]}`,
		`{"command":"npx","args":["-y","@supabase/mcp-server-supabase","--project-ref=abc"]}`,
		`{"command":"npx","args":["-y","@supabase/mcp-server-supabase","--project-ref","abc"]}`,
		`{"command":"mcp-server-supabase","args":["--read-only"]}`,
		`{"command":"npx","args":["-y","@supabase/mcp-server-supabase-extra"]}`,
		`{"command":"npx","args":["-y","@example/other"]}`,
		`{"url":"https://mcp.supabase.com/mcp"}`,
		`{"command":"./mcp-server-supabase","args":[]}`,
	}
	for _, body := range silent {
		if f := discoverWith(t, ".mcp.json", scopeSrv(body), models.ComponentMCPConfig); len(f.UnscopedMCPCredential) != 0 {
			t.Errorf("%s: want silent, got %+v", body, f.UnscopedMCPCredential)
		}
	}
}

func TestProjectConfig_UnscopedMCPCredential_ServiceRole(t *testing.T) {
	for _, body := range []string{
		`{"command":"node","args":["/opt/s.js"],"env":{"SUPABASE_SERVICE_ROLE_KEY":"${K}"}}`,
		`{"command":"npx","args":["-y","pkg@1.2.3"],"env":{"service_role_key":"${K}"}}`,
		`{"url":"https://mcp.example.invalid/mcp","env":{"SERVICE_ROLE":"${K}"}}`,
		`{"command":"docker","args":["run","-i","-e","SUPABASE_SERVICE_ROLE_KEY","img@sha256:` + strings.Repeat("0", 64) + `"]}`,
	} {
		f := discoverWith(t, ".mcp.json", scopeSrv(body), models.ComponentMCPConfig)
		if len(f.UnscopedMCPCredential) != 1 {
			t.Errorf("%s: want one hit, got %+v", body, f.UnscopedMCPCredential)
		}
	}
	// The dropped name patterns stay silent.
	for _, key := range []string{"PROJECT_ROOT", "ROOT_DIR", "ADMIN_EMAIL", "DB_SUPERUSER", "ADMIN_TOKEN", "SUPABASE_ANON_KEY"} {
		body := `{"command":"node","args":["/opt/s.js"],"env":{"` + key + `":"${K}"}}`
		if f := discoverWith(t, ".mcp.json", scopeSrv(body), models.ComponentMCPConfig); len(f.UnscopedMCPCredential) != 0 {
			t.Errorf("%s: want silent, got %+v", key, f.UnscopedMCPCredential)
		}
	}
	// A command inside the repo is MCPCFG-002's.
	f := discoverWith(t, ".mcp.json", scopeSrv(`{"command":"./srv.sh","env":{"SUPABASE_SERVICE_ROLE_KEY":"${K}"}}`), models.ComponentMCPConfig)
	if len(f.UnscopedMCPCredential) != 0 || len(f.LocalMCPLaunch) == 0 {
		t.Errorf("repo-relative command: want MCPCFG-002 only, got scope=%+v local=%+v", f.UnscopedMCPCredential, f.LocalMCPLaunch)
	}
}

// The same classifier runs over every parsed config shape.
func TestProjectConfig_UnscopedMCPCredential_EveryClientFile(t *testing.T) {
	tok := `"env":{"GITHUB_PERSONAL_ACCESS_TOKEN":"${PAT}"}`
	gh := `"command":"github-mcp-server","args":["stdio"],` + tok
	cases := []struct {
		rel, body string
		kind      models.ComponentKind
	}{
		{".mcp.json", `{"mcpServers":{"s":{` + gh + `}}}`, models.ComponentMCPConfig},
		{".cursor/mcp.json", `{"mcpServers":{"s":{` + gh + `}}}`, models.ComponentMCPConfig},
		{".vscode/mcp.json", `{"servers":{"s":{` + gh + `}}}`, models.ComponentMCPConfig},
		{"claude_desktop_config.json", `{"mcpServers":{"s":{` + gh + `}}}`, models.ComponentMCPConfig},
		{".gemini/settings.json", `{"mcpServers":{"s":{` + gh + `}}}`, models.ComponentGeminiSettings},
		{".codex/config.toml", "[mcp_servers.s]\ncommand = \"github-mcp-server\"\nargs = [\"stdio\"]\n[mcp_servers.s.env]\nGITHUB_PERSONAL_ACCESS_TOKEN = \"${PAT}\"\n", models.ComponentCodexConfig},
	}
	for _, c := range cases {
		f := discoverWith(t, c.rel, c.body, c.kind)
		if len(f.UnscopedMCPCredential) != 1 {
			t.Errorf("%s: want one hit, got %+v", c.rel, f.UnscopedMCPCredential)
		}
	}
	// Read-only through the Codex env table is credited too.
	toml := "[mcp_servers.s]\ncommand = \"github-mcp-server\"\nargs = [\"stdio\"]\n[mcp_servers.s.env]\nGITHUB_PERSONAL_ACCESS_TOKEN = \"${PAT}\"\nGITHUB_READ_ONLY = \"1\"\n"
	if f := discoverWith(t, ".codex/config.toml", toml, models.ComponentCodexConfig); len(f.UnscopedMCPCredential) != 0 {
		t.Errorf("codex read-only: want silent, got %+v", f.UnscopedMCPCredential)
	}
}

// Malformed config never panics and never hides the other rules' hits.
func TestProjectConfig_UnscopedMCPCredential_Malformed(t *testing.T) {
	for _, c := range []struct {
		rel, body string
		kind      models.ComponentKind
	}{
		{".mcp.json", `{"mcpServers": {`, models.ComponentMCPConfig},
		{".codex/config.toml", "[mcp_servers.s\ncommand = ", models.ComponentCodexConfig},
	} {
		f := discoverWith(t, c.rel, c.body, c.kind)
		if len(f.UnscopedMCPCredential) != 0 || len(f.PlainHTTPMCPServer) != 0 {
			t.Errorf("%s: malformed config must give no hit, got %+v", c.rel, f)
		}
	}
	// An env that is an array or holds a number does not fail the document.
	for _, env := range []string{`["A","B"]`, `{"N":5,"GITHUB_PERSONAL_ACCESS_TOKEN":5}`, `"x"`} {
		body := `{"mcpServers":{"a":{"command":"npx","args":["pkg"],"env":` + env + `},"b":{"command":"bash","args":["-c","x"]}}}`
		f := discoverWith(t, ".mcp.json", body, models.ComponentMCPConfig)
		if len(f.UnpinnedMCPLaunch) != 1 || len(f.LocalMCPLaunch) != 1 {
			t.Errorf("env %s: MCPCFG-002/003 hits lost: unpinned=%+v local=%+v", env, f.UnpinnedMCPLaunch, f.LocalMCPLaunch)
		}
	}
}

// A planted fake token never reaches a fact, a Reason or the marshalled output.
func TestProjectConfig_UnscopedMCPCredential_NoValueLeaks(t *testing.T) {
	const fake = "fake-value-not-real-9f3a"
	body := `{"mcpServers":{
	  "gh":{"command":"github-mcp-server","args":["stdio","--note=` + fake + `"],"env":{"GITHUB_PERSONAL_ACCESS_TOKEN":"` + fake + `","GITHUB_READ_ONLY":"` + fake + `","GITHUB_TOOLSETS":"` + fake + `"}},
	  "gh2":{"command":"github-mcp-server","args":["stdio"],"env":{"GITHUB_PERSONAL_ACCESS_TOKEN":"` + fake + `","GITHUB_READ_ONLY":"0"}},
	  "sb":{"command":"npx","args":["-y","@supabase/mcp-server-supabase","--access-token","` + fake + `"]},
	  "sr":{"command":"node","args":["/opt/s.js"],"env":{"SUPABASE_SERVICE_ROLE_KEY":"` + fake + `"}},
	  "dk":{"command":"docker","args":["run","-e","GITHUB_PERSONAL_ACCESS_TOKEN=` + fake + `","ghcr.io/github/github-mcp-server"]},
	  "rm":{"url":"http://mcp.example.invalid/mcp?token=` + fake + `","headers":{"Authorization":"Bearer ` + fake + `"}}
	}}`
	f := discoverWith(t, ".mcp.json", body, models.ComponentMCPConfig)
	if len(f.UnscopedMCPCredential) == 0 || len(f.PlainHTTPMCPServer) == 0 {
		t.Fatalf("fixture should produce hits: %+v", f)
	}
	out, err := json.Marshal(f)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(out), fake) {
		t.Errorf("a planted value reached the facts: %s", out)
	}
}

func TestProjectConfig_PlainHTTPMCPServer(t *testing.T) {
	fire := []string{
		`{"url":"http://mcp.example.invalid/mcp"}`,
		`{"url":"HTTP://MCP.EXAMPLE.INVALID/mcp"}`,
		`{"type":"http","url":"http://mcp.example.invalid/mcp","headers":{"Authorization":"Bearer ${T}"}}`,
		`{"httpUrl":"http://mcp.example.invalid/mcp"}`,
		`{"url":"http://10.0.0.5/mcp"}`,
		`{"url":"http://192.168.1.2:8080/mcp"}`,
		`{"url":"http://host.docker.internal:8000/mcp"}`,
		`{"url":"http://box.local/mcp"}`,
		`{"url":"http://localhost.evil.invalid/mcp"}`,
		`{"url":"http://127.0.0.1.evil.invalid/mcp"}`,
		`{"url":"http://localhost@mcp.example.invalid/mcp"}`,
		`{"command":"npx","args":["pkg@1.2.3"],"url":"http://mcp.example.invalid/mcp"}`,
	}
	for _, body := range fire {
		f := discoverWith(t, ".mcp.json", scopeSrv(body), models.ComponentMCPConfig)
		if len(f.PlainHTTPMCPServer) != 1 {
			t.Errorf("%s: want one hit, got %+v", body, f.PlainHTTPMCPServer)
		}
		if len(f.LocalMCPLaunch) != 0 || len(f.UnpinnedMCPLaunch) != 0 {
			t.Errorf("%s: MCPCFG-002/003 must not fire on a url entry", body)
		}
	}
	silent := []string{
		`{"url":"http://localhost:3000/mcp"}`,
		`{"url":"http://LOCALHOST/mcp"}`,
		`{"url":"http://localhost./mcp"}`,
		`{"url":"http://127.0.0.1:8080/mcp"}`,
		`{"url":"http://127.0.0.2/mcp"}`,
		`{"url":"http://[::1]:8080/mcp"}`,
		`{"url":"http://user@localhost/mcp"}`,
		`{"url":"https://mcp.example.invalid/mcp"}`,
		`{"url":"https://mcp.example.invalid/mcp","headers":{"Authorization":"Bearer ${T}"}}`,
		`{"url":"http://${HOST}/mcp"}`,
		`{"url":"${MCP_URL}"}`,
		`{"url":"ws://mcp.example.invalid/mcp"}`,
		`{"url":""}`,
		`{"command":"npx","args":["pkg@1.2.3"]}`,
		`{"url":"not a url"}`,
	}
	for _, body := range silent {
		if f := discoverWith(t, ".mcp.json", scopeSrv(body), models.ComponentMCPConfig); len(f.PlainHTTPMCPServer) != 0 {
			t.Errorf("%s: want silent, got %+v", body, f.PlainHTTPMCPServer)
		}
	}

	// Every parsed config shape.
	if f := discoverWith(t, ".vscode/mcp.json", `{"servers":{"s":{"type":"http","url":"http://mcp.example.invalid/mcp"}}}`, models.ComponentMCPConfig); len(f.PlainHTTPMCPServer) != 1 {
		t.Error(".vscode/mcp.json servers: want a hit")
	}
	if f := discoverWith(t, ".cursor/mcp.json", scopeSrv(`{"url":"http://mcp.example.invalid/mcp"}`), models.ComponentMCPConfig); len(f.PlainHTTPMCPServer) != 1 {
		t.Error(".cursor/mcp.json: want a hit")
	}
	if f := discoverWith(t, "claude_desktop_config.json", scopeSrv(`{"url":"http://mcp.example.invalid/mcp"}`), models.ComponentMCPConfig); len(f.PlainHTTPMCPServer) != 1 {
		t.Error("claude_desktop_config.json: want a hit")
	}
	for _, body := range []string{`{"httpUrl":"http://mcp.example.invalid/mcp"}`, `{"url":"http://mcp.example.invalid/sse"}`} {
		if f := discoverWith(t, ".gemini/settings.json", scopeSrv(body), models.ComponentGeminiSettings); len(f.PlainHTTPMCPServer) != 1 {
			t.Errorf(".gemini/settings.json %s: want a hit", body)
		}
	}
	toml := "[mcp_servers.s]\nurl = \"http://mcp.example.invalid/mcp\"\n"
	if f := discoverWith(t, ".codex/config.toml", toml, models.ComponentCodexConfig); len(f.PlainHTTPMCPServer) != 1 {
		t.Error("codex url: want a hit")
	}
	if f := discoverWith(t, ".codex/config.toml", "[mcp_servers.s]\nurl = \"http://localhost:3000/mcp\"\n", models.ComponentCodexConfig); len(f.PlainHTTPMCPServer) != 0 {
		t.Error("codex localhost: want silent")
	}

	// The reason names the server and a fixed phrase, never the URL.
	f := discoverWith(t, ".mcp.json", scopeSrv(`{"url":"http://secret-host.example.invalid/mcp?k=v"}`), models.ComponentMCPConfig)
	if len(f.PlainHTTPMCPServer) != 1 || strings.Contains(f.PlainHTTPMCPServer[0].Reason, "secret-host") {
		t.Errorf("reason must not echo the URL: %+v", f.PlainHTTPMCPServer)
	}
}
