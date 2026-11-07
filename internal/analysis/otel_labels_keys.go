package analysis

import (
	"slices"
	"strings"

	sitter "github.com/smacker/go-tree-sitter"

	"github.com/trustabl/trustabl/internal/analysis/astutil"
	"github.com/trustabl/trustabl/internal/models"
)

// otelLabelsKeys are the span attribute keys agent-reliability-otel-labels
// writes. TestOtelLabelsKeys_MatchVendoredManifest keeps this table equal to
// testdata/forge/otel/otel-spec-keys.json.
var otelLabelsKeys = map[string]bool{
	"trustabl.step_index":       true,
	"trustabl.run_id":           true,
	"trustabl.tool.input_fp":    true,
	"trustabl.tool.output_fp":   true,
	"trustabl.tool.side_effect": true,
	"trustabl.tool.error_class": true,
	"trustabl.tool.attempt":     true,
	"trustabl.handoff_id":       true,
	"trustabl.exit_reason":      true,
}

// OtelLabelsAttributeKeys returns the label keys, sorted.
func OtelLabelsAttributeKeys() []string {
	out := make([]string, 0, len(otelLabelsKeys))
	for k := range otelLabelsKeys {
		out = append(out, k)
	}
	slices.Sort(out)
	return out
}

// otelLabelsAttrSignals returns one attr_literal signal per label key the file
// writes to a span by hand: the first argument of set_attribute /
// setAttribute, or a key of the dict / object literal passed to set_attributes
// / setAttributes. A bare string anywhere else writes nothing and is not a
// label. Runs whether or not the library is imported: writing the keys without
// it is the case this exists for. Any key under trustabl.cot.* counts, known or
// not, and so does one of the library's constants for those keys. Such a signal
// carries ValueUnhashed when its value could be the reasoning itself.
func otelLabelsAttrSignals(pf ParsedFile, lang models.Language, test bool) []models.OtelLabelsSignal {
	callType, single, multi := "call", "set_attribute", "set_attributes"
	if lang == models.LanguageTypeScript {
		callType, single, multi = "call_expression", "setAttribute", "setAttributes"
	}
	var out []models.OtelLabelsSignal
	emit := func(call *sitter.Node, key string, value *sitter.Node) {
		if !otelLabelsKeys[key] && !strings.HasPrefix(key, otelLabelsCoTPrefix) {
			return
		}
		out = append(out, models.OtelLabelsSignal{
			Kind:       models.OtelLabelsAttrLiteral,
			File:       pf.RelPath,
			StartLine:  astutil.NodeLine(call),
			EndLine:    astutil.NodeEndLine(call),
			Language:   lang,
			TestOrigin: test,
			Key:        key,

			ValueUnhashed: otelLabelsValueUnhashed(key, value, pf.Source, lang),
		})
	}
	astutil.Walk(pf.Tree.RootNode(), func(n *sitter.Node) bool {
		if n.Type() != callType {
			return true
		}
		callee := astutil.NodeText(n.ChildByFieldName("function"), pf.Source)
		name := callee
		if i := strings.LastIndex(callee, "."); i >= 0 {
			name = callee[i+1:]
		}
		if name != single && name != multi {
			return true
		}
		args := n.ChildByFieldName("arguments")
		if args == nil {
			return true
		}
		first := firstPositionalArg(args)
		if first == nil {
			return true
		}
		if name == single {
			if key, ok := otelLabelsAttrKey(first, pf.Source, lang); ok {
				emit(n, key, otelLabelsSingleValue(args, pf.Source))
			}
			return true
		}
		if first.Type() != "dictionary" && first.Type() != "object" {
			return true
		}
		for i := 0; i < int(first.NamedChildCount()); i++ {
			p := first.NamedChild(i)
			if p.Type() != "pair" {
				continue
			}
			if key, ok := otelLabelsAttrKey(p.ChildByFieldName("key"), pf.Source, lang); ok {
				emit(n, key, p.ChildByFieldName("value"))
			}
		}
		return true
	})
	return out
}

func otelLabelsStringLiteral(n *sitter.Node, src []byte, lang models.Language) (string, bool) {
	if n == nil || n.Type() != "string" {
		return "", false
	}
	if lang == models.LanguagePython {
		return pyStringLiteral(astutil.NodeText(n, src))
	}
	s := tsStringValue(n, src)
	return s, s != ""
}

// pyStringLiteral returns the content of a plain Python string literal ("x",
// 'x', or a triple-quoted form). A prefixed string (f"", b"", r"") or an
// implicit concatenation is not a literal here: an f-string can vary per run.
func pyStringLiteral(text string) (string, bool) {
	text = strings.TrimSpace(text)
	if len(text) < 2 {
		return "", false
	}
	q := text[0]
	if (q != '"' && q != '\'') || text[len(text)-1] != q {
		return "", false
	}
	inner := strings.Trim(text, string(q))
	if strings.IndexByte(inner, q) >= 0 {
		return "", false
	}
	return inner, true
}

// tsStringValue returns the content of a TypeScript string literal node, or ""
// when the node is absent or not a plain string (a template string can vary).
func tsStringValue(n *sitter.Node, src []byte) string {
	if n == nil || n.Type() != "string" {
		return ""
	}
	t := astutil.NodeText(n, src)
	if len(t) < 2 {
		return ""
	}
	return t[1 : len(t)-1]
}

// firstPositionalArg returns the first argument that is not a Python keyword
// argument or a comment.
func firstPositionalArg(args *sitter.Node) *sitter.Node {
	for i := 0; i < int(args.NamedChildCount()); i++ {
		c := args.NamedChild(i)
		if c.Type() == "keyword_argument" || c.Type() == "comment" {
			continue
		}
		return c
	}
	return nil
}
