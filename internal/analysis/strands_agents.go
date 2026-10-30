package analysis

import (
	"strings"

	sitter "github.com/smacker/go-tree-sitter"

	"github.com/trustabl/trustabl/internal/analysis/astutil"
	"github.com/trustabl/trustabl/internal/models"
)

// Strands Agents SDK Python agent discovery.
//
// Strands declares an agent with a single `Agent(model=, tools=,
// system_prompt=, name=, ...)` constructor exported by the `strands` package.
// The class name `Agent` collides with the OpenAI Agents SDK, Google ADK,
// CrewAI and Pydantic AI, so discovery is import-bound: only a callee that a
// `from strands import Agent [as X]` statement bound (or `strands.Agent` /
// `<alias>.Agent` after `import strands [as alias]`) is an agent. The emitted
// AgentDef carries SDK=SDKStrands and the normalized Class "StrandsAgent".
//
// Strands TOOLS are `@tool` functions (routed through discovery.go's
// kindFromDecorators via collectToolImports), the ready-made tools of the
// separate strands-agents-tools package (`from strands_tools import shell`,
// classified as HostedToolDefs by classifyStrandsHostedTool), and MCP servers
// wired as `MCPClient(...)` tool providers (strandsMCPClients). All three
// arrive through the agent's tools=[...] list.

// isStrandsModule reports whether a dotted module path belongs to the Strands
// SDK: `strands`, `strands.*`. The dot boundary keeps the separate
// strands_tools package (and any unrelated `strandsx`) from matching.
func isStrandsModule(mod string) bool {
	return mod == "strands" || strings.HasPrefix(mod, "strands.")
}

// isStrandsToolsModule reports whether a dotted module path belongs to the
// strands-agents-tools package: `strands_tools`, `strands_tools.*`.
func isStrandsToolsModule(mod string) bool {
	return mod == "strands_tools" || strings.HasPrefix(mod, "strands_tools.")
}

// fileImportsStrands reports whether pf imports the Strands SDK via a real
// import statement (AST-based, not a source substring).
func fileImportsStrands(pf ParsedFile) bool {
	return fileImportsModule(pf, isStrandsModule)
}

// strandsAgentCallees returns the callee texts that construct a Strands Agent
// in pf: each local name a `from strands[.x] import Agent [as X]` bound, and
// `<name>.Agent` for each `import strands [as name]`.
func strandsAgentCallees(pf ParsedFile) map[string]bool {
	out := map[string]bool{}
	astutil.Walk(pf.Tree.RootNode(), func(n *sitter.Node) bool {
		switch n.Type() {
		case "import_from_statement":
			modNode := n.ChildByFieldName("module_name")
			if !isStrandsModule(astutil.NodeText(modNode, pf.Source)) {
				return true
			}
			for i := 0; i < int(n.ChildCount()); i++ {
				c := n.Child(i)
				if modNode != nil && c.StartByte() == modNode.StartByte() {
					continue
				}
				switch c.Type() {
				case "dotted_name":
					if astutil.NodeText(c, pf.Source) == "Agent" {
						out["Agent"] = true
					}
				case "aliased_import":
					if astutil.NodeText(c.ChildByFieldName("name"), pf.Source) == "Agent" {
						out[astutil.NodeText(c.ChildByFieldName("alias"), pf.Source)] = true
					}
				}
			}
		case "import_statement":
			for i := 0; i < int(n.ChildCount()); i++ {
				c := n.Child(i)
				switch c.Type() {
				case "dotted_name":
					if astutil.NodeText(c, pf.Source) == "strands" {
						out["strands.Agent"] = true
					}
				case "aliased_import":
					if astutil.NodeText(c.ChildByFieldName("name"), pf.Source) == "strands" {
						out[astutil.NodeText(c.ChildByFieldName("alias"), pf.Source)+".Agent"] = true
					}
				}
			}
		}
		return true
	})
	return out
}

