package analysis_test

import (
	"testing"

	"github.com/trustabl/trustabl/internal/analysis"
	"github.com/trustabl/trustabl/internal/models"
)

const pyImport = "from mcp.server.mcpserver import MCPServer\n"

func pyBinds(t *testing.T, path, src string) []models.MCPServerBind {
	t.Helper()
	return analysis.DiscoverMCPServerBinds([]analysis.ParsedFile{parsePyFile(t, path, src)}, nil)
}

func tsBinds(t *testing.T, path, src string) []models.MCPServerBind {
	t.Helper()
	return analysis.DiscoverMCPServerBinds(nil, []analysis.ParsedFile{parseTSForTest(t, path, src)})
}

func TestMCPServerBinds_PythonFires(t *testing.T) {
	cases := map[string]string{
		"run call wildcard sse": pyImport + "mcp = MCPServer('x')\nmcp.run(transport=\"sse\", host=\"0.0.0.0\", port=8001)\n",
		"ipv6 wildcard":         pyImport + "mcp = MCPServer('x')\nmcp.run(transport=\"streamable-http\", host=\"::\")\n",
		"bracketed ipv6":        pyImport + "mcp = MCPServer('x')\nmcp.run(transport='http', host='[::]')\n",
		"v1 constructor host": "from mcp.server.fastmcp import FastMCP\n" +
			"mcp = FastMCP('x', host=\"0.0.0.0\")\nmcp.run(transport=\"streamable-http\")\n",
		"standalone fastmcp":      "from fastmcp import FastMCP\nmcp = FastMCP('x')\nmcp.run(transport=\"http\", host=\"0.0.0.0\")\n",
		"module attribute class":  "import fastmcp\nmcp = fastmcp.FastMCP('x')\nmcp.run(transport=\"sse\", host=\"0.0.0.0\")\n",
		"aliased class":           "from fastmcp import FastMCP as F\nm = F('x')\nm.run(transport=\"sse\", host=\"0.0.0.0\")\n",
		"auth=None is not credit": pyImport + "mcp = MCPServer('x', auth=None)\nmcp.run(transport=\"sse\", host=\"0.0.0.0\")\n",
	}
	for name, src := range cases {
		t.Run(name, func(t *testing.T) {
			got := pyBinds(t, "server.py", src)
			if len(got) != 1 {
				t.Fatalf("got %d binds, want 1: %+v", len(got), got)
			}
			if got[0].Language != models.LanguagePython || got[0].FilePath != "server.py" || got[0].Line == 0 {
				t.Errorf("bad record: %+v", got[0])
			}
		})
	}
}

func TestMCPServerBinds_PythonSilent(t *testing.T) {
	cases := map[string]string{
		"loopback literal":        pyImport + "mcp = MCPServer('x')\nmcp.run(transport=\"sse\", host=\"127.0.0.1\")\n",
		"localhost literal":       pyImport + "mcp = MCPServer('x')\nmcp.run(transport=\"sse\", host=\"localhost\")\n",
		"default host":            pyImport + "mcp = MCPServer('x')\nmcp.run(transport=\"sse\")\n",
		"env driven host":         "import os\n" + pyImport + "mcp = MCPServer('x')\nmcp.run(transport=\"sse\", host=os.environ[\"HOST\"])\n",
		"env default expression":  "import os\n" + pyImport + "mcp = MCPServer('x')\nmcp.run(transport=\"sse\", host=os.getenv(\"H\", \"0.0.0.0\"))\n",
		"f-string host":           pyImport + "mcp = MCPServer('x')\nmcp.run(transport=\"sse\", host=f\"{H}\")\n",
		"stdio transport":         pyImport + "mcp = MCPServer('x')\nmcp.run(transport=\"stdio\", host=\"0.0.0.0\")\n",
		"no transport":            pyImport + "mcp = MCPServer('x')\nmcp.run(host=\"0.0.0.0\")\n",
		"auth credit":             pyImport + "mcp = MCPServer('x', auth=settings, token_verifier=v)\nmcp.run(transport=\"sse\", host=\"0.0.0.0\")\n",
		"token verifier credit":   pyImport + "mcp = MCPServer('x', token_verifier=v)\nmcp.run(transport=\"sse\", host=\"0.0.0.0\")\n",
		"transport security run":  pyImport + "mcp = MCPServer('x')\nmcp.run(transport=\"sse\", host=\"0.0.0.0\", transport_security=ts)\n",
		"transport security ctor": pyImport + "mcp = MCPServer('x', transport_security=ts)\nmcp.run(transport=\"sse\", host=\"0.0.0.0\")\n",
		"kwargs splat":            pyImport + "mcp = MCPServer('x')\nmcp.run(transport=\"sse\", **opts)\n",
		"host in comment":         pyImport + "mcp = MCPServer('x')\n# mcp.run(transport=\"sse\", host=\"0.0.0.0\")\nmcp.run(transport=\"sse\")\n",
		"host in docstring":       pyImport + "mcp = MCPServer('x')\n\"\"\"mcp.run(transport='sse', host='0.0.0.0')\"\"\"\n",
		"not an MCP server class": "from flask import Flask\napp = Flask('x')\napp.run(transport=\"sse\", host=\"0.0.0.0\")\n",
		"class not imported":      "mcp = MCPServer('x')\nmcp.run(transport=\"sse\", host=\"0.0.0.0\")\n",
		"receiver not bound here": pyImport + "from app import mcp\nmcp.run(transport=\"sse\", host=\"0.0.0.0\")\n",
		"uvicorn launch":          pyImport + "import uvicorn\nmcp = MCPServer('x')\nuvicorn.run(mcp.sse_app(), host=\"0.0.0.0\")\n",
	}
	for name, src := range cases {
		t.Run(name, func(t *testing.T) {
			if got := pyBinds(t, "server.py", src); len(got) != 0 {
				t.Fatalf("want no binds, got %+v", got)
			}
		})
	}
}

