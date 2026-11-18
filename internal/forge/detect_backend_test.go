package forge

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/trustabl/trustabl/internal/models"
)

func TestDetectionFromRecon_LangWatchBackend(t *testing.T) {
	det := detectionFromRecon(
		[]models.SDKDep{{Name: "langchain", Source: "requirements.txt"}, {Name: "vercel-ai", Source: "package.json"}},
		[]models.ObsDep{
			{Vendor: models.VendorLangWatch, Source: "package.json"},
			{Vendor: models.VendorLangWatch, Source: "requirements.txt"},
			{Vendor: models.VendorLangfuse, Source: "requirements.txt"}, // no backend block
		},
	)
	want := []TracingBackend{
		{Vendor: models.VendorLangWatch, Language: models.LanguagePython},
		{Vendor: models.VendorLangWatch, Language: models.LanguageTypeScript},
	}
	if !reflect.DeepEqual(det.Backends, want) {
		t.Fatalf("Backends = %+v, want %+v", det.Backends, want)
	}
}

// A lock file lists transitive packages; nobody chose LangWatch there.
func TestDetectionFromRecon_LockFileIsNotABackend(t *testing.T) {
	det := detectionFromRecon(nil, []models.ObsDep{{Vendor: models.VendorLangWatch, Source: "poetry.lock"}})
	if len(det.Backends) != 0 {
		t.Fatalf("Backends = %+v, want none", det.Backends)
	}
}

func TestDetect_LangWatchFromManifest(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "requirements.txt"), []byte("langchain\nlangwatch==1.4.0\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	det, err := Detect(context.Background(), dir)
	if err != nil {
		t.Fatal(err)
	}
	want := []TracingBackend{{Vendor: models.VendorLangWatch, Language: models.LanguagePython}}
	if !reflect.DeepEqual(det.Backends, want) {
		t.Fatalf("Backends = %+v, want %+v", det.Backends, want)
	}
}

func TestSelectTracing_BackendFollowsLanguage(t *testing.T) {
	det := Detection{
		Categories: []models.DetectorCategory{models.CategoryLangChain},
		Languages:  []models.Language{models.LanguagePython},
		Pairs:      []SDKLanguage{{Category: models.CategoryLangChain, Language: models.LanguagePython}},
		Backends: []TracingBackend{
			{Vendor: models.VendorLangWatch, Language: models.LanguagePython},
			{Vendor: models.VendorLangWatch, Language: models.LanguageTypeScript},
		},
	}
	sel := SelectTracing(det, det.Categories, nil, nil)
	if want := []TracingBackend{{Vendor: models.VendorLangWatch, Language: models.LanguagePython}}; !reflect.DeepEqual(sel.Backends, want) {
		t.Fatalf("Backends = %+v, want %+v (TypeScript is not selected)", sel.Backends, want)
	}
	sel = SelectTracing(det, det.Categories, nil, []models.Language{models.LanguageTypeScript})
	if len(sel.Backends) != 2 {
		t.Fatalf("--lang typescript selects the language, so its detected backend follows: %+v", sel.Backends)
	}
	if sel := SelectTracing(Detection{}, det.Categories, nil, []models.Language{models.LanguagePython}); len(sel.Backends) != 0 {
		t.Fatalf("--lang never invents a backend: %+v", sel.Backends)
	}
	if sel := SelectTracing(det, []models.DetectorCategory{models.CategoryClaudeSkill}, nil, nil); len(sel.Backends) != 0 {
		t.Fatalf("a skill-only selection has no tracing section, so no backend: %+v", sel.Backends)
	}
}
