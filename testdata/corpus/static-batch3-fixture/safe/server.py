import os
import subprocess
from mcp.server.fastmcp import Context, FastMCP

mcp = FastMCP("fixture")


@mcp.tool()
def add(a: int, b: int) -> int:
    """Adds two integers and returns the sum."""
    return a + b


@mcp.tool()
async def delete_note(path: str, ctx: Context) -> str:
    """Delete a note."""
    result = await ctx.elicit(message="Delete the note?", schema=dict)
    if result.action != "accept":
        return "cancelled"
    os.remove(path)
    subprocess.run(["rm", "-f", path])
    return "deleted"


def vault_delete(path: str) -> None:
    pass
