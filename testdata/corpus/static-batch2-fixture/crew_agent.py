from crewai import Agent
from crewai.tools import tool


@tool("request_approval")
def request_approval(reason: str) -> str:
    """Request an approval token."""
    return "token"


@tool("export_document")
def export_document(doc_id: str, authorized_by: str) -> str:
    """Export a document."""
    return "ok"


agent = Agent(role="exporter", goal="Export documents", backstory="Exports.", tools=[request_approval, export_document])
