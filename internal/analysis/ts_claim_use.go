package analysis

import (
	"strings"

	sitter "github.com/smacker/go-tree-sitter"

	"github.com/trustabl/trustabl/internal/analysis/astutil"
	"github.com/trustabl/trustabl/internal/models"
)

// tsClaimSinkMethods are the method names whose arguments are output, not a
// decision, on any receiver (console.log, logger.info, this.log.debug).
var tsClaimSinkMethods = map[string]bool{
	"debug": true, "info": true, "warn": true, "warning": true,
	"error": true, "log": true, "trace": true,
}

// tsClaimUse classifies how a TypeScript tool's execute function uses the
// `claim` key of its input, returning one of the models.ClaimUse* values. The
// caller has already checked that `claim` is a parameters key.
//
// The input is the function's first parameter. When it is destructured, the
// claim is the local name bound to the `claim` key (`({ claim })`, or
// `({ claim: c })`, which binds c); a destructuring that omits the key reads
// nothing. When it is an identifier (input), the claim is input.claim or
// input["claim"], and a read of the whole input counts as a claim read, since a
// callee handed the input may check it. Each read is classified as
// pythonClaimUse classifies one: an argument of an output call (console.*,
// JSON.stringify, String, a logger method) or part of a returned value is a
// passthrough read; anything else is checked.
func tsClaimUse(execute *sitter.Node, src []byte) string {
	body := execute.ChildByFieldName("body")
	input := tsFirstParamPattern(execute)
	if body == nil || input == nil {
		return models.ClaimUseUnused
	}

	var isRead func(n *sitter.Node) (*sitter.Node, bool)
	switch input.Type() {
	case "identifier":
		name := astutil.NodeText(input, src)
		isRead = func(n *sitter.Node) (*sitter.Node, bool) {
			if n.Type() != "identifier" || astutil.NodeText(n, src) != name {
				return nil, false
			}
			return tsInputClaimRead(n, src)
		}
	case "object_pattern":
		local := tsDestructuredClaim(input, src)
		if local == "" {
			return models.ClaimUseUnused
		}
		isRead = func(n *sitter.Node) (*sitter.Node, bool) {
			t := n.Type()
			if (t == "identifier" || t == "shorthand_property_identifier") && astutil.NodeText(n, src) == local {
				return n, true
			}
			return nil, false
		}
	default:
		return models.ClaimUseUnused
	}

	reads, passthrough := 0, 0
	astutil.Walk(body, func(n *sitter.Node) bool {
		read, ok := isRead(n)
		if !ok {
			return true
		}
		reads++
		if tsClaimReadIsPassthrough(read, execute, src) {
			passthrough++
		}
		return true
	})
	switch {
	case reads == 0:
		return models.ClaimUseUnused
	case passthrough == reads:
		return models.ClaimUsePassthrough
	default:
		return models.ClaimUseChecked
	}
}

// tsFirstParamPattern returns the binding of a function's first parameter (an
// identifier or an object_pattern), or nil.
func tsFirstParamPattern(fn *sitter.Node) *sitter.Node {
	if p := fn.ChildByFieldName("parameter"); p != nil {
		return p // x => ...
	}
	params := fn.ChildByFieldName("parameters")
	if params == nil {
		return nil
	}
	for i := 0; i < int(params.NamedChildCount()); i++ {
		p := params.NamedChild(i)
		switch p.Type() {
		case "required_parameter", "optional_parameter":
			return p.ChildByFieldName("pattern")
		}
	}
	return nil
}

// tsDestructuredClaim returns the local name an object pattern binds the
// `claim` key to, or "".
func tsDestructuredClaim(pattern *sitter.Node, src []byte) string {
	for i := 0; i < int(pattern.NamedChildCount()); i++ {
		c := pattern.NamedChild(i)
		switch c.Type() {
		case "shorthand_property_identifier_pattern":
			if astutil.NodeText(c, src) == claimParam {
				return claimParam
			}
		case "object_assignment_pattern":
			if l := c.ChildByFieldName("left"); astutil.NodeText(l, src) == claimParam {
				return claimParam
			}
		case "pair_pattern":
			if unquoteKey(astutil.NodeText(c.ChildByFieldName("key"), src)) != claimParam {
				continue
			}
			if v := c.ChildByFieldName("value"); v != nil && v.Type() == "identifier" {
				return astutil.NodeText(v, src)
			}
		}
	}
	return ""
}

