"""Synthetic Deep Agents fixture for LC-118. Not real code; paths are placeholders."""
from deepagents import create_deep_agent
from deepagents.backends import LocalShellBackend, StateBackend

MODEL = "anthropic:claude-sonnet-4-5"

# Fires LC-118: a host shell backend and no interrupt_on.
ungated = create_deep_agent(
    model=MODEL,
    system_prompt="You run commands in the workspace.",
    backend=LocalShellBackend(root_dir="/srv/workspace", inherit_env=True),
)

# Silent: the same backend with approval on the execute tool.
gated = create_deep_agent(
    model=MODEL,
    system_prompt="You run commands in the workspace.",
    backend=LocalShellBackend(root_dir="/srv/workspace"),
    interrupt_on={"execute": True},
)

# Silent: a backend that cannot run commands.
files_only = create_deep_agent(
    model=MODEL,
    system_prompt="You edit notes.",
    backend=StateBackend(),
)
