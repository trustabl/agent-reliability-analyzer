package rules

import (
	"regexp"
	"strings"

	sitter "github.com/smacker/go-tree-sitter"

	"github.com/trustabl/trustabl/internal/analysis"
	"github.com/trustabl/trustabl/internal/analysis/astutil"
	"github.com/trustabl/trustabl/internal/models"
	"github.com/trustabl/trustabl/internal/pathclass"
)

// Effect values stamped on ToolDef.Effects.
const (
	EffectNamed     = "named"
	EffectShell     = "shell"
	EffectCodeExec  = "code_exec"
	EffectFileWrite = "file_write"
)

// effectNamePrefixes are the leading words of a tool name that mark a send, pay, delete or
// deploy side effect. The tool name is split at lower-to-upper case changes and at hyphens
// into underscore form and lowercased before the prefix test (normalizeToolName).
var effectNamePrefixes = []string{
	"send_", "notify_", "refund_", "charge_", "pay_", "payout_", "transfer_",
	"delete_", "drop_", "destroy_", "deploy_", "publish_", "terminate_", "revoke_", "truncate_",
}

// bodyGateCallees are the callee names, last dotted segment, that a tool body calls to stop
// for a person: LangGraph interrupt() and the Google ADK ToolContext.request_confirmation().
var bodyGateCallees = map[string]bool{"interrupt": true, "request_confirmation": true}

// bodyGateRaises are the exception names a tool body raises to ask for approval: the
// Pydantic AI ApprovalRequired.
var bodyGateRaises = []string{"ApprovalRequired"}

// AttachToolEffects stamps ToolDef.Effects and ToolDef.BodyGates on each tool in place. The
// slice must be the one the agents' ToolRef.Resolved pointers point into, so the stamp is
// visible through them. A bare shell-invoking function (not registered as an agent tool), a
// tool of unknown kind and a tool under a test path are left unstamped.
func AttachToolEffects(tools []models.ToolDef, parsed []analysis.ParsedFile) {
	byPath := make(map[string]analysis.ParsedFile, len(parsed))
	for _, pf := range parsed {
		byPath[pf.RelPath] = pf
	}
	for i := range tools {
		t := &tools[i]
		if t.Kind == models.KindShellInvocation || t.Kind == models.KindUnknown || pathclass.Classify(t.FilePath) == models.OriginTest {
			continue
		}
		name := normalizeToolName(t.Name)
		for _, p := range effectNamePrefixes {
			if strings.HasPrefix(name, p) {
				t.Effects = append(t.Effects, EffectNamed)
				break
			}
		}
		pf, ok := byPath[t.FilePath]
		if !ok {
			continue
		}
		if PredHasShellCall(*t, pf) {
			t.Effects = append(t.Effects, EffectShell)
		}
		if PredHasCodeExecCall(*t, pf) {
			t.Effects = append(t.Effects, EffectCodeExec)
		}
		if PredHasWriteCall(*t, pf) {
			t.Effects = append(t.Effects, EffectFileWrite)
		}
		if t.Language == models.LanguagePython {
			t.BodyGates = pythonBodyGates(*t, pf)
			t.BodyGates = append(t.BodyGates, assignedGateAttrs(t.Name, pf.Source)...)
		}
	}
}

// pythonBodyGates returns the approval calls a Python tool body makes: a call to a
// bodyGateCallees name, or a raise of a bodyGateRaises exception. Comments and strings are
// not read, since only call and raise nodes are walked.
func pythonBodyGates(t models.ToolDef, pf analysis.ParsedFile) []string {
	root := analysis.FindFunctionNode(t, pf)
	if root == nil {
		return nil
	}
	seen := map[string]bool{}
	var out []string
	add := func(g string) {
		if !seen[g] {
			seen[g] = true
			out = append(out, g)
		}
	}
	astutil.Walk(root, func(n *sitter.Node) bool {
		switch n.Type() {
		case "call":
			fn := n.ChildByFieldName("function")
			if fn == nil {
				return true
			}
			callee := astutil.NodeText(fn, pf.Source)
			if i := strings.LastIndex(callee, "."); i >= 0 {
				callee = callee[i+1:]
			}
			if bodyGateCallees[callee] {
				add(callee)
			}
		case "raise_statement":
			text := astutil.NodeText(n, pf.Source)
			for _, name := range bodyGateRaises {
				rest := strings.TrimSpace(strings.TrimPrefix(text, "raise"))
				if rest == name || strings.HasPrefix(rest, name+"(") {
					add(name)
				}
			}
		}
		return true
	})
	return out
}

