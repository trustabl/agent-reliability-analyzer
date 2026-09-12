package scanner_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/trustabl/trustabl/internal/scanner"
)

// TestScanRun_OAI118_PythonMCPServerToolFilter is the end-to-end contract for
// OAI-118: an OpenAI Agents SDK Python agent that wires an MCP server
// (MCPServerStdio) with no tool_filter must fire, and the same shape with
// tool_filter set must go silent — proving discovery -> ResolveEdges -> the
// evaluator all the way through, not just the hand-built AgentDef the
// per-rule table in internal/rules/policies_test.go exercises. Also covers
// the harder `async with MCPServerStdio(...) as fs:` alias path, which is
// the dominant real-world shape (see testdata/corpus/openai-mcp-filesystem).
func TestScanRun_OAI118_PythonMCPServerToolFilter(t *testing.T) {
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
	mustWrite("pyproject.toml", "[project]\nname = \"f\"\ndependencies = [\"openai-agents\"]\n")
	mustWrite("open.py", `
from agents import Agent
from agents.mcp import MCPServerStdio

open_agent = Agent(
    name="open",
    mcp_servers=[MCPServerStdio(params={"command": "npx"})],
)
`)
	mustWrite("closed.py", `
from agents import Agent
from agents.mcp import MCPServerStdio, create_static_tool_filter

closed_agent = Agent(
    name="closed",
    mcp_servers=[MCPServerStdio(params={"command": "npx"}, tool_filter=create_static_tool_filter(allowed_tool_names=["read_file"]))],
)
`)
	mustWrite("aliased.py", `
from agents import Agent
from agents.mcp import MCPServerStdio

async def main():
    async with MCPServerStdio(params={"command": "npx"}) as fs:
        aliased_agent = Agent(name="aliased", mcp_servers=[fs])
`)

	res, err := scanner.Run(scanner.Config{Target: dir, RulesFS: rulesFixture(t)})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	var sawOpenFinding, sawClosedFinding, sawAliasedFinding bool
	for _, f := range res.Findings {
		if f.RuleID != "OAI-118" {
			continue
		}
		switch f.FilePath {
		case "open.py":
			sawOpenFinding = true
		case "closed.py":
			sawClosedFinding = true
		case "aliased.py":
			sawAliasedFinding = true
		}
	}
	if !sawOpenFinding {
		t.Errorf("expected OAI-118 to fire on open.py (MCPServerStdio with no tool_filter); Findings=%+v", res.Findings)
	}
	if sawClosedFinding {
		t.Errorf("expected OAI-118 to stay silent on closed.py (MCPServerStdio with tool_filter set); Findings=%+v", res.Findings)
	}
	if !sawAliasedFinding {
		t.Errorf("expected OAI-118 to fire on aliased.py (async-with MCPServerStdio alias with no tool_filter); Findings=%+v", res.Findings)
	}
}

// TestScanRun_OAI119_TSMCPServerToolFilter is the end-to-end contract for
// OAI-119, the TypeScript sibling of OAI-118: an OpenAI Agents SDK TS agent
// that wires an MCP server (MCPServerStdio) with no toolFilter must fire, and
// the same shape with toolFilter set must go silent.
func TestScanRun_OAI119_TSMCPServerToolFilter(t *testing.T) {
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
import { Agent, MCPServerStdio } from "@openai/agents";

const fs = new MCPServerStdio({ command: "npx" });

export const open = new Agent({
  name: "open",
  instructions: "Look things up",
  mcpServers: [fs],
});
`)
	mustWrite("src/closed.ts", `
import { Agent, MCPServerStdio, createMCPToolStaticFilter } from "@openai/agents";

const fs = new MCPServerStdio({ command: "npx", toolFilter: createMCPToolStaticFilter({ allowed: ["read_file"] }) });

export const closed = new Agent({
  name: "closed",
  instructions: "Look things up, narrowly",
  mcpServers: [fs],
});
`)

	res, err := scanner.Run(scanner.Config{Target: dir, RulesFS: rulesFixture(t)})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	var sawOpenFinding, sawClosedFinding bool
	for _, f := range res.Findings {
		if f.RuleID != "OAI-119" {
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
		t.Errorf("expected OAI-119 to fire on src/open.ts (MCPServerStdio with no toolFilter); Findings=%+v", res.Findings)
	}
	if sawClosedFinding {
		t.Errorf("expected OAI-119 to stay silent on src/closed.ts (MCPServerStdio with toolFilter set); Findings=%+v", res.Findings)
	}
}
