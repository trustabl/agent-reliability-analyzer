package analysis

import (
	"strings"
	"testing"

	"github.com/trustabl/trustabl/internal/models"
)

func settingsHits(t *testing.T, body string) models.ProjectConfigFacts {
	t.Helper()
	return discoverWith(t, ".claude/settings.json", body, models.ComponentClaudeSettings)
}

func TestProjectConfig_UnscopedAllow(t *testing.T) {
	cases := []struct {
		name, body string
		want       int
	}{
		{"python prefix colon", `{"permissions":{"allow":["Bash(python:*)"]}}`, 1},
		{"curl prefix space", `{"permissions":{"allow":["Bash(curl *)"]}}`, 1},
		{"node bash sh wget", `{"permissions":{"allow":["Bash(node:*)","Bash(bash *)","Bash(sh:*)","Bash(wget *)","Bash(python3:*)"]}}`, 5},
		{"bare WebFetch", `{"permissions":{"allow":["WebFetch"]}}`, 1},
		{"bare Edit and Write", `{"permissions":{"allow":["Edit","Write"]}}`, 2},
		{"Edit star", `{"permissions":{"allow":["Edit(*)"]}}`, 1},
		{"whole mcp server", `{"permissions":{"allow":["mcp__github"]}}`, 1},
		{"whole mcp server glob", `{"permissions":{"allow":["mcp__github__*"]}}`, 1},
		{"narrow list is silent", `{"permissions":{"allow":["Bash(npm run test)","WebFetch(domain:docs.python.org)","mcp__github__get_issue","Edit(src/**)","Bash(python scripts/lint.py)","Bash(python -m pytest *)"]}}`, 0},
		{"bare Bash is AGCFG-016's", `{"permissions":{"allow":["Bash"]}}`, 0},
		{"git prefix is AGCFG-012's", `{"permissions":{"allow":["Bash(git:*)"]}}`, 0},
		{"deny of same shape cancels", `{"permissions":{"allow":["Bash(python:*)","WebFetch","mcp__github"],"deny":["Bash(python:*)","WebFetch","mcp__github"]}}`, 0},
		{"ask of bare Bash cancels prefixes", `{"permissions":{"allow":["Bash(curl *)"],"ask":["Bash"]}}`, 0},
		{"narrower deny does not cancel", `{"permissions":{"allow":["Bash(curl *)"],"deny":["Bash(curl http://evil/*)"]}}`, 1},
		{"deny of another server does not cancel", `{"permissions":{"allow":["mcp__github"],"deny":["mcp__slack"]}}`, 1},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := len(settingsHits(t, c.body).UnscopedAllow); got != c.want {
				t.Errorf("UnscopedAllow = %d, want %d", got, c.want)
			}
		})
	}
}

func TestProjectConfig_UnscopedAllow_ReasonNamesEntry(t *testing.T) {
	f := settingsHits(t, `{"permissions":{"allow":["Bash(curl:*)"]}}`)
	if len(f.UnscopedAllow) != 1 || !strings.Contains(f.UnscopedAllow[0].Reason, "Bash(curl:*)") {
		t.Fatalf("hits = %+v", f.UnscopedAllow)
	}
}

