This project sends its traces to LangWatch. Keep its `langwatch.setup()` call,
and do not add another tracer provider or exporter: `setup()` registers
LangWatch's provider globally, or attaches LangWatch's exporter to a provider
the project already registered. If the call is missing, add it once, at
startup, before the agent runs. It reads the key from `LANGWATCH_API_KEY`.

Install: `pip install langwatch`

```python
import langwatch
from opentelemetry import trace

langwatch.setup()
provider = trace.get_tracer_provider()
tracer = provider.get_tracer("agent")
```

LangWatch's spans are OpenTelemetry spans, so the labels in Step 2 attach to
them. When the run is already wrapped in `@langwatch.trace()`, that span is
the root: inside it, use `trace.get_current_span()` as the root span in Step 2
rather than starting another one. A tool decorated with
`@langwatch.span(type="tool")` is the tool span.

If a part of the project uses OpenTelemetry without the LangWatch SDK, keep
that provider and point its exporter at LangWatch over HTTP, with protobuf or
JSON bodies. LangWatch does not accept gRPC. Set
`OTEL_EXPORTER_OTLP_TRACES_ENDPOINT` in the deployment's environment to the
LangWatch trace endpoint: scheme `https`, host `app.langwatch.ai`, path
`/api/otel/v1/traces`. The exporter reads it from there.

```python
import os

from opentelemetry.exporter.otlp.proto.http.trace_exporter import OTLPSpanExporter

exporter = OTLPSpanExporter(
    headers={"Authorization": f"Bearer {os.environ['LANGWATCH_API_KEY']}"},
)
```