func unquoteKey(k string) string {
	if len(k) >= 2 && (k[0] == '"' || k[0] == '\'') {
		return k[1 : len(k)-1]
	}
	return k
}

// tsInputClaimRead decides whether one read of the input identifier reads the
// claim: input.claim and input["claim"] do, another named key does not, and
// any other use of input is a whole-input read.
func tsInputClaimRead(n *sitter.Node, src []byte) (*sitter.Node, bool) {
	p := n.Parent()
	if p == nil {
		return n, true
	}
	if obj := p.ChildByFieldName("object"); obj == nil || !obj.Equal(n) {
		return n, true
	}
	switch p.Type() {
	case "member_expression":
		return p, astutil.NodeText(p.ChildByFieldName("property"), src) == claimParam
	case "subscript_expression":
		idx := p.ChildByFieldName("index")
		if idx != nil && idx.Type() == "string" {
			return p, unquoteKey(astutil.NodeText(idx, src)) == claimParam
		}
		return p, true
	}
	return n, true
}

// tsClaimReadIsPassthrough follows one claim read outward, stopping at fn. An
// expression body of an arrow function is its return value.
func tsClaimReadIsPassthrough(n, fn *sitter.Node, src []byte) bool {
	cur := n
	for p := n.Parent(); p != nil; cur, p = p, p.Parent() {
		if p.Equal(fn) {
			return true // reached the arrow function's expression body
		}
		switch p.Type() {
		case "call_expression":
			args := p.ChildByFieldName("arguments")
			if args == nil || !args.Equal(cur) {
				continue // claim.method(...): a value read from the claim
			}
			return isTSClaimSinkCallee(p.ChildByFieldName("function"), src)
		case "return_statement":
			return true
		case "expression_statement", "lexical_declaration", "variable_declaration",
			"assignment_expression", "augmented_assignment_expression",
			"if_statement", "while_statement", "do_statement", "for_statement",
			"for_in_statement", "switch_statement", "throw_statement",
			"statement_block":
			return false
		}
	}
	return false
}

func isTSClaimSinkCallee(callee *sitter.Node, src []byte) bool {
	if callee == nil {
		return false
	}
	text := astutil.NodeText(callee, src)
	if text == "JSON.stringify" || text == "String" || strings.HasPrefix(text, "console.") {
		return true
	}
	if callee.Type() == "member_expression" {
		return tsClaimSinkMethods[astutil.NodeText(callee.ChildByFieldName("property"), src)]
	}
	return false
}

// stampTSOpenAIClaim sets the claim_use fact on a TS OpenAI Agents tool whose
// parameters declare `claim`, and returns facts (allocated when needed). A
// tool input guardrail (`inputGuardrails: [...]` on the tool) runs before the
// tool with the call's arguments and can reject it, so it may check the claim:
// an unchecked claim on such a tool is ClaimUseWrapped.
func stampTSOpenAIClaim(opts *sitter.Node, params []string, facts map[string]string, src []byte) map[string]string {
	declared := false
	for _, p := range params {
		if p == claimParam {
			declared = true
			break
		}
	}
	execute := tsObjectMethodOrValue(opts, "execute", src)
	if !declared || execute == nil {
		return facts
	}
	switch execute.Type() {
	case "arrow_function", "function_expression", "function", "method_definition":
	default:
		return facts // a reference to a function defined elsewhere
	}
	use := tsClaimUse(execute, src)
	if use != models.ClaimUseChecked && getObjectProperty(opts, "inputGuardrails", src) != nil {
		use = models.ClaimUseWrapped
	}
	if facts == nil {
		facts = map[string]string{}
	}
	facts[models.FactClaimUse] = use
	return facts
}

// tsObjectMethodOrValue returns the value of the object property key, or the
// method_definition when the property is written in method shorthand
// (`async execute(input) { ... }`).
func tsObjectMethodOrValue(obj *sitter.Node, key string, src []byte) *sitter.Node {
	if v := getObjectProperty(obj, key, src); v != nil {
		return v
	}
	for i := 0; i < int(obj.NamedChildCount()); i++ {
		m := obj.NamedChild(i)
		if m.Type() == "method_definition" && astutil.NodeText(m.ChildByFieldName("name"), src) == key {
			return m
		}
	}
	return nil
}