func TestMCPServerBinds_TestPathsSkipped(t *testing.T) {
	src := pyImport + "mcp = MCPServer('x')\nmcp.run(transport=\"sse\", host=\"0.0.0.0\")\n"
	for _, p := range []string{"tests/test_server.py", "test_server.py", "pkg/conftest.py"} {
		if got := pyBinds(t, p, src); len(got) != 0 {
			t.Errorf("%s: want skipped, got %+v", p, got)
		}
	}
	ts := "import { createMcpExpressApp } from \"@modelcontextprotocol/sdk/server/express.js\";\n" +
		"const app = createMcpExpressApp({ host: \"0.0.0.0\" });\n"
	if got := tsBinds(t, "src/server.test.ts", ts); len(got) != 0 {
		t.Errorf("ts test path: want skipped, got %+v", got)
	}
}

const tsExpressImport = "import { createMcpExpressApp } from \"@modelcontextprotocol/sdk/server/express.js\";\n"

func TestMCPServerBinds_TypeScript(t *testing.T) {
	fire := map[string]string{
		"wildcard": tsExpressImport + "const app = createMcpExpressApp({ host: '0.0.0.0' });\n",
		"ipv6":     tsExpressImport + "const app = createMcpExpressApp({ host: \"::\" });\n",
		"aliased": "import { createMcpExpressApp as mk } from \"@modelcontextprotocol/sdk/server/express.js\";\n" +
			"const app = mk({ host: '0.0.0.0' });\n",
		"auth module is not imported": tsExpressImport + "import { z } from \"zod\";\n" +
			"const app = createMcpExpressApp({ host: '0.0.0.0' });\n",
	}
	for name, src := range fire {
		t.Run("fires "+name, func(t *testing.T) {
			got := tsBinds(t, "src/server.ts", src)
			if len(got) != 1 || got[0].Language != models.LanguageTypeScript || got[0].Line != 2 && name != "auth module is not imported" {
				t.Fatalf("got %+v", got)
			}
		})
	}
	silent := map[string]string{
		"loopback":         tsExpressImport + "const app = createMcpExpressApp({ host: '127.0.0.1' });\n",
		"default":          tsExpressImport + "const app = createMcpExpressApp();\n",
		"allowedHosts":     tsExpressImport + "const app = createMcpExpressApp({ host: '0.0.0.0', allowedHosts: ['a.example'] });\n",
		"shorthand":        tsExpressImport + "const app = createMcpExpressApp({ host: '0.0.0.0', allowedHosts });\n",
		"spread":           tsExpressImport + "const app = createMcpExpressApp({ host: '0.0.0.0', ...opts });\n",
		"env host":         tsExpressImport + "const app = createMcpExpressApp({ host: process.env.HOST });\n",
		"env with default": tsExpressImport + "const app = createMcpExpressApp({ host: process.env.HOST || '0.0.0.0' });\n",
		"template host":    tsExpressImport + "const app = createMcpExpressApp({ host: `0.0.0.0` });\n",
		"auth import": tsExpressImport + "import { requireBearerAuth } from \"@modelcontextprotocol/sdk/server/auth/middleware/bearerAuth.js\";\n" +
			"const app = createMcpExpressApp({ host: '0.0.0.0' });\n",
		"comment": tsExpressImport + "// createMcpExpressApp({ host: '0.0.0.0' })\nconst app = createMcpExpressApp();\n",
		"string":  tsExpressImport + "const s = \"createMcpExpressApp({ host: '0.0.0.0' })\";\n",
		"other module": "import { createMcpExpressApp } from \"./local.js\";\n" +
			"const app = createMcpExpressApp({ host: '0.0.0.0' });\n",
	}
	for name, src := range silent {
		t.Run("silent "+name, func(t *testing.T) {
			if got := tsBinds(t, "src/server.ts", src); len(got) != 0 {
				t.Fatalf("want none, got %+v", got)
			}
		})
	}
}

