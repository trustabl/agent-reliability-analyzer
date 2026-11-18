package forge

import (
	"flag"
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/trustabl/trustabl/internal/analysis"
	"github.com/trustabl/trustabl/internal/models"
	"github.com/trustabl/trustabl/internal/rules"
)

var update = flag.Bool("update", false, "update golden files")

func TestSeverityRank_Order(t *testing.T) {
	order := []models.Severity{
		models.SeverityCritical,
		models.SeverityHigh,
		models.SeverityMedium,
		models.SeverityLow,
	}
	for i := 1; i < len(order); i++ {
		if severityRank(order[i-1]) >= severityRank(order[i]) {
			t.Errorf("severityRank(%q) >= severityRank(%q), want strictly less",
				order[i-1], order[i])
		}
	}
}

func TestSeverityRank_Unknown(t *testing.T) {
	if severityRank("bogus") != 4 {
		t.Error("unknown severity should return 4")
	}
}

func TestFirstSentence_Basic(t *testing.T) {
	cases := []struct {
		input  string
		maxLen int
		want   string
	}{
		{"Hello world. Second sentence.", 0, "Hello world."},
		{"No period here", 0, "No period here"},
		{"A very long sentence that exceeds the limit.", 10, "A very…"},
		{"Héllo wörld. Next.", 5, "Héll…"},
		{"  Trimmed.  More text.", 0, "Trimmed."},
		{"Folded\nnewline here. Next.", 0, "Folded newline here."},
	}
	for _, c := range cases {
		got := firstSentence(c.input, c.maxLen)
		if got != c.want {
			t.Errorf("firstSentence(%q, %d) = %q, want %q", c.input, c.maxLen, got, c.want)
		}
	}
}

func TestMatchCondition_SkillPredicates(t *testing.T) {
	boolTrue := true
	cases := []struct {
		name string
		expr rules.MatchExpr
		want string // must be non-empty and not the fallback
	}{
		{
			name: "unrestricted shell",
			expr: rules.MatchExpr{SkillAllowsUnrestrictedShell: &boolTrue},
			want: "Every skill — always verify allowed-tools before emitting.",
		},
		{
			name: "dynamic exec",
			expr: rules.MatchExpr{SkillBodyHasDynamicExec: &boolTrue},
			want: "Any skill body that uses the backtick-exec or fenced-exec form.",
		},
		{
			name: "missing description",
			expr: rules.MatchExpr{SkillHasDescription: &boolTrue},
			want: "Every skill — description is required.",
		},
		{
			name: "agent specific",
			expr: rules.MatchExpr{SkillIsAgentSpecific: &boolTrue},
			want: "Only when frontmatter includes a context: or agent: binding field.",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := matchCondition(c.expr)
			if got != c.want {
				t.Errorf("matchCondition() = %q, want %q", got, c.want)
			}
		})
	}
}

func TestMatchCondition_Compound_All(t *testing.T) {
	boolTrue := true
	expr := rules.MatchExpr{
		All: []rules.MatchExpr{
			{SkillModelInvocable: &boolTrue},
			{SkillAllowsTool: []string{"Bash", "Write"}},
		},
	}
	got := matchCondition(expr)
	want := "Any skill where disable-model-invocation is not set to true, and Any skill pre-approving Bash, Write, Edit, WebFetch, or NotebookEdit in allowed-tools."
	if got != want {
		t.Errorf("matchCondition(compound all) = %q, want %q", got, want)
	}
}

func TestMatchCondition_Unknown_Fallback(t *testing.T) {
	got := matchCondition(rules.MatchExpr{})
	want := "When the condition described by this rule is met."
	if got != want {
		t.Errorf("matchCondition({}) = %q, want %q", got, want)
	}
}

