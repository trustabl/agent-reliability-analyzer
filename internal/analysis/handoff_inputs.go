package analysis

import (
	"strings"

	sitter "github.com/smacker/go-tree-sitter"

	"github.com/trustabl/trustabl/internal/analysis/astutil"
	"github.com/trustabl/trustabl/internal/models"
	"github.com/trustabl/trustabl/internal/pathclass"
)

// Handoff input facts for OAI-204. The OpenAI Agents SDK documents that a handoff gives the new
// agent the entire previous conversation history unless an input_filter is set, either on the
// handoff(...) call or on a RunConfig (handoff_input_filter; handoffInputFilter in the
// TypeScript SDK), and that the per-handoff filter takes precedence.

// pyHandoffInput reads one handoffs= list entry: a bare agent name has no filter, a
// handoff(agent, input_filter=...) call has one unless the value is None. Any other entry is
// not judged.
func pyHandoffInput(item models.Expr) (models.HandoffInput, bool) {
	switch item.Kind {
	case models.ExprNameRef:
		if strings.Contains(item.Text, ".") {
			return models.HandoffInput{}, false
		}
		return models.HandoffInput{Target: item.Text}, true
	case models.ExprCall:
		callee := item.Text
		if i := strings.Index(callee, "("); i >= 0 {
			callee = callee[:i]
		}
		if callee != "handoff" && !strings.HasSuffix(callee, ".handoff") {
			return models.HandoffInput{}, false
		}
		target := ""
		if len(item.CallArgs) > 0 {
			target = item.CallArgs[0].Text
		} else if kw := item.CallKwargs["agent"]; kw != nil && kw.Value != nil {
			target = kw.Value.Text
		}
		has := false
		if kw := item.CallKwargs["input_filter"]; kw != nil && kw.Value != nil && strings.TrimSpace(kw.Value.Text) != "None" {
			has = true
		}
		return models.HandoffInput{Target: target, HasInputFilter: has}, true
	}
	return models.HandoffInput{}, false
}

// tsHandoffInputs reads the handoffs array of a TypeScript Agent options object: an identifier
// has no filter; handoff(agent, { inputFilter }) has one.
func tsHandoffInputs(opts *sitter.Node, src []byte) []models.HandoffInput {
	arr := getObjectProperty(opts, "handoffs", src)
	if arr == nil || arr.Type() != "array" {
		return nil
	}
	var out []models.HandoffInput
	for i := 0; i < int(arr.NamedChildCount()); i++ {
		item := arr.NamedChild(i)
		switch item.Type() {
		case "identifier":
			out = append(out, models.HandoffInput{Target: astutil.NodeText(item, src)})
		case "call_expression":
			fn := item.ChildByFieldName("function")
			args := item.ChildByFieldName("arguments")
			if fn == nil || args == nil || astutil.NodeText(fn, src) != "handoff" || args.NamedChildCount() == 0 {
				continue
			}
			hi := models.HandoffInput{Target: astutil.NodeText(args.NamedChild(0), src)}
			if args.NamedChildCount() > 1 && args.NamedChild(1).Type() == "object" {
				if f := getObjectProperty(args.NamedChild(1), "inputFilter", src); f != nil {
					hi.HasInputFilter = true
				} else if has, spread := tsObjectHasKey(args.NamedChild(1), "inputFilter", src); has || spread {
					hi.HasInputFilter = true
				}
			}
			out = append(out, hi)
		}
	}
	return out
}

// DiscoverHandoffInputFilterConfigured reports whether any non-test Python or TypeScript file
// passes handoff_input_filter (handoffInputFilter) to a call.
func DiscoverHandoffInputFilterConfigured(py, ts []ParsedFile) bool {
	for _, group := range []struct {
		files []ParsedFile
		name  string
	}{{py, "handoff_input_filter"}, {ts, "handoffInputFilter"}} {
		for _, pf := range group.files {
			if pf.Tree == nil || pathclass.Classify(pf.RelPath) == models.OriginTest {
				continue
			}
			if !strings.Contains(string(pf.Source), group.name) {
				continue
			}
			found := false
			astutil.Walk(pf.Tree.RootNode(), func(n *sitter.Node) bool {
				if found {
					return false
				}
				switch n.Type() {
				case "keyword_argument":
					found = astutil.NodeText(n.ChildByFieldName("name"), pf.Source) == group.name
				case "pair":
					k := astutil.NodeText(n.ChildByFieldName("key"), pf.Source)
					found = k == group.name || k == `"`+group.name+`"` || k == `'`+group.name+`'`
				case "shorthand_property_identifier":
					found = astutil.NodeText(n, pf.Source) == group.name
				}
				return !found
			})
			if found {
				return true
			}
		}
	}
	return false
}
