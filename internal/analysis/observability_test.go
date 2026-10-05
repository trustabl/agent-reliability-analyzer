package analysis_test

import (
	"testing"

	"github.com/trustabl/trustabl/internal/analysis"
	"github.com/trustabl/trustabl/internal/models"
)

// An import proves the package is present and nothing more. It must NOT be
// recorded as an init — that distinction is what OBS-001 fires on.
func TestDiscoverObservability_PythonImportOnly(t *testing.T) {
	src := `import langfuse

def handler():
    return 1
`
	pf := parsePyFile(t, "app.py", src)
	got := analysis.DiscoverObservability([]analysis.ParsedFile{pf})
	if len(got) != 1 {
		t.Fatalf("got %d signals %+v, want 1", len(got), got)
	}
	if got[0].Vendor != models.VendorLangfuse {
		t.Errorf("vendor: got %q, want langfuse", got[0].Vendor)
	}
	if got[0].Kind != models.ObsSignalImport {
		t.Errorf("kind: got %q, want import", got[0].Kind)
	}
	if got[0].Language != models.LanguagePython {
		t.Errorf("language: got %q, want python", got[0].Language)
	}
}

func TestDiscoverObservability_PythonInit(t *testing.T) {
	src := `import langfuse
from langfuse.callback import CallbackHandler

handler = CallbackHandler()
`
	pf := parsePyFile(t, "tracing.py", src)
	got := analysis.DiscoverObservability([]analysis.ParsedFile{pf})
	var sawInit bool
	for _, s := range got {
		if s.Vendor == models.VendorLangfuse && s.Kind == models.ObsSignalInit {
			sawInit = true
			if s.File != "tracing.py" {
				t.Errorf("file: got %q, want tracing.py", s.File)
			}
			if s.StartLine == 0 {
				t.Error("StartLine must be 1-indexed, got 0")
			}
		}
	}
	if !sawInit {
		t.Fatalf("no langfuse init signal in %+v", got)
	}
}

// init_logger is a generic helper-function name that plenty of projects
// define for their own unrelated logging setup. Without an import gate,
// matching it as bare evidence of Braintrust would misclassify any such
// project as having observability wired up.
func TestDiscoverObservability_PythonBareInitCalleeRequiresImport(t *testing.T) {
	src := `def init_logger():
    return None

init_logger()
`
	pf := parsePyFile(t, "helper.py", src)
	got := analysis.DiscoverObservability([]analysis.ParsedFile{pf})
	for _, s := range got {
		if s.Kind == models.ObsSignalInit {
			t.Fatalf("unrelated init_logger() must not fire without a braintrust import: %+v", got)
		}
	}
}

func TestDiscoverObservability_PythonBareInitCalleeFiresWithImport(t *testing.T) {
	src := `import braintrust

logger = init_logger()
`
	pf := parsePyFile(t, "tracing.py", src)
	got := analysis.DiscoverObservability([]analysis.ParsedFile{pf})
	var sawInit bool
	for _, s := range got {
		if s.Vendor == models.VendorBraintrust && s.Kind == models.ObsSignalInit {
			sawInit = true
		}
	}
	if !sawInit {
		t.Fatalf("no braintrust init signal in %+v", got)
	}
}

// Same gating for MLflow's autolog() — a generic name that must be
// corroborated by the mlflow import.
func TestDiscoverObservability_PythonAutologRequiresImport(t *testing.T) {
	src := `def autolog():
    return None

autolog()
`
	pf := parsePyFile(t, "helper.py", src)
	got := analysis.DiscoverObservability([]analysis.ParsedFile{pf})
	for _, s := range got {
		if s.Kind == models.ObsSignalInit {
			t.Fatalf("unrelated autolog() must not fire without an mlflow import: %+v", got)
		}
	}
}

func TestDiscoverObservability_OTelSetTracerProvider(t *testing.T) {
	src := `from opentelemetry import trace

trace.set_tracer_provider(provider)
`
	pf := parsePyFile(t, "otel.py", src)
	got := analysis.DiscoverObservability([]analysis.ParsedFile{pf})
	var sawInit bool
	for _, s := range got {
		if s.Vendor == models.VendorOTel && s.Kind == models.ObsSignalInit {
			sawInit = true
		}
	}
	if !sawInit {
		t.Fatalf("no otel init signal in %+v", got)
	}
}

