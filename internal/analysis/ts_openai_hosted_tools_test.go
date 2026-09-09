package analysis_test

import (
	"testing"

	"github.com/trustabl/trustabl/internal/analysis"
	"github.com/trustabl/trustabl/internal/models"
)

func TestIsTSOpenAIHostedToolFactory(t *testing.T) {
	cases := []struct {
		name string
		want bool
	}{
		{"webSearchTool", true},
		{"fileSearchTool", true},
		{"codeInterpreterTool", true},
		{"imageGenerationTool", true},
		{"toolSearchTool", true},
		{"computerTool", true},
		{"shellTool", true},
		{"applyPatchTool", true},
		{"hostedMcpTool", true},
		{"WebSearchTool", false}, // PascalCase = Python; must not match
		{"customTool", false},
		{"tool", false},
		{"", false},
	}
	for _, c := range cases {
		if got := analysis.IsTSOpenAIHostedToolFactory(c.name); got != c.want {
			t.Errorf("IsTSOpenAIHostedToolFactory(%q) = %v, want %v", c.name, got, c.want)
		}
	}
}

func TestTSOpenAIHostedToolFactories_NineFactories(t *testing.T) {
	if got := len(analysis.TSOpenAIHostedToolFactories); got != 9 {
		t.Errorf("expected 9 factories, got %d: %v", got, analysis.TSOpenAIHostedToolFactories)
	}
}

// discoverTSOpenAIAgentHostedRef parses src, discovers the single expected
// agent's tools, resolves edges, and returns its first HostedToolRef —
// exercising the same DiscoverTSOpenAIAgents + ResolveEdges path the scanner
// uses (classifyTSOpenAIHostedFactoryCall is unexported).
func discoverTSOpenAIAgentHostedRef(t *testing.T, src string) models.HostedToolRef {
	t.Helper()
	pf := parseTSForTest(t, "src/a.ts", src)
	inv := models.RepoInventory{Agents: analysis.DiscoverTSOpenAIAgents([]analysis.ParsedFile{pf}, nil)}
	analysis.ResolveEdges(&inv, []analysis.ParsedFile{pf})
	if len(inv.Agents) != 1 {
		t.Fatalf("got %d agents, want 1", len(inv.Agents))
	}
	a := inv.Agents[0]
	if len(a.HostedToolRefs) != 1 {
		t.Fatalf("got %d HostedToolRefs, want 1: %+v", len(a.HostedToolRefs), a.HostedToolRefs)
	}
	return a.HostedToolRefs[0]
}

// kwargChild navigates a dotted path within a KwargTree's Children map,
// mirroring the rule engine's lookupKwargInTree (internal/rules/predicates.go)
// without importing the unexported helper.
func kwargChild(kt *models.KwargTree, path ...string) *models.KwargTree {
	cur := kt
	for _, p := range path {
		if cur == nil || cur.Children == nil {
			return nil
		}
		next, ok := cur.Children[p]
		if !ok {
			return nil
		}
		cur = next
	}
	return cur
}

func TestClassifyTSOpenAIHostedFactoryCall_HostedMcpToolNestedOptions(t *testing.T) {
	src := `
import { Agent } from "@openai/agents";
import { hostedMcpTool } from "@openai/agents-core";
export const researcher = new Agent({
  name: "researcher",
  instructions: "research",
  tools: [
    hostedMcpTool({
      serverLabel: "deepwiki",
      serverUrl: "https://mcp.deepwiki.com/mcp",
      allowedTools: { toolNames: ["ask_question"] },
      requireApproval: { never: { toolNames: ["read_wiki"] } },
    }),
  ],
});
`
	ref := discoverTSOpenAIAgentHostedRef(t, src)
	if ref.Class != "hostedMcpTool" {
		t.Errorf("Class = %q, want hostedMcpTool", ref.Class)
	}
	if ref.Resolved == nil {
		t.Fatalf("Resolved is nil, want a materialized HostedToolDef")
	}
	if ref.Resolved.Kwargs == nil {
		t.Fatalf("Kwargs is nil, want a populated tree")
	}
	if got := kwargChild(ref.Resolved.Kwargs, "allowedTools", "toolNames"); got == nil {
		t.Errorf("allowedTools.toolNames not found in Kwargs: %+v", ref.Resolved.Kwargs)
	}
	if got := kwargChild(ref.Resolved.Kwargs, "requireApproval", "never", "toolNames"); got == nil {
		t.Errorf("requireApproval.never.toolNames not found in Kwargs: %+v", ref.Resolved.Kwargs)
	}
	if got := kwargChild(ref.Resolved.Kwargs, "serverLabel"); got == nil || got.Value == nil ||
		got.Value.Text != `"deepwiki"` {
		t.Errorf("serverLabel = %+v, want literal \"deepwiki\"", got)
	}
}

