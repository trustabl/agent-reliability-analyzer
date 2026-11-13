package forge

import (
	"context"
	"sort"
	"strings"

	"github.com/trustabl/trustabl/internal/ingestion"
	"github.com/trustabl/trustabl/internal/models"
	"github.com/trustabl/trustabl/internal/rules"
)

// depCategoryMap maps dep-file package names (as returned by ingestion.Recon's
// SDKDeps slice) to the detector category that audits them.
// Mirrors scanner.depNameToSDK + scanner.sdkToCategory in one table.
var depCategoryMap = map[string]models.DetectorCategory{
	"claude-agent-sdk": models.CategoryClaudeSDK,
	"openai-agents":    models.CategoryOpenAISDK,
	"google-adk":       models.CategoryGoogleADK,
	"mcp":              models.CategoryMCP,
	"langchain":        models.CategoryLangChain,
	"crewai":           models.CategoryCrewAI,
	"pydantic-ai":      models.CategoryPydanticAI,
	"vercel-ai":        models.CategoryVercelAI,
	"autogen":          models.CategoryAutoGen,
	// Recon-only name: there is no Go ADK discovery, so scanner.depNameToSDK
	// does not know it. Forge maps it so a Go ADK repo gets its tracing recipe.
	"google-adk-go": models.CategoryGoogleADK,
}

// tracingLanguages are the languages agent-reliability-otel-labels ships a
// binding for, in the fixed order the Runtime Tracing section emits them.
var tracingLanguages = []models.Language{
	models.LanguagePython, models.LanguageTypeScript, models.LanguageGo,
}

// SDKLanguage is one SDK category as declared in one language.
type SDKLanguage struct {
	Category models.DetectorCategory
	Language models.Language
}

// TracingBackend is an observability vendor's SDK declared in one language.
// A selected backend replaces the generic OpenTelemetry setup for its
// language, so the project keeps one tracer provider.
type TracingBackend struct {
	Vendor   models.ObservabilityVendor
	Language models.Language
}

// Detection is what forge learns from a target's dependency manifests.
type Detection struct {
	Categories []models.DetectorCategory // sorted, deduplicated
	Languages  []models.Language         // tracing languages only, fixed order
	Pairs      []SDKLanguage             // language order, then category
	// Unplaced are categories declared in a manifest whose language forge
	// does not know (composer.json, say), so their languages are incomplete.
	Unplaced []models.DetectorCategory // sorted, deduplicated
	Backends []TracingBackend          // language order, then vendor
}

// TracingSelection is what the Runtime Tracing section is generated for. The
// zero value selects nothing, and the section is then omitted.
type TracingSelection struct {
	Languages []models.Language
	Pairs     []SDKLanguage
	Backends  []TracingBackend
}

// backendManifests are the manifests a backend may be declared in: the
// hand-edited ones, as for repo_observability_declared. A lock file lists
// transitive packages nobody chose.
var backendManifests = map[string]bool{
	"pyproject.toml":   true,
	"requirements.txt": true,
	"Pipfile":          true,
	"package.json":     true,
}

func tracingLanguageRank(l models.Language) int {
	for i, t := range tracingLanguages {
		if t == l {
			return i
		}
	}
	return -1
}

func orderedLanguages(set map[models.Language]bool) []models.Language {
	out := make([]models.Language, 0, len(set))
	for _, l := range tracingLanguages {
		if set[l] {
			out = append(out, l)
		}
	}
	return out
}

func orderedBackends(set map[TracingBackend]bool) []TracingBackend {
	out := make([]TracingBackend, 0, len(set))
	for b := range set {
		out = append(out, b)
	}
	sort.Slice(out, func(i, j int) bool {
		ri, rj := tracingLanguageRank(out[i].Language), tracingLanguageRank(out[j].Language)
		if ri != rj {
			return ri < rj
		}
		return out[i].Vendor < out[j].Vendor
	})
	return out
}

func orderedPairs(set map[SDKLanguage]bool) []SDKLanguage {
	out := make([]SDKLanguage, 0, len(set))
	for p := range set {
		out = append(out, p)
	}
	sort.Slice(out, func(i, j int) bool {
		ri, rj := tracingLanguageRank(out[i].Language), tracingLanguageRank(out[j].Language)
		if ri != rj {
			return ri < rj
		}
		return out[i].Category < out[j].Category
	})
	return out
}

