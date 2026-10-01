package analysis

import (
	sitter "github.com/smacker/go-tree-sitter"

	"github.com/trustabl/trustabl/internal/analysis/astutil"
)

// Wall-clock timeout detection for agent run calls.
//
// max_turns / usage_limits cap step count and token spend, but neither bounds
// how long ONE step can block: a stalled model call or a slow tool hangs the run
// indefinitely. The only wall-clock bounds visible at a call site are an
// enclosing asyncio.wait_for(...) or a timeout context manager.
//
// The check is STRUCTURAL: it walks up from the run call looking for an
// ancestor that actually encloses it. It is deliberately not a same-file text
// search — an unrelated wait_for / timeout elsewhere in the file must not be
// credited. The walk stops at the enclosing function boundary, so a timeout
// that wraps a call to the function (rather than the run call itself) is not
// credited either (a known v1 limit, erring toward firing).

// wallClockCallees are the callees that bound a call by wall-clock time when
// they enclose it as an argument (wait_for) ...
var wallClockWaitCallees = map[string]bool{
	"asyncio.wait_for": true, "wait_for": true,
}

// ... or as a `with` context manager.
var wallClockContextCallees = map[string]bool{
	"asyncio.timeout": true, "timeout": true,
	"anyio.move_on_after": true, "move_on_after": true,
	"anyio.fail_after": true, "fail_after": true,
}

func nodeHasWallClockTimeoutAncestor(n *sitter.Node, src []byte) bool {
	child := n
	for p := n.Parent(); p != nil; child, p = p, p.Parent() {
		switch p.Type() {
		case "function_definition", "lambda", "class_definition":
			return false
		case "call":
			// n must sit inside the arguments, not merely be the callee itself.
			if args := p.ChildByFieldName("arguments"); args != nil && args.Equal(child) {
				if wallClockWaitCallees[astutil.NodeText(p.ChildByFieldName("function"), src)] {
					return true
				}
			}
		case "with_statement":
			// Only the body is bounded; the context-manager expression itself is not.
			if body := p.ChildByFieldName("body"); body != nil && body.Equal(child) && withHasTimeoutItem(p, src) {
				return true
			}
		}
	}
	return false
}

// withHasTimeoutItem reports whether any with_item of the statement is a call
// to a wall-clock timeout context manager, unwrapping `as` bindings.
func withHasTimeoutItem(w *sitter.Node, src []byte) bool {
	found := false
	astutil.Walk(w, func(n *sitter.Node) bool {
		if found || n == nil {
			return false
		}
		if n.Type() == "block" && n.Parent() != nil && n.Parent().Equal(w) {
			return false // don't descend into the body
		}
		if n.Type() != "with_item" {
			return true
		}
		v := n.ChildByFieldName("value")
		if v != nil && v.Type() == "as_pattern" && v.NamedChildCount() > 0 {
			v = v.NamedChild(0)
		}
		if v != nil && v.Type() == "call" &&
			wallClockContextCallees[astutil.NodeText(v.ChildByFieldName("function"), src)] {
			found = true
		}
		return false
	})
	return found
}
