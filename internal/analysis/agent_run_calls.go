package analysis

import (
	"strings"

	sitter "github.com/smacker/go-tree-sitter"

	"github.com/trustabl/trustabl/internal/analysis/astutil"
	"github.com/trustabl/trustabl/internal/models"
)

// Agent execution-call discovery: OpenAI Agents SDK, Pydantic AI, Google ADK
// and AutoGen / AG2.
//
// Execution limits (max_turns for OpenAI Agents SDK, usage_limits for
// Pydantic AI) are set on the call that RUNS an agent, not on the agent's
// constructor — Runner.run(agent, ..., max_turns=N) / agent.run(...,
// usage_limits=UsageLimits(...)). Neither is visible to DiscoverAgents /
// DiscoverPydanticAIAgents, which only capture the constructor call. This
// file adds a second, independent discovery pass over the same call-node
// shape so agent-scope rules can correlate a specific AgentDef to the call
// that executes it (by same-file VarName), mirroring how
// DiscoverClaudeAgentOptions captures ClaudeAgentOptions(...) constructions
// separately from agent discovery.
//
// The two SDKs need different discriminators:
//
//   - OpenAI Agents SDK: Runner is a fixed class (imported from `agents`),
//     so the callee is matched by requiring the attribute's object be
//     exactly "Runner" or end in ".Runner" (e.g. `agents.Runner.run(...)`).
//     Matching on a suffix of the whole callee text (e.g. "Runner.run")
//     would false-positive on any unrelated class merely named *Runner
//     (TaskRunner.run(), TestRunner.run(), ...) — the object segment must
//     match exactly, not just the tail of the dotted string. The agent
//     being run is the call's first positional argument (mirrors
//     Runner.run(starting_agent, input, ...)).
//
//   - Pydantic AI: `<agent>.run(...)` is a bare method call on an
//     arbitrary local variable — there is no fixed class name to match, so
//     the agent being run is the method's RECEIVER, not a positional
//     argument (unlike OpenAI). Over-capturing here is safe: a call whose
//     receiver name doesn't correspond to any discovered PydanticAgent
//     VarName in the same file simply never correlates in a downstream
//     predicate (same same-file-VarName-only limitation ClaudeAgentOptions
//     and other call-capture discoverers already accept). The file-level
//     `fileImportsPydanticAI` gate keeps this from firing in files that
//     never touch Pydantic AI at all.
//
//   - Google ADK: agents are run through a Runner, not by the agent itself.
//     `runner = Runner(agent=a, ...)` / `InMemoryRunner(a)` binds a runner
//     variable to an agent identifier (same-file), and `runner.run(...)` /
//     `runner.run_async(...)` executes it — so the agent is two hops from the
//     call. A same-file pre-pass resolves runner variable -> agent identifiers
//     (and the inline `Runner(agent=a).run_async(...)` receiver). Runners built
//     from `app=App(...)` stay unresolved and never correlate. `run_live` is a
//     long-lived bidirectional session by design and is not a run call here.
//
//   - AutoGen / AG2: `<agent>.initiate_chat(recipient, ...)` /
//     `a_initiate_chat` (AG2) and `<agent>.run(...)` / `a_run(...)` /
//     `run_stream(...)` (AG2 and the v0.4 line). The receiver takes part in the
//     chat, and so does the recipient of initiate_chat (first positional
//     identifier or `recipient=`), so one call yields a record for each. Teams
//     (RoundRobinGroupChat, ...) are not discovered as agents, so a team.run()
//     record never correlates.
//
// All four discoverers require the agent reference (first positional arg for
// OpenAI, receiver for Pydantic AI / AutoGen, Runner(agent=) identifier for ADK)
// to be a plain identifier, silently skipping anything else (a call expression,
// an attribute access), mirroring adk_agents.go's DiscoverADKTools.

// openAIRunnerMethods is the closed set of Runner class methods that execute
// an agent and accept max_turns.
var openAIRunnerMethods = map[string]bool{
	"run": true, "run_sync": true, "run_streamed": true,
}

