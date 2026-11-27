package analysis

import (
	"context"
	"testing"

	"github.com/trustabl/trustabl/internal/analysis/astutil"
	"github.com/trustabl/trustabl/internal/models"
)

func kindClasses(fs []models.TaintFlow, kind models.SourceKind) string {
	var out []models.TaintFlow
	for _, f := range fs {
		if f.SourceKind == kind {
			out = append(out, f)
		}
	}
	return classes(out)
}

func TestSecretEnvName(t *testing.T) {
	for n, want := range map[string]bool{"API_KEY": true, "GITHUB_TOKEN": true, "DB_PASSWORD": true, "OPENAIAPIKEY": true,
		"SERVICE_AUTH_TOKEN": true, "API_KEY_FILE": false, "PUBLIC_KEY": false, "TOKEN_URL": false, "KEY_ID": false,
		"HOME": false, "MONKEY": false, "PATH": false} {
		if got := IsSecretEnvName(n); got != want {
			t.Errorf("IsSecretEnvName(%q) = %v, want %v", n, got, want)
		}
	}
}

func TestPySecretReturn(t *testing.T) {
	fire := map[string]string{
		"get":      "@mcp.tool()\ndef t() -> str:\n    return os.environ.get('API_KEY', '')\n",
		"index":    "@mcp.tool()\ndef t() -> str:\n    return os.environ['API_KEY']\n",
		"getenv":   "@mcp.tool()\ndef t() -> str:\n    k = os.getenv('GITHUB_TOKEN')\n    return f'key {k}'\n",
		"dict":     "@mcp.tool()\ndef t(x: str) -> dict:\n    k = os.environ['API_KEY']\n    return {'k': k}\n",
		"withparm": "@mcp.tool()\ndef t(x: str) -> str:\n    k = os.environ['API_KEY']\n    return 'a' + k\n",
	}
	for name, body := range fire {
		if got := kindClasses(pyTaint(t, "import os\n"+body), models.SourceSecretEnv); got == "" {
			t.Errorf("%s: no secret flow", name)
		}
	}
	silent := map[string]string{
		"slice":    "@mcp.tool()\ndef t() -> str:\n    k = os.environ['API_KEY']\n    return k[-4:]\n",
		"bool":     "@mcp.tool()\ndef t() -> str:\n    k = os.environ.get('API_KEY')\n    return 'set' if k else 'missing'\n",
		"cmp":      "@mcp.tool()\ndef t() -> bool:\n    return bool(os.environ.get('API_KEY'))\n",
		"filename": "@mcp.tool()\ndef t() -> str:\n    return os.environ.get('API_KEY_FILE', '')\n",
		"nonsec":   "@mcp.tool()\ndef t() -> str:\n    return os.environ.get('HOME', '')\n",
		"helper":   "@mcp.tool()\ndef t() -> str:\n    return mask(os.environ['API_KEY'])\n",
		"killed":   "@mcp.tool()\ndef t() -> str:\n    k = os.environ['API_KEY']\n    k = 'x'\n    return k\n",
		"nonlit":   "@mcp.tool()\ndef t(n: str) -> str:\n    name = 'API_KEY'\n    return os.environ[name]\n",
	}
	for name, body := range silent {
		if got := kindClasses(pyTaint(t, "import os\n"+body), models.SourceSecretEnv); got != "" {
			t.Errorf("%s: unexpected secret flow %s", name, got)
		}
	}
}

func TestPyParamFlowsUnchangedByEnv(t *testing.T) {
	fl := pyTaint(t, "import os\n@mcp.tool()\ndef t(p: str) -> str:\n    return p\n")
	if len(fl) != 0 {
		t.Errorf("a parameter returned must not be a flow, got %v", fl)
	}
}

func TestPyMemoryWrite(t *testing.T) {
	body := "from mem0 import Memory\nm = Memory()\n@mcp.tool()\ndef t(user: str, password: str) -> str:\n    m.add(f'{user}:{password}', user_id='x')\n    return 'ok'\n"
	got := pyTaint(t, body)
	n := 0
	for _, f := range got {
		if f.SinkClass == models.SinkMemoryWrite && f.Param == "password" {
			n++
		}
	}
	if n != 1 {
		t.Errorf("want one memory_write flow for password, got %v", got)
	}
}

func TestTSSecretReturn(t *testing.T) {
	fire := []string{
		"server.tool('a', 'd', { n: z.string() }, async ({ n }) => {\n  const k = process.env.API_KEY;\n  return { content: [{ type: 'text', text: `k=${k}` }] };\n});\n",
		"server.tool('a', 'd', { n: z.string() }, async ({ n }) => process.env['SERVICE_TOKEN']);\n",
	}
	for i, b := range fire {
		if kindClasses(tsTaint(t, b), models.SourceSecretEnv) == "" {
			t.Errorf("fire %d: no flow", i)
		}
	}
	silent := []string{
		"server.tool('a', 'd', { n: z.string() }, async ({ n }) => {\n  const k = process.env.API_KEY;\n  return k.slice(-4);\n});\n",
		"server.tool('a', 'd', { n: z.string() }, async ({ n }) => {\n  const k = process.env.API_KEY_FILE;\n  return k;\n});\n",
		"server.tool('a', 'd', { n: z.string() }, async ({ n }) => {\n  const k = process.env.API_KEY;\n  const r = [1].map((x) => { return k; });\n  return 'ok';\n});\n",
	}
	for i, b := range silent {
		if got := kindClasses(tsTaint(t, b), models.SourceSecretEnv); got != "" {
			t.Errorf("silent %d: %s", i, got)
		}
	}
}

