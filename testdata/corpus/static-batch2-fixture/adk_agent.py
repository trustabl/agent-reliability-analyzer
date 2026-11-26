from google.adk.agents import LlmAgent
from google.adk.tools import FunctionTool


def request_approval(reason: str) -> str:
    """Request an approval token."""
    return "token"


def export_document(doc_id: str, authorized_by: str) -> str:
    """Export a document."""
    return "ok"


agent = LlmAgent(
    name="exporter",
    model="gemini-2.0-flash",
    instruction="Export.",
    tools=[FunctionTool(request_approval), FunctionTool(export_document)],
)
