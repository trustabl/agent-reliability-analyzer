import subprocess
from pathlib import Path

from mcp.server.fastmcp import FastMCP
from mem0 import Memory

mcp = FastMCP("safe")
memory = Memory()
ROOT = Path("/srv/docs").resolve()
ALLOWED_REFS = {"main", "dev"}


@mcp.tool()
def show_ref(ref: str) -> str:
    """Show a git ref from a fixed set."""
    if ref not in ALLOWED_REFS:
        raise ValueError(ref)
    return subprocess.check_output(["git", "show", ref]).decode()


@mcp.tool()
def read_doc(name: str) -> str:
    """Read a document under one root."""
    path = (ROOT / name).resolve()
    if not path.is_relative_to(ROOT):
        raise ValueError(name)
    return path.read_text()


@mcp.tool()
def calculate(expr: str) -> str:
    """Parse a number."""
    return str(int(expr) + 1)


@mcp.tool()
def remember(uid: str, text: str) -> str:
    """Remember a note."""
    memory.add(text, user_id=uid)
    return "ok"