func TestClassifyTSOpenAIHostedFactoryCall_AllowedToolsFlatArray(t *testing.T) {
	src := `
import { Agent } from "@openai/agents";
import { hostedMcpTool } from "@openai/agents-core";
export const researcher = new Agent({
  name: "researcher",
  instructions: "research",
  tools: [
    hostedMcpTool({
      serverLabel: "deepwiki",
      allowedTools: ["a", "b"],
    }),
  ],
});
`
	ref := discoverTSOpenAIAgentHostedRef(t, src)
	kw := kwargChild(ref.Resolved.Kwargs, "allowedTools")
	if kw == nil || kw.Value == nil {
		t.Fatalf("allowedTools not found or has no Value: %+v", kw)
	}
	if kw.Value.Kind != models.ExprList {
		t.Errorf("allowedTools.Value.Kind = %q, want %q", kw.Value.Kind, models.ExprList)
	}
	if len(kw.Value.List) != 2 {
		t.Errorf("allowedTools list len = %d, want 2", len(kw.Value.List))
	}
}

func TestClassifyTSOpenAIHostedFactoryCall_NoAllowListIsAbsent(t *testing.T) {
	src := `
import { Agent } from "@openai/agents";
import { hostedMcpTool } from "@openai/agents-core";
export const researcher = new Agent({
  name: "researcher",
  instructions: "research",
  tools: [
    hostedMcpTool({ serverLabel: "x", serverUrl: "y" }),
  ],
});
`
	ref := discoverTSOpenAIAgentHostedRef(t, src)
	if ref.Resolved == nil || ref.Resolved.Kwargs == nil {
		t.Fatalf("expected non-nil Kwargs (call has an options object), got %+v", ref.Resolved)
	}
	if got := kwargChild(ref.Resolved.Kwargs, "allowedTools"); got != nil {
		t.Errorf("allowedTools should be absent, got %+v", got)
	}
}

func TestClassifyTSOpenAIHostedFactoryCall_NoArgFactoryHasNilKwargs(t *testing.T) {
	src := `
import { Agent } from "@openai/agents";
import { fileSearchTool } from "@openai/agents-openai";
export const researcher = new Agent({
  name: "researcher",
  instructions: "research",
  tools: [fileSearchTool()],
});
`
	ref := discoverTSOpenAIAgentHostedRef(t, src)
	if ref.Resolved == nil {
		t.Fatalf("Resolved is nil")
	}
	if ref.Resolved.Kwargs != nil {
		t.Errorf("expected nil Kwargs for a no-arg factory call, got %+v", ref.Resolved.Kwargs)
	}
}

func TestClassifyTSOpenAIHostedFactoryCall_LocationIsFactoryCallSite(t *testing.T) {
	src := `import { Agent } from "@openai/agents";
import { hostedMcpTool } from "@openai/agents-core";
export const researcher = new Agent({
  name: "researcher",
  instructions: "research",
  tools: [
    hostedMcpTool({ serverLabel: "x" }),
  ],
});
`
	ref := discoverTSOpenAIAgentHostedRef(t, src)
	if ref.Resolved == nil {
		t.Fatalf("Resolved is nil")
	}
	// The hostedMcpTool(...) call itself is on line 7 (1-indexed) — distinct
	// from the agent's own Line (the `new Agent({` call starts on line 3).
	// Guards against regressing to the agent-line approximation ResolveEdges
	// used before HostedToolRef.Pending existed.
	const wantLine = 7
	if ref.Resolved.Line != wantLine {
		t.Errorf("Line = %d, want %d (the factory call site, not the agent's)", ref.Resolved.Line, wantLine)
	}
}

func TestResolveEdges_TSADKHostedTool_StillUsesAgentLineApproximation(t *testing.T) {
	// Non-regression: TS ADK hosted-tool refs carry no Pending yet, so they
	// must still fall through to the synthesized-def branch (agent-line
	// Location, no kwargs) rather than a nil-pointer dereference on Pending.
	src := `
import { LlmAgent, GoogleSearchTool } from "@google/adk";
export const root = new LlmAgent({
  name: "root",
  tools: [new GoogleSearchTool()],
});
`
	pf := parseTSForTest(t, "src/a.ts", src)
	inv := models.RepoInventory{Agents: analysis.DiscoverTSADKAgents([]analysis.ParsedFile{pf}, nil)}
	analysis.ResolveEdges(&inv, []analysis.ParsedFile{pf})
	if len(inv.Agents) != 1 {
		t.Fatalf("got %d agents, want 1", len(inv.Agents))
	}
	a := inv.Agents[0]
	if len(a.HostedToolRefs) != 1 {
		t.Fatalf("got %d HostedToolRefs, want 1: %+v", len(a.HostedToolRefs), a.HostedToolRefs)
	}
	ref := a.HostedToolRefs[0]
	if ref.Resolved == nil {
		t.Fatalf("Resolved is nil")
	}
	if ref.Resolved.Kwargs != nil {
		t.Errorf("TS ADK hosted tools carry no Pending yet, expected nil Kwargs, got %+v", ref.Resolved.Kwargs)
	}
	if ref.Resolved.Line != a.Line {
		t.Errorf("Line = %d, want agent's Line %d (unchanged approximation)", ref.Resolved.Line, a.Line)
	}
}