func TestDiscoverObservability_IgnoresUnrelatedCode(t *testing.T) {
	src := `import os

def f():
    return os.getcwd()
`
	pf := parsePyFile(t, "plain.py", src)
	if got := analysis.DiscoverObservability([]analysis.ParsedFile{pf}); len(got) != 0 {
		t.Fatalf("got %+v, want no signals", got)
	}
}

func TestDiscoverObservability_TSImportOnly(t *testing.T) {
	src := `import { Langfuse } from "langfuse";

export const x = 1;
`
	pf := parseTSForTest(t, "trace.ts", src)
	got := analysis.DiscoverObservability([]analysis.ParsedFile{pf})
	if len(got) != 1 {
		t.Fatalf("got %d signals %+v, want 1", len(got), got)
	}
	if got[0].Vendor != models.VendorLangfuse || got[0].Kind != models.ObsSignalImport {
		t.Errorf("got %+v, want langfuse/import", got[0])
	}
	if got[0].Language != models.LanguageTypeScript {
		t.Errorf("language: got %q, want typescript", got[0].Language)
	}
}

func TestDiscoverObservability_TSNodeSDKInit(t *testing.T) {
	src := `import { NodeSDK } from "@opentelemetry/sdk-node";

const sdk = new NodeSDK({});
sdk.start();
`
	pf := parseTSForTest(t, "otel.ts", src)
	got := analysis.DiscoverObservability([]analysis.ParsedFile{pf})
	var sawInit bool
	for _, s := range got {
		if s.Vendor == models.VendorOTel && s.Kind == models.ObsSignalInit {
			sawInit = true
		}
	}
	if !sawInit {
		t.Fatalf("no otel init signal in %+v", got)
	}
}

// A NodeSDK-shaped call with no observability import is someone else's class.
// The import gate is what keeps this from firing.
func TestDiscoverObservability_TSInitRequiresImport(t *testing.T) {
	src := `const sdk = new NodeSDK({});
`
	pf := parseTSForTest(t, "unrelated.ts", src)
	if got := analysis.DiscoverObservability([]analysis.ParsedFile{pf}); len(got) != 0 {
		t.Fatalf("got %+v, want no signals", got)
	}
}

// scanner.allParsed carries Go files too; they must contribute nothing rather
// than being run through the Python matcher.
func TestDiscoverObservability_GoFileIgnored(t *testing.T) {
	pf := parseGoForTest(t, "package main\n\nfunc main() {}\n")
	if got := analysis.DiscoverObservability([]analysis.ParsedFile{pf}); len(got) != 0 {
		t.Fatalf("got %+v, want no signals from a Go file", got)
	}
}

func TestDiscoverObservability_ConsoleExporter(t *testing.T) {
	src := `from opentelemetry.sdk.trace.export import ConsoleSpanExporter

exporter = ConsoleSpanExporter()
`
	pf := parsePyFile(t, "otel.py", src)
	got := analysis.DiscoverObservability([]analysis.ParsedFile{pf})
	var detail string
	for _, s := range got {
		if s.Kind == models.ObsSignalExporter {
			detail = s.Detail
		}
	}
	if detail != "console" {
		t.Fatalf("exporter detail: got %q, want console (signals: %+v)", detail, got)
	}
}

func TestDiscoverObservability_OTLPExporterNotConsole(t *testing.T) {
	src := `from opentelemetry.exporter.otlp.proto.http.trace_exporter import OTLPSpanExporter

exporter = OTLPSpanExporter()
`
	pf := parsePyFile(t, "otel.py", src)
	for _, s := range analysis.DiscoverObservability([]analysis.ParsedFile{pf}) {
		if s.Kind == models.ObsSignalExporter && s.Detail != "otlp" {
			t.Fatalf("exporter detail: got %q, want otlp", s.Detail)
		}
	}
}

func TestDiscoverObservability_ContentCaptureKwarg(t *testing.T) {
	src := `from pydantic_ai.agent import InstrumentationSettings

settings = InstrumentationSettings(include_content=True)
`
	pf := parsePyFile(t, "settings.py", src)
	var sawCapture bool
	for _, s := range analysis.DiscoverObservability([]analysis.ParsedFile{pf}) {
		if s.Kind == models.ObsSignalContentCapture && s.Detail == "include_content" {
			sawCapture = true
		}
	}
	if !sawCapture {
		t.Fatal("expected a content_capture signal for include_content=True")
	}
}