// Detect resolves target (local path) and returns the detector categories for
// SDKs found in dep manifests, plus the language each was declared in. The
// language comes from the manifest that declared the SDK, not from file
// extensions, so a Python agent with a JavaScript frontend is Python only.
func Detect(ctx context.Context, target string) (Detection, error) {
	src, err := ingestion.Resolve(ctx, target, nil)
	if err != nil {
		return Detection{}, err
	}
	defer src.Cleanup()

	profile, err := ingestion.Recon(src, nil)
	if err != nil {
		return Detection{}, err
	}
	return detectionFromRecon(profile.SDKDeps, profile.ObsDeps), nil
}

func detectionFromRecon(deps []models.SDKDep, obs []models.ObsDep) Detection {
	cats := make(map[models.DetectorCategory]bool)
	unplaced := make(map[models.DetectorCategory]bool)
	langs := make(map[models.Language]bool)
	pairs := make(map[SDKLanguage]bool)
	for _, dep := range deps {
		cat, ok := depCategoryMap[dep.Name]
		if !ok {
			continue
		}
		cats[cat] = true
		lang, ok := ingestion.ManifestLanguage(dep.Source)
		if !ok || tracingLanguageRank(lang) < 0 {
			unplaced[cat] = true
			continue
		}
		langs[lang] = true
		pairs[SDKLanguage{Category: cat, Language: lang}] = true
	}

	backends := make(map[TracingBackend]bool)
	for _, d := range obs {
		if !backendManifests[d.Source] {
			continue
		}
		lang, ok := ingestion.ManifestLanguage(d.Source)
		if !ok || !hasBackendBlock(d.Vendor, lang) {
			continue
		}
		backends[TracingBackend{Vendor: d.Vendor, Language: lang}] = true
	}

	return Detection{
		Categories: sortedCategories(cats),
		Languages:  orderedLanguages(langs),
		Pairs:      orderedPairs(pairs),
		Unplaced:   sortedCategories(unplaced),
		Backends:   orderedBackends(backends),
	}
}