func TestProjectConfig_LocalSettingsCommitted(t *testing.T) {
	kinds := func(files map[string]string) map[string]models.ComponentKind {
		k := map[string]models.ComponentKind{}
		for rel := range files {
			if strings.HasSuffix(rel, ".json") {
				k[rel] = models.ComponentClaudeSettings
			}
		}
		return k
	}
	cases := []struct {
		name  string
		files map[string]string
		want  int
	}{
		{"present without gitignore", map[string]string{".claude/settings.local.json": "{}"}, 1},
		{"shared settings.json is not judged", map[string]string{".claude/settings.json": "{}"}, 0},
		{"ignored by name", map[string]string{".claude/settings.local.json": "{}", ".gitignore": "settings.local.json\n"}, 0},
		{"ignored by path", map[string]string{".claude/settings.local.json": "{}", ".gitignore": ".claude/settings.local.json\n"}, 0},
		{"ignored by glob", map[string]string{".claude/settings.local.json": "{}", ".gitignore": "**/.claude/*.local.json\n"}, 0},
		{"ignored by parent folder", map[string]string{".claude/settings.local.json": "{}", ".gitignore": ".claude/\n"}, 0},
		{"unrelated gitignore", map[string]string{".claude/settings.local.json": "{}", ".gitignore": "node_modules/\n*.log\n"}, 1},
		{"comment is not a pattern", map[string]string{".claude/settings.local.json": "{}", ".gitignore": "# settings.local.json\n"}, 1},
		{"negation re-includes", map[string]string{".claude/settings.local.json": "{}", ".gitignore": "settings.local.json\n!settings.local.json\n"}, 1},
		{"nested gitignore", map[string]string{"app/.claude/settings.local.json": "{}", "app/.gitignore": ".claude/settings.local.json\n"}, 0},
		{"anchored root pattern does not reach nested file", map[string]string{"app/.claude/settings.local.json": "{}", ".gitignore": "/.claude/settings.local.json\n"}, 1},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			m := projectConfigFixture(t, c.files, kinds(c.files))
			f := DiscoverProjectConfig(m, DiscoverClaudeSettings(m))
			if got := len(f.LocalSettingsCommitted); got != c.want {
				t.Errorf("LocalSettingsCommitted = %d, want %d (%+v)", got, c.want, f.LocalSettingsCommitted)
			}
		})
	}
}

func TestProjectConfig_DeprecatedMCPLaunch(t *testing.T) {
	cases := []struct {
		name, body string
		want       int
	}{
		{"npx github bare", `{"mcpServers":{"gh":{"command":"npx","args":["-y","@modelcontextprotocol/server-github"]}}}`, 1},
		{"npx github latest", `{"mcpServers":{"gh":{"command":"npx","args":["-y","@modelcontextprotocol/server-github@latest"]}}}`, 1},
		{"npx github pinned", `{"mcpServers":{"gh":{"command":"npx","args":["-y","@modelcontextprotocol/server-github@2025.4.8"]}}}`, 1},
		{"npx --package= form", `{"mcpServers":{"p":{"command":"npx","args":["--package=@modelcontextprotocol/server-puppeteer","server"]}}}`, 1},
		{"npx -p form", `{"mcpServers":{"p":{"command":"npx","args":["-p","@modelcontextprotocol/server-puppeteer","server"]}}}`, 1},
		{"bunx slack", `{"mcpServers":{"s":{"command":"bunx","args":["@modelcontextprotocol/server-slack"]}}}`, 1},
		{"pnpm dlx postgres", `{"mcpServers":{"s":{"command":"pnpm","args":["dlx","@modelcontextprotocol/server-postgres","postgresql://x"]}}}`, 1},
		{"uvx sqlite pinned", `{"mcpServers":{"s":{"command":"uvx","args":["mcp-server-sqlite==2025.1.1","--db-path","x.db"]}}}`, 1},
		{"uvx sentry", `{"mcpServers":{"s":{"command":"uvx","args":["mcp-server-sentry"]}}}`, 1},
		{"git server is not archived", `{"mcpServers":{"g":{"command":"uvx","args":["mcp-server-git"]}}}`, 0},
		{"maintained filesystem server", `{"mcpServers":{"f":{"command":"npx","args":["-y","@modelcontextprotocol/server-filesystem","."]}}}`, 0},
		{"memory server", `{"mcpServers":{"m":{"command":"npx","args":["-y","@modelcontextprotocol/server-memory"]}}}`, 0},
		{"remote url", `{"mcpServers":{"r":{"url":"https://example.com/mcp"}}}`, 0},
		{"docker image", `{"mcpServers":{"d":{"command":"docker","args":["run","-i","mcp/postgres"]}}}`, 0},
		{"shell string with -c", `{"mcpServers":{"c":{"command":"npx","args":["-c","server-github"]}}}`, 0},
		{"similar name", `{"mcpServers":{"c":{"command":"npx","args":["@modelcontextprotocol/server-github-extra"]}}}`, 0},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f := discoverWith(t, ".mcp.json", c.body, models.ComponentMCPConfig)
			if got := len(f.DeprecatedMCPLaunch); got != c.want {
				t.Errorf("DeprecatedMCPLaunch = %d, want %d", got, c.want)
			}
		})
	}
}
