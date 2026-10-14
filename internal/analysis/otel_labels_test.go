package analysis_test

import (
	"encoding/json"
	"os"
	"slices"
	"testing"

	"github.com/trustabl/trustabl/internal/analysis"
	"github.com/trustabl/trustabl/internal/models"
)

// otelLabelsKinds returns the kinds of the signals in order, for compact
// assertions.
func otelLabelsKinds(sigs []models.OtelLabelsSignal) []models.OtelLabelsSignalKind {
	out := make([]models.OtelLabelsSignalKind, 0, len(sigs))
	for _, s := range sigs {
		out = append(out, s.Kind)
	}
	return out
}

func hasOtelLabelsKind(sigs []models.OtelLabelsSignal, kind models.OtelLabelsSignalKind) bool {
	for _, s := range sigs {
		if s.Kind == kind {
			return true
		}
	}
	return false
}

// An import proves the library is present and nothing more: it must not be
// recorded as a started run or a labelled span. That gap is what OBS-007 fires on.
func TestDiscoverOtelLabels_PythonImportOnly(t *testing.T) {
	src := `from agent_reliability_otel_labels import Run

def handler():
    return 1
`
	pf := parsePyFile(t, "app.py", src)
	got := analysis.DiscoverOtelLabels([]analysis.ParsedFile{pf})
	if len(got) != 1 {
		t.Fatalf("got %d signals %+v, want 1", len(got), got)
	}
	if got[0].Kind != models.OtelLabelsImport {
		t.Errorf("kind: got %q, want import", got[0].Kind)
	}
	if got[0].File != "app.py" || got[0].Language != models.LanguagePython {
		t.Errorf("got file %q language %q, want app.py python", got[0].File, got[0].Language)
	}
}

func TestDiscoverOtelLabels_PythonFullUse(t *testing.T) {
	src := `from opentelemetry import trace
from agent_reliability_otel_labels import Run, mark_run_end, mark_tool_span

def run_agent(prompt):
    run = Run()
    with tracer.start_as_current_span("invoke_agent") as root:
        run.mark_start(root)
        span = trace.get_current_span()
        mark_tool_span(span, args={}, result=None, attempt=1)
        mark_run_end(root, exit_reason="final_answer")
`
	pf := parsePyFile(t, "agent.py", src)
	got := analysis.DiscoverOtelLabels([]analysis.ParsedFile{pf})
	want := []models.OtelLabelsSignalKind{
		models.OtelLabelsImport,
		models.OtelLabelsRunConstruct,
		models.OtelLabelsRunStart,
		models.OtelLabelsToolSpan,
		models.OtelLabelsRunEnd,
	}
	kinds := otelLabelsKinds(got)
	if len(kinds) != len(want) {
		t.Fatalf("got kinds %v, want %v", kinds, want)
	}
	for i := range want {
		if kinds[i] != want[i] {
			t.Errorf("signal %d: got %q, want %q", i, kinds[i], want[i])
		}
	}
	if got[2].StartLine != 7 {
		t.Errorf("run_start line: got %d, want 7", got[2].StartLine)
	}
}

// mark_run_end and mark_tool_span are plausible names for a project's own
// helpers. With no agent_reliability_otel_labels import anywhere in the repo they are not
// evidence of the library.
func TestDiscoverOtelLabels_CallsWithoutAnyImportAreIgnored(t *testing.T) {
	src := `def mark_run_end(span, exit_reason):
    return None

mark_run_end(None, exit_reason="error")
`
	pf := parsePyFile(t, "helpers.py", src)
	got := analysis.DiscoverOtelLabels([]analysis.ParsedFile{pf})
	if len(got) != 0 {
		t.Fatalf("got %+v, want no signals without a agent_reliability_otel_labels import", got)
	}
}

