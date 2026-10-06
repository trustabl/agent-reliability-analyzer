Install: `pip install opentelemetry-sdk opentelemetry-exporter-otlp-proto-http`

```python
from opentelemetry import trace
from opentelemetry.sdk.trace import TracerProvider
from opentelemetry.sdk.trace.export import SimpleSpanProcessor
from opentelemetry.exporter.otlp.proto.http.trace_exporter import OTLPSpanExporter

provider = TracerProvider()
provider.add_span_processor(SimpleSpanProcessor(OTLPSpanExporter()))
trace.set_tracer_provider(provider)
tracer = provider.get_tracer("agent")
```

Create the provider once, at startup. For production traffic, use the SDK's
batching span processor in place of the simple one.
