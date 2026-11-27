import subprocess

from claude_agent_sdk import ClaudeAgentOptions
from openai import OpenAI

client = OpenAI()


def run_suggested_command(task: str) -> str:
    resp = client.chat.completions.create(model="gpt-4o", messages=[{"role": "user", "content": task}])
    cmd = resp.choices[0].message.content
    return subprocess.run(cmd, shell=True, capture_output=True, text=True).stdout


def options_for(session_id: str) -> ClaudeAgentOptions:
    return ClaudeAgentOptions(resume=session_id, max_turns=3)