// pydanticAIRunMethods is the closed set of Agent instance methods that
// execute an agent and accept usage_limits.
var pydanticAIRunMethods = map[string]bool{
	"run": true, "run_sync": true, "run_stream": true,
}

// adkRunnerMethods are the Runner methods that execute an agent to completion.
// run_live is excluded: a live session is open-ended by design.
var adkRunnerMethods = map[string]bool{"run": true, "run_async": true}

// autoGenRunMethods are the agent methods that execute a chat or a task.
// initiate_chat-family methods also name a recipient agent.
var autoGenRunMethods = map[string]bool{
	"run": true, "a_run": true, "run_stream": true,
	"initiate_chat": true, "a_initiate_chat": true,
}

// DiscoverAgentRunCalls walks each ParsedFile and emits one AgentRunCallDef
// per recognized Runner.run-family call (OpenAI Agents SDK),
// <agent>.run-family call (Pydantic AI), Runner.run/run_async call (Google ADK)
// or initiate_chat/run-family call (AutoGen / AG2). Purely additive: it does not modify
// or depend on the AgentDef list produced by DiscoverAgents /
// DiscoverPydanticAIAgents, only on the same-file VarName convention they
// already populate.
func DiscoverAgentRunCalls(files []ParsedFile) []models.AgentRunCallDef {
	var out []models.AgentRunCallDef
	for _, pf := range files {
		imp := runCallImports{
			openAI:   fileImportsOpenAIAgentsSDK(pf),
			pydantic: fileImportsPydanticAI(pf),
			adk:      fileImportsGoogleADK(pf),
			autoGen:  fileImportsAutoGen(pf),
		}
		if !imp.openAI && !imp.pydantic && !imp.adk && !imp.autoGen {
			continue
		}
		out = append(out, discoverAgentRunCallsInFile(pf, imp)...)
	}
	return out
}

// isOpenAIAgentsModule reports whether a dotted module path belongs to the
// OpenAI Agents SDK: `agents`, `agents.*`. Mirrors isPydanticAIModule.
func isOpenAIAgentsModule(mod string) bool {
	return mod == "agents" || strings.HasPrefix(mod, "agents.")
}

// fileImportsOpenAIAgentsSDK reports whether pf imports the OpenAI Agents SDK
// via a real import statement (AST-based, not a source substring).
func fileImportsOpenAIAgentsSDK(pf ParsedFile) bool {
	return fileImportsModule(pf, isOpenAIAgentsModule)
}

// runCallImports records which run-call SDKs a file imports.
type runCallImports struct{ openAI, pydantic, adk, autoGen bool }

func discoverAgentRunCallsInFile(pf ParsedFile, imp runCallImports) []models.AgentRunCallDef {
	var out []models.AgentRunCallDef
	var runners map[string][]string
	if imp.adk {
		runners = adkRunnerBindings(pf)
	}
	astutil.Walk(pf.Tree.RootNode(), func(n *sitter.Node) bool {
		if n.Type() != "call" {
			return true
		}
		fn := n.ChildByFieldName("function")
		if fn == nil || fn.Type() != "attribute" {
			return true
		}
		attr := fn.ChildByFieldName("attribute")
		obj := fn.ChildByFieldName("object")
		if attr == nil || obj == nil {
			return true
		}
		method := astutil.NodeText(attr, pf.Source)

		if imp.openAI && openAIRunnerMethods[method] && isRunnerObject(obj, pf) {
			if rc, ok := buildOpenAIRunCall(n, fn, pf); ok {
				out = append(out, rc)
			}
			return true
		}
		if imp.pydantic && pydanticAIRunMethods[method] && obj.Type() == "identifier" {
			out = append(out, buildPydanticRunCall(n, obj, method, pf))
		}
		if imp.adk && adkRunnerMethods[method] {
			for _, agent := range adkRunCallAgents(obj, runners, pf) {
				out = append(out, buildSimpleRunCall(models.SDKGoogleADK, n, fn, agent, pf))
			}
		}
		if imp.autoGen && autoGenRunMethods[method] && obj.Type() == "identifier" {
			for _, agent := range autoGenRunCallAgents(n, obj, method, pf) {
				out = append(out, buildSimpleRunCall(models.SDKAutoGen, n, fn, agent, pf))
			}
		}
		return true
	})
	return out
}