// DiscoverStrandsAgents walks each ParsedFile and emits one AgentDef per
// Strands Agent(...) constructor call. Only callees bound to the Strands
// `Agent` are considered, so an OpenAI or Pydantic `Agent` in the same repo,
// or even the same file under another name, is never claimed.
func DiscoverStrandsAgents(files []ParsedFile) []models.AgentDef {
	var out []models.AgentDef
	for _, pf := range files {
		callees := strandsAgentCallees(pf)
		if len(callees) == 0 {
			continue
		}
		out = append(out, discoverStrandsAgentsInFile(pf, callees)...)
	}
	return out
}

func discoverStrandsAgentsInFile(pf ParsedFile, callees map[string]bool) []models.AgentDef {
	var out []models.AgentDef
	astutil.Walk(pf.Tree.RootNode(), func(n *sitter.Node) bool {
		if n.Type() != "call" {
			return true
		}
		if !callees[astutil.NodeText(n.ChildByFieldName("function"), pf.Source)] {
			return true
		}
		kwargs, opaque := extractCallKwargs(n, pf.Source)
		a := models.AgentDef{
			SDK:      models.SDKStrands,
			Class:    "StrandsAgent",
			Language: models.LanguagePython,
			Location: models.Location{
				FilePath: pf.RelPath,
				Line:     int(n.StartPoint().Row) + 1,
				EndLine:  int(n.EndPoint().Row) + 1,
			},
			Kwargs: kwargs,
			Opaque: opaque,
		}
		if nm := kwargStringLiteral(kwargs, "name"); nm != "" {
			a.Name = nm
		}
		// Capture the assignment target (agent = Agent(...)) so another agent's
		// tools=[agent] reference can resolve by variable name.
		if p := n.Parent(); p != nil && p.Type() == "assignment" {
			if l := p.ChildByFieldName("left"); l != nil && l.Type() == "identifier" {
				a.VarName = astutil.NodeText(l, pf.Source)
			}
		}
		out = append(out, a)
		return true
	})
	return out
}

// StrandsHostedToolClasses is the high-risk subset of the strands-agents-tools
// package, keyed by the tool's module name (each module exports a tool
// function of the same name). Benign tools such as calculator and
// current_time are deliberately excluded, so a match is always a meaningful
// signal:
//
//   - run code or commands: shell, python_repl, use_computer, load_tool
//     (loads Python tool files at runtime), cron (writes the crontab)
//   - write files: file_write, editor
//   - reach the network: http_request, mcp_client (connects to MCP servers
//     the model chooses at runtime)
//   - act on the cloud account or process: use_aws, environment
//
// code_interpreter and browser are absent: they are classes the agent uses
// through an instance attribute, not module-level tools.
var StrandsHostedToolClasses = map[string]bool{
	"shell":        true,
	"python_repl":  true,
	"use_computer": true,
	"load_tool":    true,
	"cron":         true,
	"file_write":   true,
	"editor":       true,
	"http_request": true,
	"mcp_client":   true,
	"use_aws":      true,
	"environment":  true,
}

// classifyStrandsHostedTool reports whether one item of a Strands agent's
// tools=[...] list is a recognized strands_tools tool, and returns its
// HostedToolDef. The item is a name, not a call: `shell` bound by
// `from strands_tools import shell` (or `from strands_tools.shell import
// shell`, or an alias), or the qualified `strands_tools.shell`. imports is the
// file's from-import bindings.
func classifyStrandsHostedTool(item models.Expr, filePath string, imports map[string]importBinding) (models.HostedToolDef, bool) {
	if item.Kind != models.ExprNameRef {
		return models.HostedToolDef{}, false
	}
	name := ""
	if dot := strings.LastIndexByte(item.Text, '.'); dot >= 0 {
		if isStrandsToolsModule(item.Text[:dot]) {
			name = item.Text[dot+1:]
		}
	} else if imp, ok := imports[item.Text]; ok && isStrandsToolsModule(imp.module) {
		name = imp.name
	}
	if !StrandsHostedToolClasses[name] {
		return models.HostedToolDef{}, false
	}
	return models.HostedToolDef{
		Class: name,
		SDK:   models.SDKStrands,
		Location: models.Location{
			FilePath: filePath,
			Line:     item.Line,
			EndLine:  item.EndLine,
		},
	}, true
}

