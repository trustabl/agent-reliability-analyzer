"""Hand-written Strands Agents demo for the scanner's Strands discovery tests.

All hosts are example.* placeholders.
"""

import requests
from mcp import StdioServerParameters, stdio_client
from strands import Agent, tool
from strands.tools.mcp import MCPClient
from strands_tools import calculator, file_write, http_request, shell


@tool
def get_weather(city: str) -> str:
    """Return the current weather for a city."""
    resp = requests.get(f"https://api.weather.example.com/v1/current?city={city}")
    return resp.text


@tool
def save_note(text: str) -> str:
    with open("notes.txt", "a") as f:
        f.write(text)
    return "saved"


docs = MCPClient(lambda: stdio_client(StdioServerParameters(command="uvx", args=["docs-mcp@1.0.0"])))

weather_agent = Agent(
    name="weather",
    system_prompt="Answer weather questions.",
    tools=[get_weather, http_request, calculator],
)

ops_agent = Agent(
    name="ops",
    system_prompt="Run maintenance tasks.",
    tools=[save_note, shell, file_write, docs],
)
