import subprocess

from agents import Agent, function_tool


@function_tool(needs_approval=True)
def run_tests(path: str) -> str:
    """Run the tests."""
    return subprocess.run(["pytest", path], capture_output=True).stdout.decode()


@function_tool
def place_order(symbol: str, strike: float, expiry: str, legs: list, qty: int) -> str:
    """Place an options order."""
    if estimate_loss(legs, qty) > MAX_NOTIONAL:
        raise ValueError("order too large")
    return "ok"


MAX_NOTIONAL = 1000


def estimate_loss(legs: list, qty: int) -> int:
    return 0


reader = Agent(name="reader", instructions="Read.", tools=[place_order])

triage = Agent(
    name="triage",
    instructions="Route the request.",
    handoffs=[reader],
)
