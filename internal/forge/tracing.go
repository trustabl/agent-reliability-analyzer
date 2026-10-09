package forge

import (
	"embed"
	"fmt"
	"strings"

	"github.com/trustabl/trustabl/internal/models"
)

// tracingFS holds the text of the Runtime Tracing section. It is prose and
// code samples, kept as files so a backtick needs no escaping and a reviewer
// reads markdown as markdown.
//
// Authoring constraint: this text ends up in a SKILL.md that Trustabl scans.
// TestGenerateCombined_SkillCompliant evaluates every skill-scoped rule in the
// rules fixture against it and requires an empty firing set. Several of those
// rules match plain substrings, so read that test's failure before rewording.
//
//go:embed tracingtext
var tracingFS embed.FS

// languageBlock is the per-language part of the section. Its text lives in
// tracingtext/<language>_setup.md, _labels.md and _traps.md.
type languageBlock struct {
	Language models.Language
	Heading  string
	// APINames are the agent-reliability-otel-labels identifiers the block's text uses.
	// scripts/check-otel-sync.sh checks each one still exists in the binding.
	APINames []string
}

// frameworkRecipe is a verified per-framework delta on a language block. Its
// text lives in tracingtext/recipes/<category>_<language>_setup.md (required),
// plus optional _labels.md and _check.md.
type frameworkRecipe struct {
	Category models.DetectorCategory
	Language models.Language
	Heading  string
}

// languageBlocks is in emit order.
var languageBlocks = []languageBlock{
	{
		Language: models.LanguagePython,
		Heading:  "Python",
		APINames: []string{
			"Run", "mark_start", "next_step", "run_id", "canonical_fp",
			"mark_tool_span", "mark_run_end", "mark_handoff", "bind_authority", "Binding",
		},
	},
	{
		Language: models.LanguageTypeScript,
		Heading:  "TypeScript",
		APINames: []string{
			"Run", "markStart", "nextStep", "runId", "canonicalFp",
			"markToolSpan", "markRunEnd", "markHandoff", "bindAuthority",
		},
	},
	{
		Language: models.LanguageGo,
		Heading:  "Go",
		APINames: []string{
			"NewRun", "MarkStart", "NextStep", "RunID", "CanonicalFP", "ToolCall",
			"MarkToolSpan", "MarkRunEnd", "MarkHandoff", "BindAuthority", "FinalAnswer",
			"Read", "Error", "Timeout", "Class4xx", "Class5xx",
		},
	},
}

// frameworkRecipes is in emit order: language order, then heading.
var frameworkRecipes = []frameworkRecipe{
	{Category: models.CategoryAutoGen, Language: models.LanguagePython, Heading: "AutoGen"},
	{Category: models.CategoryCrewAI, Language: models.LanguagePython, Heading: "CrewAI"},
	{Category: models.CategoryGoogleADK, Language: models.LanguagePython, Heading: "Google ADK"},
	{Category: models.CategoryLangChain, Language: models.LanguagePython, Heading: "LangChain / LangGraph"},
	{Category: models.CategoryMCP, Language: models.LanguagePython, Heading: "MCP"},
	{Category: models.CategoryOpenAISDK, Language: models.LanguagePython, Heading: "OpenAI Agents SDK"},
	{Category: models.CategoryPydanticAI, Language: models.LanguagePython, Heading: "Pydantic AI"},
	{Category: models.CategoryOpenAISDK, Language: models.LanguageTypeScript, Heading: "OpenAI Agents SDK"},
	{Category: models.CategoryVercelAI, Language: models.LanguageTypeScript, Heading: "Vercel AI SDK"},
	{Category: models.CategoryGoogleADK, Language: models.LanguageGo, Heading: "Google ADK"},
}

// tracingText returns an embedded text file without its trailing newline, or
// "" when the file does not exist. Optional recipe parts rely on the "".
func tracingText(name string) string {
	b, err := tracingFS.ReadFile("tracingtext/" + name)
	if err != nil {
		return ""
	}
	return strings.TrimRight(string(b), "\n")
}

func recipeFile(r frameworkRecipe, part string) string {
	return fmt.Sprintf("recipes/%s_%s_%s.md", r.Category, r.Language, part)
}

// writeText emits one text file followed by a blank line. A missing file
// emits nothing.
func writeText(b *strings.Builder, name string) {
	if text := tracingText(name); text != "" {
		fmt.Fprintf(b, "%s\n\n", sanitizeEmittedText(text))
	}
}

// selectedBlocks and selectedRecipes filter the tables rather than sort the
// selection, so emit order never depends on the order the caller passed.
func selectedBlocks(sel TracingSelection) []languageBlock {
	want := make(map[models.Language]bool, len(sel.Languages))
	for _, l := range sel.Languages {
		want[l] = true
	}
	var out []languageBlock
	for _, blk := range languageBlocks {
		if want[blk.Language] {
			out = append(out, blk)
		}
	}
	return out
}

func selectedRecipes(sel TracingSelection, lang models.Language) []frameworkRecipe {
	want := make(map[SDKLanguage]bool, len(sel.Pairs))
	for _, p := range sel.Pairs {
		want[p] = true
	}
	var out []frameworkRecipe
	for _, r := range frameworkRecipes {
		if r.Language == lang && want[SDKLanguage{Category: r.Category, Language: r.Language}] {
			out = append(out, r)
		}
	}
	return out
}

// emitRuntimeTracing writes the Runtime Tracing section: how to make the agent
// emit OpenTelemetry spans, then how to label them with
// agent-reliability-otel-labels. It is a no-op when the selection names no
// language with a block.
func emitRuntimeTracing(b *strings.Builder, sel TracingSelection) {
	blocks := selectedBlocks(sel)
	if len(blocks) == 0 {
		return
	}

	fmt.Fprintf(b, "## Runtime Tracing\n\n")
	writeText(b, "intro.md")

	fmt.Fprintf(b, "### Step 1 — Emit spans (OpenTelemetry)\n\n")
	writeText(b, "step1.md")
	for _, blk := range blocks {
		fmt.Fprintf(b, "#### %s\n\n", blk.Heading)
		writeText(b, string(blk.Language)+"_setup.md")
		for _, r := range selectedRecipes(sel, blk.Language) {
			fmt.Fprintf(b, "##### %s\n\n", r.Heading)
			writeText(b, recipeFile(r, "setup"))
		}
	}
	fmt.Fprintf(b, "#### Confirm\n\n")
	writeText(b, "step1_confirm.md")

	fmt.Fprintf(b, "### Step 2 — Add the Trustabl labels (agent-reliability-otel-labels)\n\n")
	writeText(b, "step2.md")
	for _, blk := range blocks {
		fmt.Fprintf(b, "#### %s\n\n", blk.Heading)
		writeText(b, string(blk.Language)+"_labels.md")
		for _, r := range selectedRecipes(sel, blk.Language) {
			if tracingText(recipeFile(r, "labels")) == "" {
				continue
			}
			fmt.Fprintf(b, "##### %s\n\n", r.Heading)
			writeText(b, recipeFile(r, "labels"))
		}
	}

	fmt.Fprintf(b, "### Step 3 — Choose the values\n\n")
	writeText(b, "step3.md")

	fmt.Fprintf(b, "### Step 4 — Check before moving on\n\n")
	writeText(b, "step4.md")
	for _, blk := range blocks {
		fmt.Fprintf(b, "%s:\n\n", blk.Heading)
		writeText(b, string(blk.Language)+"_traps.md")
		for _, r := range selectedRecipes(sel, blk.Language) {
			writeText(b, recipeFile(r, "check"))
		}
	}
}
