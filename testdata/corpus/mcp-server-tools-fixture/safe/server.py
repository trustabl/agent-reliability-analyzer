from mcp.server.mcpserver import MCPServer

mcp = MCPServer("fixture-safe", auth=AUTH_SETTINGS, token_verifier=VERIFIER)


@mcp.tool()
def lookup(key: str) -> str:
    """Look up a record by key."""
    return key


if __name__ == "__main__":
    mcp.run(transport="sse", host="0.0.0.0", port=8001)
