package scanner_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/trustabl/trustabl/internal/scanner"
)

// TestScanRun_OAI117_TSHostedMcpRequireApproval is the end-to-end contract for
// OAI-117: a TypeScript OpenAI Agents SDK agent that wires hostedMcpTool()
// with no requireApproval must fire, and the same agent with requireApproval
// set explicitly must go silent — proving discovery -> ResolveEdges -> the
// evaluator all the way through, not just the hand-built AgentDef the
// per-rule table in internal/rules/policies_test.go exercises.
func TestScanRun_OAI117_TSHostedMcpRequireApproval(t *testing.T) {
	dir := t.TempDir()
	mustWrite := func(rel, content string) {
		t.Helper()
		full := filepath.Join(dir, rel)
		if err := os.MkdirAll(filepath.Dir(full), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(content), 0644); err != nil {
			t.Fatal(err)
		}
	}
	mustWrite("package.json", `{"dependencies": {"@openai/agents": "^0.1.0"}}`)
	mustWrite("src/ungated.ts", `
import { Agent, hostedMcpTool } from "@openai/agents";

export const ungated = new Agent({
  name: "ungated",
  instructions: "Look things up",
  tools: [hostedMcpTool({ serverLabel: "deepwiki", serverUrl: "https://mcp.deepwiki.com/mcp" })],
});
`)
	// gated.ts also sets allowedTools so this file stays a clean single-rule
	// assertion — it is silent for OAI-116 too, not just OAI-117.
	mustWrite("src/gated.ts", `
import { Agent, hostedMcpTool } from "@openai/agents";

export const gated = new Agent({
  name: "gated",
  instructions: "Look things up, narrowly, with confirmation",
  tools: [hostedMcpTool({ serverLabel: "deepwiki", serverUrl: "https://mcp.deepwiki.com/mcp", allowedTools: ["ask_question"], requireApproval: "always" })],
});
`)

	res, err := scanner.Run(scanner.Config{Target: dir, RulesFS: rulesFixture(t)})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	var sawUngatedFinding, sawGatedFinding bool
	for _, f := range res.Findings {
		if f.RuleID != "OAI-117" {
			continue
		}
		switch f.FilePath {
		case "src/ungated.ts":
			sawUngatedFinding = true
		case "src/gated.ts":
			sawGatedFinding = true
		}
	}
	if !sawUngatedFinding {
		t.Errorf("expected OAI-117 to fire on src/ungated.ts (hostedMcpTool with no requireApproval); Findings=%+v", res.Findings)
	}
	if sawGatedFinding {
		t.Errorf("expected OAI-117 to stay silent on src/gated.ts (hostedMcpTool with requireApproval set); Findings=%+v", res.Findings)
	}
}
