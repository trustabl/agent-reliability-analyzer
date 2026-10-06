Inside a tool's `execute`, the current span is the SDK's `execute_tool` span.
The SDK's own `invoke_agent` span cannot be reached from application code, so
the run labels go on a root span the application creates around
`generateText`, as in the TypeScript block above. Mark `max_steps` when the
step cap stopped a run whose last finish reason was still `tool-calls`. A
step's tool calls may run concurrently, so `trustabl.step_index` follows the
order the calls reach the tool.