// A Run is routinely built in one module and handed to another, so the file
// that calls run.mark_start(root) need not import agent_reliability_otel_labels itself. The
// import gate is repo-wide for that reason.
func TestDiscoverOtelLabels_RunStartInFileThatReceivesTheRun(t *testing.T) {
	factory := parsePyFile(t, "factory.py", `from agent_reliability_otel_labels import Run

def new_run():
    return Run()
`)
	runner := parsePyFile(t, "runner.py", `def start(run, root):
    run.mark_start(root)
`)
	got := analysis.DiscoverOtelLabels([]analysis.ParsedFile{runner, factory})
	var start *models.OtelLabelsSignal
	for i := range got {
		if got[i].Kind == models.OtelLabelsRunStart {
			start = &got[i]
		}
	}
	if start == nil {
		t.Fatalf("no run_start signal in %+v", got)
	}
	if start.File != "runner.py" {
		t.Errorf("run_start file: got %q, want runner.py", start.File)
	}
}

// mark_start is a method on Run. A receiver-less mark_start(...) is some other
// function and must not read as a started run.
func TestDiscoverOtelLabels_BareMarkStartIsNotARunStart(t *testing.T) {
	src := `from agent_reliability_otel_labels import Run

def mark_start(x):
    return x

mark_start(1)
`
	pf := parsePyFile(t, "app.py", src)
	got := analysis.DiscoverOtelLabels([]analysis.ParsedFile{pf})
	if hasOtelLabelsKind(got, models.OtelLabelsRunStart) {
		t.Fatalf("receiver-less mark_start() must not be a run_start: %+v", got)
	}
}

func TestDiscoverOtelLabels_TypeScriptFullUse(t *testing.T) {
	src := `import { Run, markRunEnd, markToolSpan } from "@trustabl/agent-reliability-otel-labels";

export async function runAgent() {
  const run = new Run();
  await tracer.startActiveSpan("invoke_agent", async (root) => {
    run.markStart(root);
    markToolSpan(span, { args, result, attempt: 1 });
    markRunEnd(root, "final_answer");
    root.end();
  });
}
`
	pf := parseTSForTest(t, "agent.ts", src)
	got := analysis.DiscoverOtelLabels([]analysis.ParsedFile{pf})
	for _, kind := range []models.OtelLabelsSignalKind{
		models.OtelLabelsImport,
		models.OtelLabelsRunStart,
		models.OtelLabelsToolSpan,
		models.OtelLabelsRunEnd,
	} {
		if !hasOtelLabelsKind(got, kind) {
			t.Errorf("no %q signal in %+v", kind, got)
		}
	}
	for _, s := range got {
		if s.Language != models.LanguageTypeScript {
			t.Errorf("language: got %q, want typescript (%+v)", s.Language, s)
		}
	}
}

// Report output must be byte-stable: signals are ordered by file then line
// whatever order the parsed files arrive in.
func TestDiscoverOtelLabels_SortedByFileThenLine(t *testing.T) {
	b := parsePyFile(t, "b.py", `from agent_reliability_otel_labels import mark_run_end

mark_run_end(root, exit_reason="error")
`)
	a := parsePyFile(t, "a.py", `from agent_reliability_otel_labels import mark_run_end

mark_run_end(root, exit_reason="error")
`)
	got := analysis.DiscoverOtelLabels([]analysis.ParsedFile{b, a})
	if len(got) != 4 {
		t.Fatalf("got %d signals %+v, want 4", len(got), got)
	}
	wantFiles := []string{"a.py", "a.py", "b.py", "b.py"}
	for i, f := range wantFiles {
		if got[i].File != f {
			t.Errorf("signal %d file: got %q, want %q", i, got[i].File, f)
		}
	}
	if got[0].StartLine > got[1].StartLine {
		t.Errorf("signals within a file must be ordered by line: %+v", got[:2])
	}
}

// A renamed import must still be followed: `mts(...)` is mark_tool_span.
func TestDiscoverOtelLabels_FollowsPythonAliases(t *testing.T) {
	src := `from agent_reliability_otel_labels import mark_tool_span as mts, mark_run_end as end_run

def tool(span, root):
    mts(span, args={}, result=None, attempt=n)
    end_run(root, exit_reason=reason)
`
	got := analysis.DiscoverOtelLabels([]analysis.ParsedFile{parsePyFile(t, "agent.py", src)})
	if !hasOtelLabelsKind(got, models.OtelLabelsToolSpan) {
		t.Errorf("aliased mark_tool_span not recognised: %+v", got)
	}
	if !hasOtelLabelsKind(got, models.OtelLabelsRunEnd) {
		t.Errorf("aliased mark_run_end not recognised: %+v", got)
	}
}

