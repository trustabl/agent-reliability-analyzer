package analysis_test

import (
	"testing"

	"github.com/trustabl/trustabl/internal/analysis"
	"github.com/trustabl/trustabl/internal/models"
)

func pyCallKwargs(t *testing.T, path, src string) []models.CallKwargHit {
	t.Helper()
	return analysis.DiscoverCallKwargs([]analysis.ParsedFile{parsePyFile(t, path, src)}, nil)
}

func tsCallKwargs(t *testing.T, path, src string) []models.CallKwargHit {
	t.Helper()
	return analysis.DiscoverCallKwargs(nil, []analysis.ParsedFile{parseTSForTest(t, path, src)})
}

const mcpPyHead = "import os\nfrom mcp import StdioServerParameters\ncmd = load()\n"

func TestCallKwargs_PythonEnvironFires(t *testing.T) {
	cases := map[string]string{
		"os.environ":          mcpPyHead + "p = StdioServerParameters(command=cmd, env=os.environ)\n",
		"environ copy":        mcpPyHead + "p = StdioServerParameters(command=cmd, env=os.environ.copy())\n",
		"dict of environ":     mcpPyHead + "p = StdioServerParameters(command=cmd, env=dict(os.environ))\n",
		"dict splat":          mcpPyHead + "p = StdioServerParameters(command=cmd, env=dict(**os.environ))\n",
		"dict with extras":    mcpPyHead + "p = StdioServerParameters(command=cmd, env=dict(os.environ, A='b'))\n",
		"display splat":       mcpPyHead + "p = StdioServerParameters(command=cmd, env={**os.environ, 'A': 'b'})\n",
		"from os import":      "from os import environ\nfrom mcp import StdioServerParameters\np = StdioServerParameters(command=cmd, env=environ)\n",
		"os alias":            "import os as o\nfrom mcp import StdioServerParameters\np = StdioServerParameters(command=cmd, env=o.environ)\n",
		"class alias":         "import os\nfrom mcp import StdioServerParameters as SSP\np = SSP(command=cmd, env=os.environ)\n",
		"module attribute":    "import os\nimport mcp\np = mcp.StdioServerParameters(command=cmd, env=os.environ)\n",
		"stdio submodule":     "import os\nfrom mcp.client.stdio import StdioServerParameters\np = StdioServerParameters(command=cmd, env=os.environ)\n",
		"command from config": mcpPyHead + "p = StdioServerParameters(command=cfg['command'], env=os.environ)\n",
		"command positional":  mcpPyHead + "p = StdioServerParameters(cmd, env=os.environ)\n",
		"command f-string":    mcpPyHead + "p = StdioServerParameters(command=f'{base}/server', env=os.environ)\n",
		"multi-line call":     mcpPyHead + "p = StdioServerParameters(\n    command=cmd,\n    env=os.environ,\n)\n",
	}
	for name, src := range cases {
		t.Run(name, func(t *testing.T) {
			got := pyCallKwargs(t, "client.py", src)
			if len(got) != 1 {
				t.Fatalf("got %d hits, want 1: %+v", len(got), got)
			}
			h := got[0]
			if h.Callee != "StdioServerParameters" || h.Kwarg != "env" || h.Value != "environ" ||
				h.Language != models.LanguagePython || h.FilePath != "client.py" || h.Line == 0 {
				t.Errorf("bad record: %+v", h)
			}
		})
	}
}

