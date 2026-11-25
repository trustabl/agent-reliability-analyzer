package analysis

import (
	"strings"

	"gopkg.in/yaml.v3"
)

// parseLenientBool reads a YAML scalar as a boolean. ok is false when the node
// is not a scalar or its text is not a recognised spelling, so a caller can
// tell "false" from "not understood".
//
// Spellings read as true: true, yes, on, y and 1. Spellings read as false:
// false, no, off, n and 0. Matching ignores letter case and surrounding
// space, and ignores quoting, because the decoder hands over the scalar text.
//
// Claude Code documents yes, no, on, off, 1 and 0 in any letter case, in
// addition to true and false, for boolean frontmatter fields. y and n are not
// in that list; they are kept because yaml.v3 already read an unquoted y or n
// as a boolean before this type existed, so dropping them would be a change.
// The docs do not say what a quoted value or an unrecognised value means; the
// callers below pick the reading, it is not a statement of how Claude Code
// parses the key.
func parseLenientBool(n *yaml.Node) (value, ok bool) {
	if n == nil || n.Kind != yaml.ScalarNode {
		return false, false
	}
	switch strings.ToLower(strings.TrimSpace(n.Value)) {
	case "true", "yes", "on", "y", "1":
		return true, true
	case "false", "no", "off", "n", "0":
		return false, true
	}
	return false, false
}

// lenientBool decodes a boolean frontmatter field without ever failing. A
// strict bool field makes yaml.v3 reject a quoted "true" or the number 1, and
// the caller then drops the whole frontmatter, so the skill or command is never
// judged. A recognised true spelling reads true; every other value, including
// an unrecognised scalar, a list or a map, reads false. Use it where false is
// the field's default and an unrecognised value should behave as if the key
// were absent (disable-model-invocation).
type lenientBool bool

func (b *lenientBool) UnmarshalYAML(value *yaml.Node) error {
	v, _ := parseLenientBool(value)
	*b = lenientBool(v)
	return nil
}

// lenientOptBool is lenientBool for a field where "absent" must stay distinct
// from "false" (user-invocable, whose default is true). An unrecognised value
// leaves the field unset instead of claiming false. A null or missing key never
// reaches UnmarshalYAML, so it is unset too.
type lenientOptBool struct {
	value bool
	set   bool
}

func (b *lenientOptBool) UnmarshalYAML(value *yaml.Node) error {
	v, ok := parseLenientBool(value)
	*b = lenientOptBool{value: v, set: ok}
	return nil
}

// ptr returns nil when the field was absent or unrecognised.
func (b lenientOptBool) ptr() *bool {
	if !b.set {
		return nil
	}
	v := b.value
	return &v
}
