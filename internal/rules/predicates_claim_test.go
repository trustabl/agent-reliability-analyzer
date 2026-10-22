package rules_test

import (
	"strings"
	"testing"

	"github.com/trustabl/trustabl/internal/models"
	"github.com/trustabl/trustabl/internal/rules"
)

func TestPredToolClaimUse(t *testing.T) {
	withClaim := func(use string) models.ToolDef {
		td := models.ToolDef{ParamNames: []string{"to", "claim"}, Facts: map[string]string{}}
		if use != "" {
			td.Facts[models.FactClaimUse] = use
		}
		return td
	}
	noClaim := models.ToolDef{ParamNames: []string{"to", "body"}}

	cases := []struct {
		name string
		uses []string
		tool models.ToolDef
		want bool
	}{
		{"absent matches a tool with no claim param", []string{"absent"}, noClaim, true},
		{"absent does not match a declared claim", []string{"absent"}, withClaim(models.ClaimUseUnused), false},
		{"unused matches its fact", []string{"unused"}, withClaim(models.ClaimUseUnused), true},
		{"passthrough matches its fact", []string{"passthrough"}, withClaim(models.ClaimUsePassthrough), true},
		{"checked is not unused", []string{"unused", "passthrough"}, withClaim(models.ClaimUseChecked), false},
		{"wrapped is not unused", []string{"unused"}, withClaim(models.ClaimUseWrapped), false},
		// A discovery path that does not classify the claim leaves no fact:
		// unknown is never a match, for any value.
		{"declared claim with no fact matches nothing", []string{"absent", "unused", "passthrough", "checked", "wrapped"}, withClaim(""), false},
		// A stale fact on a tool with no claim param cannot match.
		// A Claude Agent SDK handler takes one args dict; the claim is an input
		// schema key, not a parameter.
		{"claim as a Claude input key is declared", []string{"absent"}, models.ToolDef{ParamNames: []string{"args"}, InputKeys: []string{"to", "claim"}}, false},
		{"claim as a Claude input key matches its fact", []string{"unused"}, models.ToolDef{ParamNames: []string{"args"}, InputKeys: []string{"claim"}, Facts: map[string]string{models.FactClaimUse: models.ClaimUseUnused}}, true},
		{"fact without param is ignored", []string{"unused"}, models.ToolDef{Facts: map[string]string{models.FactClaimUse: models.ClaimUseUnused}}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := rules.PredToolClaimUse(tc.uses, tc.tool); got != tc.want {
				t.Errorf("PredToolClaimUse(%v) = %v, want %v", tc.uses, got, tc.want)
			}
		})
	}
}

func TestLoader_RejectsUnknownToolClaimUse(t *testing.T) {
	const pack = `policy:
  id: test_policy
  name: Test
  category: openai_sdk
  description: Test.
rules:
  - id: TEST-001
    title: Test rule
    scope: tool
    severity: medium
    confidence: 0.5
    applies_to:
      - openai_tool
    match:
      not:
        tool_claim_use: [chekced]
    explanation: Some explanation.
    fix: Some fix.
`
	_, err := rules.Load(makeFS(map[string]string{"test.yaml": pack}))
	if err == nil {
		t.Fatal("expected an error for an unknown tool_claim_use value, got nil")
	}
	if !strings.Contains(err.Error(), `tool_claim_use value "chekced"`) {
		t.Errorf("error does not name the bad value: %v", err)
	}
}
