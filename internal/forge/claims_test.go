package forge

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/trustabl/trustabl/internal/models"
	"github.com/trustabl/trustabl/internal/rules"
)

func claimStamp(cats ...models.DetectorCategory) PolicyStamp {
	return PolicyStamp{
		Date:          "2026-01-01",
		RulesSHA:      "abc1234",
		SchemaVersion: 13,
		Categories:    cats,
		Template:      TemplateVersion,
	}
}

func generateForClaims(t *testing.T, cats ...models.DetectorCategory) string {
	t.Helper()
	inputDir := filepath.Join("..", "..", "testdata", "forge", "multi_sdk", "input")
	policies, err := rules.Load(os.DirFS(inputDir))
	if err != nil {
		t.Fatalf("load fixture rules: %v", err)
	}
	stamp := claimStamp(cats...)
	return GenerateCombined(stamp.Categories, policies, stamp, allTracingSelection(), nil)
}

func TestGenerateCombined_EmitsToolCallClaims(t *testing.T) {
	got := generateForClaims(t, models.CategoryClaudeSDK, models.CategoryOpenAISDK)

	claimsIdx := strings.Index(got, "## Tool-Call Claims")
	if claimsIdx < 0 {
		t.Fatal("generated skill is missing the Tool-Call Claims section")
	}
	// The claim is part of how a tool is defined, so it is read after the
	// apply loop and before the tracing and per-SDK rule material.
	loopIdx := strings.Index(got, "## How to Apply These Constraints")
	tracingIdx := strings.Index(got, "## Runtime Tracing")
	rulesIdx := strings.Index(got, "### Tool Rules")
	if loopIdx < 0 || claimsIdx < loopIdx {
		t.Error("Tool-Call Claims must follow the apply loop")
	}
	if tracingIdx >= 0 && claimsIdx > tracingIdx {
		t.Error("Tool-Call Claims must precede Runtime Tracing")
	}
	if rulesIdx >= 0 && claimsIdx > rulesIdx {
		t.Error("Tool-Call Claims must precede the per-SDK rule sections")
	}

	for _, want := range []string{
		"### The claim",
		"### The mediation step",
		"### Where the step goes",
		"### Check before moving on",
		"ToolInvocationExplanation",
		// The non-negotiable EBTE properties: untrusted, cannot widen
		// authority, conflict denies, unknown reviews, most restrictive wins.
		"never grants authority",
		"a malformed claim, or an unknown `schema_version` is a deny.",
		// The claim is required where there is an effect; pure reads may
		// skip it, and the registry tells a skipped claim from a missing one.
		"Every tool with an effect takes one extra argument, `claim`",
		"may skip the claim",
		"Unknown is never a match.",
		"Deny beats review, review beats allow.",
		"Only allow runs the tool.",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("Tool-Call Claims section is missing %q", want)
		}
	}
}

func TestGenerateCombined_ToolCallClaims_OmittedForSkillOnly(t *testing.T) {
	got := generateForClaims(t, models.CategoryClaudeSkill)
	if strings.Contains(got, "## Tool-Call Claims") {
		t.Error("a claude_skill-only selection has no tool calls to bind; the section must be omitted")
	}
}

func TestGenerateCombined_ToolCallClaims_PlacementOnlyForSelectedSDKs(t *testing.T) {
	got := generateForClaims(t, models.CategoryOpenAISDK, models.CategoryClaudeSDK)

	claude := strings.Index(got, "#### Claude Agent SDK")
	openai := strings.Index(got, "#### OpenAI Agents SDK")
	if claude < 0 || openai < 0 {
		t.Fatalf("missing placement block for a selected SDK (claude=%d openai=%d)", claude, openai)
	}
	// Table order, not caller order, so the output is byte-stable.
	if claude > openai {
		t.Error("placement blocks must follow the table order, not the order categories were passed")
	}
	if strings.Contains(got, "#### Google ADK\n\nRun the mediation step") {
		t.Error("placement block emitted for an SDK that was not selected")
	}
}

func TestGenerateCombined_ToolCallClaims_GenericPlacement(t *testing.T) {
	// raw_llm_sdk has tool calls but no SDK hook to name: it gets the
	// generic placement rather than nothing.
	got := generateForClaims(t, models.CategoryRawLLMSDK)
	if !strings.Contains(got, "## Tool-Call Claims") {
		t.Fatal("raw_llm_sdk selection must carry the Tool-Call Claims section")
	}
	if !strings.Contains(got, claimText("generic.md")) {
		t.Error("an SDK with no placement block must get the generic placement text")
	}
}

func TestClaimPlacements_HaveText(t *testing.T) {
	seen := map[models.DetectorCategory]bool{}
	for _, p := range claimPlacements {
		if seen[p.Category] {
			t.Errorf("duplicate placement for %s", p.Category)
		}
		seen[p.Category] = true
		if claimText(placementFile(p)) == "" {
			t.Errorf("placement for %s has no text file %s", p.Category, placementFile(p))
		}
	}
	for _, name := range []string{"intro.md", "schema.md", "mediate.md", "generic.md", "check.md"} {
		if claimText(name) == "" {
			t.Errorf("claim section text %s is missing", name)
		}
	}
}

func TestGenerateCombined_ToolCallClaims_SkillCompliantForEverySDK(t *testing.T) {
	// The base compliance test covers two SDKs. The placement blocks are
	// per-SDK prose, so select every category that emits one and require the
	// same empty firing set.
	var cats []models.DetectorCategory
	for _, p := range claimPlacements {
		cats = append(cats, p.Category)
	}
	cats = append(cats, models.CategoryRawLLMSDK)
	got := generateForClaims(t, cats...)
	for _, p := range claimPlacements {
		if !strings.Contains(got, "#### "+p.Heading+"\n") {
			t.Fatalf("placement for %s absent — compliance would not cover it", p.Category)
		}
	}

	fired := fireSkillRules(t, discoverGeneratedSkill(t, got))
	for _, id := range fired {
		t.Errorf("generated skill trips %s — the Tool-Call Claims prose must not self-flag", id)
	}
}