func TestGenerate_GoldenFile(t *testing.T) {
	inputDir := filepath.Join("..", "..", "testdata", "forge", "claude_skill", "input")
	fsys := os.DirFS(inputDir)
	policies, err := rules.Load(fsys)
	if err != nil {
		t.Fatalf("load fixture rules: %v", err)
	}
	if len(policies) == 0 {
		t.Fatal("no policies loaded from fixture")
	}
	pf := policies[0]

	var skillRules []rules.RuleDef
	for _, r := range pf.Rules {
		if r.Scope == models.ScopeSkill {
			skillRules = append(skillRules, r)
		}
	}
	if len(skillRules) == 0 {
		t.Fatal("no skill-scoped rules found in fixture")
	}

	got := Generate(pf.Policy, skillRules, Stamp{
		Date:     "2026-01-01",
		SHA:      "0000000000000000000000000000000000000000",
		Schema:   1,
		SDKs:     []string{"claude_skill"},
		Template: TemplateVersion,
	})

	goldenPath := filepath.Join("..", "..", "testdata", "forge", "claude_skill", "expected", "SKILL.md")
	if *update {
		if err := os.MkdirAll(filepath.Dir(goldenPath), 0o755); err != nil {
			t.Fatalf("mkdir golden dir: %v", err)
		}
		if err := os.WriteFile(goldenPath, []byte(got), 0o644); err != nil {
			t.Fatalf("write golden file: %v", err)
		}
		t.Logf("updated golden file at %s", goldenPath)
		return
	}

	wantBytes, err := os.ReadFile(goldenPath)
	if err != nil {
		t.Fatalf("golden file not found at %s; run tests with -update to create it:\n  go test ./internal/forge/... -update", goldenPath)
	}
	want := string(wantBytes)
	if got != want {
		t.Errorf("Generate() output does not match golden file %s\n\nrun:\n  go test ./internal/forge/... -update\n\n%s",
			goldenPath, lineDiff(want, got))
	}
}

func TestGenerate_Deterministic(t *testing.T) {
	inputDir := filepath.Join("..", "..", "testdata", "forge", "claude_skill", "input")
	fsys := os.DirFS(inputDir)
	policies, err := rules.Load(fsys)
	if err != nil {
		t.Fatalf("load fixture rules: %v", err)
	}
	pf := policies[0]
	var skillRules []rules.RuleDef
	for _, r := range pf.Rules {
		if r.Scope == models.ScopeSkill {
			skillRules = append(skillRules, r)
		}
	}
	a := Generate(pf.Policy, skillRules, Stamp{})
	b := Generate(pf.Policy, skillRules, Stamp{})
	if a != b {
		t.Error("Generate is not deterministic: two calls with same input produced different output")
	}

	// prove order-independence: shuffled input must produce identical output
	shuffled := make([]rules.RuleDef, len(skillRules))
	copy(shuffled, skillRules)
	rand.Shuffle(len(shuffled), func(i, j int) { shuffled[i], shuffled[j] = shuffled[j], shuffled[i] })
	c := Generate(pf.Policy, shuffled, Stamp{})
	if a != c {
		t.Error("Generate is not order-independent: shuffled input produced different output")
	}
}

func TestGenerate_EmptyRules(t *testing.T) {
	meta := rules.PolicyMeta{
		ID:          "test_policy",
		Name:        "Test Policy",
		Description: "A test policy. Second sentence.",
	}
	got := Generate(meta, nil, Stamp{})
	if !strings.Contains(got, "name: test-policy") {
		t.Errorf("expected name: test-policy in output, got:\n%s", got)
	}
	if !strings.Contains(got, "allowed-tools: Read") {
		t.Error("expected allowed-tools: Read in frontmatter")
	}
	if strings.Contains(got, "## [") {
		t.Error("empty rules should produce no constraint blocks")
	}
}

func TestGenerate_SkillCompliant(t *testing.T) {
	inputDir := filepath.Join("..", "..", "testdata", "forge", "claude_skill", "input")
	fsys := os.DirFS(inputDir)
	policies, err := rules.Load(fsys)
	if err != nil {
		t.Fatalf("load fixture rules: %v", err)
	}
	pf := policies[0]
	var skillRules []rules.RuleDef
	for _, r := range pf.Rules {
		if r.Scope == models.ScopeSkill {
			skillRules = append(skillRules, r)
		}
	}
	got := Generate(pf.Policy, skillRules, Stamp{})

	// The generated skill must itself be CSKILL-compliant.
	if strings.Contains(got, "allowed-tools: Bash") {
		t.Error("generated skill must not grant bare Bash in allowed-tools")
	}
	// Only URLs in the body are a problem; the body starts after the second "---".
	parts := strings.SplitN(got, "---", 3)
	if len(parts) >= 3 && (strings.Contains(parts[2], "http://") || strings.Contains(parts[2], "https://")) {
		t.Error("generated skill body must not reference external URLs (CSKILL-020)")
	}
	// Emitted text must not contain literal trigger patterns that cause the
	// generated skill to self-flag under trustabl scan.
	if strings.Contains(got, "!`") {
		t.Error("generated skill body must not contain !` (inline-exec pattern, CSKILL-002)")
	}
	if strings.Contains(got, "ignore previous instructions") {
		t.Error("generated skill body must not contain the injection phrase (CSKILL-040)")
	}
}

