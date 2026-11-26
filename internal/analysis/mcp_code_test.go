package analysis_test

import (
	"testing"

	"github.com/trustabl/trustabl/internal/analysis"
	"github.com/trustabl/trustabl/internal/models"
)

func codePy(t *testing.T, path, src string) []models.MCPCodeFact {
	t.Helper()
	return analysis.DiscoverMCPCode([]analysis.ParsedFile{parsePyFile(t, path, src)}, nil)
}

func codeTS(t *testing.T, path, src string) []models.MCPCodeFact {
	t.Helper()
	return analysis.DiscoverMCPCode(nil, []analysis.ParsedFile{parseTSForTest(t, path, src)})
}

func kinds(fs []models.MCPCodeFact) map[models.MCPCodeKind]int {
	m := map[models.MCPCodeKind]int{}
	for _, f := range fs {
		m[f.Kind]++
	}
	return m
}

type codeCase struct {
	name string
	path string
	src  string
	kind models.MCPCodeKind
}

func runFire(t *testing.T, ts bool, cases []codeCase) {
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			var got []models.MCPCodeFact
			if ts {
				got = codeTS(t, c.path, c.src)
			} else {
				got = codePy(t, c.path, c.src)
			}
			if kinds(got)[c.kind] != 1 {
				t.Fatalf("want one %s fact, got %+v", c.kind, got)
			}
			for _, f := range got {
				if f.Line == 0 || f.FilePath != c.path {
					t.Errorf("bad record %+v", f)
				}
			}
		})
	}
}

func runSilent(t *testing.T, ts bool, cases []codeCase) {
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			var got []models.MCPCodeFact
			if ts {
				got = codeTS(t, c.path, c.src)
			} else {
				got = codePy(t, c.path, c.src)
			}
			if kinds(got)[c.kind] != 0 {
				t.Fatalf("want no %s fact, got %+v", c.kind, got)
			}
		})
	}
}

const tsMCPImport = "import { McpServer } from \"@modelcontextprotocol/sdk/server/mcp.js\";\n"

func TestMCPCode_PrefixContainment(t *testing.T) {
	k := models.MCPCodePrefixContainment
	runFire(t, true, []codeCase{
		{"some arrow", "fs.ts", tsMCPImport + "const ok = allowedDirectories.some((dir) => normalizedPath.startsWith(dir));\n", k},
		{"direct root", "fs.ts", tsMCPImport + "if (!resolvedPath.startsWith(rootDir)) throw new Error('x');\n", k},
		{"member root", "fs.js", tsMCPImport + "if (!absolutePath.startsWith(this.baseDir)) throw new Error('x');\n", k},
	})
	runSilent(t, true, []codeCase{
		{"url path prefix", "fs.ts", tsMCPImport + "const ok = urlObj.pathname.startsWith(proxyUrlObj.pathname);\n", k},
		{"url path helper", "fs.ts", tsMCPImport + "const ok = pathname.startsWith(pathUnderBase(options.mcpPath, \"oauth/\"));\n", k},
		{"sep appended", "fs.ts", tsMCPImport + "const ok = dirs.some((dir) => normalizedPath.startsWith(dir + path.sep));\n", k},
		{"sep variable", "fs.ts", tsMCPImport + "const ok = filePath.startsWith(rootWithSep);\n", k},
		{"slash string", "fs.ts", tsMCPImport + "const ok = filePath.startsWith(rootDir + \"/\");\n", k},
		{"literal argument", "fs.ts", tsMCPImport + "const ok = filePath.startsWith(\"/tmp/allowed\");\n", k},
		{"not path-like receiver", "fs.ts", tsMCPImport + "const ok = name.startsWith(rootDir);\n", k},
		{"not dir-like argument", "fs.ts", tsMCPImport + "const ok = filePath.startsWith(prefix);\n", k},
		{"no mcp import", "fs.ts", "const ok = filePath.startsWith(rootDir);\n", k},
		{"comment only", "fs.ts", tsMCPImport + "// if (!resolvedPath.startsWith(rootDir)) throw\n", k},
		{"string only", "fs.ts", tsMCPImport + "const s = \"resolvedPath.startsWith(rootDir)\";\n", k},
		{"test path", "tests/fs.test.ts", tsMCPImport + "const ok = resolvedPath.startsWith(rootDir);\n", k},
		{"python is not read", "fs.py", "import mcp\nok = resolved_path.startswith(root_dir)\n", k},
	})
}

