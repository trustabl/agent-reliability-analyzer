from langchain.agents import create_agent
from langchain.agents.middleware import HumanInTheLoopMiddleware
from langchain_core.tools import tool
from langgraph.types import interrupt


@tool
def send_message(channel: str, text: str) -> str:
    """Send a message."""
    return "sent"


@tool
def drop_table(name: str) -> str:
    """Drop a table."""
    return "dropped"


@tool
def transfer_funds(account: str, amount: int) -> str:
    """Transfer funds."""
    if interrupt({"account": account, "amount": amount}) != "yes":
        return "declined"
    return "moved"


@tool
def search_docs(query: str) -> str:
    """Search the docs."""
    return "docs"


notifier = create_agent("openai:gpt-4o", name="notifier", tools=[send_message])
dba = create_agent(
    "openai:gpt-4o",
    name="dba",
    tools=[drop_table],
    middleware=[HumanInTheLoopMiddleware(interrupt_on={"drop_table": True})],
)
banker = create_agent("openai:gpt-4o", name="banker", tools=[transfer_funds])
searcher = create_agent("openai:gpt-4o", name="searcher", tools=[search_docs])
