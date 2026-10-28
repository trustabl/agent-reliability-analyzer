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

func TestProjectConfig_ExecCapableAllow(t *testing.T) {
	settings := func(allow, deny, ask string) string {
		return `{"permissions": {"allow": [` + allow + `], "deny": [` + deny + `], "ask": [` + ask + `]}}`
	}
	cases := []struct {
		name, body string
		want       int
	}{
		{"git colon star", settings(`"Bash(git:*)"`, "", ""), 1},
		{"git space star", settings(`"Bash(git *)"`, "", ""), 1},
		{"xargs colon star", settings(`"Bash(xargs:*)"`, "", ""), 1},
		{"xargs space star", settings(`"Bash(xargs *)"`, "", ""), 1},
		{"npx colon star", settings(`"Bash(npx:*)"`, "", ""), 1},
		{"npx space star", settings(`"Bash(npx *)"`, "", ""), 1},
		{"two entries", settings(`"Bash(git:*)", "Bash(npx *)"`, "", ""), 2},
		{"narrower deny does not cancel", settings(`"Bash(git:*)"`, `"Bash(git push *)"`, ""), 1},
		{"deny for another prefix does not cancel", settings(`"Bash(git:*)"`, `"Bash(npx *)"`, ""), 1},

		{"exact git status", settings(`"Bash(git status)"`, "", ""), 0},
		{"exact git diff stat", settings(`"Bash(git diff --stat)"`, "", ""), 0},
		{"narrower git prefix", settings(`"Bash(git status *)"`, "", ""), 0},
		{"narrower git prefix colon", settings(`"Bash(git diff:*)"`, "", ""), 0},
		{"find is not listed", settings(`"Bash(find:*)"`, "", ""), 0},
		{"exec wrapper is not listed", settings(`"Bash(watch *)"`, "", ""), 0},
		{"unrelated prefix", settings(`"Bash(npm run *)"`, "", ""), 0},
		{"bare Bash is out of scope", settings(`"Bash"`, "", ""), 0},
		{"empty parens is out of scope", settings(`"Bash()"`, "", ""), 0},
		{"Bash star is out of scope", settings(`"Bash(*)"`, "", ""), 0},
		{"no space before star", settings(`"Bash(git*)"`, "", ""), 0},
		{"two words before star", settings(`"Bash(git status:*)"`, "", ""), 0},
		{"other tool", settings(`"Read(git:*)"`, "", ""), 0},
		{"same prefix deny cancels", settings(`"Bash(git:*)"`, `"Bash(git *)"`, ""), 0},
		{"same prefix ask cancels", settings(`"Bash(npx *)"`, "", `"Bash(npx:*)"`), 0},
		{"bare Bash deny cancels", settings(`"Bash(xargs *)"`, `"Bash"`, ""), 0},
		{"Bash star ask cancels", settings(`"Bash(git:*)"`, "", `"Bash(*)"`), 0},
		{"allow list under another key", `{"other": {"allow": ["Bash(git:*)"]}}`, 0},
		{"malformed json", `{"permissions": {"allow": ["Bash(git:*)",]}}`, 0},
	}
	for _, c := range cases {
		f := discoverWith(t, ".claude/settings.json", c.body, models.ComponentClaudeSettings)
		if got := len(f.ExecCapableAllow); got != c.want {
			t.Errorf("%s: got %d hits, want %d (%+v)", c.name, got, c.want, f.ExecCapableAllow)
		}
	}
}

func TestProjectConfig_ExecCapableAllowCancelScope(t *testing.T) {
	run := func(files map[string]string) models.ProjectConfigFacts {
		k := map[string]models.ComponentKind{}
		for rel := range files {
			k[rel] = models.ComponentClaudeSettings
		}
		m := projectConfigFixture(t, files, k)
		return DiscoverProjectConfig(m, DiscoverClaudeSettings(m))
	}

	same := run(map[string]string{
		".claude/settings.json":       `{"permissions": {"allow": ["Bash(git:*)"]}}`,
		".claude/settings.local.json": `{"permissions": {"deny": ["Bash(git *)"]}}`,
	})
	if len(same.ExecCapableAllow) != 0 {
		t.Errorf("same-folder deny did not cancel the allow: %+v", same.ExecCapableAllow)
	}

	nested := run(map[string]string{
		".claude/settings.json":     `{"permissions": {"allow": ["Bash(git:*)"]}}`,
		"pkg/.claude/settings.json": `{"permissions": {"deny": ["Bash(git:*)"]}}`,
	})
	if len(nested.ExecCapableAllow) != 1 || nested.ExecCapableAllow[0].FilePath != ".claude/settings.json" {
		t.Errorf("nested deny must not cancel the root allow: %+v", nested.ExecCapableAllow)
	}
}

func TestProjectConfig_ExecCapableAllowLine(t *testing.T) {
	body := "{\n  \"permissions\": {\n    \"allow\": [\n      \"Read\",\n      \"Bash(xargs *)\"\n    ]\n  }\n}\n"
	f := discoverWith(t, ".claude/settings.json", body, models.ComponentClaudeSettings)
	if len(f.ExecCapableAllow) != 1 || f.ExecCapableAllow[0].Line != 5 {
		t.Fatalf("want one hit on line 5, got %+v", f.ExecCapableAllow)
	}
	if !strings.Contains(f.ExecCapableAllow[0].Reason, "Bash(xargs *)") {
		t.Errorf("reason should name the entry: %q", f.ExecCapableAllow[0].Reason)
	}
}
