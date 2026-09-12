package forge

import (
	"testing"
)

func TestStamp_Line_WithSHA(t *testing.T) {
	s := Stamp{Date: "2026-08-11", SHA: "abc1234", Schema: 13, SDKs: []string{"claude_sdk", "mcp"}, Template: 1}
	want := "<!-- generated: 2026-08-11 | rules: abc1234 | schema: 13 | sdks: claude_sdk, mcp | template: 1 -->"
	if got := s.Line(); got != want {
		t.Errorf("Line() = %q, want %q", got, want)
	}
}

func TestStamp_Line_EmptySHA(t *testing.T) {
	s := Stamp{Date: "2026-08-11", Schema: 13, SDKs: []string{"claude_sdk"}}
	if got := s.Line(); got != "" {
		t.Errorf("Line() with empty SHA = %q, want empty string", got)
	}
}

func TestStamp_Line_NoSDKs(t *testing.T) {
	s := Stamp{Date: "2026-08-11", SHA: "abc1234", Schema: 13, Template: 1}
	want := "<!-- generated: 2026-08-11 | rules: abc1234 | schema: 13 | sdks:  | template: 1 -->"
	if got := s.Line(); got != want {
		t.Errorf("Line() with nil SDKs = %q, want %q", got, want)
	}
}

func TestParseStamp_RoundTrip(t *testing.T) {
	orig := Stamp{
		Date:     "2026-08-11",
		SHA:      "abc1234def5678",
		Schema:   13,
		SDKs:     []string{"claude_sdk", "mcp"},
		Template: 1,
	}
	content := "# Header\n\n" + orig.Line() + "\n\nsome body text"
	got, ok := ParseStamp(content)
	if !ok {
		t.Fatal("ParseStamp returned false for valid stamp")
	}
	if got.Date != orig.Date {
		t.Errorf("Date: got %q, want %q", got.Date, orig.Date)
	}
	if got.SHA != orig.SHA {
		t.Errorf("SHA: got %q, want %q", got.SHA, orig.SHA)
	}
	if got.Schema != orig.Schema {
		t.Errorf("Schema: got %d, want %d", got.Schema, orig.Schema)
	}
	if got.Template != orig.Template {
		t.Errorf("Template: got %d, want %d", got.Template, orig.Template)
	}
	if len(got.SDKs) != len(orig.SDKs) {
		t.Errorf("SDKs: got %v, want %v", got.SDKs, orig.SDKs)
	}
}

func TestParseStamp_NoStamp(t *testing.T) {
	cases := []string{
		"",
		"# Just a regular markdown file\n\nNo stamp here.",
		"<!-- some other comment -->",
		"<!-- generated: bad | format -->",
	}
	for _, c := range cases {
		if _, ok := ParseStamp(c); ok {
			t.Errorf("ParseStamp(%q) returned true, want false", c)
		}
	}
}

func TestParseStamp_MidFile(t *testing.T) {
	// Stamp can appear anywhere in the content, not just the first line.
	s := Stamp{Date: "2026-01-01", SHA: "deadbeef", Schema: 1, SDKs: []string{"openai_sdk"}, Template: 1}
	content := "lots of content before\n\n" + s.Line() + "\n\nlots of content after"
	got, ok := ParseStamp(content)
	if !ok {
		t.Fatal("ParseStamp returned false")
	}
	if got.SHA != s.SHA {
		t.Errorf("SHA: got %q, want %q", got.SHA, s.SHA)
	}
}

func TestParseStamp_Legacy4Field(t *testing.T) {
	// A stamp written before the template field existed must still parse,
	// and must report template 1 (the layout that predates the field).
	content := "# Header\n\n" +
		"<!-- generated: 2026-08-11 | rules: abc1234 | schema: 13 | sdks: claude_sdk, mcp -->" +
		"\n\nbody"
	got, ok := ParseStamp(content)
	if !ok {
		t.Fatal("ParseStamp returned false for a valid legacy 4-field stamp")
	}
	if got.Template != 1 {
		t.Errorf("Template: got %d, want 1", got.Template)
	}
	if len(got.SDKs) != 2 || got.SDKs[0] != "claude_sdk" || got.SDKs[1] != "mcp" {
		t.Errorf("SDKs corrupted by arity handling: got %v, want [claude_sdk mcp]", got.SDKs)
	}
}

func TestParseStamp_5Field(t *testing.T) {
	content := "# Header\n\n" +
		"<!-- generated: 2026-08-11 | rules: abc1234 | schema: 13 | sdks: claude_sdk, mcp | template: 7 -->" +
		"\n\nbody"
	got, ok := ParseStamp(content)
	if !ok {
		t.Fatal("ParseStamp returned false for a valid 5-field stamp")
	}
	if got.Template != 7 {
		t.Errorf("Template: got %d, want 7", got.Template)
	}
	// Regression guard: SplitN arity must not absorb the template field into sdks.
	if len(got.SDKs) != 2 || got.SDKs[1] != "mcp" {
		t.Errorf("SDKs corrupted: got %v, want [claude_sdk mcp]", got.SDKs)
	}
}

func TestParseStamp_MalformedTemplate(t *testing.T) {
	cases := []string{
		"<!-- generated: 2026-08-11 | rules: abc1234 | schema: 13 | sdks: mcp | template: x -->",
		"<!-- generated: 2026-08-11 | rules: abc1234 | schema: 13 | sdks: mcp | tmpl: 2 -->",
		"<!-- generated: 2026-08-11 | rules: abc1234 | schema: 13 | sdks: mcp | template: 2 | extra: 1 -->",
		"<!-- generated: 2026-08-11 | rules: abc1234 | schema: 13 | sdks: mcp | template: 0 -->",
		"<!-- generated: 2026-08-11 | rules: abc1234 | schema: 13 | sdks: mcp | template: -3 -->",
	}
	for _, c := range cases {
		if _, ok := ParseStamp(c); ok {
			t.Errorf("ParseStamp accepted malformed stamp: %s", c)
		}
	}
}
