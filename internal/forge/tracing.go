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
	// scripts/check-otel-sync.sh checks each one is still declared in the binding.
	APINames []string
}

// languageHeading is the display name of a language: its block heading when it
// has one, else the bare identifier.
func languageHeading(l models.Language) string {
	for _, blk := range languageBlocks {
		if blk.Language == l {
			return blk.Heading
		}
	}
	return string(l)
}

// frameworkRecipe is a verified per-framework delta on a language block. Its
// text lives in tracingtext/recipes/<category>_<language>_setup.md (required),
// plus optional _labels.md and _check.md.
type frameworkRecipe struct {
	Category models.DetectorCategory
	Language models.Language
	Heading  string
}

// backendBlock is an observability vendor's variant of a language block. Its
// text lives in tracingtext/backends/<vendor>_<language>_setup.md, which
// replaces <language>_setup.md, and _traps.md, which follows
// <language>_traps.md. The rest of the section is shared.
type backendBlock struct {
	Vendor   models.ObservabilityVendor
	Language models.Language
	Heading  string
}

// backendBlocks is in emit order. Detection drops a declared vendor with no
// row here, so the project gets the generic setup.
var backendBlocks = []backendBlock{
	{Vendor: models.VendorLangWatch, Language: models.LanguagePython, Heading: "LangWatch"},
	{Vendor: models.VendorLangWatch, Language: models.LanguageTypeScript, Heading: "LangWatch"},
}

func backendFile(be backendBlock, part string) string {
	return fmt.Sprintf("backends/%s_%s_%s.md", be.Vendor, be.Language, part)
}

func hasBackendBlock(v models.ObservabilityVendor, l models.Language) bool {
	for _, be := range backendBlocks {
		if be.Vendor == v && be.Language == l {
			return true
		}
	}
	return false
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
			"Run", "NewRun", "MarkStart", "NextStep", "RunID", "CanonicalFP", "ToolCall",
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
	writeLoaded(b, tracingText(name))
}

// writeLoaded emits text already read with tracingText, followed by a blank
// line. Empty text emits nothing.
func writeLoaded(b *strings.Builder, text string) {
	if text != "" {
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

// selectedRecipes groups the selected framework recipes by language.
func selectedRecipes(sel TracingSelection) map[models.Language][]frameworkRecipe {
	want := make(map[SDKLanguage]bool, len(sel.Pairs))
	for _, p := range sel.Pairs {
		want[p] = true
	}
	out := make(map[models.Language][]frameworkRecipe)
	for _, r := range frameworkRecipes {
		if want[SDKLanguage{Category: r.Category, Language: r.Language}] {
			out[r.Language] = append(out[r.Language], r)
		}
	}
	return out
}

// selectedBackends maps each language to its selected backend block. A
// language with two selected backends keeps the first in backendBlocks order.
func selectedBackends(sel TracingSelection) map[models.Language]backendBlock {
	want := make(map[TracingBackend]bool, len(sel.Backends))
	for _, be := range sel.Backends {
		want[be] = true
	}
	out := make(map[models.Language]backendBlock)
	for _, be := range backendBlocks {
		if _, taken := out[be.Language]; taken {
			continue
		}
		if want[TracingBackend{Vendor: be.Vendor, Language: be.Language}] {
			out[be.Language] = be
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
	recipes := selectedRecipes(sel)
	backends := selectedBackends(sel)

	fmt.Fprintf(b, "## Runtime Tracing\n\n")
	writeText(b, "intro.md")

	fmt.Fprintf(b, "### Step 1 — Emit spans (OpenTelemetry)\n\n")
	writeText(b, "step1.md")
	for _, blk := range blocks {
		// A backend's setup replaces the generic one, which would create a
		// second provider beside the backend's.
		if be, ok := backends[blk.Language]; ok {
			fmt.Fprintf(b, "#### %s (%s)\n\n", blk.Heading, be.Heading)
			writeText(b, backendFile(be, "setup"))
		} else {
			fmt.Fprintf(b, "#### %s\n\n", blk.Heading)
			writeText(b, string(blk.Language)+"_setup.md")
		}
		for _, r := range recipes[blk.Language] {
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
		for _, r := range recipes[blk.Language] {
			text := tracingText(recipeFile(r, "labels"))
			if text == "" {
				continue
			}
			fmt.Fprintf(b, "##### %s\n\n", r.Heading)
			writeLoaded(b, text)
		}
	}

	fmt.Fprintf(b, "### Step 3 — Choose the values\n\n")
	writeText(b, "step3.md")

	fmt.Fprintf(b, "### Step 4 — Check before moving on\n\n")
	writeText(b, "step4.md")
	for _, blk := range blocks {
		fmt.Fprintf(b, "%s:\n\n", blk.Heading)
		writeText(b, string(blk.Language)+"_traps.md")
		if be, ok := backends[blk.Language]; ok {
			writeText(b, backendFile(be, "traps"))
		}
		for _, r := range recipes[blk.Language] {
			writeText(b, recipeFile(r, "check"))
		}
	}
}