func parsePyFile(t *testing.T, rel, src string) ParsedFile {
	t.Helper()
	tree, err := astutil.ParseCtxTimeout(context.Background(), astutil.NewPyParser(), []byte(src))
	if err != nil {
		t.Fatal(err)
	}
	return ParsedFile{RelPath: rel, Source: []byte(src), Tree: tree}
}

func TestRepoFlows(t *testing.T) {
	llm := parsePyFile(t, "a.py", "import subprocess\nfrom openai import OpenAI\nc = OpenAI()\n\ndef f(t):\n    r = c.chat.completions.create(model='m', messages=[])\n    cmd = r.choices[0].message.content\n    subprocess.run(cmd, shell=True)\n\ndef g(t):\n    r = c.chat.completions.create(model='m', messages=[])\n    v = r.choices[0].message.content\n    if v not in ALLOWED:\n        raise ValueError(v)\n    subprocess.run([v])\n\ndef h(t):\n    subprocess.run(t, shell=True)\n")
	got := DiscoverRepoFlows([]ParsedFile{llm})
	if len(got) != 1 || got[0].Func != "f" || got[0].Source != models.SourceLLMOutput || got[0].SinkLine != 8 {
		t.Errorf("llm flows: %+v", got)
	}
	res := parsePyFile(t, "b.py", "import uuid\nfrom claude_agent_sdk import ClaudeAgentOptions\n\ndef a(sid):\n    return ClaudeAgentOptions(resume=sid)\n\ndef b(sid):\n    return ClaudeAgentOptions(resume=str(uuid.UUID(sid)))\n\ndef c(n: int):\n    return ClaudeAgentOptions(resume=n)\n\ndef d():\n    return ClaudeAgentOptions(resume='fixed')\n")
	got = DiscoverRepoFlows([]ParsedFile{res})
	if len(got) != 1 || got[0].Func != "a" || got[0].Source != models.SourceHandlerParam || got[0].SinkClass != models.SinkClaudeResume {
		t.Errorf("resume flows: %+v", got)
	}
	test := parsePyFile(t, "tests/test_b.py", "from claude_agent_sdk import ClaudeAgentOptions\n\ndef a(sid):\n    return ClaudeAgentOptions(resume=sid)\n")
	if got := DiscoverRepoFlows([]ParsedFile{test}); len(got) != 0 {
		t.Errorf("test path walked: %+v", got)
	}
}

func TestSkillScriptPrintsSecret(t *testing.T) {
	cases := map[string]struct {
		src  string
		want int
	}{
		"module-level": {"import os\nK = os.environ['API_KEY']\n\ndef m():\n    print(K)\n", 5},
		"direct":       {"import os\nprint(os.getenv('GH_TOKEN'))\n", 2},
		"stdout":       {"import os, sys\nsys.stdout.write(os.environ['API_KEY'])\n", 2},
		"stderr":       {"import os, sys\nprint(os.environ['API_KEY'], file=sys.stderr)\n", 0},
		"masked":       {"import os\nk = os.environ['API_KEY']\nprint(k[:4])\n", 0},
		"set-only":     {"import os\nprint('set' if os.environ.get('API_KEY') else 'unset')\n", 0},
		"non-secret":   {"import os\nprint(os.environ['HOME'])\n", 0},
		"param-shadow": {"import os\nK = os.environ['API_KEY']\n\ndef m(K):\n    print(K)\n", 0},
	}
	for name, c := range cases {
		if got := SkillScriptPrintsSecret("s.py", []byte(c.src)); got != c.want {
			t.Errorf("%s: got %d, want %d", name, got, c.want)
		}
	}
	if SkillScriptPrintsSecret("s.sh", []byte("echo $API_KEY")) != 0 {
		t.Errorf("shell script must not be read")
	}
}

func TestPySQLReceiverNotCodeRunner(t *testing.T) {
	if got := pyTaint(t, "@mcp.tool()\ndef t(code: str) -> str:\n    return sandbox.execute(code)\n"); len(got) != 0 {
		t.Errorf("sandbox.execute is not SQL: %v", got)
	}
	if got := pyTaint(t, "@mcp.tool()\ndef t(q: str) -> str:\n    return db.execute(f'select {q}')\n"); classes(got) == "" {
		t.Errorf("db.execute must still be a SQL sink")
	}
}
