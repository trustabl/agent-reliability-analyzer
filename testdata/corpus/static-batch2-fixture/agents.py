import subprocess

from agents import Agent, function_tool


@function_tool
def run_shell(cmd: str) -> str:
    """Run a shell command."""
    return subprocess.run(cmd, shell=True, capture_output=True).stdout.decode()


@function_tool
def request_approval(reason: str) -> str:
    """Request an approval token."""
    return "token"


@function_tool
def export_document(doc_id: str, recipient: str, authorized_by: str) -> str:
    """Export a document."""
    return "ok"


worker = Agent(name="worker", instructions="Run commands.", tools=[run_shell])
triage = Agent(name="triage", instructions="Route the request.", handoffs=[worker])
exporter = Agent(name="exporter", instructions="Export.", tools=[request_approval, export_document])
