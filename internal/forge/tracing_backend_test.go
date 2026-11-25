package forge

import (
	"regexp"
	"strings"
	"testing"

	"github.com/trustabl/trustabl/internal/models"
)

func langWatchSelection() TracingSelection {
	return TracingSelection{
		Languages: []models.Language{models.LanguagePython, models.LanguageTypeScript},
		Pairs: []SDKLanguage{
			{Category: models.CategoryLangChain, Language: models.LanguagePython},
			{Category: models.CategoryVercelAI, Language: models.LanguageTypeScript},
		},
		Backends: []TracingBackend{
			{Vendor: models.VendorLangWatch, Language: models.LanguagePython},
			{Vendor: models.VendorLangWatch, Language: models.LanguageTypeScript},
		},
	}
}

// allBackendsSelection is allTracingSelection plus every backend block, so a
// test that scans the output covers the backend text too.
func allBackendsSelection() TracingSelection {
	sel := allTracingSelection()
	for _, be := range backendBlocks {
		sel.Backends = append(sel.Backends, TracingBackend{Vendor: be.Vendor, Language: be.Language})
	}
	return sel
}

func TestGenerateCombined_LangWatchReplacesGenericSetup(t *testing.T) {
	policies, stamp := multiSDKFixture(t)
	got := GenerateCombined(stamp.Categories, policies, stamp, langWatchSelection(), nil)
	for _, want := range []string{
		"#### Python (LangWatch)", "#### TypeScript (LangWatch)",
		"langwatch.setup()", "setupObservability(", "attachToExistingProvider",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q", want)
		}
	}
	for _, name := range []string{"python_setup.md", "typescript_setup.md"} {
		if strings.Contains(got, tracingText(name)) {
			t.Errorf("%s must not appear beside the LangWatch block: it creates a second provider", name)
		}
	}
	// The recipes and the shared steps are kept, and the backend traps follow
	// the language traps.
	for _, name := range []string{
		"python_labels.md", "typescript_labels.md", "python_traps.md", "typescript_traps.md",
		"backends/langwatch_python_traps.md", "backends/langwatch_typescript_traps.md",
	} {
		if !strings.Contains(got, sanitizeEmittedText(tracingText(name))) {
			t.Errorf("missing the text of %s", name)
		}
	}
	for _, want := range []string{"##### LangChain / LangGraph", "##### Vercel AI SDK"} {
		if !strings.Contains(got, want) {
			t.Errorf("missing recipe %q", want)
		}
	}
	langTraps := strings.Index(got, sanitizeEmittedText(tracingText("python_traps.md")))
	lwTraps := strings.Index(got, sanitizeEmittedText(tracingText("backends/langwatch_python_traps.md")))
	if langTraps < 0 || lwTraps < langTraps {
		t.Errorf("the LangWatch traps must follow the Python traps: %d, %d", langTraps, lwTraps)
	}
}

// A backend for an unselected language changes nothing.
func TestGenerateCombined_BackendOnlyForItsLanguage(t *testing.T) {
	policies, stamp := multiSDKFixture(t)
	sel := TracingSelection{
		Languages: []models.Language{models.LanguageGo},
		Backends:  []TracingBackend{{Vendor: models.VendorLangWatch, Language: models.LanguagePython}},
	}
	got := GenerateCombined(stamp.Categories, policies, stamp, sel, nil)
	plain := GenerateCombined(stamp.Categories, policies, stamp, TracingSelection{Languages: sel.Languages}, nil)
	if got != plain {
		t.Errorf("a backend for an unselected language changed the output:\n%s", lineDiff(plain, got))
	}
}

// R2.2 and R2.4: no second provider, no gRPC, the key only from the
// environment, and the bindings the recipes and Step 2 read. The endpoint is
// given as scheme, host and path, never as one URL: a URL in a skill body
// trips CSKILL-020 (see TestGenerateCombined_SkillCompliant).
func TestBackendText_Hygiene(t *testing.T) {
	key := regexp.MustCompile(`(sk-lw-|lw_[A-Za-z0-9]{8,}|Bearer [A-Za-z0-9]{8,})`)
	// Constructing or registering a provider, or a gRPC exporter. Reading the
	// global provider (get_tracer_provider, getTracerProvider) is the point.
	banned := map[string]*regexp.Regexp{
		"a provider constructor":         regexp.MustCompile(`(^|[^A-Za-z_.])TracerProvider\(|new \w*TracerProvider|NodeSDK\(`),
		"a global provider registration": regexp.MustCompile(`set_tracer_provider|setGlobalTracerProvider`),
		"a gRPC exporter":                regexp.MustCompile(`(?i)otlp[._-]proto[._-]grpc|otlp-grpc|exporter\.otlp\.grpc`),
	}
	for _, be := range backendBlocks {
		setup := tracingText(backendFile(be, "setup"))
		traps := tracingText(backendFile(be, "traps"))
		if setup == "" || traps == "" {
			t.Fatalf("%s/%s: setup or traps text missing", be.Vendor, be.Language)
		}
		all := setup + "\n" + traps
		for what, re := range banned {
			if m := re.FindString(all); m != "" {
				t.Errorf("%s/%s text contains %s: %q", be.Vendor, be.Language, what, m)
			}
		}
		if key.MatchString(all) {
			t.Errorf("%s/%s text contains something shaped like a key", be.Vendor, be.Language)
		}
		for _, want := range []string{
			"LANGWATCH_API_KEY", "OTEL_EXPORTER_OTLP_TRACES_ENDPOINT",
			"`app.langwatch.ai`", "`/api/otel/v1/traces`", "provider = ", "tracer = ",
		} {
			if !strings.Contains(setup, want) {
				t.Errorf("%s/%s setup lacks %q", be.Vendor, be.Language, want)
			}
		}
	}
}

func TestBackendBlocks_NoOrphanFiles(t *testing.T) {
	known := map[string]bool{}
	for _, be := range backendBlocks {
		for _, part := range []string{"setup", "traps"} {
			known[strings.TrimPrefix(backendFile(be, part), "backends/")] = true
		}
	}
	entries, err := tracingFS.ReadDir("tracingtext/backends")
	if err != nil {
		t.Fatalf("read backends dir: %v", err)
	}
	for _, e := range entries {
		if !known[e.Name()] {
			t.Errorf("tracingtext/backends/%s belongs to no entry in backendBlocks", e.Name())
		}
	}
}
