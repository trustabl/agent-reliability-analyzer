import subprocess

from agents import Agent, function_tool, handoff
from agents.extensions import handoff_filters


@function_tool
def run_shell(cmd: str) -> str:
    """Run a shell command."""
    return subprocess.run(cmd, shell=True, capture_output=True).stdout.decode()


@function_tool
def export_document(doc_id: str, recipient: str) -> str:
    """Export a document using the session's approval."""
    return "ok"


worker = Agent(name="worker", instructions="Run commands.", tools=[run_shell])
triage = Agent(
    name="triage",
    instructions="Route the request.",
    handoffs=[handoff(worker, input_filter=handoff_filters.remove_all_tools)],
)
exporter = Agent(name="exporter", instructions="Export.", tools=[export_document])