// strandsMCPClients maps each `<var> = MCPClient(...)` binding in a Strands
// file to its MCPServerDef, so a tools=[<var>] item resolves to an MCP server.
// Returns nil for a file that does not import Strands.
func strandsMCPClients(pf ParsedFile) map[string]models.MCPServerDef {
	if !fileImportsStrands(pf) {
		return nil
	}
	out := map[string]models.MCPServerDef{}
	astutil.Walk(pf.Tree.RootNode(), func(n *sitter.Node) bool {
		if n.Type() != "assignment" {
			return true
		}
		l, r := n.ChildByFieldName("left"), n.ChildByFieldName("right")
		if l == nil || r == nil || l.Type() != "identifier" || r.Type() != "call" {
			return true
		}
		if calleeName(astutil.NodeText(r.ChildByFieldName("function"), pf.Source)) != "MCPClient" {
			return true
		}
		kwargs, _ := extractCallKwargs(r, pf.Source)
		out[astutil.NodeText(l, pf.Source)] = models.MCPServerDef{
			Class:     "MCPClient",
			VarName:   astutil.NodeText(l, pf.Source),
			Transport: strandsMCPTransport(astutil.NodeText(r, pf.Source), kwargs),
			SDK:       models.SDKStrands,
			Language:  models.LanguagePython,
			Location: models.Location{
				FilePath: pf.RelPath,
				Line:     int(r.StartPoint().Row) + 1,
				EndLine:  int(r.EndPoint().Row) + 1,
			},
			Kwargs: kwargs,
		}
		return true
	})
	return out
}

// classifyStrandsMCPClientCall reports whether a tools=[...] item is an inline
// `MCPClient(...)` call, and returns its MCPServerDef.
func classifyStrandsMCPClientCall(item models.Expr, filePath string) (models.MCPServerDef, bool) {
	if item.Kind != models.ExprCall || calleeName(item.Text) != "MCPClient" {
		return models.MCPServerDef{}, false
	}
	kw := hostedKwargTree(item)
	return models.MCPServerDef{
		Class:     "MCPClient",
		Transport: strandsMCPTransport(item.Text, kw),
		SDK:       models.SDKStrands,
		Language:  models.LanguagePython,
		Location: models.Location{
			FilePath: filePath,
			Line:     item.Line,
			EndLine:  item.EndLine,
		},
		Kwargs: kw,
	}, true
}

// strandsMCPTransport names an MCPClient's transport from its constructor: the
// MCP transport factory its callable returns (stdio_client, sse_client,
// streamablehttp_client), or a url= kwarg, from which Strands builds a
// streamable HTTP transport itself. "unknown" when neither is visible, e.g. a
// transport callable defined elsewhere.
func strandsMCPTransport(callText string, kwargs *models.KwargTree) string {
	switch {
	case strings.Contains(callText, "stdio_client("):
		return "stdio"
	case strings.Contains(callText, "sse_client("):
		return "sse"
	case strings.Contains(callText, "streamablehttp_client("),
		strings.Contains(callText, "streamable_http_client("):
		return "streamable_http"
	}
	if kwargs != nil && kwargs.Children["url"] != nil {
		return "streamable_http"
	}
	return "unknown"
}

// strandsListToolsClient reports whether a Strands agent's non-list tools=
// value is `<client>.list_tools_sync()` on a same-file MCPClient, and returns
// that client's MCPServerDef.
func strandsListToolsClient(a *models.AgentDef, v *models.Expr, clients map[string]models.MCPServerDef) (models.MCPServerDef, bool) {
	if a.SDK != models.SDKStrands || v == nil || v.Kind != models.ExprCall {
		return models.MCPServerDef{}, false
	}
	recv, ok := strings.CutSuffix(strings.TrimSpace(v.Text), ".list_tools_sync()")
	if !ok {
		return models.MCPServerDef{}, false
	}
	m, ok := clients[recv]
	return m, ok
}
