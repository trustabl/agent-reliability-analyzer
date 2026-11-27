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
	// flowTaintSources and flowTaintSinks are the closed value sets of tool_flow_reaches_sink
	// (schema 53). They are a separate predicate so a build at schema 51 skips these rules with
	// META-005 instead of failing to load a sink value it does not know.
	flowTaintSources = []string{string(models.SourceParam), string(models.SourceSecretEnv)}
	flowTaintSinks   = []string{models.SinkReturnValue, models.SinkMemoryWrite}
	// repoTaintSources and repoTaintSinks are the closed value sets of repo_source_reaches_sink.
	repoTaintSources = []string{string(models.SourceLLMOutput), string(models.SourceHandlerParam)}
	repoTaintSinks   = []string{models.SinkShell, models.SinkCodeExec, models.SinkClaudeResume}
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

// ToolFlowReachesSinkExpr fires when the one-function taint pass found a flow from a listed
// source to a listed sink inside the tool's handler, over the batch 7 additions: the secret_env
// source (an environment variable whose literal name reads as a credential,
// analysis.IsSecretEnvName) and the return_value and memory_write sinks. ParamNames and
// ParamRegex restrict a param source by name. It is separate from tool_source_reaches_sink,
// whose value sets are the batch 6 ones.
type ToolFlowReachesSinkExpr struct {
	Sources    []string `yaml:"sources"`
	Sinks      []string `yaml:"sinks"`
	ParamNames []string `yaml:"param_names,omitempty"`
	ParamRegex string   `yaml:"param_regex,omitempty"`
}

func (x ToolFlowReachesSinkExpr) asSource() ToolSourceReachesSinkExpr {
	return ToolSourceReachesSinkExpr{Sources: x.Sources, Sinks: x.Sinks, ParamNames: x.ParamNames, ParamRegex: x.ParamRegex}
}

func (x ToolFlowReachesSinkExpr) validate() error {
	if len(x.Sources) == 0 || len(x.Sinks) == 0 {
		return fmt.Errorf("tool_flow_reaches_sink needs non-empty sources and sinks")
	}
	for _, s := range x.Sources {
		if !slices.Contains(flowTaintSources, s) {
			return fmt.Errorf("tool_flow_reaches_sink source %q is not known (want one of: %s)", s, strings.Join(flowTaintSources, ", "))
		}
	}
	for _, s := range x.Sinks {
		if !slices.Contains(flowTaintSinks, s) {
			return fmt.Errorf("tool_flow_reaches_sink sink %q is not known (want one of: %s)", s, strings.Join(flowTaintSinks, ", "))
		}
	}
	if x.ParamRegex != "" {
		if _, err := regexp.Compile(x.ParamRegex); err != nil {
			return fmt.Errorf("tool_flow_reaches_sink param_regex: %v", err)
		}
	}
	return nil
}

// PredToolFlowReachesSink reports whether the tool has a matching flow.
func PredToolFlowReachesSink(x ToolFlowReachesSinkExpr, t models.ToolDef) bool {
	return PredToolSourceReachesSink(x.asSource(), t)
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

// flowTaintExprs collects every tool_flow_reaches_sink expression in the tree.
func (e MatchExpr) flowTaintExprs() []ToolFlowReachesSinkExpr {
	var out []ToolFlowReachesSinkExpr
	if e.ToolFlowReachesSink != nil {
		out = append(out, *e.ToolFlowReachesSink)
	}
	for _, sub := range e.All {
		out = append(out, sub.flowTaintExprs()...)
	}
	for _, sub := range e.Any {
		out = append(out, sub.flowTaintExprs()...)
	}
	return out
}

// taintEvidence returns the first matching flow of a rule's top-level (or
// all/any-nested) tool_source_reaches_sink, used to place the finding on the sink line.
func taintEvidence(m MatchExpr, t models.ToolDef) *models.TaintFlow {
	exprs := m.taintExprs()
	for _, x := range m.flowTaintExprs() {
		exprs = append(exprs, x.asSource())
	}
	for _, x := range exprs {
		if fl := x.MatchingFlows(t); len(fl) > 0 {
			f := fl[0]
			return &f
		}
	}
	return nil
}

// RepoSourceReachesSinkExpr fires when the one-function taint pass found a flow from a listed
// source to a listed sink in any function of repo code outside test paths (Python): model output
// reaching a shell or code-execution call, or a function parameter reaching the resume keyword
// of ClaudeAgentOptions. See analysis.DiscoverRepoFlows.
type RepoSourceReachesSinkExpr struct {
	Sources []string `yaml:"sources"`
	Sinks   []string `yaml:"sinks"`
}

func (x RepoSourceReachesSinkExpr) validate() error {
	if len(x.Sources) == 0 || len(x.Sinks) == 0 {
		return fmt.Errorf("repo_source_reaches_sink needs non-empty sources and sinks")
	}
	for _, s := range x.Sources {
		if !slices.Contains(repoTaintSources, s) {
			return fmt.Errorf("repo_source_reaches_sink source %q is not known (want one of: %s)", s, strings.Join(repoTaintSources, ", "))
		}
	}
	for _, s := range x.Sinks {
		if !slices.Contains(repoTaintSinks, s) {
			return fmt.Errorf("repo_source_reaches_sink sink %q is not known (want one of: %s)", s, strings.Join(repoTaintSinks, ", "))
		}
	}
	return nil
}

// RepoFlowsFor returns the recorded repo flows that satisfy the predicate, in inventory order.
func (x RepoSourceReachesSinkExpr) RepoFlowsFor(inv models.RepoInventory) []models.RepoFlow {
	var out []models.RepoFlow
	for _, f := range inv.RepoFlows {
		if slices.Contains(x.Sources, string(f.Source)) && slices.Contains(x.Sinks, f.SinkClass) {
			out = append(out, f)
		}
	}
	return out
}

// PredSkillBundledScriptPrintsSecret reports whether a bundled Python script prints a
// secret-named environment value (BundledFile.PrintsSecret).
func PredSkillBundledScriptPrintsSecret(s models.SkillDef) bool {
	for _, b := range s.BundledFiles {
		if b.Kind == "script" && b.PrintsSecret {
			return true
		}
	}
	return false
}
