from agents import Agent, function_tool


@function_tool
def send_email(to: str, body: str) -> str:
    """Send an email."""
    return "sent"


test_mailer = Agent(name="test_mailer", instructions="Send mail.", tools=[send_email])
