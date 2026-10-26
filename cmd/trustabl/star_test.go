package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestStarPromptShownOnce(t *testing.T) {
	marker := filepath.Join(t.TempDir(), "trustabl", "star-prompt-shown")

	var first bytes.Buffer
	maybeStarPrompt(&first, "human", true, marker)
	if !strings.Contains(first.String(), "Star Agent Reliability Analyzer") {
		t.Fatalf("first interactive scan: want the star line, got %q", first.String())
	}

	var second bytes.Buffer
	maybeStarPrompt(&second, "human", true, marker)
	if second.Len() != 0 {
		t.Fatalf("second scan: want nothing, got %q", second.String())
	}
}

func TestStarPromptSkipped(t *testing.T) {
	cases := []struct {
		name        string
		format      string
		interactive bool
	}{
		{"not interactive", "human", false},
		{"json", "json", true},
		{"sarif", "sarif", true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			marker := filepath.Join(t.TempDir(), "star-prompt-shown")
			var out bytes.Buffer
			maybeStarPrompt(&out, c.format, c.interactive, marker)
			if out.Len() != 0 {
				t.Fatalf("want nothing, got %q", out.String())
			}
			if _, err := os.Stat(marker); err == nil {
				t.Fatal("marker written although the line was not shown")
			}
		})
	}
}