func TestDiscoverObservability_ContentCaptureFalseDoesNotFire(t *testing.T) {
	src := `from pydantic_ai.agent import InstrumentationSettings

settings = InstrumentationSettings(include_content=False)
`
	pf := parsePyFile(t, "settings.py", src)
	for _, s := range analysis.DiscoverObservability([]analysis.ParsedFile{pf}) {
		if s.Kind == models.ObsSignalContentCapture {
			t.Fatalf("include_content=False must not produce a capture signal: %+v", s)
		}
	}
}

// instrument=True on a Pydantic AI agent is native instrumentation evidence
// even with no explicit init call anywhere in the repo — this is exactly the
// signal repo_observability_initialized promises to honor (see
// PredRepoObservabilityInitialized's doc comment).
func TestDiscoverAgentObservabilitySignals_PydanticInstrumentTrue(t *testing.T) {
	a := models.AgentDef{
		SDK:      models.SDKPydanticAI,
		Language: models.LanguagePython,
		Location: models.Location{FilePath: "agent.py", Line: 10, EndLine: 10},
		Kwargs: &models.KwargTree{
			Children: map[string]*models.KwargTree{
				"instrument": {Value: &models.Expr{Kind: models.ExprLiteralBool, Text: "True"}},
			},
		},
	}
	got := analysis.DiscoverAgentObservabilitySignals([]models.AgentDef{a})
	if len(got) != 1 {
		t.Fatalf("got %d signals %+v, want 1", len(got), got)
	}
	if got[0].Kind != models.ObsSignalInstrumentKwarg {
		t.Errorf("kind: got %q, want instrument_kwarg", got[0].Kind)
	}
	if got[0].Vendor != models.VendorNative {
		t.Errorf("vendor: got %q, want native", got[0].Vendor)
	}
}

// instrument=False is an explicit opt-out, not evidence of instrumentation.
func TestDiscoverAgentObservabilitySignals_PydanticInstrumentFalseDoesNotFire(t *testing.T) {
	a := models.AgentDef{
		SDK:      models.SDKPydanticAI,
		Language: models.LanguagePython,
		Kwargs: &models.KwargTree{
			Children: map[string]*models.KwargTree{
				"instrument": {Value: &models.Expr{Kind: models.ExprLiteralBool, Text: "False"}},
			},
		},
	}
	if got := analysis.DiscoverAgentObservabilitySignals([]models.AgentDef{a}); len(got) != 0 {
		t.Fatalf("got %+v, want no signals for instrument=False", got)
	}
}

func TestDiscoverAgentObservabilitySignals_VercelExperimentalTelemetry(t *testing.T) {
	a := models.AgentDef{
		SDK:      models.SDKVercelAI,
		Language: models.LanguageTypeScript,
		Kwargs: &models.KwargTree{
			Children: map[string]*models.KwargTree{
				"experimental_telemetry": {Children: map[string]*models.KwargTree{
					"isEnabled": {Value: &models.Expr{Kind: models.ExprLiteralBool, Text: "true"}},
				}},
			},
		},
	}
	got := analysis.DiscoverAgentObservabilitySignals([]models.AgentDef{a})
	if len(got) != 1 || got[0].Kind != models.ObsSignalInstrumentKwarg {
		t.Fatalf("got %+v, want one instrument_kwarg signal", got)
	}
}

func TestDiscoverAgentObservabilitySignals_LangChainCallbacks(t *testing.T) {
	a := models.AgentDef{
		SDK:      models.SDKLangChain,
		Language: models.LanguagePython,
		Kwargs: &models.KwargTree{
			Children: map[string]*models.KwargTree{
				"callbacks": {Value: &models.Expr{Kind: models.ExprList, List: []models.Expr{
					{Kind: models.ExprNameRef, Text: "handler"},
				}}},
			},
		},
	}
	got := analysis.DiscoverAgentObservabilitySignals([]models.AgentDef{a})
	if len(got) != 1 || got[0].Kind != models.ObsSignalInstrumentKwarg {
		t.Fatalf("got %+v, want one instrument_kwarg signal", got)
	}
}