func TestMCPCode_StdioCommand(t *testing.T) {
	k := models.MCPCodeStdioCommand
	runFire(t, false, []codeCase{
		{"kwarg from request body", "c.py", "from mcp import StdioServerParameters\np = StdioServerParameters(command=request.json['command'], args=[])\n", k},
		{"fstring from payload", "c.py", "from mcp import StdioServerParameters\np = StdioServerParameters(command=f'{payload.dir}/run', args=[])\n", k},
		{"shell with -c", "c.py", "from mcp import StdioServerParameters\np = StdioServerParameters(command='bash', args=['-c', cmd])\n", k},
		{"shell path with -c", "c.py", "from mcp import StdioServerParameters\np = StdioServerParameters(command='/bin/sh', args=['-c', cmd])\n", k},
		{"agents params dict", "c.py", "from agents.mcp import MCPServerStdio\ns = MCPServerStdio(params={'command': body['command'], 'args': []})\n", k},
		{"langchain adapter entry", "c.py", "from langchain_mcp_adapters.client import MultiServerMCPClient\nc = MultiServerMCPClient({'x': {'transport': 'stdio', 'command': req.cmd, 'args': []}})\n", k},
		{"module attribute", "c.py", "import mcp\np = mcp.StdioServerParameters(command=query['cmd'])\n", k},
	})
	runSilent(t, false, []codeCase{
		{"identifier rebound to a non-request value", "c.py", "from mcp import StdioServerParameters\nCMD = 'npx'\nCMD = other()\np = StdioServerParameters(command=CMD, args=[])\n", k},
		{"command from a configuration object", "c.py", "from mcp import StdioServerParameters\np = StdioServerParameters(command=cfg.command, args=cfg.args)\n", k},
		{"command from a function parameter", "c.py", "from mcp import StdioServerParameters\ndef f(command):\n    return StdioServerParameters(command=command)\n", k},
		{"word that contains a request term", "c.py", "from mcp import StdioServerParameters\np = StdioServerParameters(command=prequel_cmd)\n", k},
		{"literal", "c.py", "from mcp import StdioServerParameters\np = StdioServerParameters(command='uvx', args=['pkg==1.0'])\n", k},
		{"identifier bound once to literal", "c.py", "from mcp import StdioServerParameters\nCMD = 'npx'\np = StdioServerParameters(command=CMD, args=[])\n", k},
		{"sys.executable", "c.py", "import sys\nfrom mcp import StdioServerParameters\np = StdioServerParameters(command=sys.executable, args=['s.py'])\n", k},
		{"allow-list named", "c.py", "from mcp import StdioServerParameters\nALLOWED_COMMANDS = {'uvx'}\nif cmd in ALLOWED_COMMANDS:\n    p = StdioServerParameters(command=cmd)\n", k},
		{"not an mcp import", "c.py", "from mylib import StdioServerParameters\np = StdioServerParameters(command=cfg)\n", k},
		{"comment only", "c.py", "from mcp import StdioServerParameters\n# p = StdioServerParameters(command=cfg)\n", k},
		{"string only", "c.py", "from mcp import StdioServerParameters\ns = \"StdioServerParameters(command=cfg)\"\n", k},
		{"test path", "tests/test_c.py", "from mcp import StdioServerParameters\np = StdioServerParameters(command=cfg)\n", k},
		{"shell without -c", "c.py", "from mcp import StdioServerParameters\np = StdioServerParameters(command='bash', args=['script.sh'])\n", k},
	})
	runFire(t, true, []codeCase{
		{"ts request-derived", "c.ts", "import { StdioClientTransport } from \"@modelcontextprotocol/sdk/client/stdio.js\";\nconst t = new StdioClientTransport({ command: req.body.command, args: [] });\n", k},
		{"ts shell -c", "c.ts", "import { StdioClientTransport } from \"@modelcontextprotocol/sdk/client/stdio.js\";\nconst t = new StdioClientTransport({ command: \"bash\", args: [\"-c\", cmd] });\n", k},
		{"ts template", "c.ts", "import { StdioClientTransport } from \"@modelcontextprotocol/sdk/client/stdio.js\";\nconst t = new StdioClientTransport({ command: `${req.dir}/run` });\n", k},
	})
	runSilent(t, true, []codeCase{
		{"ts config value", "c.ts", "import { StdioClientTransport } from \"@modelcontextprotocol/sdk/client/stdio.js\";\nconst t = new StdioClientTransport({ command: config.command, args: config.args });\n", k},
		{"ts literal", "c.ts", "import { StdioClientTransport } from \"@modelcontextprotocol/sdk/client/stdio.js\";\nconst t = new StdioClientTransport({ command: \"npx\", args: [\"-y\", \"pkg@1.0.0\"] });\n", k},
		{"ts const literal", "c.ts", "import { StdioClientTransport } from \"@modelcontextprotocol/sdk/client/stdio.js\";\nconst CMD = \"node\";\nconst t = new StdioClientTransport({ command: CMD });\n", k},
		{"ts comment", "c.ts", "import { StdioClientTransport } from \"@modelcontextprotocol/sdk/client/stdio.js\";\n// new StdioClientTransport({ command: cmd })\n", k},
		{"ts test path", "c.test.ts", "import { StdioClientTransport } from \"@modelcontextprotocol/sdk/client/stdio.js\";\nconst t = new StdioClientTransport({ command: cmd });\n", k},
		{"ts allow-list", "c.ts", "import { StdioClientTransport } from \"@modelcontextprotocol/sdk/client/stdio.js\";\nconst allowedCommands = new Set([\"node\"]);\nconst t = new StdioClientTransport({ command: req.body.cmd });\n", k},
	})
}

