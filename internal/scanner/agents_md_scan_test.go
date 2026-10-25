package scanner_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/trustabl/trustabl/internal/scanner"
)

// End to end: an AGENTS.md carrying a Tags-block payload fires the hidden-text
// rule at that file, and a plain AGENTS.md does not. The hidden characters are
// written from escape sequences so no invisible text is committed.
func TestScan_AgentsMdHiddenTextFiresOnlyOnHiddenText(t *testing.T) {
	write := func(dir, body string) {
		if err := os.WriteFile(filepath.Join(dir, "AGENTS.md"), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	dirHidden, dirPlain := t.TempDir(), t.TempDir()
	write(dirHidden, "# Guidance\n\nUse tabs.\U000E0049\U000E0067\U000E006E\U000E006F\U000E0072\U000E0065\n")
	write(dirPlain, "# Guidance\n\nUse tabs.\n")

	res, err := scanner.Run(scanner.Config{Target: dirHidden, RulesFS: rulesFixture(t)})
	if err != nil {
		t.Fatalf("scan: %v", err)
	}
	var found bool
	for _, f := range res.Findings {
		if f.RuleID != "AGCFG-009" {
			continue
		}
		found = true
		if !strings.HasSuffix(f.FilePath, "AGENTS.md") || f.StartLine != 3 {
			t.Errorf("AGCFG-009 at %s:%d, want AGENTS.md:3", f.FilePath, f.StartLine)
		}
		if f.Severity != "medium" {
			t.Errorf("severity = %q, want medium", f.Severity)
		}
		if strings.ContainsRune(f.Explanation+f.Title, 0xE0049) {
			t.Error("finding echoes the hidden text")
		}
	}
	if !found {
		t.Error("AGCFG-009 did not fire on an AGENTS.md with a Tags-block payload")
	}

	plain, err := scanner.Run(scanner.Config{Target: dirPlain, RulesFS: rulesFixture(t)})
	if err != nil {
		t.Fatalf("scan: %v", err)
	}
	for _, f := range plain.Findings {
		if f.RuleID == "AGCFG-009" {
			t.Errorf("AGCFG-009 fired on a plain AGENTS.md (%s:%d)", f.FilePath, f.StartLine)
		}
	}
}
