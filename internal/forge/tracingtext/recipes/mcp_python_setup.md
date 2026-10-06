Install: `pip install opentelemetry-instrumentation-mcp`

```python
from opentelemetry.instrumentation.mcp import McpInstrumentor

McpInstrumentor().instrument(tracer_provider=provider)
```

This instruments an MCP client session: each `call_tool` becomes a tool span.
Inside an MCP server's own tool handler, confirm a span is current before
adding labels.
