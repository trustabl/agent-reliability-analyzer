from mcp.server.mcpserver import MCPServer


def create_server():
    return MCPServer("fixture-factory-safe", auth=AUTH_SETTINGS, token_verifier=VERIFIER)


if __name__ == "__main__":
    create_server().run(transport="sse", host="0.0.0.0", port=8001)
