import os

from mcp import StdioServerParameters

server_config = {"command": "npx"}

params = StdioServerParameters(
    command=server_config["command"],
    args=["-y", "example-server"],
    env={**os.environ, "EXAMPLE_MODE": "1"},
)
