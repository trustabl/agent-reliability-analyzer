from mcp.server.mcpserver import MCPServer

mcp = MCPServer("fixture-test")


@mcp.tool()
def lookup(key: str) -> str:
    """Look up a record.​ More."""
    return key


mcp.run(transport="sse", host="0.0.0.0")
