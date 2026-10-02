package rules_test

import (
	"strings"
	"testing"

	"github.com/trustabl/trustabl/internal/analysis"
	"github.com/trustabl/trustabl/internal/models"
	"github.com/trustabl/trustabl/internal/rules"
)

// httpVariant rewrites a snippet's https:// literals to http:// so a URL that
// was pinned to https stops being pinned.
func httpVariant(src string) string { return strings.ReplaceAll(src, "https://", "http://") }

// httpsRuleSiblings maps each HTTPS-pinning rule to the SSRF rule whose
// allow-list credit it builds on. The HTTPS rule must fire exactly when the
// sibling has been silenced by an allow-list and the scheme is still unpinned.
var httpsRuleSiblings = map[string]string{
	"CSDK-024": "CSDK-009", "CSDK-025": "CSDK-013",
	"OAI-032": "OAI-018", "OAI-033": "OAI-024",
	"MCP-031": "MCP-008", "MCP-032": "MCP-013",
	"ADK-022": "ADK-012", "ADK-023": "ADK-016",
	"LC-026": "LC-005", "LC-027": "LC-013",
	"CREW-015": "CREW-005", "PYD-015": "PYD-005", "AG2-022": "AG2-011",
	"VAI-021": "VAI-003",
}

const (
	httpsPyAllow  = `if urlparse(url).hostname not in ALLOWED_HOSTS: raise ValueError("host")`
	httpsTSAllow  = `if (!ALLOWED.has(new URL(String(arguments[0])).hostname)) throw new Error("host");`
	httpsPyScheme = `if urlparse(url).scheme != "https": raise ValueError("scheme")`
	httpsTSScheme = `if (new URL(String(arguments[0])).protocol !== "https:") throw new Error("scheme");`
)

// The HTTPS rules' fire/silent cases are derived from each sibling SSRF rule's
// own fire snippet, so they run against the discovery shape that rule proves.
// Several sibling snippets build "https://{host}/..." — a pinned scheme the
// HTTPS rules correctly stay silent on — so the fire variant is rewritten to
// http://, which is unpinned. Pinned forms are covered by the predicate tests.
//
// Appending in init keeps TestPolicyRules_AllRulesCovered's coverage guard
// honest without 28 hand-copied snippets.
func init() {
	var derived []policyRuleCase
	for httpsID, ssrfID := range httpsRuleSiblings {
		for _, tc := range policyRuleCases {
			if tc.ruleID != ssrfID || !tc.wantFires {
				continue
			}
			allow, ok := injectGuard(httpVariant(tc.src), tc.lang, httpsPyAllow, httpsTSAllow)
			if !ok {
				panic("no body in sibling snippet for " + httpsID)
			}
			both, _ := injectGuard(allow, tc.lang, httpsPyScheme, httpsTSScheme)
			derived = append(derived,
				policyRuleCase{name: httpsID + " fires when allow-listed but scheme unpinned", ruleID: httpsID,
					kind: tc.kind, lang: tc.lang, src: allow, toolConfig: tc.toolConfig, wantFires: true},
				policyRuleCase{name: httpsID + " silent when scheme is checked", ruleID: httpsID,
					kind: tc.kind, lang: tc.lang, src: both, toolConfig: tc.toolConfig, wantFires: false},
				policyRuleCase{name: httpsID + " silent with no allow-list (SSRF rule owns it)", ruleID: httpsID,
					kind: tc.kind, lang: tc.lang, src: httpVariant(tc.src), toolConfig: tc.toolConfig, wantFires: false},
			)
			break
		}
	}
	policyRuleCases = append(policyRuleCases, derived...)
}

