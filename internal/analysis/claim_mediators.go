package analysis

import (
	"strings"

	sitter "github.com/smacker/go-tree-sitter"

	"github.com/trustabl/trustabl/internal/analysis/astutil"
	"github.com/trustabl/trustabl/internal/models"
)

// ApplyClaimMediators marks a tool's claim as wrapped when its SDK has a
// pre-tool hook somewhere in the repo that receives the call's arguments and
// can block the call, so the claim may be checked there rather than in the
// tool. It only upgrades unused and passthrough; checked stays checked.
//
// The credit is repo-wide per SDK, not per tool: these hooks are registered
// globally, per crew or per agent, and may filter by tool name, and linking a
// tool to the exact hook that covers it needs edges discovery does not build.
// Over-crediting leaves a rule silent, which the claim rules already prefer to
// a false finding.
//
// Hook shapes, each from the SDK's own docs:
//   - CrewAI: @before_tool_call, register_before_tool_call_hook(...), and
//     @on(InterceptionPoint.PRE_TOOL_CALL).
//   - Pydantic AI: @<hooks>.on.before_tool_execute / .on.tool_execute,
//     Hooks(before_tool_execute=...) / Hooks(tool_execute=...), and a tool's
//     own args_validator=.
//   - Claude Agent SDK: a "PreToolUse" key in a hooks dict, or
//     ClaudeAgentOptions(can_use_tool=...) when the same call sets neither
//     allowed_tools= nor permission_mode="bypassPermissions" (both approve
//     calls without running the callback).
func ApplyClaimMediators(tools []models.ToolDef, files []ParsedFile) {
	crewHook, pydHook, claudeHook := false, false, false
	for _, pf := range files {
		if !claudeHook && fileImportsClaudeSDK(pf) && fileHasClaudeToolHook(pf) {
			claudeHook = true
		}
		if !crewHook && fileImportsCrewAI(pf) && fileHasCrewAIToolHook(pf) {
			crewHook = true
		}
		if !pydHook && fileImportsPydanticAI(pf) && fileHasPydanticAIToolHook(pf) {
			pydHook = true
		}
	}
	for i := range tools {
		t := &tools[i]
		use := t.Facts[models.FactClaimUse]
		if use != models.ClaimUseUnused && use != models.ClaimUsePassthrough {
			continue
		}
		mediated := false
		switch t.Kind {
		case models.KindCrewAITool:
			mediated = crewHook
		case models.KindPydanticAITool:
			_, validator := t.Config["args_validator"]
			mediated = pydHook || validator
		case models.KindClaudeSDKTool:
			mediated = claudeHook
		}
		if mediated {
			t.Facts[models.FactClaimUse] = models.ClaimUseWrapped
		}
	}
}

// fileHasCrewAIToolHook reports whether the file registers a CrewAI hook that
// runs before every tool call.
func fileHasCrewAIToolHook(pf ParsedFile) bool {
	found := false
	astutil.Walk(pf.Tree.RootNode(), func(n *sitter.Node) bool {
		if found {
			return false
		}
		switch n.Type() {
		case "decorator":
			callee := decoratorCallee(n, pf.Source)
			if lastSegment(callee) == "before_tool_call" {
				found = true
			} else if lastSegment(callee) == "on" &&
				strings.Contains(astutil.NodeText(n, pf.Source), "PRE_TOOL_CALL") {
				found = true
			}
		case "call":
			if lastSegment(astutil.NodeText(n.ChildByFieldName("function"), pf.Source)) == "register_before_tool_call_hook" {
				found = true
			}
		}
		return !found
	})
	return found
}

// fileHasPydanticAIToolHook reports whether the file registers a Pydantic AI
// hook that runs around tool execution with the validated arguments.
func fileHasPydanticAIToolHook(pf ParsedFile) bool {
	found := false
	astutil.Walk(pf.Tree.RootNode(), func(n *sitter.Node) bool {
		if found {
			return false
		}
		switch n.Type() {
		case "decorator":
			callee := decoratorCallee(n, pf.Source)
			if strings.HasSuffix(callee, ".on.before_tool_execute") || strings.HasSuffix(callee, ".on.tool_execute") {
				found = true
			}
		case "call":
			if lastSegment(astutil.NodeText(n.ChildByFieldName("function"), pf.Source)) != "Hooks" {
				return true
			}
			if kwargs, _ := extractCallKwargs(n, pf.Source); kwargs != nil {
				if kwargs.Children["before_tool_execute"] != nil || kwargs.Children["tool_execute"] != nil {
					found = true
				}
			}
		}
		return !found
	})
	return found
}

// fileHasClaudeToolHook reports whether the file registers a Claude Agent SDK
// hook or permission callback that sees a tool call's input before it runs.
func fileHasClaudeToolHook(pf ParsedFile) bool {
	found := false
	astutil.Walk(pf.Tree.RootNode(), func(n *sitter.Node) bool {
		if found {
			return false
		}
		switch n.Type() {
		case "pair":
			if k, ok := pyStringLiteral(astutil.NodeText(n.ChildByFieldName("key"), pf.Source)); ok && k == "PreToolUse" {
				found = true
			}
		case "call":
			if lastSegment(astutil.NodeText(n.ChildByFieldName("function"), pf.Source)) != "ClaudeAgentOptions" {
				return true
			}
			if _, ok := astutil.KwargValue(n, pf.Source, "can_use_tool"); !ok {
				return true
			}
			if _, ok := astutil.KwargValue(n, pf.Source, "allowed_tools"); ok {
				return true
			}
			if mode, _ := astutil.KwargValue(n, pf.Source, "permission_mode"); mode == `"bypassPermissions"` || mode == `'bypassPermissions'` {
				return true
			}
			found = true
		}
		return !found
	})
	return found
}

func lastSegment(dotted string) string {
	if i := strings.LastIndex(dotted, "."); i >= 0 {
		return dotted[i+1:]
	}
	return dotted
}
