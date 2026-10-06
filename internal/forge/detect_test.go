package forge

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/trustabl/trustabl/internal/models"
)

func TestDetectCategories_PyprojectOpenAI(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "pyproject.toml"),
		[]byte(`[project]\ndependencies = ["openai-agents>=0.1"]\n`), 0o644); err != nil {
		t.Fatal(err)
	}
	cats, err := DetectCategories(context.Background(), dir)
	if err != nil {
		t.Fatalf("DetectCategories: %v", err)
	}
	if len(cats) != 1 || cats[0] != models.CategoryOpenAISDK {
		t.Errorf("got %v, want [openai_sdk]", cats)
	}
}

func TestDetectCategories_PackageJSON_VercelAI(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "package.json"),
		[]byte(`{"dependencies":{"ai":"^3.0.0"}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	cats, err := DetectCategories(context.Background(), dir)
	if err != nil {
		t.Fatalf("DetectCategories: %v", err)
	}
	if len(cats) != 1 || cats[0] != models.CategoryVercelAI {
		t.Errorf("got %v, want [vercel_ai]", cats)
	}
}

func TestDetectCategories_NoManifest(t *testing.T) {
	dir := t.TempDir()
	cats, err := DetectCategories(context.Background(), dir)
	if err != nil {
		t.Fatalf("DetectCategories: %v", err)
	}
	if len(cats) != 0 {
		t.Errorf("expected empty, got %v", cats)
	}
}

func TestDetectCategories_MultipleManifests(t *testing.T) {
	dir := t.TempDir()
	// Python + TS in same repo
	if err := os.WriteFile(filepath.Join(dir, "pyproject.toml"),
		[]byte(`[project]\ndependencies = ["crewai>=0.1"]\n`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "package.json"),
		[]byte(`{"dependencies":{"@ai-sdk/openai":"^0.0.1"}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	cats, err := DetectCategories(context.Background(), dir)
	if err != nil {
		t.Fatalf("DetectCategories: %v", err)
	}
	// crewai + vercel_ai, sorted
	if len(cats) != 2 {
		t.Fatalf("got %d categories, want 2: %v", len(cats), cats)
	}
	if cats[0] != models.CategoryCrewAI || cats[1] != models.CategoryVercelAI {
		t.Errorf("got %v, want [crewai vercel_ai]", cats)
	}
}

func TestMergeCategories_Dedup(t *testing.T) {
	detected := []models.DetectorCategory{models.CategoryOpenAISDK, models.CategoryMCP}
	explicit := []models.DetectorCategory{models.CategoryOpenAISDK, models.CategoryClaudeSkill}
	got := MergeCategories(detected, explicit)
	want := []models.DetectorCategory{models.CategoryClaudeSkill, models.CategoryMCP, models.CategoryOpenAISDK}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("index %d: got %q, want %q", i, got[i], want[i])
		}
	}
}

func TestMergeCategories_EmptyDetected(t *testing.T) {
	got := MergeCategories(nil, []models.DetectorCategory{models.CategoryClaudeSDK})
	if len(got) != 1 || got[0] != models.CategoryClaudeSDK {
		t.Errorf("got %v, want [claude_sdk]", got)
	}
}

