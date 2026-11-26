from mcp import StdioServerParameters


def test_launch(config):
    return StdioServerParameters(command=config["command"])