func TestMatchConditionForScope_ToolScope(t *testing.T) {
	got := matchConditionForScope(models.ScopeTool, rules.MatchExpr{})
	want := "When defining a tool."
	if got != want {
		t.Errorf("matchConditionForScope(tool) = %q, want %q", got, want)
	}
}

func TestMatchConditionForScope_AgentScope(t *testing.T) {
	got := matchConditionForScope(models.ScopeAgent, rules.MatchExpr{})
	want := "When declaring an agent."
	if got != want {
		t.Errorf("matchConditionForScope(agent) = %q, want %q", got, want)
	}
}

func TestMatchConditionForScope_RepoScope(t *testing.T) {
	got := matchConditionForScope(models.ScopeRepo, rules.MatchExpr{})
	want := "For any repo using this SDK."
	if got != want {
		t.Errorf("matchConditionForScope(repo) = %q, want %q", got, want)
	}
}

func TestMatchConditionForScope_SubagentScope(t *testing.T) {
	got := matchConditionForScope(models.ScopeSubagent, rules.MatchExpr{})
	want := "When declaring a subagent."
	if got != want {
		t.Errorf("matchConditionForScope(subagent) = %q, want %q", got, want)
	}
}

func TestMatchConditionForScope_SkillScope_DelegatesToExisting(t *testing.T) {
	boolTrue := true
	expr := rules.MatchExpr{SkillAllowsUnrestrictedShell: &boolTrue}
	got := matchConditionForScope(models.ScopeSkill, expr)
	want := "Every skill — always verify allowed-tools before emitting."
	if got != want {
		t.Errorf("matchConditionForScope(skill) = %q, want %q", got, want)
	}
}

func TestGenerateCombined_GoldenFile(t *testing.T) {
	inputDir := filepath.Join("..", "..", "testdata", "forge", "multi_sdk", "input")
	fsys := os.DirFS(inputDir)
	policies, err := rules.Load(fsys)
	if err != nil {
		t.Fatalf("load fixture rules: %v", err)
	}
	if len(policies) == 0 {
		t.Fatal("no policies loaded from fixture")
	}

	stamp := PolicyStamp{
		Date:          "2026-01-01",
		RulesSHA:      "abc1234",
		SchemaVersion: 13,
		Categories:    []models.DetectorCategory{models.CategoryClaudeSDK, models.CategoryOpenAISDK},
		Template:      TemplateVersion,
	}
	got := GenerateCombined(stamp.Categories, policies, stamp, TracingSelection{}, nil)

	goldenPath := filepath.Join("..", "..", "testdata", "forge", "multi_sdk", "expected", "SKILL.md")
	if *update {
		if err := os.MkdirAll(filepath.Dir(goldenPath), 0o755); err != nil {
			t.Fatalf("mkdir golden dir: %v", err)
		}
		if err := os.WriteFile(goldenPath, []byte(got), 0o644); err != nil {
			t.Fatalf("write golden file: %v", err)
		}
		t.Logf("updated golden file at %s", goldenPath)
		return
	}

	wantBytes, err := os.ReadFile(goldenPath)
	if err != nil {
		t.Fatalf("golden file not found at %s; run:\n  go test ./internal/forge/... -update", goldenPath)
	}
	if got != string(wantBytes) {
		t.Errorf("GenerateCombined() does not match golden file %s\n\n%s",
			goldenPath, lineDiff(string(wantBytes), got))
	}
}

func TestGenerateCombined_Deterministic(t *testing.T) {
	inputDir := filepath.Join("..", "..", "testdata", "forge", "multi_sdk", "input")
	fsys := os.DirFS(inputDir)
	policies, err := rules.Load(fsys)
	if err != nil {
		t.Fatalf("load fixture rules: %v", err)
	}
	stamp := PolicyStamp{
		Date:          "2026-01-01",
		RulesSHA:      "abc1234",
		SchemaVersion: 13,
		Categories:    []models.DetectorCategory{models.CategoryClaudeSDK, models.CategoryOpenAISDK},
		Template:      TemplateVersion,
	}
	a := GenerateCombined(stamp.Categories, policies, stamp, TracingSelection{}, nil)
	b := GenerateCombined(stamp.Categories, policies, stamp, TracingSelection{}, nil)
	if a != b {
		t.Error("GenerateCombined is not deterministic")
	}
}

