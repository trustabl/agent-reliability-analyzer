package analysis

import (
	sitter "github.com/smacker/go-tree-sitter"

	"github.com/trustabl/trustabl/internal/analysis/astutil"
	"github.com/trustabl/trustabl/internal/models"
)

// claudeInputKeys returns the keys of a Claude Agent SDK Python tool's input
// schema: the third positional argument (or input_schema=) of the first
// decorator call that has one, @tool(name, description, schema). The schema is
// a short dict ({"to": str}), the JSON-schema form (the keys of "properties"),
// or the name of a same-file TypedDict class or module-level dict. Returns nil
// when no schema resolves.
func claudeInputKeys(dec, root *sitter.Node, src []byte) []string {
	for _, d := range astutil.Decorators(dec) {
		call := d.NamedChild(0)
		if call == nil || call.Type() != "call" {
			continue
		}
		schema := positionalArgNode(call, 2)
		if schema == nil {
			schema = keywordArgValue(call, "input_schema", src)
		}
		if schema != nil {
			return schemaKeys(schema, root, src)
		}
	}
	return nil
}

// keywordArgValue returns the value node of the call's direct keyword argument
// name, or nil.
func keywordArgValue(call *sitter.Node, name string, src []byte) *sitter.Node {
	args := call.ChildByFieldName("arguments")
	if args == nil {
		return nil
	}
	for i := 0; i < int(args.NamedChildCount()); i++ {
		kw := args.NamedChild(i)
		if kw.Type() == "keyword_argument" && astutil.NodeText(kw.ChildByFieldName("name"), src) == name {
			return kw.ChildByFieldName("value")
		}
	}
	return nil
}

func schemaKeys(schema, root *sitter.Node, src []byte) []string {
	switch schema.Type() {
	case "dictionary":
		if props := dictValue(schema, "properties", src); props != nil && props.Type() == "dictionary" {
			return dictKeys(props, src)
		}
		return dictKeys(schema, src)
	case "identifier":
		name := astutil.NodeText(schema, src)
		for i := 0; i < int(root.NamedChildCount()); i++ {
			c := root.NamedChild(i)
			switch c.Type() {
			case "class_definition":
				if astutil.NodeText(c.ChildByFieldName("name"), src) == name {
					return classAnnotatedFields(c, src)
				}
			case "expression_statement":
				a := c.NamedChild(0)
				if a != nil && a.Type() == "assignment" &&
					astutil.NodeText(a.ChildByFieldName("left"), src) == name {
					if right := a.ChildByFieldName("right"); right != nil && right.Type() == "dictionary" {
						return schemaKeys(right, root, src)
					}
				}
			}
		}
	}
	return nil
}

// dictValue returns the value of the string key in a dict literal, or nil.
func dictValue(dict *sitter.Node, key string, src []byte) *sitter.Node {
	for i := 0; i < int(dict.NamedChildCount()); i++ {
		p := dict.NamedChild(i)
		if p.Type() != "pair" {
			continue
		}
		if k, ok := pyStringLiteral(astutil.NodeText(p.ChildByFieldName("key"), src)); ok && k == key {
			return p.ChildByFieldName("value")
		}
	}
	return nil
}

// dictKeys returns the string-literal keys of a dict literal, in order.
func dictKeys(dict *sitter.Node, src []byte) []string {
	var out []string
	for i := 0; i < int(dict.NamedChildCount()); i++ {
		p := dict.NamedChild(i)
		if p.Type() != "pair" {
			continue
		}
		if k, ok := pyStringLiteral(astutil.NodeText(p.ChildByFieldName("key"), src)); ok {
			out = append(out, k)
		}
	}
	return out
}

// classAnnotatedFields returns the annotated field names in a class body
// (`to: str`), the keys of a TypedDict.
func classAnnotatedFields(class *sitter.Node, src []byte) []string {
	body := class.ChildByFieldName("body")
	if body == nil {
		return nil
	}
	var out []string
	for i := 0; i < int(body.NamedChildCount()); i++ {
		s := body.NamedChild(i)
		if s.Type() != "expression_statement" {
			continue
		}
		a := s.NamedChild(0)
		if a == nil || a.Type() != "assignment" || a.ChildByFieldName("type") == nil {
			continue
		}
		if left := a.ChildByFieldName("left"); left != nil && left.Type() == "identifier" {
			out = append(out, astutil.NodeText(left, src))
		}
	}
	return out
}

// claudeClaimUse classifies how a Claude Agent SDK Python tool uses the `claim`
// key of its input, returning one of the models.ClaimUse* values, or "" when
// the input schema has no such key. The handler takes the input as one dict
// (its first parameter, conventionally args), so a read of the claim is
// args["claim"] or args.get("claim"); each is classified as pythonClaimUse
// classifies a read of a `claim` parameter. A read of the whole dict (handed
// to another call, or written out) counts as a read of the claim too, since
// the callee may check it. Reads of other keys are ignored.
func claudeClaimUse(fn, dec *sitter.Node, inputKeys []string, src []byte) string {
	declared := false
	for _, k := range inputKeys {
		if k == claimParam {
			declared = true
			break
		}
	}
	if !declared {
		return ""
	}
	if dec != nil && len(astutil.Decorators(dec)) > 1 {
		return models.ClaimUseWrapped
	}
	params := astutil.FunctionParams(fn, src)
	body := fn.ChildByFieldName("body")
	if len(params) == 0 || body == nil {
		return models.ClaimUseUnused
	}
	argsName := params[0]

	reads, passthrough := 0, 0
	astutil.Walk(body, func(n *sitter.Node) bool {
		if n.Type() != "identifier" || astutil.NodeText(n, src) != argsName || !isClaimRead(n) {
			return true
		}
		read, ok := claudeClaimRead(n, src)
		if !ok {
			return true
		}
		reads++
		if claimReadIsPassthrough(read, fn, src) {
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

// claudeClaimRead decides whether one read of the args dict reads the claim.
// It returns the node to classify (args["claim"], args.get("claim"), or args
// itself for a whole-dict read) and false for a read of another literal key.
func claudeClaimRead(n *sitter.Node, src []byte) (*sitter.Node, bool) {
	p := n.Parent()
	if p == nil {
		return n, true
	}
	switch p.Type() {
	case "subscript":
		if v := p.ChildByFieldName("value"); v != nil && v.Equal(n) {
			if key, ok := pyStringLiteral(astutil.NodeText(p.ChildByFieldName("subscript"), src)); ok {
				return p, key == claimParam
			}
			return p, true
		}
	case "attribute":
		if astutil.NodeText(p.ChildByFieldName("attribute"), src) != "get" {
			return n, true
		}
		call := p.Parent()
		if call == nil || call.Type() != "call" {
			return n, true
		}
		if key, ok := pyStringLiteral(astutil.NodeText(positionalArgNode(call, 0), src)); ok {
			return call, key == claimParam
		}
		return call, true
	}
	return n, true
}
