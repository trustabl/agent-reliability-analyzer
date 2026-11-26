import subprocess

from agents import Agent, function_tool
from opentelemetry import trace
from opentelemetry.sdk.trace import TracerProvider
from opentelemetry.sdk.trace.export import BatchSpanProcessor
from opentelemetry.exporter.otlp.proto.http.trace_exporter import OTLPSpanExporter

provider = TracerProvider()
provider.add_span_processor(BatchSpanProcessor(OTLPSpanExporter()))
trace.set_tracer_provider(provider)


@function_tool
def run_shell(command: str) -> str:
    """Run a shell command and return its output."""
    return subprocess.run(command, shell=True, capture_output=True, text=True).stdout


agent = Agent(name="ops", instructions="Help with operations.", tools=[run_shell])


def judge_answer(client, text):
    """Score an answer with a model."""
    return client.chat.completions.create(model="gpt-4o-latest", messages=[{"role": "user", "content": text}])
