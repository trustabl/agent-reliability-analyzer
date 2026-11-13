import langwatch
from agents import Agent, Runner

langwatch.setup()  # reads LANGWATCH_API_KEY from the environment

agent = Agent(name="support", instructions="Answer support questions.")


@langwatch.trace()
async def answer(question: str) -> str:
    result = await Runner.run(agent, question)
    return result.final_output
