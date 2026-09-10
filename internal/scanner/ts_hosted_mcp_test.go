package scanner_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/trustabl/trustabl/internal/scanner"
)

// TestScanRun_OAI116_TSHostedMcpAllowedTools is the end-to-end contract for
// OAI-116: a TypeScript OpenAI Agents SDK agent that wires hostedMcpTool()
// with no allowedTools must fire, and the same agent with allowedTools set
// must go silent — proving discovery -> ResolveEdges -> the evaluator all the
// way through, not just the hand-built AgentDef the per-rule table in
// internal/rules/policies_test.go exercises.
func TestScanRun_OAI116_TSHostedMcpAllowedTools(t *testing.T) {
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
	mustWrite("src/open.ts", `
import { Agent, hostedMcpTool } from "@openai/agents";

export const open = new Agent({
  name: "open",
  instructions: "Look things up",
  tools: [hostedMcpTool({ serverLabel: "deepwiki", serverUrl: "https://mcp.deepwiki.com/mcp" })],
});
`)
	mustWrite("src/closed.ts", `
import { Agent, hostedMcpTool } from "@openai/agents";

export const closed = new Agent({
  name: "closed",
  instructions: "Look things up, narrowly",
  tools: [hostedMcpTool({ serverLabel: "deepwiki", serverUrl: "https://mcp.deepwiki.com/mcp", allowedTools: ["ask_question"] })],
});
`)

	res, err := scanner.Run(scanner.Config{Target: dir, RulesFS: rulesFixture(t)})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	var sawOpenFinding, sawClosedFinding bool
	for _, f := range res.Findings {
		if f.RuleID != "OAI-116" {
			continue
		}
		switch f.FilePath {
		case "src/open.ts":
			sawOpenFinding = true
		case "src/closed.ts":
			sawClosedFinding = true
		}
	}
	if !sawOpenFinding {
		t.Errorf("expected OAI-116 to fire on src/open.ts (hostedMcpTool with no allowedTools); Findings=%+v", res.Findings)
	}
	if sawClosedFinding {
		t.Errorf("expected OAI-116 to stay silent on src/closed.ts (hostedMcpTool with allowedTools set); Findings=%+v", res.Findings)
	}
}
