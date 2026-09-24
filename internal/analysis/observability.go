package analysis

import (
	"sort"
	"strings"

	sitter "github.com/smacker/go-tree-sitter"

	"github.com/trustabl/trustabl/internal/analysis/astutil"
	"github.com/trustabl/trustabl/internal/models"
)

// obsPyModules maps a Python module prefix to the vendor an import of it
// proves. Matched against the dotted module name, so "opentelemetry" also
// matches "opentelemetry.sdk.trace".
var obsPyModules = []struct {
	Prefix string
	Vendor models.ObservabilityVendor
}{
	{"opentelemetry", models.VendorOTel},
	{"traceloop", models.VendorOpenLLMetry},
	{"openinference", models.VendorOpenInference},
	{"phoenix", models.VendorOpenInference},
	{"langfuse", models.VendorLangfuse},
	{"langsmith", models.VendorLangSmith},
	{"logfire", models.VendorLogfire},
	{"braintrust", models.VendorBraintrust},
	{"weave", models.VendorWeave},
	{"agentops", models.VendorAgentOps},
	{"mlflow", models.VendorMLflow},
	{"ddtrace", models.VendorDatadogLLMObs},
}

// obsInitCallees maps a call expression to the vendor whose instrumentation it
// ACTIVATES. A pattern matches when the lowercased dotted callee equals it or
// ends with "." + pattern, so both `weave.init(...)` and `wandb.weave.init(...)`
// hit while a bare `init(...)` does not.
//
// "init" and "register" are deliberately absent — they must carry a receiver.
// A pattern with no "." (e.g. "init_logger", "autolog") is generic enough that
// a same-named local function is a real collision risk (a project's own
// `init_logger()` helper, unrelated to Braintrust) — see
// tsObservabilitySignals' import gate for the same reasoning on the TS side.
// pyObservabilitySignals applies the identical gate: a bare pattern only
// counts as evidence when the file also imports that vendor's package.
var obsInitCallees = []struct {
	Pattern string
	Vendor  models.ObservabilityVendor
}{
	{"set_tracer_provider", models.VendorOTel},
	{"traceloop.init", models.VendorOpenLLMetry},
	{"phoenix.otel.register", models.VendorOpenInference},
	{"otel.register", models.VendorOpenInference},
	{"callbackhandler", models.VendorLangfuse},
	{"langfuse.init", models.VendorLangfuse},
	{"logfire.configure", models.VendorLogfire},
	{"braintrust.init_logger", models.VendorBraintrust},
	{"init_logger", models.VendorBraintrust},
	{"wrap_openai", models.VendorBraintrust},
	{"weave.init", models.VendorWeave},
	{"agentops.init", models.VendorAgentOps},
	{"autolog", models.VendorMLflow},
	{"llmobs.enable", models.VendorDatadogLLMObs},
}

// obsExporterNames maps a lowercased span-exporter class name to the sink it
// writes to. The console sinks are what OBS-002 turns on.
var obsExporterNames = []struct {
	Needle string
	Sink   string
}{
	{"consolespanexporter", "console"},
	{"consoleexporter", "console"},
	{"otlpspanexporter", "otlp"},
	{"otlptraceexporter", "otlp"},
	{"jaegerexporter", "jaeger"},
	{"zipkinexporter", "zipkin"},
	{"inmemoryspanexporter", "memory"},
}

// obsContentCaptureKwargs are constructor/function kwargs that, when set to a
// truthy literal, turn on capture of full prompt and response text.
var obsContentCaptureKwargs = []string{
	"include_content",
	"capture_content",
	"log_prompts",
	"log_completions",
}

// obsContentCaptureEnvVars are env-var names whose appearance as a string
// literal indicates the repo is switching on GenAI message-content capture.
var obsContentCaptureEnvVars = []string{
	"OTEL_INSTRUMENTATION_GENAI_CAPTURE_MESSAGE_CONTENT",
	"TRACELOOP_TRACE_CONTENT",
	"LANGFUSE_CAPTURE_INPUT",
	"LANGFUSE_CAPTURE_OUTPUT",
}