func writeManifest(t *testing.T, dir, name, body string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestDetect_PairsFollowTheDeclaringManifest(t *testing.T) {
	dir := t.TempDir()
	writeManifest(t, dir, "pyproject.toml", "[project]\ndependencies = [\"openai-agents>=0.1\"]\n")
	writeManifest(t, dir, "package.json", `{"dependencies":{"ai":"^7.0.0"}}`)

	det, err := Detect(context.Background(), dir)
	if err != nil {
		t.Fatalf("Detect: %v", err)
	}
	wantLangs := []models.Language{models.LanguagePython, models.LanguageTypeScript}
	if len(det.Languages) != 2 || det.Languages[0] != wantLangs[0] || det.Languages[1] != wantLangs[1] {
		t.Errorf("Languages = %v, want %v", det.Languages, wantLangs)
	}
	want := []SDKLanguage{
		{Category: models.CategoryOpenAISDK, Language: models.LanguagePython},
		{Category: models.CategoryVercelAI, Language: models.LanguageTypeScript},
	}
	if len(det.Pairs) != len(want) {
		t.Fatalf("Pairs = %v, want %v (no cross product)", det.Pairs, want)
	}
	for i := range want {
		if det.Pairs[i] != want[i] {
			t.Errorf("Pairs[%d] = %v, want %v", i, det.Pairs[i], want[i])
		}
	}
}

func TestDetect_GoADK(t *testing.T) {
	dir := t.TempDir()
	writeManifest(t, dir, "go.mod", "module example.com/agent\n\ngo 1.23\n\nrequire google.golang.org/adk v1.7.0\n")

	det, err := Detect(context.Background(), dir)
	if err != nil {
		t.Fatalf("Detect: %v", err)
	}
	if len(det.Categories) != 1 || det.Categories[0] != models.CategoryGoogleADK {
		t.Errorf("Categories = %v, want [google_adk]", det.Categories)
	}
	want := SDKLanguage{Category: models.CategoryGoogleADK, Language: models.LanguageGo}
	if len(det.Pairs) != 1 || det.Pairs[0] != want {
		t.Errorf("Pairs = %v, want [%v]", det.Pairs, want)
	}
}

func TestDetect_UnsupportedManifestHasCategoryButNoLanguage(t *testing.T) {
	dir := t.TempDir()
	writeManifest(t, dir, "composer.json", `{"require":{"mcp/sdk":"^1.0"}}`)

	det, err := Detect(context.Background(), dir)
	if err != nil {
		t.Fatalf("Detect: %v", err)
	}
	if len(det.Categories) != 1 || det.Categories[0] != models.CategoryMCP {
		t.Errorf("Categories = %v, want [mcp]", det.Categories)
	}
	if len(det.Languages) != 0 || len(det.Pairs) != 0 {
		t.Errorf("PHP is not a tracing language: Languages=%v Pairs=%v", det.Languages, det.Pairs)
	}
}

func TestParseTracingLanguage(t *testing.T) {
	cases := []struct {
		in   string
		want models.Language
		ok   bool
	}{
		{"python", models.LanguagePython, true},
		{" TypeScript ", models.LanguageTypeScript, true},
		{"javascript", models.LanguageTypeScript, true},
		{"go", models.LanguageGo, true},
		{"rust", "", false},
		{"", "", false},
	}
	for _, c := range cases {
		got, ok := ParseTracingLanguage(c.in)
		if got != c.want || ok != c.ok {
			t.Errorf("ParseTracingLanguage(%q) = (%q, %v), want (%q, %v)", c.in, got, ok, c.want, c.ok)
		}
	}
}

func TestSelectTracing_DetectedOnly(t *testing.T) {
	det := Detection{
		Categories: []models.DetectorCategory{models.CategoryOpenAISDK, models.CategoryVercelAI},
		Languages:  []models.Language{models.LanguagePython, models.LanguageTypeScript},
		Pairs: []SDKLanguage{
			{Category: models.CategoryOpenAISDK, Language: models.LanguagePython},
			{Category: models.CategoryVercelAI, Language: models.LanguageTypeScript},
		},
	}
	sel := SelectTracing(det, det.Categories, nil, nil)
	if len(sel.Pairs) != 2 {
		t.Fatalf("Pairs = %v, want exactly the two detected pairs", sel.Pairs)
	}
	for _, p := range sel.Pairs {
		if p.Category == models.CategoryOpenAISDK && p.Language == models.LanguageTypeScript {
			t.Errorf("cross product leaked: %v", p)
		}
	}
}

func TestSelectTracing_ExplicitLanguageAppliesToEveryCategory(t *testing.T) {
	// Brand-new repo: nothing detected, both given on the command line.
	cats := []models.DetectorCategory{models.CategoryGoogleADK}
	sel := SelectTracing(Detection{}, cats, cats, []models.Language{models.LanguageGo})
	if len(sel.Languages) != 1 || sel.Languages[0] != models.LanguageGo {
		t.Errorf("Languages = %v, want [go]", sel.Languages)
	}
	want := SDKLanguage{Category: models.CategoryGoogleADK, Language: models.LanguageGo}
	if len(sel.Pairs) != 1 || sel.Pairs[0] != want {
		t.Errorf("Pairs = %v, want [%v]", sel.Pairs, want)
	}
}

func TestSelectTracing_ExplicitCategoryAppliesToEveryLanguage(t *testing.T) {
	det := Detection{
		Categories: []models.DetectorCategory{models.CategoryCrewAI},
		Languages:  []models.Language{models.LanguagePython},
		Pairs:      []SDKLanguage{{Category: models.CategoryCrewAI, Language: models.LanguagePython}},
	}
	explicit := []models.DetectorCategory{models.CategoryMCP}
	cats := MergeCategories(det.Categories, explicit)
	sel := SelectTracing(det, cats, explicit, nil)
	want := []SDKLanguage{
		{Category: models.CategoryCrewAI, Language: models.LanguagePython},
		{Category: models.CategoryMCP, Language: models.LanguagePython},
	}
	if len(sel.Pairs) != len(want) {
		t.Fatalf("Pairs = %v, want %v", sel.Pairs, want)
	}
	for i := range want {
		if sel.Pairs[i] != want[i] {
			t.Errorf("Pairs[%d] = %v, want %v", i, sel.Pairs[i], want[i])
		}
	}
}

func TestSelectTracing_LanguagesInFixedOrder(t *testing.T) {
	sel := SelectTracing(Detection{}, nil, nil,
		[]models.Language{models.LanguageGo, models.LanguagePython, models.LanguageGo})
	want := []models.Language{models.LanguagePython, models.LanguageGo}
	if len(sel.Languages) != 2 || sel.Languages[0] != want[0] || sel.Languages[1] != want[1] {
		t.Errorf("Languages = %v, want %v (deduped, python before go)", sel.Languages, want)
	}
}

// mixedDetection is a repo with OpenAI Agents in Python and Vercel AI in
// TypeScript.
func mixedDetection() Detection {
	return Detection{
		Categories: []models.DetectorCategory{models.CategoryOpenAISDK, models.CategoryVercelAI},
		Languages:  []models.Language{models.LanguagePython, models.LanguageTypeScript},
		Pairs: []SDKLanguage{
			{Category: models.CategoryOpenAISDK, Language: models.LanguagePython},
			{Category: models.CategoryVercelAI, Language: models.LanguageTypeScript},
		},
	}
}

func TestSelectTracing_RedundantPolicyDoesNotWiden(t *testing.T) {
	det := mixedDetection()
	explicit := []models.DetectorCategory{models.CategoryOpenAISDK}
	sel := SelectTracing(det, MergeCategories(det.Categories, explicit), explicit, nil)
	if len(sel.Pairs) != 2 {
		t.Fatalf("Pairs = %v, want only the two detected pairs", sel.Pairs)
	}
	for _, p := range sel.Pairs {
		if p.Category == models.CategoryOpenAISDK && p.Language == models.LanguageTypeScript {
			t.Errorf("repeating a detected category must not add %v", p)
		}
	}
}

func TestSelectTracing_RedundantLangDoesNotWiden(t *testing.T) {
	det := mixedDetection()
	sel := SelectTracing(det, det.Categories, nil, []models.Language{models.LanguageTypeScript})
	if len(sel.Pairs) != 2 {
		t.Fatalf("Pairs = %v, want only the two detected pairs", sel.Pairs)
	}
}

func TestSelectTracing_NewLangStillWidens(t *testing.T) {
	det := mixedDetection()
	sel := SelectTracing(det, det.Categories, nil, []models.Language{models.LanguageGo})
	want := map[SDKLanguage]bool{
		{Category: models.CategoryOpenAISDK, Language: models.LanguageGo}: true,
		{Category: models.CategoryVercelAI, Language: models.LanguageGo}:  true,
	}
	found := 0
	for _, p := range sel.Pairs {
		if want[p] {
			found++
		}
	}
	if found != 2 || len(sel.Pairs) != 4 {
		t.Errorf("Pairs = %v, want the two detected pairs plus both categories in go", sel.Pairs)
	}
}
