import os

from opentelemetry import trace
from opentelemetry.exporter.otlp.proto.http.trace_exporter import OTLPSpanExporter
from opentelemetry.sdk.trace import TracerProvider
from opentelemetry.sdk.trace.export import BatchSpanProcessor

provider = TracerProvider()
provider.add_span_processor(BatchSpanProcessor(OTLPSpanExporter(
    endpoint="https://app.langwatch.ai/api/otel/v1/traces",
    headers={"Authorization": f"Bearer {os.environ['LANGWATCH_API_KEY']}"},
)))
trace.set_tracer_provider(provider)
