import os
import sqlite3

from mcp.server.fastmcp import FastMCP
from mem0 import Memory

mcp = FastMCP("batch7-safe")
memory = Memory()
db = sqlite3.connect("app.db")
TABLES = {"users", "orders"}


@mcp.tool()
def describe_table(table_name: str) -> str:
    """Describe a table."""
    if table_name not in TABLES:
        raise ValueError("unknown table")
    cur = db.execute(f"PRAGMA table_info('{table_name}')")
    return str(cur.fetchall())


@mcp.tool()
def lookup(user_id: str) -> str:
    """Look a user up with a bound value."""
    cur = db.execute("SELECT name FROM users WHERE id = ?", (user_id,))
    return str(cur.fetchall())


@mcp.tool()
def whoami() -> str:
    """Show whether the account is configured."""
    key = os.environ.get("SERVICE_API_KEY", "")
    return "configured" if key else "missing"


@mcp.tool()
def whoami_masked() -> str:
    """Show the last four characters of the key."""
    key = os.environ["SERVICE_API_KEY"]
    return "key ends " + key[-4:]


@mcp.tool()
def key_path() -> str:
    """Show where the key file lives."""
    return os.environ.get("SERVICE_API_KEY_FILE", "/etc/key")


@mcp.tool()
def remember_note(user: str, note: str) -> str:
    """Remember a note."""
    memory.add(f"note from {user}: {note}", user_id=user)
    return "saved"
