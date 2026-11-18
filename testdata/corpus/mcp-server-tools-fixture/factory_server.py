from mcp.server.mcpserver import MCPServer


def create_server():
    mcp = MCPServer("fixture-factory")

    @mcp.tool()
    def echo(text: str) -> str:
        """Return the text unchanged."""
        return text

    return mcp


if __name__ == "__main__":
    create_server().run(transport="sse", host="0.0.0.0", port=8001, path="/sse")
