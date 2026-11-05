package analysis

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/trustabl/trustabl/internal/models"
)

// scriptFixture writes files under a temp repo and lists them in the manifest
// the way the normalizer does: package.json in JSONFiles, .husky files in
// HuskyFiles, JS in JavaScriptFiles and TS in TypeScriptFiles.
func scriptFixture(t *testing.T, files map[string]string) models.ScanManifest {
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
		switch {
		case strings.HasSuffix(rel, ".json"):
			m.JSONFiles = append(m.JSONFiles, rel)
		case strings.Contains(rel, ".husky/"):
			m.HuskyFiles = append(m.HuskyFiles, rel)
		case strings.HasSuffix(rel, ".ts"):
			m.TypeScriptFiles = append(m.TypeScriptFiles, rel)
		default:
			m.JavaScriptFiles = append(m.JavaScriptFiles, rel)
		}
	}
	return m
}

func pkgScripts(key, val string) string {
	b, _ := json.Marshal(map[string]any{"name": "x", "scripts": map[string]string{key: val}})
	return string(b)
}

func TestScriptAgentBypass_PackageJSON(t *testing.T) {
	cases := []struct {
		name, key, val string
		want           bool
	}{
		{"postinstall claude", "postinstall", "claude -p 'x' --dangerously-skip-permissions", true},
		{"preinstall claude", "preinstall", "claude --dangerously-skip-permissions -p x", true},
		{"install claude", "install", "claude --dangerously-skip-permissions", true},
		{"prepare claude", "prepare", "claude --dangerously-skip-permissions", true},
		{"permission-mode two tokens", "postinstall", "claude --permission-mode bypassPermissions -p x", true},
		{"permission-mode equals", "postinstall", "claude --permission-mode=bypassPermissions -p x", true},
		{"gemini yolo", "postinstall", "gemini --yolo -p x", true},
		{"gemini approval-mode", "postinstall", "gemini --approval-mode yolo -p x", true},
		{"gemini approval-mode equals", "postinstall", "gemini --approval-mode=yolo", true},
		{"codex bypass", "postinstall", "codex exec --dangerously-bypass-approvals-and-sandbox x", true},
		{"codex yolo", "postinstall", "codex --yolo x", true},
		{"aider yes-always", "postinstall", "aider --yes-always --message x", true},
		{"q chat trust-all-tools", "postinstall", "q chat --trust-all-tools --no-interactive x", true},
		{"path prefix", "postinstall", "/usr/local/bin/claude --dangerously-skip-permissions", true},
		{"npx runner", "postinstall", "npx -y @anthropic-ai/claude-code --dangerously-skip-permissions -p x", true},
		{"npx with version", "postinstall", "npx @google/gemini-cli@latest --yolo", true},
		{"env prefix", "postinstall", "FOO=1 claude --dangerously-skip-permissions", true},
		{"after &&", "postinstall", "npm run build && claude --dangerously-skip-permissions", true},
		{"after semicolon", "postinstall", "echo hi; gemini --yolo", true},
		{"double quoted prompt", "postinstall", `claude -p "summarize it" --dangerously-skip-permissions`, true},

		{"claude without a bypass flag", "postinstall", `claude -p "summarize" --allowedTools Read`, false},
		{"allow-dangerously flag is not a bypass", "postinstall", "claude --allow-dangerously-skip-permissions", false},
		{"permission-mode plan", "postinstall", "claude --permission-mode plan", false},
		{"q alone", "postinstall", "q --trust-all-tools", false},
		{"q chat without the flag", "postinstall", "q chat --no-interactive x", false},
		{"flag on an unrelated tool", "postinstall", "rm --force x && apt -y install y", false},
		{"force on a non-agent", "postinstall", "git clean --force", false},
		{"yolo on an unrelated tool", "postinstall", "somecli --yolo", false},
		{"echo of a command", "postinstall", "echo claude --dangerously-skip-permissions", false},
		{"cline is not judged", "postinstall", "cline --auto-approve", false},
		{"agent is not judged", "postinstall", "agent --force", false},
		{"-y on gemini is not judged", "postinstall", "gemini -y", false},
		{"codex full-auto is not judged", "postinstall", "codex --full-auto", false},
		{"named script is not read", "lint", "claude --dangerously-skip-permissions", false},
		{"build script is not read", "build", "gemini --yolo", false},
		{"test script is not read", "test", "claude --dangerously-skip-permissions", false},
	}
	for _, c := range cases {
		m := scriptFixture(t, map[string]string{"package.json": pkgScripts(c.key, c.val)})
		f := DiscoverProjectConfig(m, nil)
		if got := len(f.ScriptAgentBypass) > 0; got != c.want {
			t.Errorf("%s: got %v, want %v (%+v)", c.name, got, c.want, f.ScriptAgentBypass)
		}
	}
}