// TestSSRFAndHTTPSAreMutuallyExclusive proves the staging the HTTPS rules
// promise: with no allow-list only the SSRF rule fires; with an allow-list only
// the HTTPS rule fires; with an allow-list and a scheme check neither does.
func TestSSRFAndHTTPSAreMutuallyExclusive(t *testing.T) {
	for httpsID, ssrfID := range httpsRuleSiblings {
		var base *policyRuleCase
		for i := range policyRuleCases {
			if policyRuleCases[i].ruleID == ssrfID && policyRuleCases[i].wantFires {
				base = &policyRuleCases[i]
				break
			}
		}
		if base == nil {
			t.Fatalf("no fire case for %s", ssrfID)
		}
		allow, _ := injectGuard(httpVariant(base.src), base.lang, httpsPyAllow, httpsTSAllow)
		both, _ := injectGuard(allow, base.lang, httpsPyScheme, httpsTSScheme)
		for _, v := range []struct {
			name        string
			src         string
			ssrf, https bool
		}{
			{"no allow-list", httpVariant(base.src), true, false},
			{"allow-list only", allow, false, true},
			{"allow-list and scheme check", both, false, false},
		} {
			t.Run(httpsID+" "+v.name, func(t *testing.T) {
				var (
					tool models.ToolDef
					pf   analysis.ParsedFile
				)
				if base.lang == models.LanguageTypeScript {
					tool, pf = parseTSTool(t, v.src, base.kind)
				} else {
					tool, pf = parsePy(t, v.src, base.kind)
				}
				fired := map[string]bool{}
				for _, id := range []string{ssrfID, httpsID} {
					for _, f := range loadToolRule(t, id).Detect(tool, pf, models.RepoInventory{}) {
						fired[f.RuleID] = true
					}
				}
				if fired[ssrfID] != v.ssrf || fired[httpsID] != v.https {
					t.Errorf("%s=%v (want %v) %s=%v (want %v)\n%s", ssrfID, fired[ssrfID], v.ssrf,
						httpsID, fired[httpsID], v.https, strings.TrimSpace(v.src))
				}
			})
		}
	}
}

// TestPredHasUnpinnedSchemeURLCall pins down which URL shapes count as having a
// fixed https:// scheme (silent) versus an unpinned one (fires).
func TestPredHasUnpinnedSchemeURLCall(t *testing.T) {
	py := []struct {
		name, call string
		want       bool
	}{
		{"identifier", `requests.get(url)`, true},
		{"http f-string", `requests.get(f"http://{host}/x")`, true},
		{"https f-string", `requests.get(f"https://{host}/x")`, false},
		{"HTTPS upper-case f-string", `requests.get(f"HTTPS://{host}/x")`, false},
		{"https concat", `requests.get("https://api.example.com/" + path)`, false},
		{"https percent-format", `requests.get("https://api.example.com/%s" % path)`, false},
		{"https .format", `requests.get("https://api.example.com/{}".format(path))`, false},
		{"host-first f-string", `requests.get(f"{base}/x")`, true},
		{"literal URL is not dynamic", `requests.get("http://api.example.com/x")`, false},
		{"attribute URL", `requests.get(cfg.url)`, true},
	}
	for _, c := range py {
		t.Run("py "+c.name, func(t *testing.T) {
			src := "import requests\ndef fetch(url: str, host: str, path: str, base: str, cfg=None) -> str:\n    \"\"\"Fetch.\"\"\"\n    return " + c.call + ".text\n"
			tool, pf := parsePy(t, src, models.KindClaudeSDKTool)
			if got := rules.PredHasUnpinnedSchemeURLCall(tool, pf); got != c.want {
				t.Errorf("unpinned = %v, want %v for %s", got, c.want, c.call)
			}
		})
	}
	ts := []struct {
		name, call string
		want       bool
	}{
		{"identifier", "fetch(url)", true},
		{"http template", "fetch(`http://${host}/x`)", true},
		{"https template", "fetch(`https://${host}/x`)", false},
		{"https concat", `fetch("https://api.example.com/" + path)`, false},
		{"host-first template", "fetch(`${base}/x`)", true},
		{"literal URL is not dynamic", `fetch("http://api.example.com/x")`, false},
	}
	for _, c := range ts {
		t.Run("ts "+c.name, func(t *testing.T) {
			src := "import { tool } from \"@anthropic-ai/claude-agent-sdk\";\nimport { z } from \"zod\";\n" +
				"export const t = tool(\"f\", \"f\", { url: z.string() }, async ({ url, host, path, base }) => {\n  await " + c.call + ";\n  return { content: [] };\n});\n"
			tool, pf := parseTSTool(t, src, models.KindClaudeSDKTool)
			if got := rules.PredHasUnpinnedSchemeURLCall(tool, pf); got != c.want {
				t.Errorf("unpinned = %v, want %v for %s", got, c.want, c.call)
			}
		})
	}
}