// No kwarg at all is absence, not instrumentation.
func TestDiscoverAgentObservabilitySignals_NoKwargDoesNotFire(t *testing.T) {
	a := models.AgentDef{SDK: models.SDKPydanticAI, Language: models.LanguagePython}
	if got := analysis.DiscoverAgentObservabilitySignals([]models.AgentDef{a}); len(got) != 0 {
		t.Fatalf("got %+v, want no signals", got)
	}
}

// An unrelated SDK's agent kwargs are not inspected for native instrumentation
// — only the three SDKs whose native instrument-kwarg shape is documented.
func TestDiscoverAgentObservabilitySignals_OtherSDKIgnored(t *testing.T) {
	a := models.AgentDef{
		SDK:      models.SDKOpenAIAgents,
		Language: models.LanguagePython,
		Kwargs: &models.KwargTree{
			Children: map[string]*models.KwargTree{
				"instrument": {Value: &models.Expr{Kind: models.ExprLiteralBool, Text: "True"}},
			},
		},
	}
	if got := analysis.DiscoverAgentObservabilitySignals([]models.AgentDef{a}); len(got) != 0 {
		t.Fatalf("got %+v, want no signals for an unrelated SDK", got)
	}
}

func TestDiscoverObservability_ContentCaptureEnvVar(t *testing.T) {
	src := `import os

os.environ["OTEL_INSTRUMENTATION_GENAI_CAPTURE_MESSAGE_CONTENT"] = "true"
`
	pf := parsePyFile(t, "env.py", src)
	var sawCapture bool
	for _, s := range analysis.DiscoverObservability([]analysis.ParsedFile{pf}) {
		if s.Kind == models.ObsSignalContentCapture {
			sawCapture = true
		}
	}
	if !sawCapture {
		t.Fatal("expected a content_capture signal for the GenAI capture env var")
	}
}

func obsInits(got []models.ObservabilitySignal) []models.ObservabilitySignal {
	var out []models.ObservabilitySignal
	for _, s := range got {
		if s.Kind == models.ObsSignalInit {
			out = append(out, s)
		}
	}
	return out
}

// Process-wide instrumentor calls activate tracing, so they are init evidence.
func TestDiscoverObservability_GlobalInstrumentorCalls(t *testing.T) {
	cases := []struct {
		name, src, detail string
		vendor            models.ObservabilityVendor
	}{
		{"logfire pydantic", "import logfire\n\nlogfire.instrument_pydantic_ai()\n", "logfire.instrument_pydantic_ai", models.VendorLogfire},
		{"logfire openai agents", "import logfire\n\nlogfire.instrument_openai_agents()\n", "logfire.instrument_openai_agents", models.VendorLogfire},
		{"logfire mcp", "import logfire\n\nlogfire.instrument_mcp()\n", "logfire.instrument_mcp", models.VendorLogfire},
		{"Agent.instrument_all", "from pydantic_ai import Agent\n\nAgent.instrument_all()\n", "agent.instrument_all", models.VendorNative},
		{"instrument_all with logfire import", "import logfire\n\nPAgent.instrument_all()\n", "agent.instrument_all", models.VendorNative},
		{"openinference instrumentor", "from openinference.instrumentation.openai import OpenAIInstrumentor\n\nOpenAIInstrumentor().instrument()\n", "openaiinstrumentor.instrument", models.VendorOpenInference},
		{"qualified instrumentor", "import openinference.instrumentation.langchain as li\n\nli.LangChainInstrumentor().instrument(tracer_provider=tp)\n", "langchaininstrumentor.instrument", models.VendorOpenInference},
		{"openllmetry instrumentor", "from opentelemetry.instrumentation.openai import OpenAIInstrumentor\n\nOpenAIInstrumentor().instrument()\n", "openaiinstrumentor.instrument", models.VendorOTel},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			pf := parsePyFile(t, "obs.py", tc.src)
			inits := obsInits(analysis.DiscoverObservability([]analysis.ParsedFile{pf}))
			if len(inits) != 1 || inits[0].Detail != tc.detail || inits[0].Vendor != tc.vendor {
				t.Fatalf("got %+v, want one init %s/%s", inits, tc.vendor, tc.detail)
			}
		})
	}
}