func TestDiscoverOtelLabels_FollowsTSAliases(t *testing.T) {
	src := `import { markToolSpan as label } from "@trustabl/agent-reliability-otel-labels";

export function tool(span: any, n: number) {
  label(span, { args: {}, result: null, attempt: n });
}
`
	got := analysis.DiscoverOtelLabels([]analysis.ParsedFile{parseTSForTest(t, "agent.ts", src)})
	if !hasOtelLabelsKind(got, models.OtelLabelsToolSpan) {
		t.Errorf("aliased markToolSpan not recognised: %+v", got)
	}
}

// Every signal from a test file carries TestOrigin (spec D8); production
// files do not.
func TestDiscoverOtelLabels_StampsTestOrigin(t *testing.T) {
	src := `from agent_reliability_otel_labels import mark_run_end

def end(root):
    mark_run_end(root, exit_reason="error")
`
	got := analysis.DiscoverOtelLabels([]analysis.ParsedFile{
		parsePyFile(t, "tests/test_agent.py", src),
		parsePyFile(t, "app/agent.py", src),
	})
	if len(got) == 0 {
		t.Fatal("no signals")
	}
	for _, s := range got {
		want := s.File == "tests/test_agent.py"
		if s.TestOrigin != want {
			t.Errorf("%s:%d %s: TestOrigin = %v, want %v", s.File, s.StartLine, s.Kind, s.TestOrigin, want)
		}
	}
}

func attrKeys(sigs []models.OtelLabelsSignal) []string {
	var out []string
	for _, s := range sigs {
		if s.Kind == models.OtelLabelsAttrLiteral {
			out = append(out, s.Key)
		}
	}
	return out
}

// Labels written by hand, with no library import, are still labels.
func TestDiscoverOtelLabels_HandWrittenKeys(t *testing.T) {
	py := `def label(span, root):
    span.set_attribute("trustabl.tool.input_fp", fp)
    root.set_attributes({"trustabl.run_id": rid, "trustabl.exit_reason": "error", "other.key": 1})
`
	ts := `export function label(span: any, root: any) {
  span.setAttribute("trustabl.tool.attempt", 2);
  root.setAttributes({ "trustabl.run_id": rid });
}
`
	got := attrKeys(analysis.DiscoverOtelLabels([]analysis.ParsedFile{
		parsePyFile(t, "agent.py", py),
		parseTSForTest(t, "agent.ts", ts),
	}))
	want := []string{
		"trustabl.tool.input_fp", "trustabl.exit_reason", "trustabl.run_id",
		"trustabl.tool.attempt", "trustabl.run_id",
	}
	slices.Sort(got)
	slices.Sort(want)
	if !slices.Equal(got, want) {
		t.Errorf("got keys %v, want %v", got, want)
	}
}

// A key that is never written to a span is not a label: a dashboard query, a
// constants module, a vendored copy of the library's keys.py.
func TestDiscoverOtelLabels_BareKeyLiteralIsNotALabel(t *testing.T) {
	src := `RUN_ID = "trustabl.run_id"
QUERY = "SELECT * FROM spans WHERE attributes['trustabl.exit_reason'] = 'error'"
lookup(span.attributes["trustabl.tool.attempt"])
`
	if got := attrKeys(analysis.DiscoverOtelLabels([]analysis.ParsedFile{parsePyFile(t, "dash.py", src)})); len(got) != 0 {
		t.Errorf("bare literals recorded as labels: %v", got)
	}
}

// The engine's key table must match the vendored key manifest of the library.
func TestOtelLabelsKeys_MatchVendoredManifest(t *testing.T) {
	raw, err := os.ReadFile("../../testdata/forge/otel/otel-spec-keys.json")
	if err != nil {
		t.Fatal(err)
	}
	var manifest struct {
		Attributes []string `json:"attributes"`
	}
	if err := json.Unmarshal(raw, &manifest); err != nil {
		t.Fatal(err)
	}
	want := slices.Clone(manifest.Attributes)
	slices.Sort(want)
	if got := analysis.OtelLabelsAttributeKeys(); !slices.Equal(got, want) {
		t.Errorf("engine keys %v, manifest attributes %v", got, want)
	}
}

