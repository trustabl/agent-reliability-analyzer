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

func TestRepoRuleDetector_ReasoningFindingLocations(t *testing.T) {
	yes, no := true, false
	cases := []struct {
		name    string
		match   MatchExpr
		signals []models.OtelLabelsSignal
		file    string
		line    int
	}{
		{"unhashed value", MatchExpr{RepoOtelLabelsReasoningUnhashed: &yes}, []models.OtelLabelsSignal{
			{Kind: models.OtelLabelsAttrLiteral, Key: "trustabl.cot.text", ValueUnhashed: true, File: "tests/test_agent.py", StartLine: 5, EndLine: 5, TestOrigin: true},
			{Kind: models.OtelLabelsAttrLiteral, Key: "trustabl.cot.text", ValueUnhashed: true, File: "app/span.py", StartLine: 12, EndLine: 12},
		}, "app/span.py", 12},
		{"reasoning option", MatchExpr{All: []MatchExpr{
			{RepoOtelLabelsImplemented: &yes},
			{RepoModelReasoningEnabled: &yes},
			{RepoOtelLabelsReasoningLabelled: &no},
		}}, []models.OtelLabelsSignal{
			{Kind: models.OtelLabelsRunEnd, File: "app/agent.py", StartLine: 2, EndLine: 2},
			{Kind: models.OtelLabelsReasoningConfig, Key: "thinking", File: "tests/test_agent.py", StartLine: 3, EndLine: 3, TestOrigin: true},
			{Kind: models.OtelLabelsReasoningConfig, Key: "thinking", File: "app/llm.py", StartLine: 8, EndLine: 9},
		}, "app/llm.py", 8},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rule := RuleDef{ID: "OBS-TEST", Title: "t", Scope: models.ScopeRepo, Severity: "low", Confidence: 0.5, Match: tc.match}
			got := repoRuleDetector{rule: rule}.Detect(models.RepoProfile{}, models.RepoInventory{OtelLabelsSignals: tc.signals})
			if len(got) != 1 {
				t.Fatalf("got %d findings, want 1", len(got))
			}
			if got[0].FilePath != tc.file || got[0].StartLine != tc.line {
				t.Errorf("location = %s:%d, want %s:%d", got[0].FilePath, got[0].StartLine, tc.file, tc.line)
			}
		})
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
