package forge

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/trustabl/trustabl/internal/models"
	"github.com/trustabl/trustabl/internal/rules"
)

// multiSDKFixture loads the two-pack rule fixture and a fixed stamp, the same
// inputs the existing GenerateCombined tests use.
func multiSDKFixture(t *testing.T) ([]rules.PolicyFile, PolicyStamp) {
	t.Helper()
	inputDir := filepath.Join("..", "..", "testdata", "forge", "multi_sdk", "input")
	policies, err := rules.Load(os.DirFS(inputDir))
	if err != nil {
		t.Fatalf("load fixture rules: %v", err)
	}
	return policies, PolicyStamp{
		Date:          "2026-01-01",
		RulesSHA:      "abc1234",
		SchemaVersion: 13,
		Categories:    []models.DetectorCategory{models.CategoryClaudeSDK, models.CategoryOpenAISDK},
		Template:      TemplateVersion,
	}
}

// allTracingSelection selects every language block and every recipe, so a
// test that scans the output covers every piece of emitted text.
func allTracingSelection() TracingSelection {
	var sel TracingSelection
	for _, blk := range languageBlocks {
		sel.Languages = append(sel.Languages, blk.Language)
	}
	for _, r := range frameworkRecipes {
		sel.Pairs = append(sel.Pairs, SDKLanguage{Category: r.Category, Language: r.Language})
	}
	return sel
}

func TestGenerateCombined_NoTracingSelection_OmitsSection(t *testing.T) {
	policies, stamp := multiSDKFixture(t)
	got := GenerateCombined(stamp.Categories, policies, stamp, TracingSelection{})
	if strings.Contains(got, "## Runtime Tracing") {
		t.Error("the section must be omitted when no language is selected")
	}
}

func TestGenerateCombined_UnsupportedLanguage_OmitsSection(t *testing.T) {
	policies, stamp := multiSDKFixture(t)
	sel := TracingSelection{Languages: []models.Language{models.LanguageRust}}
	got := GenerateCombined(stamp.Categories, policies, stamp, sel)
	if strings.Contains(got, "## Runtime Tracing") {
		t.Error("a language with no block must not produce the section")
	}
}

func TestGenerateCombined_Python_EmitsRuntimeTracing(t *testing.T) {
	policies, stamp := multiSDKFixture(t)
	sel := TracingSelection{Languages: []models.Language{models.LanguagePython}}
	got := GenerateCombined(stamp.Categories, policies, stamp, sel)

	loop := strings.Index(got, "## How to Apply These Constraints")
	tracing := strings.Index(got, "## Runtime Tracing")
	firstSDK := strings.Index(got, "\n---\n\n## ")
	if loop < 0 || tracing < 0 || firstSDK < 0 {
		t.Fatalf("missing a section: loop=%d tracing=%d firstSDK=%d", loop, tracing, firstSDK)
	}
	if !(loop < tracing && tracing < firstSDK) {
		t.Errorf("order must be apply loop, tracing, SDK rules: loop=%d tracing=%d firstSDK=%d", loop, tracing, firstSDK)
	}
	for _, want := range []string{
		"### Step 1 — Emit spans (OpenTelemetry)",
		"### Step 2 — Add the Trustabl labels (agent-reliability-otel-labels)",
		"### Step 3 — Choose the values",
		"### Step 4 — Check before moving on",
		"#### Python",
		"pip install agent-reliability-otel-labels",
		"mark_tool_span(",
		"`trustabl.tool.input_fp`",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("output is missing %q", want)
		}
	}
	if strings.Contains(got, "#### TypeScript") || strings.Contains(got, "#### Go") {
		t.Error("only the selected language may appear")
	}
}

func TestGenerateCombined_Tracing_InputOrderDoesNotMatter(t *testing.T) {
	policies, stamp := multiSDKFixture(t)
	all := allTracingSelection()

	reversed := TracingSelection{}
	for i := len(all.Languages) - 1; i >= 0; i-- {
		reversed.Languages = append(reversed.Languages, all.Languages[i])
	}
	for i := len(all.Pairs) - 1; i >= 0; i-- {
		reversed.Pairs = append(reversed.Pairs, all.Pairs[i])
	}

	a := GenerateCombined(stamp.Categories, policies, stamp, all)
	b := GenerateCombined(stamp.Categories, policies, stamp, reversed)
	if a != b {
		t.Errorf("output depends on selection order:\n%s", lineDiff(a, b))
	}
	if c := GenerateCombined(stamp.Categories, policies, stamp, all); a != c {
		t.Error("two runs over the same input differ")
	}
}