// isRunnerObject reports whether the attribute's object segment is exactly
// "Runner" or a dotted-qualified access ending in ".Runner" (e.g.
// `agents.Runner`). Deliberately NOT a suffix check on the whole callee text
// — that would also match an unrelated TaskRunner.run() / TestRunner.run().
func isRunnerObject(obj *sitter.Node, pf ParsedFile) bool {
	text := astutil.NodeText(obj, pf.Source)
	return text == "Runner" || strings.HasSuffix(text, ".Runner")
}

func buildOpenAIRunCall(n, fn *sitter.Node, pf ParsedFile) (models.AgentRunCallDef, bool) {
	args := n.ChildByFieldName("arguments")
	if args == nil || args.NamedChildCount() == 0 {
		return models.AgentRunCallDef{}, false
	}
	first := args.NamedChild(0)
	// Only a positional identifier is resolvable (e.g. Runner.run(my_agent, ...)).
	// Anything else (Runner.run(get_agent()), Runner.run(agent=my_agent)) is
	// silently skipped — those cannot be correlated to an AgentDef by name.
	if first.Type() != "identifier" {
		return models.AgentRunCallDef{}, false
	}
	kwargs, opaque := extractCallKwargs(n, pf.Source)
	return models.AgentRunCallDef{
		SDK:          models.SDKOpenAIAgents,
		Callee:       astutil.NodeText(fn, pf.Source),
		AgentVarName: astutil.NodeText(first, pf.Source),
		Location: models.Location{
			FilePath: pf.RelPath,
			Line:     int(n.StartPoint().Row) + 1,
			EndLine:  int(n.EndPoint().Row) + 1,
		},
		Kwargs: kwargs,
		Opaque: opaque,

		WallClockTimeoutWrapped: nodeHasWallClockTimeoutAncestor(n, pf.Source),
	}, true
}

func buildPydanticRunCall(n, obj *sitter.Node, method string, pf ParsedFile) models.AgentRunCallDef {
	kwargs, opaque := extractCallKwargs(n, pf.Source)
	return models.AgentRunCallDef{
		SDK:          models.SDKPydanticAI,
		Callee:       astutil.NodeText(obj, pf.Source) + "." + method,
		AgentVarName: astutil.NodeText(obj, pf.Source),
		Location: models.Location{
			FilePath: pf.RelPath,
			Line:     int(n.StartPoint().Row) + 1,
			EndLine:  int(n.EndPoint().Row) + 1,
		},
		Kwargs: kwargs,
		Opaque: opaque,

		WallClockTimeoutWrapped: nodeHasWallClockTimeoutAncestor(n, pf.Source),
	}
}

// isADKRunnerCtor reports whether callee text names an ADK runner constructor.
func isADKRunnerCtor(callee string) bool {
	for _, c := range []string{"Runner", "InMemoryRunner"} {
		if callee == c || strings.HasSuffix(callee, "."+c) {
			return true
		}
	}
	return false
}

// adkRunnerAgents returns the agent identifiers a Runner / InMemoryRunner
// constructor call binds: the `agent=` identifier kwarg, or (InMemoryRunner)
// the first positional identifier. Anything non-identifier (a call, an
// `app=App(...)`) yields nothing, so that runner stays unresolved.
func adkRunnerAgents(call *sitter.Node, pf ParsedFile) []string {
	args := call.ChildByFieldName("arguments")
	if args == nil {
		return nil
	}
	var out []string
	for i := 0; i < int(args.NamedChildCount()); i++ {
		c := args.NamedChild(i)
		switch c.Type() {
		case "identifier":
			if i == 0 && strings.HasSuffix(astutil.NodeText(call.ChildByFieldName("function"), pf.Source), "InMemoryRunner") {
				out = append(out, astutil.NodeText(c, pf.Source))
			}
		case "keyword_argument":
			v := c.ChildByFieldName("value")
			if astutil.NodeText(c.ChildByFieldName("name"), pf.Source) == "agent" && v != nil && v.Type() == "identifier" {
				out = append(out, astutil.NodeText(v, pf.Source))
			}
		}
	}
	return out
}

