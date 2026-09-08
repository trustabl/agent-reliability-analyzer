package forge

import (
	"fmt"
	"strconv"
	"strings"
)

// Stamp holds provenance metadata embedded in a forge-generated SKILL.md.
type Stamp struct {
	Date     string   // YYYY-MM-DD
	SHA      string   // resolved rules commit SHA
	Schema   int      // pack manifest schema_version
	SDKs     []string // sorted detected SDK IDs
	Template int      // emitted layout version; >= 1 on a successful parse
}

// stampLine renders the stamp comment. It is the single definition of the
// stamp format — both Stamp.Line and PolicyStamp.Line delegate here so the
// format cannot drift between the two generators. Returns empty string when
// sha is empty so a zero-value stamp silently omits the comment.
func stampLine(date, sha string, schema int, sdks []string, template int) string {
	if sha == "" {
		return ""
	}
	return fmt.Sprintf(
		"<!-- generated: %s | rules: %s | schema: %d | sdks: %s | template: %d -->",
		date, sha, schema, strings.Join(sdks, ", "), template,
	)
}

// Line renders the stamp as an HTML comment. Returns empty string when SHA is
// empty so a zero-value Stamp silently omits the comment.
func (s Stamp) Line() string {
	return stampLine(s.Date, s.SHA, s.Schema, s.SDKs, s.Template)
}

// ParseStamp scans content for a forge stamp HTML comment and parses its
// fields. Returns (Stamp{}, false) when no stamp is found or the comment is
// malformed.
func ParseStamp(content string) (Stamp, bool) {
	const prefix = "<!-- generated: "
	const suffix = " -->"

	idx := strings.Index(content, prefix)
	if idx < 0 {
		return Stamp{}, false
	}
	rest := content[idx+len(prefix):]
	end := strings.Index(rest, suffix)
	if end < 0 {
		return Stamp{}, false
	}
	body := rest[:end]

	// body: "2026-08-11 | rules: abc123 | schema: 13 | sdks: claude_sdk, mcp"
	// SplitN with 5 so a 5-field stamp yields 5 parts. Indices 0..3 are
	// unchanged in both arities — the template field is appended last —
	// so `sdks` stays parts[3] and is never absorbed by the extra field.
	parts := strings.SplitN(body, " | ", 5)
	if len(parts) < 4 || len(parts) > 5 {
		return Stamp{}, false
	}

	date := parts[0]

	rulesVal := strings.TrimPrefix(parts[1], "rules: ")
	if rulesVal == parts[1] { // prefix not found
		return Stamp{}, false
	}

	schemaStr := strings.TrimPrefix(parts[2], "schema: ")
	if schemaStr == parts[2] {
		return Stamp{}, false
	}
	schema, err := strconv.Atoi(schemaStr)
	if err != nil {
		return Stamp{}, false
	}

	sdksStr := strings.TrimPrefix(parts[3], "sdks: ")
	var sdks []string
	for _, s := range strings.Split(sdksStr, ", ") {
		if s = strings.TrimSpace(s); s != "" {
			sdks = append(sdks, s)
		}
	}

	// A stamp with no template field predates the field; that layout is
	// version 1. A successful parse therefore always yields Template >= 1.
	template := 1
	if len(parts) == 5 {
		tmplStr := strings.TrimPrefix(parts[4], "template: ")
		if tmplStr == parts[4] {
			return Stamp{}, false
		}
		parsed, err := strconv.Atoi(tmplStr)
		if err != nil {
			return Stamp{}, false
		}
		if parsed < 1 {
			return Stamp{}, false
		}
		template = parsed
	}

	return Stamp{Date: date, SHA: rulesVal, Schema: schema, SDKs: sdks, Template: template}, true
}
