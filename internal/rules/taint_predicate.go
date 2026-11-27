package rules

import (
	"fmt"
	"regexp"
	"slices"
	"strings"

	"github.com/trustabl/trustabl/internal/models"
)

// taintSources and taintSinks are the closed value sets of tool_source_reaches_sink.
var (
	taintSources = []string{string(models.SourceParam)}
	taintSinks   = []string{
		models.SinkShell, models.SinkCodeExec, models.SinkFileWrite, models.SinkPathIO, models.SinkSQLExecute,
		models.SinkVectorFilter, models.SinkStoreNamespace, models.SinkMemoryScopeKey,
	}
)

// ToolSourceReachesSinkExpr fires when the one-function taint pass found a flow
// from a listed source to a listed sink in the tool's handler (see
// analysis.AttachTaintFlows). ParamNames and ParamRegex optionally restrict the
// source parameter by name.
type ToolSourceReachesSinkExpr struct {
	Sources    []string `yaml:"sources"`
	Sinks      []string `yaml:"sinks"`
	ParamNames []string `yaml:"param_names,omitempty"`
	ParamRegex string   `yaml:"param_regex,omitempty"`
}

func (x ToolSourceReachesSinkExpr) validate() error {
	if len(x.Sources) == 0 || len(x.Sinks) == 0 {
		return fmt.Errorf("tool_source_reaches_sink needs non-empty sources and sinks")
	}
	for _, s := range x.Sources {
		if !slices.Contains(taintSources, s) {
			return fmt.Errorf("tool_source_reaches_sink source %q is not known (want one of: %s)", s, strings.Join(taintSources, ", "))
		}
	}
	for _, s := range x.Sinks {
		if !slices.Contains(taintSinks, s) {
			return fmt.Errorf("tool_source_reaches_sink sink %q is not known (want one of: %s)", s, strings.Join(taintSinks, ", "))
		}
	}
	if x.ParamRegex != "" {
		if _, err := regexp.Compile(x.ParamRegex); err != nil {
			return fmt.Errorf("tool_source_reaches_sink param_regex: %v", err)
		}
	}
	return nil
}

// MatchingFlows returns the tool's taint flows that satisfy the predicate.
func (x ToolSourceReachesSinkExpr) MatchingFlows(t models.ToolDef) []models.TaintFlow {
	var re *regexp.Regexp
	if x.ParamRegex != "" {
		re, _ = regexp.Compile(x.ParamRegex)
	}
	var out []models.TaintFlow
	for _, f := range t.TaintFlows {
		if !slices.Contains(x.Sources, string(f.SourceKind)) || !slices.Contains(x.Sinks, f.SinkClass) {
			continue
		}
		if len(x.ParamNames) > 0 && !slices.ContainsFunc(x.ParamNames, func(n string) bool { return strings.EqualFold(n, f.Param) }) {
			continue
		}
		if re != nil && !re.MatchString(f.Param) {
			continue
		}
		out = append(out, f)
	}
	return out
}

// PredToolSourceReachesSink reports whether the tool has a matching taint flow.
func PredToolSourceReachesSink(x ToolSourceReachesSinkExpr, t models.ToolDef) bool {
	return len(x.MatchingFlows(t)) > 0
}

// taintExprs collects every tool_source_reaches_sink expression in the tree.
func (e MatchExpr) taintExprs() []ToolSourceReachesSinkExpr {
	var out []ToolSourceReachesSinkExpr
	if e.ToolSourceReachesSink != nil {
		out = append(out, *e.ToolSourceReachesSink)
	}
	for _, sub := range e.All {
		out = append(out, sub.taintExprs()...)
	}
	for _, sub := range e.Any {
		out = append(out, sub.taintExprs()...)
	}
	return out
}

// taintEvidence returns the first matching flow of a rule's top-level (or
// all/any-nested) tool_source_reaches_sink, used to place the finding on the sink line.
func taintEvidence(m MatchExpr, t models.ToolDef) *models.TaintFlow {
	for _, x := range m.taintExprs() {
		if fl := x.MatchingFlows(t); len(fl) > 0 {
			f := fl[0]
			return &f
		}
	}
	return nil
}
