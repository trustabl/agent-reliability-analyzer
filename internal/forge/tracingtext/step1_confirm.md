Before Step 2, run the agent once and check that it emits one tool span per
tool call, usually under an agent span. How to recognise them depends on the
instrumentation:

- `gen_ai` conventions (Google ADK, Pydantic AI, Vercel AI SDK, OpenLLMetry
  for LangChain): spans named `invoke_agent` and `execute_tool`, with
  `gen_ai.operation.name` set to match.
- OpenInference instrumentors (OpenAI Agents SDK, LangChain, CrewAI, AutoGen):
  `openinference.span.kind` is `AGENT` or `TOOL`, and the span is named after
  the agent or the tool.
- OpenLLMetry's MCP instrumentor: `traceloop.span.kind` is `tool`, with no
  agent span.

If no tool spans appear, fix that first; the labels in Step 2 have nothing to
attach to. To send spans to a collector, set `OTEL_EXPORTER_OTLP_ENDPOINT`;
the exporter reads it from the environment.
