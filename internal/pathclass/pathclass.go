// Package pathclass classifies a repo-relative source path by provenance —
// today, only "does this path look like test code" — using path shape alone.
// Classify is a pure function: no AST, no I/O, no map iteration order, so
// classification never affects ScanID and the report stays byte-stable.
//
// This is deliberately narrow: it answers "does this path look like test
// code," not "is this file safe to ignore." See models.SurfaceOrigin for how
// the result is consumed — a test-path finding is still reported, only
// de-weighted (excluded from scoring and the exit-code gate by default), so a
// wrong classification here costs a demotion, not a disappearance.
package pathclass

import (
	"path"
	"strings"

	"github.com/trustabl/trustabl/internal/models"
)

// testDirSegments are directory names that mark everything beneath them as
// test code, matched case-insensitively against any path segment (not just
// the last one), so nested layouts like "pkg/tests/fixtures/agent.py" or
// "src/__tests__/tool.spec.ts" are caught at the first qualifying segment.
//
// Deliberately excluded, on purpose, not by omission:
//   - "spec" — widely used for OpenAPI/JSON-schema specs and as a production
//     source directory name; the "*.spec.*" filename pattern below covers the
//     real test-file case without this directory false positive.
//   - "integration", "qa" — both common production package/directory names
//     (e.g. a payments "integration" adapter); a false demotion there is worse
//     than missing an unconventional test layout.
//   - "examples", "demo", "samples" — a separate provenance question (shipped
//     example code, not test code); out of scope for this classifier.
//   - "fixture" (singular) and anything containing but not equal to a listed
//     segment, e.g. "claude-settings-fixture" or "test-utils" — matching is
//     exact-segment, never substring, so a fixture *repo* used as scanner test
//     data (testdata/corpus/claude-settings-fixture/) is not itself flagged.
var testDirSegments = map[string]bool{
	"test":         true,
	"tests":        true,
	"testing":      true,
	"testdata":     true,
	"__tests__":    true,
	"__mocks__":    true,
	"__fixtures__": true,
	"fixtures":     true,
	"mocks":        true,
	"e2e":          true,
}

// testBasenameExact are exact (lowercased) filenames that are always test
// code wherever they appear: pytest's fixture/collection config file and the
// conventional "tests.py" module some layouts use for an in-package suite.
var testBasenameExact = map[string]bool{
	"conftest.py": true,
	"tests.py":    true,
}

// testBasenamePrefixes are filename beginnings (checked against the
// lowercased basename) that mark a single file as test code: pytest's
// "test_*.py" convention. The trailing underscore matters — it is what keeps
// "testosterone_boost.py" from matching.
var testBasenamePrefixes = []string{
	"test_",
}

// testBasenameSuffixes are filename endings (checked against the lowercased
// basename) that mark a single file as test code regardless of which
// directory it lives in, e.g. "adapter_test.go" sitting next to production
// code with no dedicated test directory. Covers Python, TS/JS, Go, C#, PHP.
var testBasenameSuffixes = []string{
	// Python: "foo_test.py". The leading underscore keeps "contest.py" from
	// matching.
	"_test.py",
	// Go: "foo_test.go" is the language's own test-file convention.
	"_test.go",
	// TS/JS: Jest/Vitest/Mocha "*.test.<ext>" and "*.spec.<ext>" conventions,
	// across every extension the engine's TS/JS discovery reads.
	".test.ts", ".test.tsx", ".test.js", ".test.jsx", ".test.mts", ".test.cts", ".test.mjs", ".test.cjs",
	".spec.ts", ".spec.tsx", ".spec.js", ".spec.jsx", ".spec.mts", ".spec.cts", ".spec.mjs", ".spec.cjs",
	// C#: "FooTest.cs" / "FooTests.cs" (both suffixes needed; neither is a
	// suffix of the other).
	"test.cs", "tests.cs",
	// PHP: "FooTest.php" (PHPUnit convention).
	"test.php",
}

// Classify returns the SurfaceOrigin for a repo-relative, forward-slash path
// — the form models.ScanManifest and models.Location already store paths in
// (see internal/ingestion/normalizer.go's filepath.ToSlash normalization).
// The zero value (production) is returned for anything that doesn't match a
// test-path signal; Classify never returns anything else today, but returns
// models.SurfaceOrigin rather than bool so a future provenance class (e.g.
// vendored, generated) can slot in without changing every call site.
func Classify(relPath string) models.SurfaceOrigin {
	if relPath == "" {
		return ""
	}
	segments := strings.Split(relPath, "/")
	for _, seg := range segments[:len(segments)-1] {
		if testDirSegments[strings.ToLower(seg)] {
			return models.OriginTest
		}
	}

	base := strings.ToLower(path.Base(relPath))
	if testBasenameExact[base] {
		return models.OriginTest
	}
	for _, p := range testBasenamePrefixes {
		if strings.HasPrefix(base, p) {
			return models.OriginTest
		}
	}
	for _, s := range testBasenameSuffixes {
		if strings.HasSuffix(base, s) {
			return models.OriginTest
		}
	}
	return ""
}