// The generic shapes need their import: someone else's instrument_all() or
// FooInstrumentor().instrument() is not observability evidence, and must not
// flip repo_has_observability.
func TestDiscoverObservability_GlobalInstrumentorRequiresImport(t *testing.T) {
	for name, src := range map[string]string{
		"instrument_all":   "class Registry:\n    pass\n\nsomething.instrument_all()\n",
		"instrumentor":     "FooInstrumentor().instrument()\n",
		"unrelated vendor": "import langfuse\n\nsomething.instrument_all()\n",
	} {
		t.Run(name, func(t *testing.T) {
			pf := parsePyFile(t, "x.py", src)
			for _, s := range obsInits(analysis.DiscoverObservability([]analysis.ParsedFile{pf})) {
				t.Fatalf("unexpected init signal %+v", s)
			}
		})
	}
}

func TestDiscoverObservability_OpikPython(t *testing.T) {
	pf := parsePyFile(t, "obs.py", "import opik\n\nopik.configure(use_local=True)\n")
	got := analysis.DiscoverObservability([]analysis.ParsedFile{pf})
	var imp, init bool
	for _, s := range got {
		if s.Vendor != models.VendorOpik {
			continue
		}
		imp = imp || s.Kind == models.ObsSignalImport
		init = init || s.Kind == models.ObsSignalInit
	}
	if !imp || !init {
		t.Fatalf("want opik import+init, got %+v", got)
	}
}

func TestDiscoverObservability_OpikBareCalleeRequiresImport(t *testing.T) {
	pf := parsePyFile(t, "x.py", "def track_openai():\n    pass\n\ntrack_openai()\n")
	if inits := obsInits(analysis.DiscoverObservability([]analysis.ParsedFile{pf})); len(inits) != 0 {
		t.Fatalf("unrelated track_openai() must not fire without an opik import: %+v", inits)
	}
}

func TestDiscoverObservability_OpikTS(t *testing.T) {
	pf := parseTSForTest(t, "obs.ts", "import { Opik } from \"opik\";\n\nconst client = new Opik();\n")
	got := analysis.DiscoverObservability([]analysis.ParsedFile{pf})
	var imp, init bool
	for _, s := range got {
		if s.Vendor != models.VendorOpik {
			continue
		}
		imp = imp || s.Kind == models.ObsSignalImport
		init = init || s.Kind == models.ObsSignalInit
	}
	if !imp || !init {
		t.Fatalf("want opik import+init, got %+v", got)
	}
}

func TestApplyGlobalInstrumentation(t *testing.T) {
	sig := func(detail string, lang models.Language) models.ObservabilitySignal {
		return models.ObservabilitySignal{Vendor: models.VendorLogfire, Kind: models.ObsSignalInit, Detail: detail, File: "obs.py", StartLine: 3, Language: lang}
	}
	agents := []models.AgentDef{
		{SDK: models.SDKPydanticAI, Language: models.LanguagePython},
		{SDK: models.SDKLangChain, Language: models.LanguagePython},
		{SDK: models.SDKVercelAI, Language: models.LanguageTypeScript},
		{SDK: models.SDKPydanticAI, Language: models.LanguageTypeScript},
	}
	analysis.ApplyGlobalInstrumentation(agents, []models.ObservabilitySignal{
		sig("logfire.instrument_pydantic_ai", models.LanguagePython),
		sig("logfire.instrument_mcp", models.LanguagePython), // init evidence, covers no agent
	})
	if got := agents[0].InstrumentedBy; got == nil || got.Kwarg != "instrument" || got.Call != "logfire.instrument_pydantic_ai" || got.Line != 3 {
		t.Errorf("pydantic agent: got %+v", got)
	}
	for i := 1; i < len(agents); i++ {
		if agents[i].InstrumentedBy != nil {
			t.Errorf("agent %d (%s/%s) must not be credited: %+v", i, agents[i].SDK, agents[i].Language, agents[i].InstrumentedBy)
		}
	}
	analysis.ApplyGlobalInstrumentation(agents, []models.ObservabilitySignal{sig("langchaininstrumentor.instrument", models.LanguagePython)})
	if got := agents[1].InstrumentedBy; got == nil || got.Kwarg != "callbacks" {
		t.Errorf("langchain agent: got %+v", got)
	}
}
