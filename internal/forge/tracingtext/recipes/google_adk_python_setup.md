ADK emits `invoke_agent` and `execute_tool` spans itself, on the global tracer
provider. `trace.set_tracer_provider(provider)` must run before the agent
does. No extra package.
