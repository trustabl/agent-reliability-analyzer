- One `setupObservability` call per process, and no second tracer provider or
  exporter for LangWatch. The key comes from `LANGWATCH_API_KEY`, never from
  the code.
- `setupObservability` registers the context manager the trap above asks
  for. Do not register a second one.
- In a process that calls `setupObservability`, do not also point an
  `OTEL_EXPORTER_OTLP_*` endpoint at LangWatch; it already exports there.