func runConstructs(sigs []models.OtelLabelsSignal) []models.OtelLabelsSignal {
	var out []models.OtelLabelsSignal
	for _, s := range sigs {
		if s.Kind == models.OtelLabelsRunConstruct {
			out = append(out, s)
		}
	}
	return out
}

func TestDiscoverOtelLabels_RunConstructScope(t *testing.T) {
	cases := []struct {
		name, src       string
		wantModuleScope bool
		wantRunIDConst  bool
	}{
		{"per run, inside a function", `from agent_reliability_otel_labels import Run

def run_agent():
    run = Run()
`, false, false},
		{"module scope", `from agent_reliability_otel_labels import Run

RUN = Run()
`, true, false},
		{"class body", `from agent_reliability_otel_labels import Run

class Service:
    run = Run()
`, true, false},
		{"default parameter, evaluated once", `from agent_reliability_otel_labels import Run

def handle(req, run=Run()):
    return req
`, true, false},
		{"script main guard", `from agent_reliability_otel_labels import Run

if __name__ == "__main__":
    run = Run()
`, false, false},
		{"script main guard, parenthesized", `from agent_reliability_otel_labels import Run

if (__name__ == "__main__"):
    run = Run()
`, false, false},
		{"literal run id, positional", `from agent_reliability_otel_labels import Run

def run_agent():
    run = Run("fixed")
`, false, true},
		{"literal run id, keyword", `from agent_reliability_otel_labels import Run

def run_agent():
    run = Run(run_id="fixed")
`, false, true},
		{"per-request run id", `from agent_reliability_otel_labels import Run

def run_agent(request_id):
    run = Run(run_id=request_id)
`, false, false},
		{"module alias", `import agent_reliability_otel_labels as otl

def run_agent():
    run = otl.Run()
`, false, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := runConstructs(analysis.DiscoverOtelLabels([]analysis.ParsedFile{parsePyFile(t, "agent.py", tc.src)}))
			if len(got) != 1 {
				t.Fatalf("got %d run_construct signals, want 1: %+v", len(got), got)
			}
			if got[0].ModuleScope != tc.wantModuleScope {
				t.Errorf("ModuleScope = %v, want %v", got[0].ModuleScope, tc.wantModuleScope)
			}
			if got[0].RunIDConst != tc.wantRunIDConst {
				t.Errorf("RunIDConst = %v, want %v", got[0].RunIDConst, tc.wantRunIDConst)
			}
		})
	}
}

// Run is too generic a name to trust repo-wide (spec D5): a Run imported from
// somewhere else is not the library's, even when the library is imported.
func TestDiscoverOtelLabels_RunFromAnotherModuleIsIgnored(t *testing.T) {
	src := `from agent_reliability_otel_labels import mark_run_end
from myapp.jobs import Run

RUN = Run()
`
	got := runConstructs(analysis.DiscoverOtelLabels([]analysis.ParsedFile{parsePyFile(t, "agent.py", src)}))
	if len(got) != 0 {
		t.Errorf("myapp.jobs.Run recorded as the library's Run: %+v", got)
	}
}

