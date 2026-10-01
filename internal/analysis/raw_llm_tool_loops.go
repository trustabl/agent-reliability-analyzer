package analysis

import (
	"strings"

	sitter "github.com/smacker/go-tree-sitter"

	"github.com/trustabl/trustabl/internal/analysis/astutil"
)

// Raw provider-SDK tool-loop discovery (category raw_llm_sdk).
//
// A repo that drives the bare `anthropic` / `openai` Python client in a manual
// tool-calling loop can feed a tool's output back into the conversation as
// trusted instructions — built into a dynamic system prompt instead of returned
// through the API's structured tool-result mechanism (Anthropic `tool_result`
// content blocks, OpenAI `role: "tool"` messages). The structured channel is
// what lets the model treat the content as data; a system-role message promotes
// attacker-influenced tool output to instruction authority.
//
// This is a deliberately NARROW heuristic, not a general "is this loop safe"
// check. Per function (and per vendor) it requires ALL of:
//
//  1. the file imports the vendor SDK (`anthropic` / `openai`, not the `agents`
//     framework);
//  2. the function calls `.messages.create(...)` (Anthropic) or
//     `.chat.completions.create(...)` (OpenAI) with a `tools=` kwarg — manual
//     tool calling, not a plain chat completion;
//  3. a system prompt that is built at runtime — Anthropic: the `system=` kwarg
//     of that call (the Messages API takes no role "system" message); OpenAI: a
//     dict literal {"role": "system"|"developer", "content": ...} — AND that
//     prompt references a name tainted by tool use in the same function.
//
// "Tainted" is a same-function, name-level dataflow: a name assigned from an
// expression containing the model's tool-call arguments (`<x>.arguments` for
// OpenAI, `<x>.input` for Anthropic) or any already-tainted name — so the usual
// `args = json.loads(tc.function.arguments); result = run(**args)` chain taints
// both `args` and `result`. A dynamic system prompt that only interpolates a
// date or a username is NOT flagged. What it cannot see: flow across functions
// or files, the Responses API, dict-building via helpers, and attribute/
// subscript targets (self.x = ...).

// DetectRawLLMToolOutputInSystem reports whether any Python file shows the
// Anthropic and/or OpenAI anti-pattern described above.
func DetectRawLLMToolOutputInSystem(files []ParsedFile) (anthropic, openai bool) {
	for _, pf := range files {
		if pf.Tree == nil {
			continue
		}
		impA := fileImportsModule(pf, func(m string) bool { return m == "anthropic" || strings.HasPrefix(m, "anthropic.") })
		impO := fileImportsModule(pf, func(m string) bool { return m == "openai" || strings.HasPrefix(m, "openai.") })
		if !impA && !impO {
			continue
		}
		astutil.Walk(pf.Tree.RootNode(), func(n *sitter.Node) bool {
			if n.Type() != "function_definition" {
				return true
			}
			if impA && !anthropic && funcFeedsToolOutputToSystem(n, pf.Source, true) {
				anthropic = true
			}
			if impO && !openai && funcFeedsToolOutputToSystem(n, pf.Source, false) {
				openai = true
			}
			return true
		})
		if anthropic && openai {
			break
		}
	}
	return anthropic, openai
}

func funcFeedsToolOutputToSystem(fn *sitter.Node, src []byte, isAnthropic bool) bool {
	calleeSuffix, sourceAttr := ".chat.completions.create", "arguments"
	if isAnthropic {
		calleeSuffix, sourceAttr = ".messages.create", "input"
	}

	// 1. The manual tool-calling call(s) in this function.
	var toolCalls []*sitter.Node
	astutil.Walk(fn, func(n *sitter.Node) bool {
		if n.Type() != "call" {
			return true
		}
		if !strings.HasSuffix(astutil.NodeText(n.ChildByFieldName("function"), src), calleeSuffix) {
			return true
		}
		if kwargValueNode(n, "tools", src) != nil {
			toolCalls = append(toolCalls, n)
		}
		return true
	})
	if len(toolCalls) == 0 {
		return false
	}

	tainted := taintedNames(fn, src, sourceAttr)
	if len(tainted) == 0 {
		return false
	}

	// 2. A runtime-built system prompt referencing a tainted name.
	if isAnthropic {
		for _, c := range toolCalls {
			if v := kwargValueNode(c, "system", src); v != nil && dynamicRefsTainted(v, src, tainted) {
				return true
			}
		}
		return false
	}
	found := false
	astutil.Walk(fn, func(n *sitter.Node) bool {
		if found || n.Type() != "dictionary" {
			return !found
		}
		role := dictChildren(n, src)["role"]
		if role == nil || role.Value == nil || role.Value.Kind != "literal_string" {
			return true
		}
		if r := strings.Trim(role.Value.Text, `"'`); r != "system" && r != "developer" {
			return true
		}
		if v := dictPairValueNode(n, "content", src); v != nil && dynamicRefsTainted(v, src, tainted) {
			found = true
		}
		return true
	})
	return found
}