// assignedGateAttrRe matches a statement that sets an attribute on a tool object after it is built,
// such as `send_email.tool_input_guardrails = [...]` (the OpenAI Agents SDK example form). The
// match is a whole line, so a comment or a string that starts differently is not read.
var assignedGateAttrRe = regexp.MustCompile(`(?m)^[ \t]*([A-Za-z_][A-Za-z0-9_]*)\.([A-Za-z_][A-Za-z0-9_]*)[ \t]*=[^=]`)

// assignedGateAttrs returns "assigned:<attr>" for each attribute set on a variable named name.
func assignedGateAttrs(name string, src []byte) []string {
	var out []string
	for _, m := range assignedGateAttrRe.FindAllSubmatch(src, -1) {
		if string(m[1]) == name {
			out = append(out, "assigned:"+string(m[2]))
		}
	}
	return out
}

// toolHasEffect reports whether the tool carries any of the listed effects.
func toolHasEffect(t models.ToolDef, effects []string) bool {
	for _, e := range t.Effects {
		for _, want := range effects {
			if e == want {
				return true
			}
		}
	}
	return false
}

// toolGated reports whether something on the tool itself is an approval gate: a decorator or
// wrapper kwarg (or the same name set as an attribute on the tool variable after it is built) named in expr.ToolGateKwargs whose value is not False or None, or a body
// approval call named in expr.ToolGateCalls.
func toolGated(t models.ToolDef, expr AgentConsequentialToolUngatedExpr) bool {
	for _, k := range expr.ToolGateKwargs {
		if v, ok := t.Config[k]; ok && v != "False" && v != "None" {
			return true
		}
	}
	for _, g := range t.BodyGates {
		for _, want := range expr.ToolGateCalls {
			if g == want {
				return true
			}
		}
		for _, k := range expr.ToolGateKwargs {
			if g == "assigned:"+k {
				return true
			}
		}
	}
	return false
}

// kwargTreeText joins the source text of every leaf under a kwarg value, so a gate class
// named inside a list or call is found.
func kwargTreeText(k *models.KwargTree) string {
	if k == nil {
		return ""
	}
	var b strings.Builder
	if k.Value != nil {
		b.WriteString(k.Value.Text)
		for _, e := range k.Value.List {
			b.WriteString(" ")
			b.WriteString(e.Text)
		}
	}
	for _, c := range k.Children {
		b.WriteString(" ")
		b.WriteString(kwargTreeText(c))
	}
	return b.String()
}

// agentGated reports whether the agent sets something that stops every tool call for a
// decision: a kwarg named in expr.AgentGateKwargs with a value other than None, or a name
// from expr.AgentGateText in the text of any agent kwarg.
func agentGated(a models.AgentDef, expr AgentConsequentialToolUngatedExpr) bool {
	for _, k := range expr.AgentGateKwargs {
		kw := lookupKwarg(a, k)
		if kw == nil {
			continue
		}
		if kw.Value != nil && kw.Value.Kind == models.ExprLiteralNone {
			continue
		}
		return true
	}
	if len(expr.AgentGateText) > 0 {
		text := kwargTreeText(a.Kwargs)
		for _, g := range expr.AgentGateText {
			if g != "" && strings.Contains(text, g) {
				return true
			}
		}
	}
	return false
}

// PredAgentConsequentialToolUngated implements agent_consequential_tool_ungated: the agent
// holds a tool with a listed effect and neither the tool nor the agent shows a listed
// approval gate. An agent built from unpacked or non-literal arguments (Opaque) is not
// judged, and a tool that discovery did not resolve is not judged.
func PredAgentConsequentialToolUngated(expr AgentConsequentialToolUngatedExpr, a models.AgentDef, inv models.RepoInventory) bool {
	if a.Opaque || len(expr.Effects) == 0 || agentGated(a, expr) {
		return false
	}
	for _, t := range agentToolDefs(a, inv) {
		if toolHasEffect(t, expr.Effects) && !toolGated(t, expr) {
			return true
		}
	}
	return false
}