// DiscoverObservability extracts every observability signal from the parsed
// files. It is the inventory-step counterpart of the recon dep needles: recon
// says what is DECLARED, this says what the code actually DOES.
//
// Phase 1 inspects Python and TypeScript/JavaScript only. A file in any other
// language contributes no signals — which is why the rules gate on
// repo_observability_inspectable rather than concluding "no observability"
// from an empty result.
func DiscoverObservability(parsed []ParsedFile) []models.ObservabilitySignal {
	var out []models.ObservabilitySignal
	for _, pf := range parsed {
		if pf.Tree == nil {
			continue
		}
		// Dispatch on extension explicitly. Do NOT treat "not TS" as "Python":
		// scanner.allParsed also carries .go/.cs/.php/.rs files, and running the
		// Python node-type matcher over a Go tree yields silent garbage.
		switch {
		case astutil.ParserKindForExtension(pf.RelPath) != "":
			out = append(out, tsObservabilitySignals(pf)...)
		case strings.HasSuffix(pf.RelPath, ".py"):
			out = append(out, pyObservabilitySignals(pf)...)
		}
	}
	sortObservabilitySignals(out)
	return out
}

// MergeObservabilitySignals combines signal sets from the file-level and
// agent-level discovery passes into one deterministically-ordered slice. Both
// passes sort their own output, but concatenating two independently-sorted
// slices does not produce a globally sorted one — this re-sorts the union.
func MergeObservabilitySignals(sets ...[]models.ObservabilitySignal) []models.ObservabilitySignal {
	var out []models.ObservabilitySignal
	for _, s := range sets {
		out = append(out, s...)
	}
	sortObservabilitySignals(out)
	return out
}

// sortObservabilitySignals imposes the deterministic order the report contract
// requires: file, then line, then vendor, then kind.
func sortObservabilitySignals(s []models.ObservabilitySignal) {
	sort.Slice(s, func(i, j int) bool {
		switch {
		case s[i].File != s[j].File:
			return s[i].File < s[j].File
		case s[i].StartLine != s[j].StartLine:
			return s[i].StartLine < s[j].StartLine
		case s[i].Vendor != s[j].Vendor:
			return s[i].Vendor < s[j].Vendor
		default:
			return s[i].Kind < s[j].Kind
		}
	})
}

// pyObservabilitySignals collects import and init signals from one Python file.
func pyObservabilitySignals(pf ParsedFile) []models.ObservabilitySignal {
	var out []models.ObservabilitySignal
	imported := make(map[models.ObservabilityVendor]bool)
	for _, m := range obsPyModules {
		prefix := m.Prefix
		if fileImportsModule(pf, func(mod string) bool {
			return mod == prefix || strings.HasPrefix(mod, prefix+".")
		}) {
			imported[m.Vendor] = true
			out = append(out, models.ObservabilitySignal{
				Vendor:    m.Vendor,
				Kind:      models.ObsSignalImport,
				Detail:    prefix,
				File:      pf.RelPath,
				StartLine: 1,
				EndLine:   1,
				Language:  models.LanguagePython,
			})
		}
	}
	astutil.Walk(pf.Tree.RootNode(), func(n *sitter.Node) bool {
		switch n.Type() {
		case "call":
			fn := n.ChildByFieldName("function")
			if fn == nil {
				return true
			}
			callee := astutil.NodeText(fn, pf.Source)
			if vendor, detail, ok := matchObsInitCallee(callee); ok && (!isBareInitCallee(detail) || imported[vendor]) {
				out = append(out, models.ObservabilitySignal{
					Vendor:    vendor,
					Kind:      models.ObsSignalInit,
					Detail:    detail,
					File:      pf.RelPath,
					StartLine: astutil.NodeLine(n),
					EndLine:   astutil.NodeEndLine(n),
					Language:  models.LanguagePython,
				})
			}
			if sink, ok := matchObsExporter(callee); ok {
				out = append(out, models.ObservabilitySignal{
					Vendor:    models.VendorOTel,
					Kind:      models.ObsSignalExporter,
					Detail:    sink,
					File:      pf.RelPath,
					StartLine: astutil.NodeLine(n),
					EndLine:   astutil.NodeEndLine(n),
					Language:  models.LanguagePython,
				})
			}
			for _, kw := range obsContentCaptureKwargs {
				v, present := astutil.KwargValue(n, pf.Source, kw)
				if !present || !isTruthyLiteral(v) {
					continue
				}
				out = append(out, models.ObservabilitySignal{
					Vendor:    models.VendorOTel,
					Kind:      models.ObsSignalContentCapture,
					Detail:    kw,
					File:      pf.RelPath,
					StartLine: astutil.NodeLine(n),
					EndLine:   astutil.NodeEndLine(n),
					Language:  models.LanguagePython,
				})
			}
		case "string":
			lit := strings.Trim(astutil.NodeText(n, pf.Source), `"'`)
			for _, env := range obsContentCaptureEnvVars {
				if lit != env {
					continue
				}
				out = append(out, models.ObservabilitySignal{
					Vendor:    models.VendorOTel,
					Kind:      models.ObsSignalContentCapture,
					Detail:    env,
					File:      pf.RelPath,
					StartLine: astutil.NodeLine(n),
					EndLine:   astutil.NodeEndLine(n),
					Language:  models.LanguagePython,
				})
			}
		}
		return true
	})
	return out
}

