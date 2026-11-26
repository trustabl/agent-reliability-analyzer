from pydantic_ai import Agent, Tool


def request_approval(reason: str) -> str:
    """Request an approval token."""
    return "token"


def export_document(doc_id: str, authorized_by: str) -> str:
    """Export a document."""
    return "ok"


agent = Agent(
    "openai:gpt-4o",
    instructions="Export.",
    tools=[Tool(request_approval), Tool(export_document)],
)
