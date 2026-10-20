package analysis

import (
	"strings"
	"testing"

	"github.com/trustabl/trustabl/internal/models"
)

// codexDocFullExample is the full hooks.json example in OpenAI's Codex hooks
// documentation (https://developers.openai.com/codex/hooks), whitespace
// normalised. It is the documented shape, not a guess.
const codexDocFullExample = `{
  "description": "Optional lifecycle hooks for this workspace.",
  "hooks": {
    "SessionStart": [
      {
        "matcher": "startup|resume",
        "hooks": [
          {
            "type": "command",
            "command": "python3 ~/.codex/hooks/session_start.py",
            "statusMessage": "Loading session notes",
            "additionalContextLimit": 5000
          }
        ]
      }
    ],
    "PreToolUse": [
      {
        "matcher": "Bash",
        "hooks": [
          {
            "type": "command",
            "command": "/usr/bin/python3 \"$(git rev-parse --show-toplevel)/.codex/hooks/pre_tool_use_policy.py\"",
            "statusMessage": "Checking Bash command"
          }
        ]
      }
    ]
  }
}`

// codexDocAsyncExample is the documented background hook: a home-relative
// script, which is not repo-controlled.
const codexDocAsyncExample = `{
  "hooks": {
    "PostToolUse": [
      {
        "matcher": "Bash",
        "hooks": [
          {
            "type": "command",
            "command": "python3 ~/.codex/hooks/post_tool_use.py",
            "async": true,
            "timeout": 120
          }
        ]
      }
    ]
  }
}`

// codexDocMCPExample is the documented mcp_tool handler: it calls an
// already-connected MCP server and carries no shell command.
const codexDocMCPExample = `{
  "hooks": {
    "PostToolUse": [
      {
        "matcher": "Write|Edit",
        "hooks": [
          {
            "type": "mcp_tool",
            "server": "scanner",
            "tool": "scan_patch",
            "input": { "patch": "${tool_input.command}" },
            "timeout": 30
          }
        ]
      }
    ]
  }
}`

func codexFacts(t *testing.T, body string) models.ProjectConfigFacts {
	t.Helper()
	return discoverWith(t, ".codex/hooks.json", body, models.ComponentCodexHooks)
}

func codexHook(cmd string) string {
	return `{"hooks":{"PreToolUse":[{"matcher":"*","hooks":[{"type":"command","command":"` + cmd + `"}]}]}}`
}

func TestCodexHooks_DocumentedRepoRootIdiomFires(t *testing.T) {
	f := codexFacts(t, codexDocFullExample)
	if len(f.CodexRiskyHooks) != 1 {
		t.Fatalf("want exactly the PreToolUse repo-root hook, got %+v", f.CodexRiskyHooks)
	}
	h := f.CodexRiskyHooks[0]
	if !strings.Contains(h.Reason, "PreToolUse") || !strings.Contains(h.Reason, "repo-relative script") {
		t.Errorf("reason = %q", h.Reason)
	}
	if h.FilePath != ".codex/hooks.json" || h.Line < 2 {
		t.Errorf("location = %s:%d, want a line inside .codex/hooks.json", h.FilePath, h.Line)
	}
	// Codex hooks must never feed the critical AGCFG-005 list.
	if len(f.RiskyHooks) != 0 {
		t.Errorf("Codex hook leaked into RiskyHooks: %+v", f.RiskyHooks)
	}
}

func TestCodexHooks_SilentCases(t *testing.T) {
	for name, body := range map[string]string{
		"documented async home-relative hook": codexDocAsyncExample,
		"documented mcp_tool handler":         codexDocMCPExample,
		"vetted binary on PATH":               codexHook(`prettier --write`),
		"absolute binary":                     codexHook(`/usr/bin/true`),
		"git diff":                            codexHook(`git diff --stat`),
		"prompt handler with a command key":   `{"hooks":{"Stop":[{"hooks":[{"type":"prompt","command":"./x.sh"}]}]}}`,
		"agent handler with a command key":    `{"hooks":{"Stop":[{"hooks":[{"type":"agent","command":"curl http://x.invalid"}]}]}}`,
		"no hooks key":                        `{"description":"nothing here"}`,
		"malformed JSON":                      `{"hooks":`,
	} {
		if f := codexFacts(t, body); len(f.CodexRiskyHooks) != 0 {
			t.Errorf("%s: want no hit, got %+v", name, f.CodexRiskyHooks)
		}
	}
}

func TestCodexHooks_FireCases(t *testing.T) {
	for name, cmd := range map[string]string{
		"explicit repo script":     `./scripts/lint.sh`,
		"parent repo script":       `../tools/check.py`,
		"curl":                     `curl -s https://example.invalid/x`,
		"inline shell":             `bash -c 'echo hi'`,
		"inline python":            `python3 -c 'print(1)'`,
		"credential read":          `cat ~/.ssh/id_fake`,
		"repo-root via git (bare)": `$(git rev-parse --show-toplevel)/.codex/hooks/check.sh`,
	} {
		if f := codexFacts(t, codexHook(cmd)); len(f.CodexRiskyHooks) == 0 {
			t.Errorf("%s (%q): want a hit", name, cmd)
		}
	}
}

func TestCodexHooks_CommandWindowsIsJudged(t *testing.T) {
	body := `{"hooks":{"PreToolUse":[{"hooks":[{"type":"command","command":"/usr/bin/true","commandWindows":"powershell -File ./scripts/check.ps1"}]}]}}`
	if f := codexFacts(t, body); len(f.CodexRiskyHooks) == 0 {
		t.Error("a repo script in commandWindows must be flagged")
	}
}

// The Claude and Gemini hook path and AGCFG-005 must not change.
func TestCodexHooks_ClaudeSettingsStillFeedRiskyHooks(t *testing.T) {
	body := `{"hooks":{"PreToolUse":[{"matcher":"*","hooks":[{"type":"command","command":"./scripts/lint.sh"}]}]}}`
	f := discoverWith(t, ".claude/settings.json", body, models.ComponentClaudeSettings)
	if len(f.RiskyHooks) == 0 {
		t.Error("Claude settings hook must still produce a RiskyHooks hit")
	}
	if len(f.CodexRiskyHooks) != 0 {
		t.Errorf("Claude settings must not feed CodexRiskyHooks: %+v", f.CodexRiskyHooks)
	}
}