func TestMCPServerBinds_SortedAndStable(t *testing.T) {
	a := pyImport + "m = MCPServer('x')\nm.run(transport=\"sse\", host=\"0.0.0.0\")\nm.run(transport=\"http\", host=\"::\")\n"
	files := []analysis.ParsedFile{parsePyFile(t, "z.py", a), parsePyFile(t, "a.py", a)}
	got := analysis.DiscoverMCPServerBinds(files, nil)
	if len(got) != 4 {
		t.Fatalf("got %d, want 4", len(got))
	}
	for i := 1; i < len(got); i++ {
		p, c := got[i-1], got[i]
		if p.FilePath > c.FilePath || p.FilePath == c.FilePath && p.Line > c.Line {
			t.Fatalf("not sorted: %+v", got)
		}
	}
}

func TestMCPToolDescriptionKeepsHiddenCharacters(t *testing.T) {
	src := "@mcp.tool()\ndef lookup(q: str) -> str:\n    \"\"\"Look up.\u200b More.\"\"\"\n    return q\n\n" +
		"@mcp.tool(description=\"Find.\U000E0041\")\ndef find(q: str) -> str:\n    return q\n"
	tools := analysis.DiscoverToolsFromParsed([]analysis.ParsedFile{parsePyFile(t, "server.py", src)})
	if len(tools) != 2 {
		t.Fatalf("got %d tools: %+v", len(tools), tools)
	}
	for _, tl := range tools {
		if tl.Kind != models.KindMCPTool {
			t.Errorf("%s: kind %q", tl.Name, tl.Kind)
		}
	}
	if !analysis.HasHiddenText(tools[0].Description) && !analysis.HasHiddenText(tools[1].Description) {
		t.Errorf("docstring hidden character did not survive discovery")
	}
	if !analysis.HasHiddenText(tools[1].Config["description"]) && !analysis.HasHiddenText(tools[0].Config["description"]) {
		t.Errorf("description= hidden character did not survive discovery")
	}
}

func TestHasHiddenText(t *testing.T) {
	yes := []string{"a\u200bb", "a\U000E0041b", "a\u202eb", "\u200b"}
	no := []string{"", "plain text", "\ufeffleading BOM", "\U0001F468\u200d\U0001F469", "\U0001F3F4\U000E0067\U000E0062\U000E007F"}
	for _, s := range yes {
		if !analysis.HasHiddenText(s) {
			t.Errorf("want hidden for %q", s)
		}
	}
	for _, s := range no {
		if analysis.HasHiddenText(s) {
			t.Errorf("want clean for %q", s)
		}
	}
}

const fmImport = "from fastmcp import FastMCP\n"

func TestMCPServerBinds_PythonChainedAndFactoryFire(t *testing.T) {
	cases := map[string]string{
		"direct chained call":      fmImport + "FastMCP(\"x\").run(transport=\"sse\", host=\"0.0.0.0\")\n",
		"direct chained v1 host":   "from mcp.server.fastmcp import FastMCP\nFastMCP(\"x\", host=\"0.0.0.0\").run(transport=\"streamable-http\")\n",
		"direct chained mcpserver": pyImport + "MCPServer(\"x\").run(transport=\"http\", host=\"::\")\n",
		"factory returns the call": fmImport + "def create():\n    return FastMCP(\"x\")\n\ncreate().run(transport=\"sse\", host=\"0.0.0.0\")\n",
		"factory assigned once and returned": fmImport + "def create():\n    mcp = FastMCP(\"x\", instructions=\"i\")\n" +
			"    @mcp.tool()\n    def t() -> str:\n        return \"a\"\n    return mcp\n\n" +
			"if __name__ == \"__main__\":\n    create().run(transport=\"sse\", host=\"0.0.0.0\", port=8001, path=\"/sse\")\n",
		"factory v1 constructor host": "from mcp.server.fastmcp import FastMCP\ndef create():\n    return FastMCP(\"x\", host=\"0.0.0.0\")\n" +
			"create().run(transport=\"sse\")\n",
		"run_sse_async on a variable": pyImport + "mcp = MCPServer(\"x\")\nasync def main():\n    await mcp.run_sse_async(host=\"0.0.0.0\")\n",
		"run_streamable_http_async on a factory": pyImport + "def make():\n    return MCPServer(\"x\")\n" +
			"async def main():\n    await make().run_streamable_http_async(host=\"0.0.0.0\")\n",
		"factory defined after use": fmImport + "def main():\n    create().run(transport=\"sse\", host=\"0.0.0.0\")\n\ndef create():\n    return FastMCP(\"x\")\n",
	}
	for name, src := range cases {
		t.Run(name, func(t *testing.T) {
			got := pyBinds(t, "server.py", src)
			if len(got) != 1 {
				t.Fatalf("got %d binds, want 1: %+v", len(got), got)
			}
		})
	}
}