// taintedNames returns the identifiers in fn assigned (transitively, by name)
// from an expression containing an attribute access ending in sourceAttr.
func taintedNames(fn *sitter.Node, src []byte, sourceAttr string) map[string]bool {
	type assign struct {
		lhs string
		rhs *sitter.Node
	}
	var assigns []assign
	astutil.Walk(fn, func(n *sitter.Node) bool {
		if n.Type() != "assignment" && n.Type() != "augmented_assignment" {
			return true
		}
		l, r := n.ChildByFieldName("left"), n.ChildByFieldName("right")
		if l != nil && l.Type() == "identifier" && r != nil {
			assigns = append(assigns, assign{astutil.NodeText(l, src), r})
		}
		return true
	})
	tainted := map[string]bool{}
	for changed, rounds := true, 0; changed && rounds < 8; rounds++ {
		changed = false
		for _, a := range assigns {
			if tainted[a.lhs] {
				continue
			}
			if exprHasSourceAttr(a.rhs, src, sourceAttr) || exprRefsAny(a.rhs, src, tainted) {
				tainted[a.lhs] = true
				changed = true
			}
		}
	}
	return tainted
}

func exprHasSourceAttr(n *sitter.Node, src []byte, attr string) bool {
	found := false
	astutil.Walk(n, func(c *sitter.Node) bool {
		if found {
			return false
		}
		if c.Type() == "attribute" && astutil.NodeText(c.ChildByFieldName("attribute"), src) == attr {
			found = true
		}
		return !found
	})
	return found
}

func exprRefsAny(n *sitter.Node, src []byte, names map[string]bool) bool {
	if len(names) == 0 {
		return false
	}
	found := false
	astutil.Walk(n, func(c *sitter.Node) bool {
		if found {
			return false
		}
		if c.Type() == "identifier" && names[astutil.NodeText(c, src)] {
			found = true
		}
		return !found
	})
	return found
}

// dynamicRefsTainted reports whether v is built at runtime (anything other than
// a plain string literal — tree-sitter types an f-string as `string` with
// `interpolation` children, so a bare node-type check is not enough) AND
// references a tainted name.
func dynamicRefsTainted(v *sitter.Node, src []byte, tainted map[string]bool) bool {
	if v.Type() == "string" && !stringHasInterpolation(v) {
		return false
	}
	return exprRefsAny(v, src, tainted)
}

func stringHasInterpolation(s *sitter.Node) bool {
	for i := 0; i < int(s.NamedChildCount()); i++ {
		if s.NamedChild(i).Type() == "interpolation" {
			return true
		}
	}
	return false
}

// kwargValueNode returns the value node of the named keyword argument of a call.
func kwargValueNode(call *sitter.Node, name string, src []byte) *sitter.Node {
	args := call.ChildByFieldName("arguments")
	if args == nil {
		return nil
	}
	for i := 0; i < int(args.NamedChildCount()); i++ {
		c := args.NamedChild(i)
		if c.Type() == "keyword_argument" && astutil.NodeText(c.ChildByFieldName("name"), src) == name {
			return c.ChildByFieldName("value")
		}
	}
	return nil
}

// dictPairValueNode returns the value node for a string-literal key of a dict
// literal (the node-level companion of dictChildren, which only keeps Exprs).
func dictPairValueNode(d *sitter.Node, key string, src []byte) *sitter.Node {
	for i := 0; i < int(d.NamedChildCount()); i++ {
		pair := d.NamedChild(i)
		if pair.Type() != "pair" {
			continue
		}
		k := pair.ChildByFieldName("key")
		if k != nil && k.Type() == "string" && strings.Trim(astutil.NodeText(k, src), `"'`) == key {
			return pair.ChildByFieldName("value")
		}
	}
	return nil
}
