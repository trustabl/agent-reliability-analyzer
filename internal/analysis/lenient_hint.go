package analysis

import (
	"strings"

	"gopkg.in/yaml.v3"
)

// argumentHint decodes the argument-hint frontmatter field from a string or a
// YAML list. Claude Code's own example is `argument-hint: [issue-number]`,
// which YAML reads as a one-item list, and a strict string field fails on a
// list and drops the whole skill. A scalar keeps its text; a list is its scalar
// items joined by a space, which loses the brackets the author wrote but keeps
// the words. A map or anything else reads empty. It never fails.
type argumentHint string

func (h *argumentHint) UnmarshalYAML(value *yaml.Node) error {
	switch value.Kind {
	case yaml.ScalarNode:
		*h = argumentHint(value.Value)
	case yaml.SequenceNode:
		var parts []string
		for _, c := range value.Content {
			if c.Kind == yaml.ScalarNode {
				parts = append(parts, c.Value)
			}
		}
		*h = argumentHint(strings.Join(parts, " "))
	default:
		*h = ""
	}
	return nil
}