func TestTracingText_RequiredFilesPresent(t *testing.T) {
	for _, name := range []string{"intro.md", "step1.md", "step1_confirm.md", "step2.md", "step3.md", "step4.md"} {
		if tracingText(name) == "" {
			t.Errorf("tracingtext/%s is missing or empty", name)
		}
	}
	for _, blk := range languageBlocks {
		for _, part := range []string{"setup", "labels", "traps"} {
			name := string(blk.Language) + "_" + part + ".md"
			if tracingText(name) == "" {
				t.Errorf("tracingtext/%s is missing or empty", name)
			}
		}
	}
	for _, r := range frameworkRecipes {
		if tracingText(recipeFile(r, "setup")) == "" {
			t.Errorf("tracingtext/%s is missing or empty", recipeFile(r, "setup"))
		}
	}
}

func TestGenerateCombined_Tracing_GoldenFiles(t *testing.T) {
	cases := []struct {
		name string
		sel  TracingSelection
	}{
		{"tracing_python", TracingSelection{
			Languages: []models.Language{models.LanguagePython},
			Pairs:     []SDKLanguage{{Category: models.CategoryOpenAISDK, Language: models.LanguagePython}},
		}},
		// OpenAI Agents in TypeScript is the one recipe with a check part.
		{"tracing_typescript", TracingSelection{
			Languages: []models.Language{models.LanguageTypeScript},
			Pairs:     []SDKLanguage{{Category: models.CategoryOpenAISDK, Language: models.LanguageTypeScript}},
		}},
		// Google ADK in Go has a labels part.
		{"tracing_go", TracingSelection{
			Languages: []models.Language{models.LanguageGo},
			Pairs:     []SDKLanguage{{Category: models.CategoryGoogleADK, Language: models.LanguageGo}},
		}},
		{"tracing_mixed", TracingSelection{
			Languages: []models.Language{models.LanguagePython, models.LanguageTypeScript},
			Pairs: []SDKLanguage{
				{Category: models.CategoryOpenAISDK, Language: models.LanguagePython},
				{Category: models.CategoryVercelAI, Language: models.LanguageTypeScript},
			},
		}},
	}
	policies, stamp := multiSDKFixture(t)
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := GenerateCombined(stamp.Categories, policies, stamp, c.sel)
			goldenPath := filepath.Join("..", "..", "testdata", "forge", c.name, "expected", "SKILL.md")
			if *update {
				if err := os.MkdirAll(filepath.Dir(goldenPath), 0o755); err != nil {
					t.Fatalf("mkdir golden dir: %v", err)
				}
				if err := os.WriteFile(goldenPath, []byte(got), 0o644); err != nil {
					t.Fatalf("write golden file: %v", err)
				}
				return
			}
			want, err := os.ReadFile(goldenPath)
			if err != nil {
				t.Fatalf("golden file not found at %s; run:\n  go test ./internal/forge/... -update", goldenPath)
			}
			if got != string(want) {
				t.Errorf("output does not match %s\n\n%s", goldenPath, lineDiff(string(want), got))
			}
		})
	}
}

func TestLanguageBlocks_FixedOrder(t *testing.T) {
	want := []models.Language{models.LanguagePython, models.LanguageTypeScript, models.LanguageGo}
	if len(languageBlocks) != len(want) {
		t.Fatalf("languageBlocks has %d entries, want %d", len(languageBlocks), len(want))
	}
	for i, l := range want {
		if languageBlocks[i].Language != l {
			t.Errorf("languageBlocks[%d] = %q, want %q", i, languageBlocks[i].Language, l)
		}
	}
}

func TestGenerateCombined_LanguageBlocksAreIndependent(t *testing.T) {
	policies, stamp := multiSDKFixture(t)
	cases := []struct {
		lang    models.Language
		present string
		absent  []string
	}{
		{models.LanguagePython, "pip install agent-reliability-otel-labels", []string{"@trustabl/agent-reliability-otel-labels", "otellabels."}},
		{models.LanguageTypeScript, "npm install @trustabl/agent-reliability-otel-labels", []string{"pip install", "otellabels."}},
		{models.LanguageGo, "go get github.com/trustabl/agent-reliability-otel-labels/go", []string{"pip install", "npm install"}},
	}
	for _, c := range cases {
		got := GenerateCombined(stamp.Categories, policies, stamp,
			TracingSelection{Languages: []models.Language{c.lang}})
		if !strings.Contains(got, c.present) {
			t.Errorf("%s: missing %q", c.lang, c.present)
		}
		for _, a := range c.absent {
			if strings.Contains(got, a) {
				t.Errorf("%s: must not contain %q", c.lang, a)
			}
		}
	}
}