func sortedCategories(set map[models.DetectorCategory]bool) []models.DetectorCategory {
	out := make([]models.DetectorCategory, 0, len(set))
	for c := range set {
		out = append(out, c)
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

// DetectCategories returns only the categories from Detect. Returns a sorted,
// deduplicated slice; empty (not an error) when no known SDK is found.
func DetectCategories(ctx context.Context, target string) ([]models.DetectorCategory, error) {
	det, err := Detect(ctx, target)
	if err != nil {
		return nil, err
	}
	return det.Categories, nil
}

// MergeCategories merges auto-detected and explicitly specified categories,
// returning a deduplicated, sorted slice.
func MergeCategories(detected, explicit []models.DetectorCategory) []models.DetectorCategory {
	seen := make(map[models.DetectorCategory]bool)
	for _, c := range detected {
		seen[c] = true
	}
	for _, c := range explicit {
		seen[c] = true
	}
	out := make([]models.DetectorCategory, 0, len(seen))
	for c := range seen {
		out = append(out, c)
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

// ParseTracingLanguage maps a --lang value to a tracing language. javascript
// is accepted as typescript: the two share one binding and one block.
func ParseTracingLanguage(s string) (models.Language, bool) {
	switch models.Language(strings.ToLower(strings.TrimSpace(s))) {
	case models.LanguagePython:
		return models.LanguagePython, true
	case models.LanguageTypeScript, models.LanguageJavaScript:
		return models.LanguageTypeScript, true
	case models.LanguageGo:
		return models.LanguageGo, true
	}
	return "", false
}

// TracingApplies reports whether any category audits agent code, which is
// what the Runtime Tracing section instruments. Agent Skills (claude_skill)
// are SKILL.md files, not running agents, so a skill-only selection has none.
func TracingApplies(categories []models.DetectorCategory) bool {
	for _, c := range categories {
		if c != models.CategoryClaudeSkill {
			return true
		}
	}
	return false
}

// SelectTracing decides what the Runtime Tracing section covers. Detected
// pairs are kept as detected, so a repo with SDK A in Python and SDK B in
// TypeScript never gets A's TypeScript recipe. An explicit value widens the
// selection only for what detection did not already place: a category with
// no detected pair pairs with every selected language, and a language that
// was not detected pairs with every category. Repeating on the command line
// something detection already found changes nothing. A detected backend is
// kept when its language is selected; --lang never adds one. When
// TracingApplies is false the selection is empty, whatever languages were
// detected or given.
func SelectTracing(det Detection, categories, explicitCats []models.DetectorCategory, explicitLangs []models.Language) TracingSelection {
	if !TracingApplies(categories) {
		return TracingSelection{}
	}
	langs := make(map[models.Language]bool)
	detectedLang := make(map[models.Language]bool)
	for _, l := range det.Languages {
		langs[l] = true
		detectedLang[l] = true
	}
	for _, l := range explicitLangs {
		if tracingLanguageRank(l) >= 0 {
			langs[l] = true
		}
	}

	pairs := make(map[SDKLanguage]bool)
	placed := make(map[models.DetectorCategory]bool)
	for _, p := range det.Pairs {
		pairs[p] = true
		placed[p.Category] = true
	}
	for _, c := range explicitCats {
		if placed[c] {
			continue
		}
		for l := range langs {
			pairs[SDKLanguage{Category: c, Language: l}] = true
		}
	}
	for _, l := range explicitLangs {
		if tracingLanguageRank(l) < 0 || detectedLang[l] {
			continue
		}
		for _, c := range categories {
			pairs[SDKLanguage{Category: c, Language: l}] = true
		}
	}
	backends := make(map[TracingBackend]bool)
	for _, be := range det.Backends {
		if langs[be.Language] {
			backends[be] = true
		}
	}
	return TracingSelection{
		Languages: orderedLanguages(langs),
		Pairs:     orderedPairs(pairs),
		Backends:  orderedBackends(backends),
	}
}

// RuleLanguages lists, per category, every language forge knows the SDK is
// used in. GenerateCombined drops a rule whose language is not listed, using
// the scanner's own language gate, so a Go-only ADK repo is not handed Python
// and TypeScript rules. A category with no entry keeps every rule, and so
// does a nil RuleLanguages.
type RuleLanguages map[models.DetectorCategory][]models.Language

// SelectRuleLanguages decides RuleLanguages. It drops rules only when it knows
// every language a category is used in, so it leaves a category unlisted when
// one of its manifests has no known language, or when the category comes from
// --policy alone with no --lang. An explicit --lang adds its language to every
// listed category; it can only bring rules back, never remove them.
func SelectRuleLanguages(det Detection, explicitCats []models.DetectorCategory, explicitLangs []models.Language) RuleLanguages {
	sets := make(map[models.DetectorCategory]map[models.Language]bool)
	add := func(c models.DetectorCategory, l models.Language) {
		if sets[c] == nil {
			sets[c] = make(map[models.Language]bool)
		}
		sets[c][l] = true
	}
	for _, p := range det.Pairs {
		add(p.Category, p.Language)
	}
	for _, c := range det.Unplaced {
		delete(sets, c)
	}
	unplaced := make(map[models.DetectorCategory]bool, len(det.Unplaced))
	for _, c := range det.Unplaced {
		unplaced[c] = true
	}
	for _, c := range explicitCats {
		if sets[c] != nil || unplaced[c] {
			continue
		}
		for _, l := range explicitLangs {
			add(c, l)
		}
	}
	for _, l := range explicitLangs {
		for c := range sets {
			sets[c][l] = true
		}
	}

	out := make(RuleLanguages, len(sets))
	for c, set := range sets {
		out[c] = orderedLanguages(set)
	}
	return out
}

// keeps reports whether a rule of category cat is emitted.
func (rl RuleLanguages) keeps(cat models.DetectorCategory, r rules.RuleDef) bool {
	langs, ok := rl[cat]
	if !ok {
		return true
	}
	for _, l := range langs {
		if r.AppliesToLanguage(l) {
			return true
		}
	}
	return false
}