func TestGenerateCombined_UnknownCategorySkipped(t *testing.T) {
	inputDir := filepath.Join("..", "..", "testdata", "forge", "multi_sdk", "input")
	fsys := os.DirFS(inputDir)
	policies, err := rules.Load(fsys)
	if err != nil {
		t.Fatalf("load fixture rules: %v", err)
	}
	stamp := PolicyStamp{
		Date:          "2026-01-01",
		RulesSHA:      "abc1234",
		SchemaVersion: 13,
		Categories:    []models.DetectorCategory{models.CategoryMCP}, // not in fixture
		Template:      TemplateVersion,
	}
	got := GenerateCombined(stamp.Categories, policies, stamp, TracingSelection{}, nil)
	// should produce valid frontmatter + header but no rule sections
	if !strings.Contains(got, "name: trustabl-pre-coding") {
		t.Error("expected frontmatter in output")
	}
	if strings.Contains(got, "#### [") {
		t.Error("expected no rule blocks when category has no rules in fixture")
	}
}

func TestPolicyStamp_Line_MatchesStampFormat(t *testing.T) {
	// Both generators must emit an identical stamp format for the same data,
	// because a single ParseStamp reads both.
	ps := PolicyStamp{
		Date:          "2026-01-01",
		RulesSHA:      "abc1234",
		SchemaVersion: 13,
		Categories:    []models.DetectorCategory{models.CategoryClaudeSDK, models.CategoryOpenAISDK},
		Template:      TemplateVersion,
	}
	s := Stamp{
		Date:     "2026-01-01",
		SHA:      "abc1234",
		Schema:   13,
		SDKs:     []string{"claude_sdk", "openai_sdk"},
		Template: TemplateVersion,
	}
	if ps.Line() != s.Line() {
		t.Errorf("PolicyStamp.Line() = %q\n            Stamp.Line() = %q\nformats must be identical", ps.Line(), s.Line())
	}
	if _, ok := ParseStamp(ps.Line()); !ok {
		t.Error("ParseStamp rejected PolicyStamp.Line() output")
	}
}

// lineDiff produces a simple line-oriented diff for test failure messages.
func lineDiff(want, got string) string {
	wantLines := strings.Split(want, "\n")
	gotLines := strings.Split(got, "\n")
	var sb strings.Builder
	max := len(wantLines)
	if len(gotLines) > max {
		max = len(gotLines)
	}
	diffs := 0
	for i := 0; i < max && diffs < 20; i++ {
		var w, g string
		if i < len(wantLines) {
			w = wantLines[i]
		}
		if i < len(gotLines) {
			g = gotLines[i]
		}
		if w != g {
			fmt.Fprintf(&sb, "line %d:\n  want: %q\n  got:  %q\n", i+1, w, g)
			diffs++
		}
	}
	if diffs == 0 && want != got {
		fmt.Fprintf(&sb, "(content differs but all compared lines match — likely trailing newline or length difference)")
	}
	return sb.String()
}

func TestGenerateCombined_EmitsApplyLoop(t *testing.T) {
	inputDir := filepath.Join("..", "..", "testdata", "forge", "multi_sdk", "input")
	policies, err := rules.Load(os.DirFS(inputDir))
	if err != nil {
		t.Fatalf("load fixture rules: %v", err)
	}
	stamp := PolicyStamp{
		Date:          "2026-01-01",
		RulesSHA:      "abc1234",
		SchemaVersion: 13,
		Categories:    []models.DetectorCategory{models.CategoryClaudeSDK, models.CategoryOpenAISDK},
		Template:      TemplateVersion,
	}
	got := GenerateCombined(stamp.Categories, policies, stamp, TracingSelection{}, nil)

	if !strings.Contains(got, "## How to Apply These Constraints") {
		t.Fatal("generated skill is missing the apply-loop section")
	}
	// The loop is the method; the rule blocks are the material it operates on.
	loopIdx := strings.Index(got, "## How to Apply These Constraints")
	sdkIdx := strings.Index(got, "### Tool Rules")
	if sdkIdx >= 0 && loopIdx > sdkIdx {
		t.Error("apply loop must be emitted before the per-SDK rule sections")
	}
	// The scope guardrail keeps the section from drifting into general advice.
	if !strings.Contains(got, "It is not a general code-review") {
		t.Error("apply loop must carry its scope-limiting closing line")
	}
}