// adkRunnerBindings maps each same-file `<var> = Runner(...)` /
// `InMemoryRunner(...)` assignment target to the agent identifiers it binds.
func adkRunnerBindings(pf ParsedFile) map[string][]string {
	out := map[string][]string{}
	astutil.Walk(pf.Tree.RootNode(), func(n *sitter.Node) bool {
		if n.Type() != "assignment" {
			return true
		}
		l, r := n.ChildByFieldName("left"), n.ChildByFieldName("right")
		if l == nil || r == nil || l.Type() != "identifier" || r.Type() != "call" {
			return true
		}
		if !isADKRunnerCtor(astutil.NodeText(r.ChildByFieldName("function"), pf.Source)) {
			return true
		}
		if agents := adkRunnerAgents(r, pf); len(agents) > 0 {
			name := astutil.NodeText(l, pf.Source)
			out[name] = append(out[name], agents...)
		}
		return true
	})
	return out
}

// adkRunCallAgents resolves the receiver of a runner.run/run_async call to the
// agent identifiers it executes: a bound runner variable, or an inline
// Runner(...) constructor call.
func adkRunCallAgents(obj *sitter.Node, runners map[string][]string, pf ParsedFile) []string {
	switch obj.Type() {
	case "identifier":
		return runners[astutil.NodeText(obj, pf.Source)]
	case "call":
		if isADKRunnerCtor(astutil.NodeText(obj.ChildByFieldName("function"), pf.Source)) {
			return adkRunnerAgents(obj, pf)
		}
	}
	return nil
}

// autoGenRunCallAgents returns the agent identifiers that take part in an
// AutoGen run/chat call: the receiver, plus the recipient of initiate_chat /
// a_initiate_chat (first positional identifier or `recipient=`).
func autoGenRunCallAgents(call, obj *sitter.Node, method string, pf ParsedFile) []string {
	recv := astutil.NodeText(obj, pf.Source)
	out := []string{recv}
	if method != "initiate_chat" && method != "a_initiate_chat" {
		return out
	}
	args := call.ChildByFieldName("arguments")
	if args == nil {
		return out
	}
	recipient := ""
	for i := 0; i < int(args.NamedChildCount()); i++ {
		c := args.NamedChild(i)
		switch {
		case c.Type() == "identifier" && i == 0:
			recipient = astutil.NodeText(c, pf.Source)
		case c.Type() == "keyword_argument" && astutil.NodeText(c.ChildByFieldName("name"), pf.Source) == "recipient":
			if v := c.ChildByFieldName("value"); v != nil && v.Type() == "identifier" {
				recipient = astutil.NodeText(v, pf.Source)
			}
		}
	}
	if recipient != "" && recipient != recv {
		out = append(out, recipient)
	}
	return out
}

// buildSimpleRunCall builds the record for the ADK / AutoGen run calls, where
// the agent identifier is resolved by the caller.
func buildSimpleRunCall(sdk models.SDK, n, fn *sitter.Node, agentVar string, pf ParsedFile) models.AgentRunCallDef {
	kwargs, opaque := extractCallKwargs(n, pf.Source)
	return models.AgentRunCallDef{
		SDK:          sdk,
		Callee:       astutil.NodeText(fn, pf.Source),
		AgentVarName: agentVar,
		Location: models.Location{
			FilePath: pf.RelPath,
			Line:     int(n.StartPoint().Row) + 1,
			EndLine:  int(n.EndPoint().Row) + 1,
		},
		Kwargs: kwargs,
		Opaque: opaque,

		WallClockTimeoutWrapped: nodeHasWallClockTimeoutAncestor(n, pf.Source),
	}
}