func TestScriptAgentBypass_PackageJSONShapes(t *testing.T) {
	for name, body := range map[string]string{
		"malformed JSON":      `{"scripts": {"postinstall": "claude --dangerously-skip-permissions"`,
		"scripts not object":  `{"scripts": "claude --dangerously-skip-permissions"}`,
		"scripts array":       `{"scripts": ["claude --dangerously-skip-permissions"]}`,
		"non-string script":   `{"scripts": {"postinstall": 42}}`,
		"null script":         `{"scripts": {"postinstall": null}}`,
		"no scripts":          `{"name": "x"}`,
		"flag in description": `{"description": "claude --dangerously-skip-permissions", "scripts": {}}`,
	} {
		m := scriptFixture(t, map[string]string{"package.json": body})
		if f := DiscoverProjectConfig(m, nil); len(f.ScriptAgentBypass) != 0 {
			t.Errorf("%s: want no hit, got %+v", name, f.ScriptAgentBypass)
		}
	}
	// A nested package.json is read; a README with the same text is not.
	m := scriptFixture(t, map[string]string{
		"pkg/package.json": pkgScripts("postinstall", "claude --dangerously-skip-permissions"),
		"README.md":        "run claude --dangerously-skip-permissions",
	})
	m.MarkdownFiles = []string{"README.md"}
	f := DiscoverProjectConfig(m, nil)
	if len(f.ScriptAgentBypass) != 1 || f.ScriptAgentBypass[0].FilePath != "pkg/package.json" {
		t.Fatalf("want one hit in pkg/package.json, got %+v", f.ScriptAgentBypass)
	}
}

func TestScriptAgentBypass_PackageJSONLine(t *testing.T) {
	body := "{\n  \"scripts\": {\n    \"build\": \"tsc\",\n    \"postinstall\": \"claude --dangerously-skip-permissions\"\n  }\n}\n"
	m := scriptFixture(t, map[string]string{"package.json": body})
	f := DiscoverProjectConfig(m, nil)
	if len(f.ScriptAgentBypass) != 1 || f.ScriptAgentBypass[0].Line != 4 {
		t.Fatalf("want one hit on line 4, got %+v", f.ScriptAgentBypass)
	}
}

func TestScriptAgentBypass_Husky(t *testing.T) {
	cases := []struct {
		name, body string
		want       bool
	}{
		{"hook line", "#!/bin/sh\nclaude -p x --dangerously-skip-permissions\n", true},
		{"hook with husky preamble", ". \"$(dirname -- \"$0\")/_/husky.sh\"\ngemini --yolo -p x\n", true},
		{"indented line", "  codex --dangerously-bypass-approvals-and-sandbox x\n", true},
		{"comment line", "# claude --dangerously-skip-permissions\n", false},
		{"no flag", "npx lint-staged\nclaude -p x\n", false},
		{"echo", "echo claude --yolo\n", false},
	}
	for _, c := range cases {
		m := scriptFixture(t, map[string]string{".husky/pre-commit": c.body})
		f := DiscoverProjectConfig(m, nil)
		if got := len(f.ScriptAgentBypass) > 0; got != c.want {
			t.Errorf("%s: got %v, want %v (%+v)", c.name, got, c.want, f.ScriptAgentBypass)
		}
	}
	m := scriptFixture(t, map[string]string{".husky/pre-commit": "#!/bin/sh\nnpm test\nclaude --dangerously-skip-permissions\n"})
	if f := DiscoverProjectConfig(m, nil); len(f.ScriptAgentBypass) != 1 || f.ScriptAgentBypass[0].Line != 3 {
		t.Fatalf("want one hit on line 3, got %+v", f.ScriptAgentBypass)
	}
}

