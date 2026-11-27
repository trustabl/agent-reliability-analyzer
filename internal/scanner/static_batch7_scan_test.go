package scanner_test

import (
	"os"
	"path/filepath"
	"sort"
	"testing"
)

// Static batch 7 over the corpus fixture: each rule fires at the flow of the unguarded shape; the
// safe/ folder and the test paths add no finding.
func TestScan_StaticBatch7Fixture(t *testing.T) {
	files := map[string]string{}
	root := "../../testdata/corpus/static-batch7-fixture"
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
		"MCP-218":    {"server.py"},
		"MCP-221":    {"server.py"},
		"MCP-222":    {"tools.ts"},
		"MCP-223":    {"server.py"},
		"RAW-003":    {"agent.py"},
		"CSDK-209":   {"agent.py"},
		"CSKILL-094": {".claude/skills/diag/SKILL.md"},
	}
	for id, w := range want {
		var got []string
		for _, f := range res.Findings {
			if f.RuleID == id {
				got = append(got, f.FilePath)
			}
		}
		sort.Strings(got)
		if len(got) != len(w) || (len(w) == 1 && got[0] != w[0]) {
			t.Errorf("%s: got %v, want %v", id, got, w)
		}
	}
}
