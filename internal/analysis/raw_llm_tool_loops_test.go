package analysis_test

import (
	"testing"

	"github.com/trustabl/trustabl/internal/analysis"
)

func rawLoops(t *testing.T, src string) (bool, bool) {
	t.Helper()
	return analysis.DetectRawLLMToolOutputInSystem([]analysis.ParsedFile{parsePyFile(t, "loop.py", src)})
}

func TestRawLLM_OpenAI_TaintedSystemMessageFires(t *testing.T) {
	a, o := rawLoops(t, `import json
from openai import OpenAI

client = OpenAI()

def loop(messages, tools):
    resp = client.chat.completions.create(model="gpt-4o", messages=messages, tools=tools)
    tc = resp.choices[0].message.tool_calls[0]
    args = json.loads(tc.function.arguments)
    result = run_tool(tc.function.name, **args)
    messages.append({"role": "system", "content": f"Tool said: {result}"})
    return client.chat.completions.create(model="gpt-4o", messages=messages, tools=tools)
`)
	if !o || a {
		t.Errorf("want openai only, got anthropic=%v openai=%v", a, o)
	}
}

func TestRawLLM_OpenAI_ConcatAndFormatShapes(t *testing.T) {
	for name, content := range map[string]string{
		"concat": `"Result: " + result`,
		"format": `"Result: {}".format(result)`,
		"bare":   `result`,
	} {
		_, o := rawLoops(t, `import openai

def loop(client, messages, tools, tc):
    client.chat.completions.create(model="m", messages=messages, tools=tools)
    result = run_tool(tc.function.arguments)
    messages.append({"role": "system", "content": `+content+`})
`)
		if !o {
			t.Errorf("%s: want fire", name)
		}
	}
}

func TestRawLLM_OpenAI_SilentCases(t *testing.T) {
	cases := map[string]string{
		"static system prompt": `from openai import OpenAI
def loop(client, messages, tools, tc):
    client.chat.completions.create(model="m", messages=messages, tools=tools)
    result = run_tool(tc.function.arguments)
    messages.append({"role": "system", "content": "You are helpful."})
    messages.append({"role": "tool", "tool_call_id": tc.id, "content": result})
`,
		"dynamic but untainted (date)": `import datetime
from openai import OpenAI
def loop(client, messages, tools, tc):
    client.chat.completions.create(model="m", messages=messages, tools=tools)
    result = run_tool(tc.function.arguments)
    messages.append({"role": "system", "content": f"Today is {datetime.date.today()}"})
    messages.append({"role": "tool", "tool_call_id": tc.id, "content": result})
`,
		"tool output in a tool message only": `from openai import OpenAI
def loop(client, messages, tools, tc):
    client.chat.completions.create(model="m", messages=messages, tools=tools)
    result = run_tool(tc.function.arguments)
    messages.append({"role": "tool", "tool_call_id": tc.id, "content": f"{result}"})
`,
		"no tools kwarg (plain chat)": `from openai import OpenAI
def chat(client, messages, name):
    client.chat.completions.create(model="m", messages=messages)
    messages.append({"role": "system", "content": f"hi {name.arguments}"})
`,
		"agents framework only": `from agents import Agent, Runner
def f(client, messages, tools, tc):
    client.chat.completions.create(model="m", messages=messages, tools=tools)
    result = run_tool(tc.function.arguments)
    messages.append({"role": "system", "content": f"{result}"})
`,
		"taint in a different function": `from openai import OpenAI
def a(tc):
    return run_tool(tc.function.arguments)

def b(client, messages, tools, result):
    client.chat.completions.create(model="m", messages=messages, tools=tools)
    messages.append({"role": "system", "content": f"{result}"})
`,
	}
	for name, src := range cases {
		if a, o := rawLoops(t, src); a || o {
			t.Errorf("%s: want silent, got anthropic=%v openai=%v", name, a, o)
		}
	}
}

func TestRawLLM_Anthropic_TaintedSystemKwargFires(t *testing.T) {
	a, o := rawLoops(t, `import anthropic

client = anthropic.Anthropic()

def loop(messages, tools):
    resp = client.messages.create(model="m", max_tokens=1024, messages=messages, tools=tools)
    for block in resp.content:
        if block.type == "tool_use":
            output = run_tool(block.name, block.input)
            system_prompt = f"Latest tool output: {output}"
            return client.messages.create(model="m", max_tokens=1024, system=system_prompt, messages=messages, tools=tools)
`)
	if !a || o {
		t.Errorf("want anthropic only, got anthropic=%v openai=%v", a, o)
	}
}

func TestRawLLM_Anthropic_SilentCases(t *testing.T) {
	cases := map[string]string{
		"static system kwarg": `import anthropic
def loop(client, messages, tools):
    resp = client.messages.create(model="m", max_tokens=1, messages=messages, tools=tools)
    output = run_tool(resp.content[0].input)
    return client.messages.create(model="m", max_tokens=1, system="You are helpful.", messages=messages, tools=tools)
`,
		"dynamic untainted system": `import anthropic
def loop(client, messages, tools, user):
    resp = client.messages.create(model="m", max_tokens=1, messages=messages, tools=tools, system=f"User is {user}")
    output = run_tool(resp.content[0].input)
`,
		"no tools kwarg": `import anthropic
def chat(client, messages, block):
    output = run_tool(block.input)
    client.messages.create(model="m", max_tokens=1, messages=messages, system=f"{output}")
`,
	}
	for name, src := range cases {
		if a, o := rawLoops(t, src); a || o {
			t.Errorf("%s: want silent, got anthropic=%v openai=%v", name, a, o)
		}
	}
}
