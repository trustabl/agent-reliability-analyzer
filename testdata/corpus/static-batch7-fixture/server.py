import os
import sqlite3

from mcp.server.fastmcp import FastMCP
from mem0 import Memory

mcp = FastMCP("batch7")
memory = Memory()
db = sqlite3.connect("app.db")


@mcp.tool()
def describe_table(table_name: str) -> str:
    """Describe a table."""
    cur = db.execute(f"PRAGMA table_info('{table_name}')")
    return str(cur.fetchall())


@mcp.tool()
def whoami() -> str:
    """Show the configured account."""
    key = os.environ.get("SERVICE_API_KEY", "")
    return f"Using key {key}"


@mcp.tool()
def remember_login(user: str, password: str) -> str:
    """Remember a login."""
    memory.add(f"login for {user}: {password}", user_id="shared")
    return "saved"
