import subprocess

from mcp.server.fastmcp import FastMCP
from mem0 import Memory

mcp = FastMCP("batch6")
memory = Memory()


@mcp.tool()
def show_ref(ref: str) -> str:
    """Show a git ref."""
    out = subprocess.check_output(["git", "show", ref])
    return out.decode()


@mcp.tool()
def read_doc(name: str) -> str:
    """Read a document."""
    path = "/srv/docs/" + name
    with open(path) as f:
        return f.read()


@mcp.tool()
def calculate(expr: str) -> str:
    """Calculate an expression."""
    return str(eval(expr))


@mcp.tool()
def remember(org: str, uid: str, text: str) -> str:
    """Remember a note."""
    memory.add(text, user_id=f"{org}:{uid}")
    return "ok"
