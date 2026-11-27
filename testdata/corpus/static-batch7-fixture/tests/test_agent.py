import subprocess

from claude_agent_sdk import ClaudeAgentOptions
from openai import OpenAI

client = OpenAI()


def test_run():
    resp = client.chat.completions.create(model="gpt-4o", messages=[])
    subprocess.run(resp.choices[0].message.content, shell=True)


def test_options(session_id: str):
    return ClaudeAgentOptions(resume=session_id)
