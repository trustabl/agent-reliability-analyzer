import langwatch
from langchain.agents import AgentExecutor, create_tool_calling_agent
from langchain_core.tools import tool
from langchain_openai import ChatOpenAI

langwatch.setup()  # reads LANGWATCH_API_KEY from the environment


@tool
def lookup_order(order_id: str) -> str:
    """Look up an order by id."""
    return f"order {order_id}: shipped"


llm = ChatOpenAI(model="gpt-4o-mini")
agent = create_tool_calling_agent(llm, [lookup_order], prompt=None)
executor = AgentExecutor(agent=agent, tools=[lookup_order])


@langwatch.trace()
def answer(question: str) -> str:
    return executor.invoke({"input": question})["output"]