// matchObsExporter reports the sink a span-exporter constructor writes to.
func matchObsExporter(callee string) (string, bool) {
	lower := strings.ToLower(strings.TrimSpace(callee))
	if i := strings.LastIndex(lower, "."); i >= 0 {
		lower = lower[i+1:]
	}
	for _, e := range obsExporterNames {
		if lower == e.Needle {
			return e.Sink, true
		}
	}
	return "", false
}

// isTruthyLiteral reports whether a captured kwarg value is an affirmative
// literal. Anything else — False, a variable, an env lookup — is not evidence.
func isTruthyLiteral(v string) bool {
	switch strings.ToLower(strings.Trim(strings.TrimSpace(v), `"'`)) {
	case "true", "1":
		return true
	}
	return false
}

// isBareInitCallee reports whether a matched obsInitCallees pattern has no
// receiver (e.g. "init_logger" vs "braintrust.init_logger"). Bare patterns are
// generic enough to collide with an unrelated local function of the same
// name, so pyObservabilitySignals requires the corresponding import as
// corroborating evidence before trusting one.
func isBareInitCallee(pattern string) bool {
	return !strings.Contains(pattern, ".")
}

// matchObsInitCallee reports whether a callee expression activates a known
// vendor's instrumentation. callee is the raw dotted text, e.g.
// "trace.set_tracer_provider".
func matchObsInitCallee(callee string) (models.ObservabilityVendor, string, bool) {
	lower := strings.ToLower(strings.TrimSpace(callee))
	if lower == "" {
		return "", "", false
	}
	for _, c := range obsInitCallees {
		if lower == c.Pattern || strings.HasSuffix(lower, "."+c.Pattern) {
			return c.Vendor, c.Pattern, true
		}
	}
	return "", "", false
}

// nativeInstrumentKwargs maps an SDK to the kwarg whose presence is that
// SDK's own native way of switching on instrumentation, per the design's
// vendor table ("native" row): Pydantic AI's instrument=, Vercel's
// experimental_telemetry, LangChain's callbacks=. This is the other half of
// repo_observability_initialized: without it, a repo that imports an
// observability package (e.g. logfire) but only ever turns tracing on
// through this per-agent kwarg — never an explicit init call — would read as
// "imported but never initialized" and misfire OBS-001.
var nativeInstrumentKwargs = map[models.SDK]string{
	models.SDKPydanticAI: "instrument",
	models.SDKVercelAI:   "experimental_telemetry",
	models.SDKLangChain:  "callbacks",
}

// DiscoverAgentObservabilitySignals extracts ObsSignalInstrumentKwarg signals
// from agent kwargs already captured by discovery. It is the AgentDef-level
// counterpart to DiscoverObservability's file-level AST walk: some vendors'
// "traces are on" evidence lives on the agent constructor call, not in a
// standalone init call anywhere in the file.
func DiscoverAgentObservabilitySignals(agents []models.AgentDef) []models.ObservabilitySignal {
	var out []models.ObservabilitySignal
	for _, a := range agents {
		name, ok := nativeInstrumentKwargs[a.SDK]
		if !ok {
			continue
		}
		kw := agentKwarg(&a, name)
		if kw == nil {
			continue
		}
		// Pydantic AI's instrument= is a plain bool: only True is evidence.
		// instrument=False is an explicit opt-out, and any other kwarg here
		// (Vercel's experimental_telemetry, LangChain's callbacks) counts as
		// evidence merely by being present and not explicitly None — the same
		// "missing means None-or-absent" contract PredAgentKwargMissing uses
		// for the paired agent-scope outlier rules (PYD-107/LC-112/VAI-101).
		if a.SDK == models.SDKPydanticAI && !isTruthyBoolLiteral(kw) {
			continue
		}
		if kw.Value != nil && kw.Value.Kind == models.ExprLiteralNone {
			continue
		}
		out = append(out, models.ObservabilitySignal{
			Vendor:    models.VendorNative,
			Kind:      models.ObsSignalInstrumentKwarg,
			Detail:    name,
			File:      a.FilePath,
			StartLine: a.Line,
			EndLine:   a.EndLine,
			Language:  a.Language,
		})
	}
	sortObservabilitySignals(out)
	return out
}

