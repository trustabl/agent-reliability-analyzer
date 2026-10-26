package rules

import (
	"testing"

	"github.com/trustabl/trustabl/internal/models"
)

func TestRepoRuleDetector_OtelLabelsFindingLocation(t *testing.T) {
	yes := true
	rule := RuleDef{
		ID: "OBS-TEST", Title: "t", Scope: models.ScopeRepo, Severity: "low", Confidence: 0.5,
		Match: MatchExpr{All: []MatchExpr{{RepoOtelLabelsAttemptConstant: &yes}}},
	}
	inv := models.RepoInventory{OtelLabelsSignals: []models.OtelLabelsSignal{
		{Kind: models.OtelLabelsToolSpan, File: "app/agent.py", StartLine: 12, EndLine: 14, AttemptConst: true},
		{Kind: models.OtelLabelsToolSpan, File: "tests/test_agent.py", StartLine: 3, EndLine: 3, AttemptConst: true, TestOrigin: true},
	}}
	got := repoRuleDetector{rule: rule}.Detect(models.RepoProfile{}, inv)
	if len(got) != 1 {
		t.Fatalf("got %d findings, want 1", len(got))
	}
	if got[0].FilePath != "app/agent.py" || got[0].StartLine != 12 || got[0].EndLine != 14 {
		t.Errorf("location = %s:%d-%d, want app/agent.py:12-14", got[0].FilePath, got[0].StartLine, got[0].EndLine)
	}
}

// A repo rule backed by none of the correctness predicates keeps its empty
// location, so existing reports do not change.
func TestRepoRuleDetector_OtherRepoRulesKeepEmptyLocation(t *testing.T) {
	yes := true
	rule := RuleDef{
		ID: "OBS-TEST", Title: "t", Scope: models.ScopeRepo, Severity: "low", Confidence: 0.5,
		Match: MatchExpr{RepoOtelLabelsInitialized: &yes},
	}
	inv := models.RepoInventory{OtelLabelsSignals: []models.OtelLabelsSignal{
		{Kind: models.OtelLabelsRunStart, File: "app/agent.py", StartLine: 7, EndLine: 7},
	}}
	got := repoRuleDetector{rule: rule}.Detect(models.RepoProfile{}, inv)
	if len(got) != 1 || got[0].FilePath != "" || got[0].StartLine != 0 {
		t.Errorf("got %+v, want one finding with an empty location", got)
	}
}