func TestScriptAgentBypass_Spawn(t *testing.T) {
	cases := []struct {
		name, src string
		want      bool
	}{
		{"spawnSync args array", `spawnSync('claude', ['--dangerously-skip-permissions', '-p', PROMPT]);`, true},
		{"spawn args array", `const p = spawn("claude", ["-p", x, "--dangerously-skip-permissions"]);`, true},
		{"qualified spawn", `child_process.spawn('gemini', ['--yolo', '-p', x]);`, true},
		{"execFileSync", `execFileSync('codex', ['exec', '--dangerously-bypass-approvals-and-sandbox', x]);`, true},
		{"execFile with options", `execFile('claude', ['--dangerously-skip-permissions'], { cwd: dir }, cb);`, true},
		{"execSync command string", "execSync('claude -p x --dangerously-skip-permissions');", true},
		{"exec template string", "exec(`gemini --yolo -p \"${prompt}\"`);", true},
		{"permission-mode pair", `spawnSync('claude', ['--permission-mode', 'bypassPermissions']);`, true},
		{"permission-mode equals", `spawnSync('claude', ['--permission-mode=bypassPermissions']);`, true},
		{"approval-mode pair", `spawn('gemini', ['--approval-mode', 'yolo']);`, true},
		{"q chat args", `spawnSync('q', ['chat', '--trust-all-tools', '--no-interactive', PROMPT]);`, true},
		{"q chat command string", `execSync('q chat --trust-all-tools x');`, true},
		{"aider", `spawn('aider', ['--yes-always']);`, true},
		{"Bun.spawn array", `Bun.spawn(["claude", "--dangerously-skip-permissions", "-p", x]);`, true},
		{"Bun.spawnSync array", `Bun.spawnSync(["gemini", "--yolo"]);`, true},
		{"path prefix", `spawn('/usr/local/bin/claude', ['--dangerously-skip-permissions']);`, true},
		{"TypeScript file", `execSync('claude --dangerously-skip-permissions');`, true},

		{"binary without a flag", `spawnSync('claude', ['-p', PROMPT, '--allowedTools', 'Read']);`, false},
		{"flag on another binary", `spawnSync('rm', ['--force', 'x']);`, false},
		{"yolo on another binary", `spawn('somecli', ['--yolo']);`, false},
		{"binary in a variable", `spawnSync(bin, ['--dangerously-skip-permissions']);`, false},
		{"flag in a variable", `spawnSync('claude', [flag, '-p', x]);`, false},
		{"args in a variable", `spawnSync('claude', args);`, false},
		{"binary nested in a call", `spawnSync(process.execPath, [relayPath("claude"), "--dangerously-skip-permissions"]);`, false},
		{"permission-mode pair split by a variable", `spawn('claude', ['--permission-mode', mode, 'bypassPermissions']);`, false},
		{"allow-dangerously flag", `spawn('claude', ['--allow-dangerously-skip-permissions']);`, false},
		{"q without chat", `spawn('q', ['--trust-all-tools']);`, false},
		{"q alone", `spawn('q', ['x']);`, false},
		{"cline is not judged", `spawn('cline', ['--auto-approve']);`, false},
		{"string only, no call", `const a = ['claude', '--dangerously-skip-permissions'];`, false},
		{"config object, no call", `const c = { cmd: 'claude', args: ['--dangerously-skip-permissions'] };`, false},
		{"method named spawn on another object", `proc.spawn('claude', ['--dangerously-skip-permissions']);`, false},
		{"template starting with interpolation", "exec(`${bin} --dangerously-skip-permissions`);", false},
		{"unbalanced call", `spawnSync('claude', ['--dangerously-skip-permissions'`, false},
	}
	for _, c := range cases {
		rel := "scripts/run.js"
		if c.name == "TypeScript file" {
			rel = "scripts/run.ts"
		}
		m := scriptFixture(t, map[string]string{rel: c.src})
		f := DiscoverProjectConfig(m, nil)
		if got := len(f.ScriptAgentBypass) > 0; got != c.want {
			t.Errorf("%s: got %v, want %v (%+v)", c.name, got, c.want, f.ScriptAgentBypass)
		}
	}
}