func TestFrameworkRecipes_Table(t *testing.T) {
	want := []SDKLanguage{
		{Category: models.CategoryAutoGen, Language: models.LanguagePython},
		{Category: models.CategoryCrewAI, Language: models.LanguagePython},
		{Category: models.CategoryGoogleADK, Language: models.LanguagePython},
		{Category: models.CategoryLangChain, Language: models.LanguagePython},
		{Category: models.CategoryMCP, Language: models.LanguagePython},
		{Category: models.CategoryOpenAISDK, Language: models.LanguagePython},
		{Category: models.CategoryPydanticAI, Language: models.LanguagePython},
		{Category: models.CategoryOpenAISDK, Language: models.LanguageTypeScript},
		{Category: models.CategoryVercelAI, Language: models.LanguageTypeScript},
		{Category: models.CategoryGoogleADK, Language: models.LanguageGo},
	}
	if len(frameworkRecipes) != len(want) {
		t.Fatalf("frameworkRecipes has %d entries, want %d", len(frameworkRecipes), len(want))
	}
	for i, w := range want {
		got := SDKLanguage{Category: frameworkRecipes[i].Category, Language: frameworkRecipes[i].Language}
		if got != w {
			t.Errorf("frameworkRecipes[%d] = %v, want %v", i, got, w)
		}
	}
}

func TestFrameworkRecipes_NoOrphanFiles(t *testing.T) {
	known := map[string]bool{}
	for _, r := range frameworkRecipes {
		for _, part := range []string{"setup", "labels", "check"} {
			known[strings.TrimPrefix(recipeFile(r, part), "recipes/")] = true
		}
	}
	entries, err := tracingFS.ReadDir("tracingtext/recipes")
	if err != nil {
		t.Fatalf("read recipes dir: %v", err)
	}
	for _, e := range entries {
		if !known[e.Name()] {
			t.Errorf("tracingtext/recipes/%s belongs to no entry in frameworkRecipes", e.Name())
		}
	}
}

func TestGenerateCombined_RecipeOnlyForItsPair(t *testing.T) {
	policies, stamp := multiSDKFixture(t)
	// Python OpenAI Agents plus TypeScript Vercel AI: the OpenAI Agents
	// TypeScript recipe must not appear.
	sel := TracingSelection{
		Languages: []models.Language{models.LanguagePython, models.LanguageTypeScript},
		Pairs: []SDKLanguage{
			{Category: models.CategoryOpenAISDK, Language: models.LanguagePython},
			{Category: models.CategoryVercelAI, Language: models.LanguageTypeScript},
		},
	}
	got := GenerateCombined(stamp.Categories, policies, stamp, sel)
	for _, want := range []string{"OpenAIAgentsInstrumentor", "registerTelemetry"} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q", want)
		}
	}
	for _, absent := range []string{"OpenInferenceTracingProcessor", "CrewAIInstrumentor"} {
		if strings.Contains(got, absent) {
			t.Errorf("must not contain %q", absent)
		}
	}
}

type otelKeys struct {
	Attributes      []string            `json:"attributes"`
	Events          map[string]string   `json:"events"`
	EventAttributes []string            `json:"event_attributes"`
	Enums           map[string][]string `json:"enums"`
}

func loadOtelKeys(t *testing.T) otelKeys {
	t.Helper()
	path := filepath.Join("..", "..", "testdata", "forge", "otel", "otel-spec-keys.json")
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read vendored otel-spec-keys.json: %v", err)
	}
	var k otelKeys
	if err := json.Unmarshal(b, &k); err != nil {
		t.Fatalf("parse vendored otel-spec-keys.json: %v", err)
	}
	return k
}

func fullTracingSection() string {
	var b strings.Builder
	emitRuntimeTracing(&b, allTracingSelection())
	return b.String()
}

var trustablKeyRe = regexp.MustCompile(`trustabl\.[a-z0-9_]+(?:\.[a-z0-9_]+)*`)

func TestRuntimeTracing_EveryKeyIsInTheSpec(t *testing.T) {
	k := loadOtelKeys(t)
	known := map[string]bool{}
	for _, a := range k.Attributes {
		known[a] = true
	}
	for _, e := range k.Events {
		known[e] = true
	}
	for _, a := range k.EventAttributes {
		known[a] = true
	}

	found := map[string]bool{}
	for _, m := range trustablKeyRe.FindAllString(fullTracingSection(), -1) {
		found[m] = true
		if !known[m] {
			t.Errorf("the section emits %q, which is not in agent-reliability-otel-labels's keys.json", m)
		}
	}
	// Every attribute the library sets must be placed somewhere by Step 2.
	for _, a := range k.Attributes {
		if !found[a] {
			t.Errorf("the section never mentions %q", a)
		}
	}
}