func TestMCPCode_BearerWithoutAudience(t *testing.T) {
	k := models.MCPCodeBearerNoAudience
	const imp = "from mcp.server.auth.settings import AuthSettings\n"
	runFire(t, false, []codeCase{
		{"unset", "a.py", imp + "m = FastMCP('x', token_verifier=V(), auth=AuthSettings(issuer_url='https://a', resource_server_url='https://r'))\n", k},
		{"false", "a.py", imp + "s = AuthSettings(issuer_url='https://a', resource_server_url='https://r', validate_token_resource=False)\nm = FastMCP('x', token_verifier=V(), auth=s)\n", k},
		{"comment naming audience is not credit", "a.py", imp + "# audience is checked upstream\ns = AuthSettings(issuer_url='https://a', resource_server_url='https://r')\nm = FastMCP('x', token_verifier=V(), auth=s)\n", k},
		{"none", "a.py", imp + "s = AuthSettings(issuer_url='https://a', resource_server_url='https://r', validate_token_resource=None)\nm = FastMCP('x', token_verifier=V(), auth=s)\n", k},
	})
	runSilent(t, false, []codeCase{
		{"validate true", "a.py", imp + "s = AuthSettings(issuer_url='https://a', resource_server_url='https://r', validate_token_resource=True)\nm = FastMCP('x', token_verifier=V(), auth=s)\n", k},
		{"aud checked in verifier", "a.py", imp + "class V:\n    async def verify_token(self, t):\n        if 'r' not in claims['aud']:\n            return None\ns = AuthSettings(issuer_url='https://a', resource_server_url='https://r')\nm = FastMCP('x', token_verifier=V(), auth=s)\n", k},
		{"no token_verifier", "a.py", imp + "s = AuthSettings(issuer_url='https://a', resource_server_url='https://r')\nm = FastMCP('x', auth_server_provider=P(), auth=s)\n", k},
		{"splat", "a.py", imp + "s = AuthSettings(**cfg)\nm = FastMCP('x', token_verifier=V(), auth=s)\n", k},
		{"test path", "tests/a.py", imp + "s = AuthSettings(issuer_url='https://a', resource_server_url='https://r')\nm = FastMCP('x', token_verifier=V(), auth=s)\n", k},
		{"string only", "a.py", imp + "s = \"AuthSettings(issuer_url='x') token_verifier\"\n", k},
	})
}

