import json
import subprocess
import uuid

from claude_agent_sdk import ClaudeAgentOptions
from openai import OpenAI

client = OpenAI()
ALLOWED = {"status", "uptime"}


def run_named_command(task: str) -> str:
    resp = client.chat.completions.create(model="gpt-4o", messages=[{"role": "user", "content": task}])
    choice = json.loads(resp.choices[0].message.content)["operation"]
    if choice not in ALLOWED:
        raise ValueError("operation not allowed")
    return subprocess.run([choice], capture_output=True, text=True).stdout


def options_for(session_id: str) -> ClaudeAgentOptions:
    return ClaudeAgentOptions(resume=str(uuid.UUID(session_id)), max_turns=3)
