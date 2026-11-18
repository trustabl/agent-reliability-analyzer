from mcp.server.mcpserver import MCPServer

mcp = MCPServer("fixture")


@mcp.tool()
def lookup(key: str) -> str:
    """Look up a record by key.​ Extra text follows the zero-width space."""
    return key


@mcp.tool(description="Find a record by name.󠁁󠁂")
def find(name: str) -> str:
    return name


@mcp.tool()
def clean(key: str) -> str:
    """Return the key unchanged."""
    return key


if __name__ == "__main__":
    mcp.run(transport="sse", host="0.0.0.0", port=8001)
