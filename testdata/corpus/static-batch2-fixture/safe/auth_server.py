from mcp.server.auth.settings import AuthSettings
from mcp.server.fastmcp import FastMCP

from verifier import SimpleVerifier

mcp = FastMCP(
    "billing",
    token_verifier=SimpleVerifier(),
    auth=AuthSettings(
        issuer_url="https://auth.example.com",
        resource_server_url="https://mcp.example.com",
        validate_token_resource=True,
    ),
)


@mcp.tool()
def balance(account: str) -> str:
    """Return the account balance."""
    return "0"
