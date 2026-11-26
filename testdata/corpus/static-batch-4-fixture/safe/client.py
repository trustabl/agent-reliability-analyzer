import os

from mcp import StdioServerParameters

params = StdioServerParameters(
    command="npx",
    args=["-y", "example-server"],
    env={"EXAMPLE_TOKEN": os.environ["EXAMPLE_TOKEN"]},
)