func TestDiscoverOtelLabels_TSRunConstructScope(t *testing.T) {
	cases := []struct {
		name, src       string
		wantModuleScope bool
		wantRunIDConst  bool
	}{
		{"per run", `import { Run } from "@trustabl/agent-reliability-otel-labels";
export function runAgent() { const run = new Run(); }
`, false, false},
		{"module scope", `import { Run } from "@trustabl/agent-reliability-otel-labels";
const RUN = new Run();
`, true, false},
		// TypeScript evaluates a default parameter per call, unlike Python.
		{"default parameter", `import { Run } from "@trustabl/agent-reliability-otel-labels";
export function handle(req: unknown, run = new Run()) { return req; }
`, false, false},
		{"literal run id", `import { Run } from "@trustabl/agent-reliability-otel-labels";
export function runAgent() { const run = new Run("fixed"); }
`, false, true},
		{"namespace import", `import * as otl from "@trustabl/agent-reliability-otel-labels";
const RUN = new otl.Run();
`, true, false},
		// A non-static class field initializer runs once per instance, like
		// Python's `self.run = Run()` in __init__, which is not flagged.
		{"instance class field", `import { Run } from "@trustabl/agent-reliability-otel-labels";
class Svc { run = new Run(); }
`, false, false},
		{"static class field is shared", `import { Run } from "@trustabl/agent-reliability-otel-labels";
class Svc { static run = new Run(); }
`, true, false},
		{"instance field of an exported class", `import { Run } from "@trustabl/agent-reliability-otel-labels";
export class Svc { private run = new Run(); }
`, false, false},
		{"arrow function in a static field", `import { Run } from "@trustabl/agent-reliability-otel-labels";
class Svc { static make = () => new Run(); }
`, false, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := runConstructs(analysis.DiscoverOtelLabels([]analysis.ParsedFile{parseTSForTest(t, "agent.ts", tc.src)}))
			if len(got) != 1 {
				t.Fatalf("got %d run_construct signals, want 1: %+v", len(got), got)
			}
			if got[0].ModuleScope != tc.wantModuleScope {
				t.Errorf("ModuleScope = %v, want %v", got[0].ModuleScope, tc.wantModuleScope)
			}
			if got[0].RunIDConst != tc.wantRunIDConst {
				t.Errorf("RunIDConst = %v, want %v", got[0].RunIDConst, tc.wantRunIDConst)
			}
		})
	}
}

func firstOfKind(t *testing.T, sigs []models.OtelLabelsSignal, kind models.OtelLabelsSignalKind) models.OtelLabelsSignal {
	t.Helper()
	for _, s := range sigs {
		if s.Kind == kind {
			return s
		}
	}
	t.Fatalf("no %q signal in %+v", kind, sigs)
	return models.OtelLabelsSignal{}
}

func TestDiscoverOtelLabels_PythonToolSpanFacts(t *testing.T) {
	const imp = "from agent_reliability_otel_labels import mark_tool_span\n\n"
	cases := []struct {
		name, body  string
		failureSafe bool
		hookUnknown bool
		attempt     bool
		sideEffect  string
	}{
		{"after the call", `def tool(span, args):
    result = find(args)
    mark_tool_span(span, args=args, result=result, attempt=n, side_effect="read")
`, false, false, false, "read"},
		{"in finally", `def tool(span, args):
    result = None
    try:
        result = find(args)
    finally:
        mark_tool_span(span, args=args, result=result, attempt=1, side_effect="write")
`, true, false, true, "write"},
		{"in a try nested in finally (forge's pattern)", `def tool(span, args):
    try:
        return find(args)
    finally:
        try:
            mark_tool_span(span, args=args, result=None, attempt=attempts[key])
        except ValueError:
            pass
`, true, false, false, ""},
		{"in except", `def tool(span, args):
    try:
        return find(args)
    except Exception:
        mark_tool_span(span, args=args, result=None, attempt=n)
        raise
`, true, false, false, ""},
		{"error callback", `class Handler:
    def on_tool_error(self, error, **kw):
        mark_tool_span(self.span, args={}, result=None, attempt=self.n)
`, true, false, false, ""},
		{"OpenAI Agents success hook", `class Hooks(RunHooks):
    async def on_tool_end(self, context, agent, tool, result):
        mark_tool_span(self.span, args={}, result=result, attempt=self.n)
`, false, true, false, ""},
		{"on_tool_end of an unrelated class", `class Hooks:
    def on_tool_end(self, result):
        mark_tool_span(self.span, args={}, result=result, attempt=self.n)
`, false, false, false, ""},
		{"finally in an outer function does not reach a nested one", `def outer(span):
    try:
        pass
    finally:
        def inner():
            mark_tool_span(span, args={}, result=None, attempt=n)
`, false, false, false, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := analysis.DiscoverOtelLabels([]analysis.ParsedFile{parsePyFile(t, "agent.py", imp+tc.body)})
			s := firstOfKind(t, got, models.OtelLabelsToolSpan)
			if s.FailureSafe != tc.failureSafe {
				t.Errorf("FailureSafe = %v, want %v", s.FailureSafe, tc.failureSafe)
			}
			if s.HookUnknown != tc.hookUnknown {
				t.Errorf("HookUnknown = %v, want %v", s.HookUnknown, tc.hookUnknown)
			}
			if s.AttemptConst != tc.attempt {
				t.Errorf("AttemptConst = %v, want %v", s.AttemptConst, tc.attempt)
			}
			if s.SideEffect != tc.sideEffect {
				t.Errorf("SideEffect = %q, want %q", s.SideEffect, tc.sideEffect)
			}
		})
	}
}