func TestScriptAgentBypass_SpawnTestPathsSkipped(t *testing.T) {
	src := `spawnSync('claude', ['--dangerously-skip-permissions']);`
	for _, rel := range []string{"test/relay.js", "src/run.test.js", "__tests__/a.js", "tests/b.ts"} {
		m := scriptFixture(t, map[string]string{rel: src})
		if f := DiscoverProjectConfig(m, nil); len(f.ScriptAgentBypass) != 0 {
			t.Errorf("%s: a test path must not be judged, got %+v", rel, f.ScriptAgentBypass)
		}
	}
}

func TestScriptAgentBypass_SpawnLine(t *testing.T) {
	src := "const a = 1;\n\nspawnSync('claude', ['--dangerously-skip-permissions']);\n"
	m := scriptFixture(t, map[string]string{"telemetry.js": src})
	f := DiscoverProjectConfig(m, nil)
	if len(f.ScriptAgentBypass) != 1 || f.ScriptAgentBypass[0].Line != 3 {
		t.Fatalf("want one hit on line 3, got %+v", f.ScriptAgentBypass)
	}
}

// A hit names the surface, the binary and the flag. The prompt and every other
// script word stay out of the facts.
func TestScriptAgentBypass_DoesNotEchoScriptText(t *testing.T) {
	m := scriptFixture(t, map[string]string{
		"package.json":    pkgScripts("postinstall", "claude -p 'FAKE-PROMPT-TEXT-123' --dangerously-skip-permissions"),
		".husky/pre-push": "gemini -p FAKE-HUSKY-PROMPT-456 --yolo\n",
		"x.js":            `spawnSync('claude', ['-p', 'FAKE-SPAWN-PROMPT-789', '--dangerously-skip-permissions']);`,
	})
	f := DiscoverProjectConfig(m, nil)
	if len(f.ScriptAgentBypass) != 3 {
		t.Fatalf("want three hits, got %+v", f.ScriptAgentBypass)
	}
	b, _ := json.Marshal(f)
	for _, secret := range []string{"FAKE-PROMPT-TEXT-123", "FAKE-HUSKY-PROMPT-456", "FAKE-SPAWN-PROMPT-789"} {
		if strings.Contains(string(b), secret) {
			t.Errorf("%q reached the facts: %s", secret, b)
		}
	}
}

func TestAllowAllBash(t *testing.T) {
	settings := func(allow, deny, ask string) string {
		return `{"permissions":{"allow":[` + allow + `],"deny":[` + deny + `],"ask":[` + ask + `]}}`
	}
	cases := []struct {
		name, file, body string
		want             bool
	}{
		{"bare Bash", ".claude/settings.json", settings(`"Bash"`, ``, ``), true},
		{"Bash(*)", ".claude/settings.json", settings(`"Bash(*)"`, ``, ``), true},
		{"local file", ".claude/settings.local.json", settings(`"Bash"`, ``, ``), true},
		{"nested folder", "pkg/.claude/settings.json", settings(`"Bash(*)"`, ``, ``), true},
		{"test-fixture folder is judged", "tests/fixtures/car/.claude/settings.json", settings(`"Bash"`, ``, ``), true},
		{"with other entries", ".claude/settings.json", settings(`"Read","Bash(*)"`, ``, ``), true},

		{"Bash() is not judged", ".claude/settings.json", settings(`"Bash()"`, ``, ``), false},
		{"Bash(:*) is not judged", ".claude/settings.json", settings(`"Bash(:*)"`, ``, ``), false},
		{"Bash( *) is not judged", ".claude/settings.json", settings(`"Bash( *)"`, ``, ``), false},
		{"exact command", ".claude/settings.json", settings(`"Bash(git status)"`, ``, ``), false},
		{"prefix rule is AGCFG-012", ".claude/settings.json", settings(`"Bash(git *)"`, ``, ``), false},
		{"other tool", ".claude/settings.json", settings(`"Read","WebFetch"`, ``, ``), false},
		{"deny bare Bash cancels", ".claude/settings.json", settings(`"Bash"`, `"Bash"`, ``), false},
		{"deny Bash(*) cancels", ".claude/settings.json", settings(`"Bash(*)"`, `"Bash(*)"`, ``), false},
		{"ask bare Bash cancels", ".claude/settings.json", settings(`"Bash(*)"`, ``, `"Bash"`), false},
		{"ask Bash(*) cancels", ".claude/settings.json", settings(`"Bash"`, ``, `"Bash(*)"`), false},
		{"narrower deny does not cancel", ".claude/settings.json", settings(`"Bash"`, `"Bash(rm *)"`, ``), true},
		{"narrower ask does not cancel", ".claude/settings.json", settings(`"Bash(*)"`, ``, `"Bash(git push *)"`), true},
		{"malformed JSON", ".claude/settings.json", `{"permissions":{"allow":["Bash"]`, false},
	}
	for _, c := range cases {
		f := discoverWith(t, c.file, c.body, models.ComponentClaudeSettings)
		if got := len(f.AllowAllBash) > 0; got != c.want {
			t.Errorf("%s: got %v, want %v (%+v)", c.name, got, c.want, f.AllowAllBash)
		}
	}
}

