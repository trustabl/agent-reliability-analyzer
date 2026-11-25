- One `langwatch.setup()` per process, and no second tracer provider or
  exporter for LangWatch. The key comes from `LANGWATCH_API_KEY`, never from
  the code.
- In a process that calls `langwatch.setup()`, do not also point an
  `OTEL_EXPORTER_OTLP_*` endpoint at LangWatch; `setup()` already exports
  there.