func TestDiscoverOtelLabels_PythonRunEndFacts(t *testing.T) {
	const imp = "from agent_reliability_otel_labels import mark_run_end\n\n"
	cases := []struct {
		name, body string
		errorPath  bool
		reason     string
	}{
		{"success path", `def run(root):
    mark_run_end(root, exit_reason="final_answer")
`, false, "final_answer"},
		{"except", `def run(root):
    try:
        work()
    except Exception:
        mark_run_end(root, exit_reason="error")
        raise
`, true, "error"},
		// A fixed success reason in finally mislabels every failed run (D9).
		{"finally is not the error path", `def run(root):
    try:
        work()
    finally:
        mark_run_end(root, exit_reason="final_answer")
`, false, "final_answer"},
		{"variable reason", `def run(root, reason):
    mark_run_end(root, exit_reason=reason)
`, false, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := analysis.DiscoverOtelLabels([]analysis.ParsedFile{parsePyFile(t, "agent.py", imp+tc.body)})
			s := firstOfKind(t, got, models.OtelLabelsRunEnd)
			if s.ErrorPath != tc.errorPath {
				t.Errorf("ErrorPath = %v, want %v", s.ErrorPath, tc.errorPath)
			}
			if s.ExitReason != tc.reason {
				t.Errorf("ExitReason = %q, want %q", s.ExitReason, tc.reason)
			}
		})
	}
}

func TestDiscoverOtelLabels_TSFacts(t *testing.T) {
	src := `import { markRunEnd, markToolSpan } from "@trustabl/agent-reliability-otel-labels";

export async function safe(span: any, args: any, attempt: number) {
  let result: unknown;
  try {
    result = await find(args);
  } finally {
    markToolSpan(span, { args, result, attempt, sideEffect: "read" });
  }
}

export function chained(span: any, args: any) {
  return find(args).finally(() => markToolSpan(span, { args, result: null, attempt: 1 }));
}

export async function run(root: any) {
  try {
    await work();
    markRunEnd(root, "final_answer");
  } catch (err) {
    markRunEnd(root, "error");
    throw err;
  }
}

export function viaCatch(root: any) {
  return work().catch(() => markRunEnd(root, "error"));
}
`
	got := analysis.DiscoverOtelLabels([]analysis.ParsedFile{parseTSForTest(t, "agent.ts", src)})
	var spans, ends []models.OtelLabelsSignal
	for _, s := range got {
		switch s.Kind {
		case models.OtelLabelsToolSpan:
			spans = append(spans, s)
		case models.OtelLabelsRunEnd:
			ends = append(ends, s)
		}
	}
	if len(spans) != 2 || len(ends) != 3 {
		t.Fatalf("got %d tool spans and %d run ends, want 2 and 3: %+v", len(spans), len(ends), got)
	}
	// try/finally; the shorthand { attempt } is a variable, not a literal.
	if !spans[0].FailureSafe || spans[0].AttemptConst || spans[0].SideEffect != "read" {
		t.Errorf("finally span: %+v", spans[0])
	}
	// .finally(() => ...) runs on rejection too.
	if !spans[1].FailureSafe || !spans[1].AttemptConst {
		t.Errorf(".finally span: %+v", spans[1])
	}
	if ends[0].ErrorPath || ends[0].ExitReason != "final_answer" {
		t.Errorf("success run end: %+v", ends[0])
	}
	if !ends[1].ErrorPath || ends[1].ExitReason != "error" {
		t.Errorf("catch run end: %+v", ends[1])
	}
	if !ends[2].ErrorPath || ends[2].ExitReason != "error" {
		t.Errorf(".catch run end: %+v", ends[2])
	}
}