func TestCallKwargs_PythonEnvironSilent(t *testing.T) {
	cases := map[string]string{
		"narrow dict":         mcpPyHead + "p = StdioServerParameters(command=cmd, env={'TOKEN': os.environ['TOKEN']})\n",
		"environ lookup":      mcpPyHead + "p = StdioServerParameters(command=cmd, env={'A': os.environ.get('A')})\n",
		"value in a variable": mcpPyHead + "e = os.environ\np = StdioServerParameters(command=cmd, env=e)\n",
		"no env":              mcpPyHead + "p = StdioServerParameters(command=cmd)\n",
		"env None":            mcpPyHead + "p = StdioServerParameters(command=cmd, env=None)\n",
		"subprocess env":      "import os, subprocess\nsubprocess.run(['x'], env=os.environ)\n",
		"other class":         "import os\nfrom mcp import StdioServerParameters\nOther(env=os.environ)\n",
		"no mcp import":       "import os\np = StdioServerParameters(command=cmd, env=os.environ)\n",
		"in a comment":        mcpPyHead + "# p = StdioServerParameters(command=cmd, env=os.environ)\n",
		"in a string":         mcpPyHead + "s = \"StdioServerParameters(command=cmd, env=os.environ)\"\n",
		"non-os environ":      mcpPyHead + "p = StdioServerParameters(command=cmd, env=config.environ)\n",
		"literal command":     mcpPyHead + "p = StdioServerParameters(command='python', env=os.environ)\n",
		"literal positional":  mcpPyHead + "p = StdioServerParameters('python', env=os.environ)\n",
		"double-quoted":       mcpPyHead + "p = StdioServerParameters(command=\"./server\", args=[], env={**os.environ})\n",
		"sys.executable":      mcpPyHead + "import sys\np = StdioServerParameters(command=sys.executable, env=os.environ.copy())\n",
		"f-string no field":   mcpPyHead + "p = StdioServerParameters(command=f'python', env=os.environ)\n",
		"no command given":    mcpPyHead + "p = StdioServerParameters(env=os.environ)\n",
		"kwargs splat":        mcpPyHead + "p = StdioServerParameters(**opts)\n",
	}
	for name, src := range cases {
		t.Run(name, func(t *testing.T) {
			if got := pyCallKwargs(t, "client.py", src); len(got) != 0 {
				t.Fatalf("got hits, want none: %+v", got)
			}
		})
	}
	t.Run("test path", func(t *testing.T) {
		src := mcpPyHead + "p = StdioServerParameters(command=cmd, env=os.environ)\n"
		if got := pyCallKwargs(t, "tests/test_client.py", src); len(got) != 0 {
			t.Fatalf("a test path must not be recorded: %+v", got)
		}
	})
}

func TestCallKwargs_PythonBoolLiterals(t *testing.T) {
	type want struct{ callee, kwarg, value string }
	fires := map[string]struct {
		src string
		w   want
	}{
		"warn_only true":     {"validate_url_for_ssrf(url, warn_only=True)\n", want{"validate_url_for_ssrf", "warn_only", "true"}},
		"method call":        {"guard.validate_ssrf(url, warn_only=True)\n", want{"validate_ssrf", "warn_only", "true"}},
		"ssrf disabled":      {"Settings(ssrf_protection_enabled=False)\n", want{"Settings", "ssrf_protection_enabled", "false"}},
		"ssrf enabled":       {"Settings(ssrf_protection_enabled=True)\n", want{"Settings", "ssrf_protection_enabled", "true"}},
		"trust remote code":  {"m = AutoModel.from_pretrained('x', trust_remote_code=True)\n", want{"from_pretrained", "trust_remote_code", "true"}},
		"trust remote false": {"m = AutoModel.from_pretrained('x', trust_remote_code=False)\n", want{"from_pretrained", "trust_remote_code", "false"}},
		"multi-line":         {"m = AutoModel.from_pretrained(\n    'x',\n    trust_remote_code=True,\n)\n", want{"from_pretrained", "trust_remote_code", "true"}},
	}
	for name, c := range fires {
		t.Run("records "+name, func(t *testing.T) {
			got := pyCallKwargs(t, "m.py", c.src)
			if len(got) != 1 || got[0].Callee != c.w.callee || got[0].Kwarg != c.w.kwarg || got[0].Value != c.w.value {
				t.Fatalf("got %+v, want %+v", got, c.w)
			}
		})
	}
	silent := map[string]string{
		"variable value":            "m = AutoModel.from_pretrained('x', trust_remote_code=flag)\n",
		"expression":                "m = AutoModel.from_pretrained('x', trust_remote_code=args.trust)\n",
		"string True":               "m = AutoModel.from_pretrained('x', trust_remote_code='True')\n",
		"in a comment":              "# m = AutoModel.from_pretrained('x', trust_remote_code=True)\n",
		"in a string":               "s = \"from_pretrained('x', trust_remote_code=True)\"\n",
		"in a docstring":            "def f():\n    \"\"\"Call from_pretrained(x, trust_remote_code=True).\"\"\"\n",
		"keyword outside the table": "m = AutoModel.from_pretrained('x', local_files_only=True)\n",
		"positional True":           "validate_url_for_ssrf(url, True)\n",
	}
	for name, src := range silent {
		t.Run("silent "+name, func(t *testing.T) {
			if got := pyCallKwargs(t, "m.py", src); len(got) != 0 {
				t.Fatalf("got hits, want none: %+v", got)
			}
		})
	}
	t.Run("silent test path", func(t *testing.T) {
		if got := pyCallKwargs(t, "tests/test_m.py", "AutoModel.from_pretrained('x', trust_remote_code=True)\n"); len(got) != 0 {
			t.Fatalf("a test path must not be recorded: %+v", got)
		}
	})
}