func TestMCPCode_ClientOpensURL(t *testing.T) {
	k := models.MCPCodeClientOpensURL
	const pyImp = "from mcp.client.auth import OAuthClientProvider\nimport webbrowser, os, subprocess\n"
	runFire(t, false, []codeCase{
		{"webbrowser", "c.py", pyImp + "async def handler(url: str):\n    webbrowser.open(url)\n", k},
		{"startfile", "c.py", pyImp + "def handler(authorization_url):\n    os.startfile(authorization_url)\n", k},
		{"xdg-open", "c.py", pyImp + "def handler(u):\n    subprocess.run(['xdg-open', u])\n", k},
		{"method with self", "c.py", pyImp + "class P:\n    async def redirect(self, url):\n        webbrowser.open_new_tab(url)\n", k},
	})
	runSilent(t, false, []codeCase{
		{"scheme checked", "c.py", pyImp + "from urllib.parse import urlparse\ndef handler(url):\n    if urlparse(url).scheme != 'https':\n        return\n    webbrowser.open(url)\n", k},
		{"print only", "c.py", pyImp + "def handler(url):\n    print(url)\n", k},
		{"constant url", "c.py", pyImp + "def handler(url):\n    webbrowser.open('https://example.com')\n", k},
		{"no mcp import", "c.py", "import webbrowser\ndef handler(url):\n    webbrowser.open(url)\n", k},
		{"two parameters", "c.py", pyImp + "def handler(url, state):\n    webbrowser.open(url)\n", k},
		{"comment only", "c.py", pyImp + "def handler(url):\n    # webbrowser.open(url)\n    pass\n", k},
		{"test path", "tests/test_c.py", pyImp + "def handler(url):\n    webbrowser.open(url)\n", k},
	})
	const tsImp = "import open from \"open\";\nimport { OAuthClientProvider } from \"@modelcontextprotocol/sdk/client/auth.js\";\n"
	runFire(t, true, []codeCase{
		{"open package", "p.ts", tsImp + "class P { async redirectToAuthorization(authorizationUrl: URL) { await open(authorizationUrl.toString()); } }\n", k},
		{"object property", "p.ts", tsImp + "const p = { redirectToAuthorization: async (u: URL) => { await open(u.href); } };\n", k},
		{"exec", "p.ts", "import { exec } from \"child_process\";\nimport { OAuthClientProvider } from \"@modelcontextprotocol/sdk/client/auth.js\";\nclass P { redirectToAuthorization(u: URL) { exec(`xdg-open ${u}`); } }\n", k},
	})
	runSilent(t, true, []codeCase{
		{"protocol checked", "p.ts", tsImp + "class P { async redirectToAuthorization(u: URL) { if (u.protocol !== \"https:\") throw new Error(\"x\"); await open(u.toString()); } }\n", k},
		{"origin checked", "p.ts", tsImp + "class P { async redirectToAuthorization(u: URL) { if (u.origin !== this.issuer) return; await open(u.toString()); } }\n", k},
		{"console only", "p.ts", tsImp + "class P { redirectToAuthorization(u: URL) { console.log(u.toString()); } }\n", k},
		{"other method name", "p.ts", tsImp + "class P { launch(u: URL) { open(u.toString()); } }\n", k},
		{"comment only", "p.ts", tsImp + "class P { redirectToAuthorization(u: URL) { /* open(u.toString()) */ } }\n", k},
		{"test path", "p.spec.ts", tsImp + "class P { async redirectToAuthorization(u: URL) { await open(u.toString()); } }\n", k},
	})
}

func TestMCPCode_SharedTransport(t *testing.T) {
	k := models.MCPCodeSharedTransport
	const imp = "import { McpServer } from \"@modelcontextprotocol/server\";\nimport { NodeStreamableHTTPServerTransport } from \"@modelcontextprotocol/node\";\n"
	runFire(t, true, []codeCase{
		{"server connected in handler", "s.ts", imp + "const server = new McpServer({ name: 'a', version: '1' });\napp.post('/mcp', async (req, res) => {\n  const t = new NodeStreamableHTTPServerTransport({});\n  await server.connect(t);\n});\n", k},
	})
	runSilent(t, true, []codeCase{
		{"server per request", "s.ts", imp + "app.post('/mcp', async (req, res) => {\n  const server = new McpServer({ name: 'a', version: '1' });\n  const t = new NodeStreamableHTTPServerTransport({});\n  await server.connect(t);\n});\n", k},
		{"connect at startup", "s.ts", imp + "const server = new McpServer({ name: 'a', version: '1' });\nawait server.connect(transport);\n", k},
		{"old sdk package", "s.ts", "import { McpServer } from \"@modelcontextprotocol/sdk/server/mcp.js\";\nconst server = new McpServer({ name: 'a', version: '1' });\napp.post('/mcp', async (req, res) => { await server.connect(t); });\n", k},
		{"comment only", "s.ts", imp + "const server = new McpServer({ name: 'a', version: '1' });\n// app.post('/mcp', async () => { await server.connect(t); });\n", k},
		{"test path", "s.test.ts", imp + "const server = new McpServer({ name: 'a', version: '1' });\napp.post('/mcp', async () => { await server.connect(t); });\n", k},
	})
}

