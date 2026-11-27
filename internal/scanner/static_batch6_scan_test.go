package scanner_test

import (
	"os"
	"path/filepath"
	"sort"
	"testing"

	"github.com/trustabl/trustabl/internal/models"
)

// Static batch 6 over the corpus fixture: each rule fires at the sink call of the
// unguarded tool; the safe/ folder adds no finding and the test-path tool is tagged test origin.
func TestScan_StaticBatch6Fixture(t *testing.T) {
	files := map[string]string{}
	root := "../../testdata/corpus/static-batch6-fixture"
	err := filepath.Walk(root, func(p string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() || filepath.Base(p) == "SOURCE.md" {
			return err
		}
		rel, _ := filepath.Rel(root, p)
		b, rerr := os.ReadFile(p)
		files[filepath.ToSlash(rel)] = string(b)
		return rerr
	})
	if err != nil {
		t.Fatal(err)
	}
	res := scanFiles(t, files)
	want := map[string][]string{
		"MCP-215": {"server.py", "server.py"},
		"MCP-216": {"server.py"},
		"MCP-217": {"server.py"},
		"MCP-219": {"tools.ts"},
		"MCP-220": {"tools.ts"},
		"LC-204":  {"lc_tools.py"},
		"LC-205":  {"lc_tools.py"},
	}
	for id, w := range want {
		var got []string
		for _, f := range res.Findings {
			if f.RuleID == id && f.Origin != models.OriginTest {
				got = append(got, f.FilePath)
			}
		}
		sort.Strings(got)
		if len(got) != len(w) {
			t.Errorf("%s: got %v, want %v", id, got, w)
			continue
		}
		for i := range w {
			if got[i] != w[i] {
				t.Errorf("%s: got %v, want %v", id, got, w)
			}
		}
	}
	for _, f := range res.Findings {
		switch f.RuleID {
		case "MCP-215", "MCP-216", "MCP-217", "MCP-219", "MCP-220", "LC-204", "LC-205":
			if filepath.Dir(f.FilePath) == "safe" {
				t.Errorf("%s fired in %s", f.RuleID, f.FilePath)
			}
			if filepath.Dir(f.FilePath) == "tests" && f.Origin != models.OriginTest {
				t.Errorf("%s in %s is not tagged as test origin", f.RuleID, f.FilePath)
			}
		}
	}
}
