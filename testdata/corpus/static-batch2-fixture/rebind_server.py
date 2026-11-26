from mcp.server.fastmcp import FastMCP
from mcp.server.transport_security import TransportSecuritySettings

security = TransportSecuritySettings(enable_dns_rebinding_protection=False)
mcp = FastMCP("notes", transport_security=security)


@mcp.tool()
def list_notes() -> str:
    """List notes."""
    return "[]"
