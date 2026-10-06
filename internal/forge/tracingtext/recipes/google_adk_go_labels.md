ADK passes each tool a context that carries its `execute_tool` span, so
`trace.SpanFromContext(toolContext)` reaches it. The run labels go on ADK's
own `invoke_agent` span through agent callbacks; no application root span is
needed.

```go
BeforeAgentCallbacks: []agent.BeforeAgentCallback{func(cc agent.CallbackContext) (*genai.Content, error) {
	run.MarkStart(trace.SpanFromContext(cc))
	return nil, nil
}},
AfterAgentCallbacks: []agent.AfterAgentCallback{func(cc agent.CallbackContext) (*genai.Content, error) {
	if mErr := otellabels.MarkRunEnd(trace.SpanFromContext(cc), otellabels.FinalAnswer); mErr != nil {
		fmt.Fprintln(os.Stderr, "trustabl: could not mark the run's end:", mErr)
	}
	return nil, nil
}},
```

- ADK runs a turn's tool calls in parallel goroutines.
- With several calls in a turn, ADK adds an `execute_tool (merged)` span
  around them. It carries no labels; skip it by span name.
- ADK skips the after-agent callback when the caller stops iterating the
  run's events, so a run that stops on its first failure carries no
  `trustabl.exit_reason`.
