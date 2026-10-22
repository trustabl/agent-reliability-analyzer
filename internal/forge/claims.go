package forge

import (
	"embed"
	"fmt"
	"strings"

	"github.com/trustabl/trustabl/internal/models"
)

// claimFS holds the text of the Tool-Call Claims section: the EBTE-style
// claim schema, the mediation step that checks it, and where that step goes
// in each SDK. Kept as files for the same reason as tracingFS.
//
// Authoring constraint: same as tracingFS. The section ends up in a SKILL.md
// that Trustabl scans, and several skill rules match plain substrings (one of
// them matches "log" anywhere, including inside "catalog").
// TestGenerateCombined_ToolCallClaims_SkillCompliantForEverySDK requires an
// empty firing set with every placement block emitted.
//
//go:embed claimtext
var claimFS embed.FS

// claimPlacement names where the mediation step goes for one SDK. Its text
// lives in claimtext/sdk/<category>.md.
type claimPlacement struct {
	Category models.DetectorCategory
	Heading  string
}

// claimPlacements is in emit order. A selected category with no entry here
// gets the generic placement text instead.
var claimPlacements = []claimPlacement{
	{Category: models.CategoryClaudeSDK, Heading: "Claude Agent SDK"},
	{Category: models.CategoryOpenAISDK, Heading: "OpenAI Agents SDK"},
	{Category: models.CategoryGoogleADK, Heading: "Google ADK"},
	{Category: models.CategoryLangChain, Heading: "LangChain / LangGraph"},
	{Category: models.CategoryMCP, Heading: "MCP"},
	{Category: models.CategoryCrewAI, Heading: "CrewAI"},
	{Category: models.CategoryPydanticAI, Heading: "Pydantic AI"},
	{Category: models.CategoryVercelAI, Heading: "Vercel AI SDK"},
	{Category: models.CategoryAutoGen, Heading: "AutoGen"},
}

// claimNoToolCalls are the categories that carry no tool-calling agent code
// of their own, so selecting them alone does not warrant the section and they
// never ask for the generic placement text.
var claimNoToolCalls = map[models.DetectorCategory]bool{
	models.CategoryClaudeSkill:   true,
	models.CategoryObservability: true,
	models.CategoryAgentConfig:   true,
	models.CategoryOpenShell:     true,
}

func placementFile(p claimPlacement) string {
	return "sdk/" + string(p.Category) + ".md"
}

// claimText returns an embedded text file without its trailing newline, or
// "" when the file does not exist.
func claimText(name string) string {
	b, err := claimFS.ReadFile("claimtext/" + name)
	if err != nil {
		return ""
	}
	return strings.TrimRight(string(b), "\n")
}

func writeClaimText(b *strings.Builder, name string) {
	writeLoaded(b, claimText(name))
}

// emitToolCallClaims writes the Tool-Call Claims section: every tool call
// with an effect carries a typed ToolInvocationExplanation, and a mediation
// step outside the model checks it against server-held facts before any
// effect. It is a no-op when no selected category has tool calls to bind.
//
// Placement blocks are filtered from claimPlacements rather than taken in
// caller order, so emit order never depends on the order categories were
// passed.
func emitToolCallClaims(b *strings.Builder, categories []models.DetectorCategory) {
	want := make(map[models.DetectorCategory]bool, len(categories))
	generic := false
	for _, c := range categories {
		if claimNoToolCalls[c] {
			continue
		}
		want[c] = true
	}
	if len(want) == 0 {
		return
	}
	var placements []claimPlacement
	known := make(map[models.DetectorCategory]bool, len(claimPlacements))
	for _, p := range claimPlacements {
		known[p.Category] = true
		if want[p.Category] {
			placements = append(placements, p)
		}
	}
	for c := range want {
		if !known[c] {
			generic = true
		}
	}

	fmt.Fprintf(b, "## Tool-Call Claims\n\n")
	writeClaimText(b, "intro.md")

	fmt.Fprintf(b, "### The claim\n\n")
	writeClaimText(b, "schema.md")

	fmt.Fprintf(b, "### The mediation step\n\n")
	writeClaimText(b, "mediate.md")

	fmt.Fprintf(b, "### Where the step goes\n\n")
	writeClaimText(b, "where.md")
	for _, p := range placements {
		fmt.Fprintf(b, "#### %s\n\n", p.Heading)
		writeClaimText(b, placementFile(p))
	}
	if generic {
		fmt.Fprintf(b, "#### Other tool loops\n\n")
		writeClaimText(b, "generic.md")
	}

	fmt.Fprintf(b, "### Check before moving on\n\n")
	writeClaimText(b, "step_check.md")
	writeClaimText(b, "check.md")
}
