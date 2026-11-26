import webbrowser
from urllib.parse import urlparse

from mcp import StdioServerParameters
from mcp.client.auth import OAuthClientProvider


def launch() -> StdioServerParameters:
    return StdioServerParameters(command="uvx", args=["mcp-server-time==0.6.2"])


async def open_in_browser(authorization_url: str) -> None:
    if urlparse(authorization_url).scheme != "https":
        raise ValueError("refusing to open")
    webbrowser.open(authorization_url)


provider = OAuthClientProvider