func TestMCPCode_DNSRebindingOff(t *testing.T) {
	k := models.MCPCodeDNSRebindingOff
	const imp = "from mcp.server.transport_security import TransportSecuritySettings\n"
	runFire(t, false, []codeCase{
		{"disabled", "d.py", imp + "s = TransportSecuritySettings(enable_dns_rebinding_protection=False)\nm = FastMCP('x', transport_security=s)\n", k},
		{"disabled with lists", "d.py", imp + "s = TransportSecuritySettings(enable_dns_rebinding_protection=False, allowed_hosts=['a'])\n", k},
	})
	runSilent(t, false, []codeCase{
		{"enabled", "d.py", imp + "s = TransportSecuritySettings(enable_dns_rebinding_protection=True, allowed_hosts=['a'])\n", k},
		{"default", "d.py", imp + "s = TransportSecuritySettings(allowed_hosts=['a'])\n", k},
		{"auth named", "d.py", imp + "s = TransportSecuritySettings(enable_dns_rebinding_protection=False)\nm = FastMCP('x', token_verifier=V(), transport_security=s)\n", k},
		{"variable value", "d.py", imp + "s = TransportSecuritySettings(enable_dns_rebinding_protection=flag)\n", k},
		{"comment only", "d.py", imp + "# TransportSecuritySettings(enable_dns_rebinding_protection=False)\n", k},
		{"string only", "d.py", imp + "x = 'TransportSecuritySettings(enable_dns_rebinding_protection=False)'\n", k},
		{"test path", "tests/test_d.py", imp + "s = TransportSecuritySettings(enable_dns_rebinding_protection=False)\n", k},
	})
}

func TestMCPCode_PkgInstallSpec(t *testing.T) {
	k := models.MCPCodePkgInstallSpec
	const imp = "import { execFileSync } from \"node:child_process\";\nimport { McpServer } from \"@modelcontextprotocol/sdk/server/mcp.js\";\n"
	runFire(t, true, []codeCase{
		{"pnpm template variable", "i.ts", imp + "const spec = `functype@${args.version}`;\nexecFileSync(\"pnpm\", [\"add\", spec], {});\n", k},
		{"npm install identifier", "i.ts", imp + "execFileSync(\"npm\", [\"install\", pkg]);\n", k},
		{"inline template", "i.ts", imp + "execFileSync(\"pnpm\", [\"add\", `${name}@latest`]);\n", k},
		{"spawn", "i.ts", "import { spawn } from \"child_process\";\nimport { McpServer } from \"@modelcontextprotocol/sdk/server/mcp.js\";\nspawn(\"npm\", [\"i\", pkg]);\n", k},
	})
	runSilent(t, true, []codeCase{
		{"literal specifier", "i.ts", imp + "execFileSync(\"pnpm\", [\"add\", \"functype@1.4.4\"]);\n", k},
		{"validated", "i.ts", imp + "if (!/^\\d+\\.\\d+\\.\\d+$/.test(v)) throw new Error('x');\nexecFileSync(\"pnpm\", [\"add\", `functype@${v}`]);\n", k},
		{"semver check", "i.ts", imp + "import semver from \"semver\";\nif (!semver.valid(v)) throw new Error('x');\nexecFileSync(\"pnpm\", [\"add\", `functype@${v}`]);\n", k},
		{"other program", "i.ts", imp + "execFileSync(\"git\", [\"add\", file]);\n", k},
		{"no install verb", "i.ts", imp + "execFileSync(\"npm\", [\"run\", script]);\n", k},
		{"no mcp import", "i.ts", "import { execFileSync } from \"node:child_process\";\nexecFileSync(\"npm\", [\"install\", pkg]);\n", k},
		{"comment only", "i.ts", imp + "// execFileSync(\"npm\", [\"install\", pkg]);\n", k},
		{"string only", "i.ts", imp + "const s = 'execFileSync(\"npm\", [\"install\", pkg])';\n", k},
		{"test path", "i.test.ts", imp + "execFileSync(\"npm\", [\"install\", pkg]);\n", k},
		{"python is not read", "i.py", "import subprocess\nsubprocess.run(['pip', 'install', pkg])\n", k},
	})
}
