package rules_test

import (
	"errors"
	"testing"

	"github.com/trustabl/trustabl/internal/models"
	"github.com/trustabl/trustabl/internal/rules"
)

// TestLoadFor_ZeroRulePackErrors covers the "engine never runs rule-less"
// contract: a pack that decodes and is schema-compatible but carries zero
// rules (an empty/truncated checkout) must hard-fail with ErrNoRulesInPack,
// never silently yield an empty registry that produces a clean report.
func TestLoadFor_ZeroRulePackErrors(t *testing.T) {
	const emptyPolicy = `
policy:
  id: empty
  name: Empty
  category: claude_sdk
  description: A pack with no rules.
rules: []
`
	fsys := makeFS(map[string]string{"empty.yaml": emptyPolicy})
	if _, _, err := rules.LoadFor(fsys, []models.SDK{models.SDKClaudeAgentSDK}); !errors.Is(err, rules.ErrNoRulesInPack) {
		t.Fatalf("LoadFor on a zero-rule pack: want ErrNoRulesInPack, got %v", err)
	}
}

// TestLoadFor_SDKWithNoMatchingPackSucceeds guards against the zero-rules check
// over-firing. The pack is non-empty but its only rule targets claude_sdk while
// the repo uses Google ADK — a legitimate "repo's SDKs have no matching pack"
// situation. The unfiltered rule count is > 0, so LoadFor must succeed and
// return a (possibly detector-light) registry, not ErrNoRulesInPack.
func TestLoadFor_SDKWithNoMatchingPackSucceeds(t *testing.T) {
	fsys := makeFS(map[string]string{"test.yaml": validYAML})
	reg, _, err := rules.LoadFor(fsys, []models.SDK{models.SDKGoogleADK})
	if err != nil {
		t.Fatalf("unexpected error for non-empty pack with non-matching SDK: %v", err)
	}
	if reg == nil {
		t.Fatal("expected a non-nil registry")
	}
}

// TestLoadFor_AllRulesForwardIncompatible covers the branch where a pack has
// rules but every one references a predicate this build does not understand (a
// rules repo wholly newer than the binary). LoadFor must report
// ErrAllRulesIncompatible — distinct from ErrNoRulesInPack (an empty pack) — so
// the CLI can tell the user to upgrade rather than to refresh the pack.
func TestLoadFor_AllRulesForwardIncompatible(t *testing.T) {
	const futurePack = `
policy:
  id: future
  name: Future
  category: claude_sdk
  description: Every rule uses a newer predicate.
rules:
  - id: FUT-001
    title: future rule
    scope: tool
    severity: high
    confidence: 0.9
    applies_to: [claude_sdk_tool]
    match:
      has_quantum_flux: true
    explanation: x
    fix: y
`
	fsys := makeFS(map[string]string{"future.yaml": futurePack})
	_, skipped, err := rules.LoadFor(fsys, []models.SDK{models.SDKClaudeAgentSDK})
	if !errors.Is(err, rules.ErrAllRulesIncompatible) {
		t.Fatalf("err = %v, want ErrAllRulesIncompatible", err)
	}
	if len(skipped) != 1 || skipped[0] != "FUT-001" {
		t.Errorf("skipped = %v, want [FUT-001]", skipped)
	}
}

// The observability pack is cross-SDK: its category is not an SDK, so the
// SDK-driven pack gate would drop it entirely and OBS-001..003 could never
// fire. It must load unconditionally (like openshell and claude_skill); its
// rules stay gated by their own applies_to and predicates.
func TestLoadFor_LoadsObservabilityPackUnconditionally(t *testing.T) {
	reg, _, err := rules.LoadFor(fixtureFS(t), []models.SDK{models.SDKOpenAIAgents})
	if err != nil {
		t.Fatalf("LoadFor: %v", err)
	}
	profile := models.RepoProfile{Languages: []models.Language{models.LanguagePython}}
	inv := models.RepoInventory{
		SDKsDetected: []models.SDK{models.SDKOpenAIAgents},
		Manifest:     models.ScanManifest{PythonFiles: []string{"app.py"}},
		ObservabilitySignals: []models.ObservabilitySignal{
			{Vendor: models.VendorLangfuse, Kind: models.ObsSignalImport, File: "app.py", StartLine: 1},
		},
	}
	var sawOBS bool
	for _, f := range reg.Run(profile, inv, nil, nil) {
		if f.RuleID == "OBS-001" {
			sawOBS = true
		}
	}
	if !sawOBS {
		t.Fatal("OBS-001 did not fire: the observability pack was not loaded by LoadFor")
	}
}