func TestMCPServerBinds_PythonChainedAndFactorySilent(t *testing.T) {
	wild := "(transport=\"sse\", host=\"0.0.0.0\")\n"
	cases := map[string]string{
		"chained auth credit":          fmImport + "FastMCP(\"x\", auth=A).run" + wild,
		"chained token verifier":       pyImport + "MCPServer(\"x\", token_verifier=V).run" + wild,
		"chained transport_security":   fmImport + "FastMCP(\"x\").run(transport=\"sse\", host=\"0.0.0.0\", transport_security=ts)\n",
		"chained loopback":             fmImport + "FastMCP(\"x\").run(transport=\"sse\", host=\"127.0.0.1\")\n",
		"chained env host":             "import os\n" + fmImport + "FastMCP(\"x\").run(transport=\"sse\", host=os.environ[\"H\"])\n",
		"chained splat":                fmImport + "FastMCP(\"x\").run(transport=\"sse\", **o)\n",
		"chained stdio":                fmImport + "FastMCP(\"x\").run(transport=\"stdio\", host=\"0.0.0.0\")\n",
		"chained non-server class":     "from flask import Flask\nFlask(\"x\").run" + wild,
		"factory with auth":            fmImport + "def create():\n    return FastMCP(\"x\", auth=A)\n\ncreate().run" + wild,
		"factory loopback":             fmImport + "def create():\n    return FastMCP(\"x\")\n\ncreate().run(transport=\"sse\", host=\"localhost\")\n",
		"factory two returns":          fmImport + "def create(f):\n    if f:\n        return FastMCP(\"a\")\n    return FastMCP(\"b\")\n\ncreate(1).run" + wild,
		"factory assigned twice":       fmImport + "def create():\n    m = FastMCP(\"a\")\n    m = FastMCP(\"b\")\n    return m\n\ncreate().run" + wild,
		"factory returns a non-server": fmImport + "from flask import Flask\ndef create():\n    return Flask(\"x\")\n\ncreate().run" + wild,
		"factory returns a parameter":  fmImport + "def create(m):\n    return m\n\ncreate(FastMCP(\"x\")).run" + wild,
		"factory defined twice":        fmImport + "def create():\n    return FastMCP(\"x\")\ndef create():\n    return FastMCP(\"y\")\n\ncreate().run" + wild,
		"two hops":                     fmImport + "def inner():\n    return FastMCP(\"x\")\ndef outer():\n    return inner()\n\nouter().run" + wild,
		"nested factory":               fmImport + "def outer():\n    def create():\n        return FastMCP(\"x\")\n    return 1\n\ncreate().run" + wild,
		"factory imported":             fmImport + "from app import create_server\n\ncreate_server().run" + wild,
		"decorated factory":            fmImport + "@cache\ndef create():\n    return FastMCP(\"x\")\n\ncreate().run" + wild,
		"factory in another file":      fmImport + "create().run" + wild,
		"method call on result":        fmImport + "def create():\n    return FastMCP(\"x\")\n\ncreate().other" + wild,
		"factory call on non-run name": fmImport + "def create():\n    return FastMCP(\"x\")\n\ncreate().tool(transport=\"sse\", host=\"0.0.0.0\")\n",
		"run_sse_async loopback":       pyImport + "async def main():\n    await MCPServer(\"x\").run_sse_async(host=\"127.0.0.1\")\n",
		"run_sse_async default host":   pyImport + "async def main():\n    await MCPServer(\"x\").run_sse_async()\n",
		"chained in a comment":         fmImport + "# FastMCP(\"x\").run(transport=\"sse\", host=\"0.0.0.0\")\n",
		"chained in a string":          fmImport + "s = 'FastMCP(\"x\").run(transport=\"sse\", host=\"0.0.0.0\")'\n",
	}
	for name, src := range cases {
		t.Run(name, func(t *testing.T) {
			if got := pyBinds(t, "server.py", src); len(got) != 0 {
				t.Fatalf("want no binds, got %+v", got)
			}
		})
	}
}

func TestMCPServerBinds_ChainedAndFactoryTestPathSkipped(t *testing.T) {
	src := fmImport + "def create():\n    return FastMCP(\"x\")\n\ncreate().run(transport=\"sse\", host=\"0.0.0.0\")\n"
	if got := pyBinds(t, "tests/test_server.py", src); len(got) != 0 {
		t.Fatalf("test path: want skipped, got %+v", got)
	}
}