func TestRuntimeTracing_EnumListsMatchTheSpec(t *testing.T) {
	k := loadOtelKeys(t)
	step3 := tracingText("step3.md")
	for _, name := range []string{"side_effect", "error_class", "exit_reason"} {
		values := k.Enums[name]
		if len(values) == 0 {
			t.Fatalf("otel-spec-keys.json has no enum %q", name)
		}
		quoted := make([]string, len(values))
		for i, v := range values {
			quoted[i] = "`" + v + "`"
		}
		// The closing period anchors the list, so an extra trailing value fails.
		want := "one of " + strings.Join(quoted, ", ") + "."
		if !strings.Contains(step3, want) {
			t.Errorf("step3.md must list %s exactly as %q", name, want)
		}
	}
}

// languageLabelText is every piece of Step 2 text for one language: the
// block's own labels file plus each recipe's.
func languageLabelText(lang models.Language) string {
	text := tracingText(string(lang) + "_labels.md")
	for _, r := range frameworkRecipes {
		if r.Language == lang {
			text += "\n" + tracingText(recipeFile(r, "labels"))
		}
	}
	return text
}

func TestTracingAPINames_Golden(t *testing.T) {
	var lines []string
	for _, blk := range languageBlocks {
		text := languageLabelText(blk.Language)
		if len(blk.APINames) == 0 {
			t.Errorf("%s declares no API names", blk.Language)
		}
		for _, name := range blk.APINames {
			if !strings.Contains(text, name) {
				t.Errorf("%s declares %q but its Step 2 text never uses it", blk.Language, name)
			}
			lines = append(lines, string(blk.Language)+"\t"+name)
		}
	}
	sort.Strings(lines)
	got := strings.Join(lines, "\n") + "\n"

	goldenPath := filepath.Join("..", "..", "testdata", "forge", "otel", "api-names.txt")
	if *update {
		if err := os.WriteFile(goldenPath, []byte(got), 0o644); err != nil {
			t.Fatalf("write %s: %v", goldenPath, err)
		}
		return
	}
	want, err := os.ReadFile(goldenPath)
	if err != nil {
		t.Fatalf("%s not found; run:\n  go test ./internal/forge/... -update", goldenPath)
	}
	if got != string(want) {
		t.Errorf("api-names.txt is stale; run go test ./internal/forge/... -update\n\n%s", lineDiff(string(want), got))
	}
}

var (
	goAPINameRe     = regexp.MustCompile(`otellabels\.([A-Z][A-Za-z0-9]*)`)
	pythonImportRe  = regexp.MustCompile(`(?m)^from agent_reliability_otel_labels import (.+)$`)
	tsImportRe      = regexp.MustCompile(`import \{([^}]*)\} from "@trustabl/agent-reliability-otel-labels"`)
	importedNameSep = regexp.MustCompile(`[\s,]+`)
)

// usedAPINames extracts the agent-reliability-otel-labels identifiers a language's Step 2
// text uses: every otellabels.X in Go, and the imported names in Python
// and TypeScript.
func usedAPINames(lang models.Language, text string) []string {
	var names []string
	switch lang {
	case models.LanguageGo:
		for _, m := range goAPINameRe.FindAllStringSubmatch(text, -1) {
			names = append(names, m[1])
		}
	case models.LanguagePython:
		for _, m := range pythonImportRe.FindAllStringSubmatch(text, -1) {
			names = append(names, importedNameSep.Split(strings.TrimSpace(m[1]), -1)...)
		}
	case models.LanguageTypeScript:
		for _, m := range tsImportRe.FindAllStringSubmatch(text, -1) {
			names = append(names, importedNameSep.Split(strings.TrimSpace(m[1]), -1)...)
		}
	}
	return names
}

// TestTracingAPINames_Complete is the other direction of the golden test
// above: an identifier the text uses must be declared, or
// scripts/check-otel-sync.sh never checks it against the binding.
func TestTracingAPINames_Complete(t *testing.T) {
	for _, blk := range languageBlocks {
		declared := map[string]bool{}
		for _, name := range blk.APINames {
			declared[name] = true
		}
		used := usedAPINames(blk.Language, languageLabelText(blk.Language))
		if len(used) == 0 {
			t.Errorf("%s: no agent-reliability-otel-labels identifier found in its Step 2 text", blk.Language)
		}
		seen := map[string]bool{}
		for _, name := range used {
			if name == "" || seen[name] {
				continue
			}
			seen[name] = true
			if !declared[name] {
				t.Errorf("%s: Step 2 text uses %q, which is not in its APINames", blk.Language, name)
			}
		}
	}
}
