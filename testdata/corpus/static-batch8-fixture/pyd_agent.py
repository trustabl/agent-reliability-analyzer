from pydantic_ai import Agent, ApprovalRequired, Tool


def send_sms(number: str, text: str) -> str:
    """Send an SMS."""
    return "sent"


def delete_file(path: str) -> str:
    """Delete a file."""
    return "deleted"


def revoke_token(token_id: str) -> str:
    """Revoke a token."""
    raise ApprovalRequired


def get_weather(city: str) -> str:
    """Get the weather."""
    return "sunny"


texter = Agent("openai:gpt-4o", name="texter", tools=[Tool(send_sms)])
remover = Agent("openai:gpt-4o", name="remover", tools=[Tool(delete_file, requires_approval=True)])
revoker = Agent("openai:gpt-4o", name="revoker", tools=[Tool(revoke_token)])
weather = Agent("openai:gpt-4o", name="weather", tools=[Tool(get_weather)])
