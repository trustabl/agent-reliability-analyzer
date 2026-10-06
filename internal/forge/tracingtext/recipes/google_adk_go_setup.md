ADK for Go emits `invoke_agent` and `execute_tool` spans itself, on the global
tracer provider. `otel.SetTracerProvider(provider)` must run before the agent
does. No extra package.
