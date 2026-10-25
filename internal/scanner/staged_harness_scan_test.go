package scanner_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/trustabl/trustabl/internal/scanner"
)

func writeStagedTree(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	for rel, body := range files {
		full := filepath.Join(dir, rel)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func firedRuleIDs(t *testing.T, dir string) map[string]string {
	t.Helper()
	res, err := scanner.Run(scanner.Config{Target: dir, RulesFS: rulesFixture(t)})
	if err != nil {
		t.Fatalf("scan: %v", err)
	}
	got := map[string]string{}
	for _, f := range res.Findings {
		got[f.RuleID] = f.FilePath
	}
	return got
}

// End to end: hidden text in a staged harness guidance file fires AGCFG-010 at that
// file and not AGCFG-009, which is for AGENTS.md. The hidden characters are written
// from escape sequences so no invisible text is committed.
func TestScan_StagedHarnessHiddenTextFiresAGCFG010(t *testing.T) {
	dir := writeStagedTree(t, map[string]string{
		"systemprompt.md": "Reconcile the ledger.\n",
		"guardrails.md":   "Never delete files.\U000E0049\U000E0067\U000E006E\U000E006F\U000E0072\U000E0065\n",
	})
	got := firedRuleIDs(t, dir)
	file, ok := got["AGCFG-010"]
	if !ok {
		t.Fatalf("AGCFG-010 did not fire; fired: %v", got)
	}
	if !strings.HasSuffix(file, "guardrails.md") {
		t.Errorf("AGCFG-010 attributed to %q, want guardrails.md", file)
	}
	if _, bad := got["AGCFG-009"]; bad {
		t.Error("AGCFG-009 must not fire on a staged harness file")
	}
}

func TestScan_StagedHarnessPlainTreeStaysSilent(t *testing.T) {
	dir := writeStagedTree(t, map[string]string{
		"systemprompt.md":                    "Reconcile the ledger using python scripts/summarize.py.\n",
		"guardrails.md":                      "",
		"LongTermMEMORY.md":                  "",
		"tool_descriptions/tool_guidance.md": "",
	})
	if _, bad := firedRuleIDs(t, dir)["AGCFG-010"]; bad {
		t.Error("AGCFG-010 fired on a plain staged harness tree")
	}
}

// Without the signature, hidden text in a lone guardrails.md is not a harness file.
func TestScan_LoneGuardrailsFileIsNotAStagedHarness(t *testing.T) {
	dir := writeStagedTree(t, map[string]string{
		"guardrails.md": "x\U0000200by\n",
	})
	if _, bad := firedRuleIDs(t, dir)["AGCFG-010"]; bad {
		t.Error("AGCFG-010 fired without the systemprompt.md and guardrails.md signature")
	}
}

// AGENTS.md hidden text fires AGCFG-009 and never AGCFG-010.
func TestScan_AgentsMdHiddenTextDoesNotFireAGCFG010(t *testing.T) {
	dir := writeStagedTree(t, map[string]string{
		"AGENTS.md": "Use tabs.\U000E0049\U000E0067\n",
	})
	got := firedRuleIDs(t, dir)
	if _, ok := got["AGCFG-009"]; !ok {
		t.Fatalf("AGCFG-009 should fire on AGENTS.md; fired: %v", got)
	}
	if _, bad := got["AGCFG-010"]; bad {
		t.Error("AGCFG-010 must not fire on AGENTS.md")
	}
}