func TestGenerateCombined_SkillCompliant(t *testing.T) {
	// GenerateCombined is the only production path, and it emits hand-authored
	// prose. That prose is itself a SKILL.md, so Trustabl scans it.
	//
	// This test does not hand-mirror a chosen subset of skill rules — that is
	// exactly how CSKILL-086 shipped unnoticed. It runs the real discovery and
	// evaluates EVERY skill-scoped rule in the rules fixture against the
	// generated output, then asserts the firing set exactly.
	inputDir := filepath.Join("..", "..", "testdata", "forge", "multi_sdk", "input")
	policies, err := rules.Load(os.DirFS(inputDir))
	if err != nil {
		t.Fatalf("load fixture rules: %v", err)
	}
	stamp := PolicyStamp{
		Date:          "2026-01-01",
		RulesSHA:      "abc1234",
		SchemaVersion: 13,
		Categories:    []models.DetectorCategory{models.CategoryClaudeSDK, models.CategoryOpenAISDK},
		Template:      TemplateVersion,
	}
	// The generic setup blocks and the backend blocks replace each other, so
	// one selection cannot emit them all: check both.
	for name, sel := range map[string]TracingSelection{
		"generic":  allTracingSelection(),
		"backends": allBackendsSelection(),
	} {
		t.Run(name, func(t *testing.T) {
			got := GenerateCombined(stamp.Categories, policies, stamp, sel, nil)

			// Anchor: this test guards the hand-authored loop prose. If that prose is
			// absent the assertions below would pass vacuously, so fail loudly rather
			// than silently guarding nothing.
			if !strings.Contains(got, "## How to Apply These Constraints") {
				t.Fatal("apply-loop section absent — the compliance assertions below would pass vacuously")
			}
			if !strings.Contains(got, "## Runtime Tracing") {
				t.Fatal("runtime tracing section absent — the compliance assertions below would not cover it")
			}

			skill := discoverGeneratedSkill(t, got)
			fired := fireSkillRules(t, skill)

			// Exact-set assertion, in both directions. want is empty: the generated
			// skill must trip NO skill-scoped rule at all.
			//
			// Anything added here needs a written justification for why forge's own
			// output may ship a finding. "It's only LOW" is not one — CSKILL-085 sat
			// here on exactly that reasoning until the description template grew its
			// purpose clause, which cost one clause and cleared it.
			want := map[string]bool{}

			for _, id := range fired {
				if !want[id] {
					t.Errorf("generated skill trips %s — the emitted prose must not self-flag", id)
				}
			}
			firedSet := make(map[string]bool, len(fired))
			for _, id := range fired {
				firedSet[id] = true
			}
			for id := range want {
				if !firedSet[id] {
					t.Errorf("%s no longer fires; remove it from want so this test keeps asserting an exact set", id)
				}
			}
		})
	}
}

// discoverGeneratedSkill writes body to a temp repo as SKILL.md and runs the
// real skill discovery over it, so the test evaluates the same SkillDef a
// `trustabl scan` of the generated file would produce.
func discoverGeneratedSkill(t *testing.T, body string) models.SkillDef {
	t.Helper()
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "SKILL.md"), []byte(body), 0o600); err != nil {
		t.Fatalf("write generated skill: %v", err)
	}
	skills := analysis.DiscoverSkills(models.ScanManifest{
		RepoRoot:      root,
		MarkdownFiles: []string{"SKILL.md"},
	})
	if len(skills) != 1 {
		t.Fatalf("discovery found %d skills in the generated output, want 1", len(skills))
	}
	return skills[0]
}

