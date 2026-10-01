package rules_test

import (
	"regexp"
	"strings"
	"testing"

	"github.com/trustabl/trustabl/internal/analysis"
	"github.com/trustabl/trustabl/internal/models"
)

// ssrfRuleIDs are the tool-scope rules whose match is `has_dynamic_url_call`
// minus an allow-list credit. Each keeps its existing fire case in
// policyRuleCases; this file derives the allow-listed variants from those
// fire snippets so the credit is proven against the same discovery shape the
// rule fires on.
var ssrfRuleIDs = map[string]bool{
	"CSDK-009": true, "CSDK-013": true, "OAI-018": true, "OAI-024": true,
	"LC-005": true, "LC-013": true, "CREW-005": true, "PYD-005": true,
	"ADK-012": true, "ADK-016": true, "MCP-008": true, "MCP-013": true,
	"AG2-011": true, "VAI-003": true,
}

var (
	pyDefLine = regexp.MustCompile(`(?m)^(async\s+)?def\s[^\n]*:[ \t]*$`)
	tsArrow   = regexp.MustCompile(`=>\s*\{`)
)

// withGuard injects one statement at the top of the tool body. It reports
// whether the snippet was changed so a shape the regexes miss fails loudly
// instead of silently testing the unmodified source.
func withGuard(t *testing.T, src string, lang models.Language, pyStmt, tsStmt string) string {
	t.Helper()
	if lang == models.LanguageTypeScript {
		loc := tsArrow.FindStringIndex(src)
		if loc == nil {
			t.Fatalf("no arrow-function body in TS snippet:\n%s", src)
		}
		return src[:loc[1]] + " " + tsStmt + " " + src[loc[1]:]
	}
	loc := pyDefLine.FindStringIndex(src)
	if loc == nil {
		t.Fatalf("no single-line def in Python snippet:\n%s", src)
	}
	return src[:loc[1]] + "\n    " + pyStmt + src[loc[1]:]
}

func TestSSRFAllowListCredit(t *testing.T) {
	const (
		pyAllow = `if urlparse(url).hostname not in ALLOWED_HOSTS: raise ValueError("host")`
		tsAllow = `if (!ALLOWED.has(new URL(String(arguments[0])).hostname)) throw new Error("host");`
		// A hostname read with no membership test, and a deny-list check, are
		// not allow-lists and must keep firing.
		pyDeny = `if urlparse(url).hostname in BLOCKED: raise ValueError("host")`
		tsRead = `const h = new URL(String(arguments[0])).hostname; console.log(h);`
	)
	seen := map[string]bool{}
	for _, tc := range policyRuleCases {
		if !ssrfRuleIDs[tc.ruleID] || !tc.wantFires {
			continue
		}
		seen[tc.ruleID] = true
		variants := []struct {
			name      string
			py, ts    string
			wantFires bool
		}{
			{"silent when hostname is checked against an allow-list", pyAllow, tsAllow, false},
			{"fires when the hostname is only read or deny-listed", pyDeny, tsRead, true},
		}
		for _, v := range variants {
			t.Run(tc.ruleID+" "+v.name, func(t *testing.T) {
				src := withGuard(t, tc.src, tc.lang, v.py, v.ts)
				d := loadToolRule(t, tc.ruleID)
				var (
					tool models.ToolDef
					pf   analysis.ParsedFile
				)
				if tc.lang == models.LanguageTypeScript {
					tool, pf = parseTSTool(t, src, tc.kind)
				} else {
					tool, pf = parsePy(t, src, tc.kind)
				}
				if !d.Applies(tool) {
					t.Fatalf("rule %s does not apply to the variant", tc.ruleID)
				}
				fired := false
				for _, f := range d.Detect(tool, pf, models.RepoInventory{}) {
					if f.RuleID == tc.ruleID {
						fired = true
					}
				}
				if fired != v.wantFires {
					t.Errorf("%s: fired=%v, want %v\n%s", tc.ruleID, fired, v.wantFires, strings.TrimSpace(src))
				}
			})
		}
	}
	for id := range ssrfRuleIDs {
		if !seen[id] {
			t.Errorf("no fire case in policyRuleCases for %s; the allow-list variants cannot be derived", id)
		}
	}
}
