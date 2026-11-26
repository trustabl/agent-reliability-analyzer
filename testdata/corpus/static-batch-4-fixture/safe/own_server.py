import os

from mcp import StdioServerParameters

# The project starts its own server by a literal command: not judged.
params = StdioServerParameters(command="python", args=["server.py"], env={**os.environ})
