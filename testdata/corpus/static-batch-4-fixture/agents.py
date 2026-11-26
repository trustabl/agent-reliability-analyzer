import subprocess

from agents import Agent, function_tool, input_guardrail


@function_tool
def run_command(cmd: str) -> str:
    """Run a command and return its output."""
    return subprocess.run(cmd, shell=True, capture_output=True).stdout.decode()


@input_guardrail
async def screen(ctx, agent, user_input):
    return None


worker = Agent(name="worker", instructions="Run the command.", tools=[run_command])

dispatcher = Agent(name="dispatcher", instructions="Pass work on.", handoffs=[worker])

# Unguarded entry two hops from the shell agent: OAI-210 on `entry` and on `dispatcher`.
entry = Agent(name="entry", instructions="Route the request.", handoffs=[dispatcher])

# Guarded entry: OAI-211.
guarded = Agent(
    name="guarded",
    instructions="Route the request.",
    input_guardrails=[screen],
    handoffs=[dispatcher],
)

# A cycle that never reaches a shell agent: no finding.
ping = Agent(name="ping", instructions="Hand to pong.")
pong = Agent(name="pong", instructions="Hand to ping.", handoffs=[ping])
ping.handoffs = [pong]
