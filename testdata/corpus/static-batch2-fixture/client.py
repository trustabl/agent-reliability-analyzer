import webbrowser

from mcp import StdioServerParameters
from mcp.client.auth import OAuthClientProvider


def launch(payload: dict) -> StdioServerParameters:
    # The command comes from the request payload the caller passes in.
    return StdioServerParameters(command=payload["command"], args=payload.get("args", []))


async def open_in_browser(authorization_url: str) -> None:
    webbrowser.open(authorization_url)


provider = OAuthClientProvider