// isTruthyBoolLiteral reports whether a KwargTree leaf is a boolean literal
// spelling "true" (case-insensitively, so it matches both Python's True and
// TS/JS's true).
func isTruthyBoolLiteral(kw *models.KwargTree) bool {
	return kw.Value != nil && kw.Value.Kind == models.ExprLiteralBool &&
		strings.EqualFold(strings.TrimSpace(kw.Value.Text), "true")
}

// obsTSModules maps a TS/JS module specifier substring to the vendor an import
// of it proves. Matched as a substring of the module string, so
// "@opentelemetry/" covers every @opentelemetry/* package.
var obsTSModules = []struct {
	Needle string
	Vendor models.ObservabilityVendor
}{
	{"@opentelemetry/", models.VendorOTel},
	{"@vercel/otel", models.VendorOTel},
	{"@traceloop/", models.VendorOpenLLMetry},
	{"@arizeai/openinference", models.VendorOpenInference},
	{"langfuse", models.VendorLangfuse},
	{"langsmith", models.VendorLangSmith},
	{"braintrust", models.VendorBraintrust},
	{"@agentops/", models.VendorAgentOps},
	{"dd-trace", models.VendorDatadogLLMObs},
}

// obsTSInitCallees maps a lowercased TS/JS callee or constructor name to the
// vendor it activates. Matching is suffix-aware like the Python table, and is
// additionally gated on the file importing that vendor — see
// tsObservabilitySignals.
var obsTSInitCallees = []struct {
	Pattern string
	Vendor  models.ObservabilityVendor
}{
	{"nodesdk", models.VendorOTel},
	{"registerotel", models.VendorOTel},
	{"registerinstrumentations", models.VendorOTel},
	{"traceloop.initialize", models.VendorOpenLLMetry},
	{"langfuse", models.VendorLangfuse},
	{"callbackhandler", models.VendorLangfuse},
	{"initlogger", models.VendorBraintrust},
	{"wrapaisdkmodel", models.VendorBraintrust},
	{"agentops.init", models.VendorAgentOps},
}

// tsObservabilitySignals collects import and init signals from one TS/JS file.
//
// Init detection is gated on the file importing that vendor. TS init names are
// generic enough (NodeSDK, Langfuse) that an ungated match would fire on any
// same-named local class, so the import is the evidence that the name means
// what we think it means.
func tsObservabilitySignals(pf ParsedFile) []models.ObservabilitySignal {
	root := pf.Tree.RootNode()
	imported := make(map[models.ObservabilityVendor]bool)
	var out []models.ObservabilitySignal
	for _, m := range obsTSModules {
		needle := m.Needle
		aliases := astutil.TSImportAliasesMatch(root, pf.Source, func(mod string) bool {
			return strings.Contains(strings.ToLower(mod), needle)
		})
		if len(aliases) == 0 || imported[m.Vendor] {
			continue
		}
		imported[m.Vendor] = true
		out = append(out, models.ObservabilitySignal{
			Vendor:    m.Vendor,
			Kind:      models.ObsSignalImport,
			Detail:    needle,
			File:      pf.RelPath,
			StartLine: 1,
			EndLine:   1,
			Language:  models.LanguageTypeScript,
		})
	}
	if len(imported) == 0 {
		return out
	}
	astutil.Walk(root, func(n *sitter.Node) bool {
		switch n.Type() {
		case "call_expression", "new_expression":
		default:
			return true
		}
		fn := n.ChildByFieldName("function")
		if fn == nil {
			fn = n.ChildByFieldName("constructor")
		}
		if fn == nil {
			return true
		}
		lower := strings.ToLower(astutil.NodeText(fn, pf.Source))
		for _, c := range obsTSInitCallees {
			if !imported[c.Vendor] {
				continue
			}
			if lower != c.Pattern && !strings.HasSuffix(lower, "."+c.Pattern) {
				continue
			}
			out = append(out, models.ObservabilitySignal{
				Vendor:    c.Vendor,
				Kind:      models.ObsSignalInit,
				Detail:    c.Pattern,
				File:      pf.RelPath,
				StartLine: astutil.NodeLine(n),
				EndLine:   astutil.NodeEndLine(n),
				Language:  models.LanguageTypeScript,
			})
			break
		}
		return true
	})
	return out
}
