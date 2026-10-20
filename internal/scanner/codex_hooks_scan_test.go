package scanner_test

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/trustabl/trustabl/internal/scanner"
)

// End to end: a committed .codex/hooks.json that runs a repo script and calls the
// network fires the Codex hook rule at that file, and does NOT fire AGCFG-005,
// whose "runs before trust" claim is not true for Codex.
func TestScan_CodexHooksFixtureFiresOnlyTheCodexRule(t *testing.T) {
	_, thisFile, _, _ := runtime.Caller(0)
	target := filepath.Join(filepath.Dir(thisFile), "..", "..", "testdata", "corpus", "codex-hooks-fixture")
	res, err := scanner.Run(scanner.Config{Target: target, RulesFS: rulesFixture(t)})
	if err != nil {
		t.Fatalf("scan: %v", err)
	}
	var found bool
	for _, f := range res.Findings {
		switch f.RuleID {
		case "AGCFG-008":
			found = true
			if !strings.HasSuffix(f.FilePath, ".codex/hooks.json") {
				t.Errorf("AGCFG-008 attributed to %q, want a path ending .codex/hooks.json", f.FilePath)
			}
			if f.StartLine < 1 {
				t.Error("AGCFG-008 has no line")
			}
			if f.Severity != "medium" {
				t.Errorf("AGCFG-008 severity = %q, want medium", f.Severity)
			}
		case "AGCFG-005":
			t.Errorf("AGCFG-005 must not fire on a Codex hook (%s:%d)", f.FilePath, f.StartLine)
		}
	}
	if !found {
		t.Error("AGCFG-008 did not fire on the Codex hooks fixture")
	}
}

// The safe counterpart: hooks that only call a binary outside the repo, plus a
// home-relative script and an mcp_tool handler, must not fire the Codex rule.
func TestScan_CodexHooksSafeVariantStaysSilent(t *testing.T) {
	dir := t.TempDir()
	body := `{"hooks":{"PostToolUse":[{"matcher":"Bash","hooks":[` +
		`{"type":"command","command":"python3 ~/.codex/hooks/post_tool_use.py","async":true,"timeout":120},` +
		`{"type":"mcp_tool","server":"scanner","tool":"scan_patch","input":{"patch":"x"}}]}]}}`
	full := filepath.Join(dir, ".codex", "hooks.json")
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(full, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	res, err := scanner.Run(scanner.Config{Target: dir, RulesFS: rulesFixture(t)})
	if err != nil {
		t.Fatalf("scan: %v", err)
	}
	for _, f := range res.Findings {
		if f.RuleID == "AGCFG-008" || f.RuleID == "AGCFG-005" {
			t.Errorf("%s fired on a safe Codex hooks file (%s:%d)", f.RuleID, f.FilePath, f.StartLine)
		}
	}
}