// fireSkillRules evaluates every skill-scoped rule in the rules fixture against
// skill and returns the IDs that match, sorted.
func fireSkillRules(t *testing.T, skill models.SkillDef) []string {
	t.Helper()
	fixture := filepath.Join("..", "..", "testdata", "rules-fixture")
	policies, err := rules.Load(os.DirFS(fixture))
	if err != nil {
		t.Fatalf("load rules fixture: %v", err)
	}
	var skillRules, fired []string
	for _, pf := range policies {
		for _, r := range pf.Rules {
			if r.Scope != models.ScopeSkill {
				continue
			}
			skillRules = append(skillRules, r.ID)
			if r.Match.EvaluateSkill(skill, models.RepoInventory{}) {
				fired = append(fired, r.ID)
			}
		}
	}
	// Guard against a fixture path change silently evaluating nothing.
	if len(skillRules) == 0 {
		t.Fatal("no skill-scoped rules loaded from the fixture — this test would pass vacuously")
	}
	sort.Strings(fired)
	return fired
}

// languagePack is a google_adk pack with one rule per language plus one with
// no language, the shape the real pack has.
func languagePack() []rules.PolicyFile {
	rule := func(id string, scope models.Scope, lang models.Language) rules.RuleDef {
		return rules.RuleDef{ID: id, Title: id + " title", Scope: scope, Severity: models.SeverityHigh,
			Confidence: 0.9, Language: lang, Category: models.CategoryGoogleADK,
			Explanation: id + " why.", Fix: id + " fix."}
	}
	return []rules.PolicyFile{{
		Policy: rules.PolicyMeta{ID: "google_adk", Name: "ADK agent safety", Category: models.CategoryGoogleADK},
		Rules: []rules.RuleDef{
			rule("ADK-PY", models.ScopeTool, models.LanguagePython),
			rule("ADK-TS", models.ScopeAgent, models.LanguageTypeScript),
			rule("ADK-JS", models.ScopeTool, models.LanguageJavaScript),
			rule("ADK-GO", models.ScopeTool, models.LanguageGo),
			rule("ADK-ANY", models.ScopeRepo, ""),
		},
	}}
}

func TestGenerateCombined_RuleLanguagesFilter(t *testing.T) {
	cats := []models.DetectorCategory{models.CategoryGoogleADK}
	stamp := PolicyStamp{Categories: cats}
	cases := []struct {
		name       string
		langs      RuleLanguages
		kept, gone []string
	}{
		{"nil keeps every rule", nil, []string{"ADK-PY", "ADK-TS", "ADK-JS", "ADK-GO", "ADK-ANY"}, nil},
		{"go", RuleLanguages{models.CategoryGoogleADK: {models.LanguageGo}},
			[]string{"ADK-GO", "ADK-ANY"}, []string{"ADK-PY", "ADK-TS", "ADK-JS"}},
		{"typescript keeps javascript", RuleLanguages{models.CategoryGoogleADK: {models.LanguageTypeScript}},
			[]string{"ADK-TS", "ADK-JS", "ADK-ANY"}, []string{"ADK-PY", "ADK-GO"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := GenerateCombined(cats, languagePack(), stamp, TracingSelection{}, c.langs)
			for _, id := range c.kept {
				if !strings.Contains(got, "["+id+"]") {
					t.Errorf("missing %s", id)
				}
			}
			for _, id := range c.gone {
				if strings.Contains(got, "["+id+"]") {
					t.Errorf("%s must be filtered out", id)
				}
			}
			if strings.Contains(got, "Trustabl has no") {
				t.Errorf("every listed language has rules; no coverage note expected")
			}
		})
	}
}

func TestGenerateCombined_NotesALanguageWithNoRules(t *testing.T) {
	cats := []models.DetectorCategory{models.CategoryGoogleADK}
	pack := languagePack()
	pack[0].Rules = pack[0].Rules[:3] // drop ADK-GO and ADK-ANY: nothing targets Go
	pack[0].Rules = append(pack[0].Rules, rules.RuleDef{ID: "ADK-ANY", Title: "t", Scope: models.ScopeRepo,
		Severity: models.SeverityLow, Confidence: 0.5, Category: models.CategoryGoogleADK, Explanation: "w.", Fix: "f."})
	got := GenerateCombined(cats, pack, PolicyStamp{Categories: cats}, TracingSelection{},
		RuleLanguages{models.CategoryGoogleADK: {models.LanguageGo}})
	if !strings.Contains(got, "Trustabl has no Go rules for this SDK yet") {
		t.Errorf("want a coverage note naming Go, got:\n%s", got)
	}
	if !strings.Contains(got, "[ADK-ANY]") {
		t.Errorf("the language-independent rule must still be listed")
	}
	if strings.Contains(got, "[ADK-PY]") {
		t.Errorf("Python rule must be filtered out")
	}
}
