import langwatch
from langchain.agents import create_agent
from langchain_core.runnables import RunnableConfig
from langchain_core.tools import tool

langwatch.setup()  # reads LANGWATCH_API_KEY from the environment


@tool
def lookup_order(order_id: str) -> str:
    """Look up an order by id."""
    return f"order {order_id}: shipped"


agent = create_agent("openai:gpt-4o-mini", tools=[lookup_order])


@langwatch.trace()
def answer(question: str) -> str:
    # LangWatch's documented LangChain wiring: the trace's callback handler is
    # passed in the run call's config, not in the constructor.
    result = agent.invoke(
        {"messages": [{"role": "user", "content": question}]},
        config=RunnableConfig(callbacks=[langwatch.get_current_trace().get_langchain_callback()]),
    )
    return result["messages"][-1].content
