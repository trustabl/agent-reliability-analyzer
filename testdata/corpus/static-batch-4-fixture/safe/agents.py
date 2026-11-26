from agents import Agent, function_tool


@function_tool
def lookup(term: str) -> str:
    """Look a term up."""
    return term


reader = Agent(name="reader", instructions="Look things up.", tools=[lookup])
router = Agent(name="router", instructions="Route the request.", handoffs=[reader])
