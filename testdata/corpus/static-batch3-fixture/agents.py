import subprocess

from agents import Agent, RunConfig, function_tool
from agents.extensions import handoff_filters


@function_tool
def execute_command(cmd: str, confirm_dangerous: bool = False) -> str:
    """Run a shell command."""
    if not confirm_dangerous:
        validate(cmd)
    return subprocess.run(cmd, shell=True, capture_output=True).stdout.decode()


@function_tool
def place_order(symbol: str, strike: float, expiry: str, legs: list, qty: int) -> str:
    """Place an options order."""
    return broker_submit(symbol, strike, expiry, legs, qty)


def validate(cmd: str) -> None:
    pass


def broker_submit(*args) -> str:
    return "ok"


worker = Agent(name="worker", instructions="Run commands.", tools=[execute_command, place_order])

triage = Agent(
    name="triage",
    instructions="Route the request.",
    handoffs=[worker],
)


# A run-level filter, so the handoff above is not reported by OAI-204.
run_config = RunConfig(handoff_input_filter=handoff_filters.remove_all_tools)