// A deny in a settings file in another .claude/ folder, and a deny in the
// sibling local file of the same folder, behave as the page's precedence says:
// only the same folder cancels.
func TestAllowAllBash_CancelScope(t *testing.T) {
	m := projectConfigFixture(t, map[string]string{
		"a/.claude/settings.json":       `{"permissions":{"allow":["Bash"]}}`,
		"b/.claude/settings.json":       `{"permissions":{"deny":["Bash"]}}`,
		"c/.claude/settings.json":       `{"permissions":{"allow":["Bash(*)"]}}`,
		"c/.claude/settings.local.json": `{"permissions":{"ask":["Bash(*)"]}}`,
	}, map[string]models.ComponentKind{
		"a/.claude/settings.json":       models.ComponentClaudeSettings,
		"b/.claude/settings.json":       models.ComponentClaudeSettings,
		"c/.claude/settings.json":       models.ComponentClaudeSettings,
		"c/.claude/settings.local.json": models.ComponentClaudeSettings,
	})
	f := DiscoverProjectConfig(m, DiscoverClaudeSettings(m))
	if len(f.AllowAllBash) != 1 || f.AllowAllBash[0].FilePath != "a/.claude/settings.json" {
		t.Fatalf("want one hit in a/.claude/settings.json, got %+v", f.AllowAllBash)
	}
}

func TestAllowAllBash_Line(t *testing.T) {
	body := "{\n  \"permissions\": {\n    \"allow\": [\n      \"Read\",\n      \"Bash(*)\"\n    ]\n  }\n}\n"
	f := discoverWith(t, ".claude/settings.json", body, models.ComponentClaudeSettings)
	if len(f.AllowAllBash) != 1 || f.AllowAllBash[0].Line != 5 {
		t.Fatalf("want one hit on line 5, got %+v", f.AllowAllBash)
	}
}

// AGCFG-012 judges git, xargs and npx prefixes; AGCFG-016 judges bare Bash and
// Bash(*). No entry fires both.
func TestAllowAllBash_DisjointFromExecCapable(t *testing.T) {
	f := discoverWith(t, ".claude/settings.json", `{"permissions":{"allow":["Bash","Bash(*)"]}}`, models.ComponentClaudeSettings)
	if len(f.ExecCapableAllow) != 0 {
		t.Errorf("AGCFG-012 must not judge a bare Bash entry, got %+v", f.ExecCapableAllow)
	}
	f = discoverWith(t, ".claude/settings.json", `{"permissions":{"allow":["Bash(git *)","Bash(npx *)"]}}`, models.ComponentClaudeSettings)
	if len(f.AllowAllBash) != 0 {
		t.Errorf("AGCFG-016 must not judge a prefix entry, got %+v", f.AllowAllBash)
	}
}
