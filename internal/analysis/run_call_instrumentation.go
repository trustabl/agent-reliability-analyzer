package analysis

import "github.com/trustabl/trustabl/internal/models"

// ApplyRunCallInstrumentation credits a LangChain agent whose run call passes
// callbacks at run time — agent.invoke(..., config={"callbacks": [...]}) or
// config=RunnableConfig(callbacks=[...]) — the way LangWatch, Langfuse and
// LangSmith document wiring their LangChain handler. The credit stands in for
// the constructor's `callbacks` kwarg, so agent_kwarg_missing (LC-112) stops
// firing. An agent already credited by a process-wide call keeps that credit.
//
// The run call is matched to its agent the way agent_server_reachable does:
// same SDK, same variable name, and the agent's defining file. A config passed
// as a variable, an empty list and None are not evidence. Run calls arrive in
// discovery order (file, then source order), so the first match is
// deterministic. Mutates agents in place.
func ApplyRunCallInstrumentation(agents []models.AgentDef, runCalls []models.AgentRunCallDef) {
	for i := range agents {
		a := &agents[i]
		if a.SDK != models.SDKLangChain || a.InstrumentedBy != nil || a.VarName == "" {
			continue
		}
		for _, rc := range runCalls {
			if rc.SDK != a.SDK || rc.AgentVarName != a.VarName {
				continue
			}
			def := rc.AgentFilePath
			if def == "" {
				def = rc.FilePath
			}
			if def != a.FilePath || !runConfigHasCallbacks(rc.Kwargs) {
				continue
			}
			a.InstrumentedBy = &models.GlobalInstrumentation{
				Kwarg: nativeInstrumentKwargs[a.SDK],
				Call:  rc.Callee + " config callbacks",
				File:  rc.FilePath,
				Line:  rc.Line,
			}
			break
		}
	}
}

// runConfigHasCallbacks reports whether a run call's config kwarg carries a
// callbacks value: a dict literal key or a RunnableConfig(...) kwarg, both of
// which exprFromNode captures as Children.
func runConfigHasCallbacks(kwargs *models.KwargTree) bool {
	if kwargs == nil {
		return false
	}
	cfg := kwargs.Children["config"]
	if cfg == nil {
		return false
	}
	cb := cfg.Children["callbacks"]
	if cb == nil || cb.Value == nil {
		return false
	}
	switch cb.Value.Kind {
	case models.ExprLiteralNone:
		return false
	case models.ExprList:
		return len(cb.Value.List) > 0
	}
	return true
}
