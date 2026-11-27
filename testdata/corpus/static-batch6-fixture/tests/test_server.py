from mcp.server.fastmcp import FastMCP

mcp = FastMCP("test")


@mcp.tool()
def calculate(expr: str) -> str:
    """Calculate in a test."""
    return str(eval(expr))