const mcpTSHead = "import { StdioClientTransport } from \"@modelcontextprotocol/sdk/client/stdio.js\";\nconst cmd = load();\n"

func TestCallKwargs_TypeScript(t *testing.T) {
	fires := map[string]string{
		"process.env":         mcpTSHead + "const t = new StdioClientTransport({ command: cmd, env: process.env });\n",
		"spread":              mcpTSHead + "const t = new StdioClientTransport({ command: cmd, env: { ...process.env, A: 'b' } });\n",
		"aliased import":      "import { StdioClientTransport as T } from \"@modelcontextprotocol/sdk/client/stdio.js\";\nconst t = new T({ command: cmd, env: process.env });\n",
		"type cast":           mcpTSHead + "const t = new StdioClientTransport({ command: cmd, env: process.env as Record<string, string> });\n",
		"cast spread":         mcpTSHead + "const t = new StdioClientTransport({ command: cmd, env: { ...process.env, A: 'b' } as Record<string, string> });\n",
		"command from config": mcpTSHead + "const t = new StdioClientTransport({ command: cfg.command, env: process.env });\n",
		"template with field": mcpTSHead + "const t = new StdioClientTransport({ command: `${dir}/s`, env: process.env });\n",
		"quoted key":          mcpTSHead + "const t = new StdioClientTransport({ command: cmd, 'env': process.env });\n",
	}
	for name, src := range fires {
		t.Run("fires "+name, func(t *testing.T) {
			got := tsCallKwargs(t, "client.ts", src)
			if len(got) != 1 || got[0].Callee != "StdioClientTransport" || got[0].Kwarg != "env" ||
				got[0].Value != "environ" || got[0].Language != models.LanguageTypeScript {
				t.Fatalf("got %+v", got)
			}
		})
	}
	silent := map[string]string{
		"narrow object":     mcpTSHead + "const t = new StdioClientTransport({ command: cmd, env: { TOKEN: process.env.TOKEN ?? '' } });\n",
		"variable":          mcpTSHead + "const e = process.env;\nconst t = new StdioClientTransport({ command: cmd, env: e });\n",
		"no env":            mcpTSHead + "const t = new StdioClientTransport({ command: cmd });\n",
		"no import":         "const t = new StdioClientTransport({ command: cmd, env: process.env });\n",
		"other import":      "import { StdioClientTransport } from \"./local.js\";\nconst t = new StdioClientTransport({ command: cmd, env: process.env });\n",
		"in a comment":      mcpTSHead + "// new StdioClientTransport({ command: cmd, env: process.env });\n",
		"in a string":       mcpTSHead + "const s = \"new StdioClientTransport({ env: process.env })\";\n",
		"literal command":   mcpTSHead + "const t = new StdioClientTransport({ command: 'node', args: ['s.js'], env: process.env });\n",
		"template literal":  mcpTSHead + "const t = new StdioClientTransport({ command: `node`, env: { ...process.env } });\n",
		"child_process env": "import { spawn } from 'node:child_process';\nspawn('x', [], { env: process.env });\n",
	}
	for name, src := range silent {
		t.Run("silent "+name, func(t *testing.T) {
			if got := tsCallKwargs(t, "client.ts", src); len(got) != 0 {
				t.Fatalf("got hits, want none: %+v", got)
			}
		})
	}
	t.Run("silent test path", func(t *testing.T) {
		if got := tsCallKwargs(t, "src/client.test.ts", mcpTSHead+"new StdioClientTransport({ env: process.env });\n"); len(got) != 0 {
			t.Fatalf("a test path must not be recorded: %+v", got)
		}
	})
}
