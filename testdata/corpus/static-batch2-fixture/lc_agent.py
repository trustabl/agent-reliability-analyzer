from langchain.agents import create_agent
from langchain_core.tools import tool


@tool
def request_approval(reason: str) -> str:
    """Request an approval token."""
    return "token"


@tool
def export_document(doc_id: str, authorized_by: str) -> str:
    """Export a document."""
    return "ok"


agent = create_agent("openai:gpt-4o", tools=[request_approval, export_document])
