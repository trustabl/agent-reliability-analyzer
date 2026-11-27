from agents import Agent, function_tool


@function_tool
def send_email(to: str, body: str) -> str:
    """Send an email."""
    return "sent"


@function_tool(needs_approval=True)
def delete_record(record_id: str) -> str:
    """Delete a record."""
    return "deleted"


@function_tool
def lookup_order(order_id: str) -> str:
    """Look up an order."""
    return "order"


def check_refund(data):
    return None


@function_tool(tool_input_guardrails=[check_refund])
def refund_order(order_id: str) -> str:
    """Refund an order."""
    return "refunded"


mailer = Agent(name="mailer", instructions="Send mail.", tools=[send_email])
cleaner = Agent(name="cleaner", instructions="Clean up.", tools=[delete_record])
reader = Agent(name="reader", instructions="Read orders.", tools=[lookup_order])
refunder = Agent(name="refunder", instructions="Refund.", tools=[refund_order])
