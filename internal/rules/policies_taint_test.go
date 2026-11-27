package rules_test

import (
	"strings"
	"testing"
	"testing/fstest"

	"github.com/trustabl/trustabl/internal/models"
	"github.com/trustabl/trustabl/internal/rules"
)

// Fire and silent cases for static batch 6: MCP-215/216/217/219/220, LC-204, LC-205.
// Appended in init so TestPolicyRules_AllRulesCovered sees them.

const tsMCPHead = "import { McpServer } from '@modelcontextprotocol/sdk/server/mcp.js';\nimport { z } from 'zod';\nconst server = new McpServer({ name: 'x', version: '1' });\n"

func init() {
	ts := models.LanguageTypeScript
	policyRuleCases = append(policyRuleCases,
		// MCP-215 (Python: subprocess, git, file sinks)
		policyRuleCase{name: "MCP-215 fires on a parameter in a subprocess call", ruleID: "MCP-215", kind: models.KindMCPTool, src: `
def run_git(ref: str) -> str:
    """Show a ref."""
    return subprocess.check_output(["git", "show", ref]).decode()
`, wantFires: true},
		policyRuleCase{name: "MCP-215 fires on a parameter built into a path", ruleID: "MCP-215", kind: models.KindMCPTool, src: `
def read_doc(name: str) -> str:
    """Read a doc."""
    path = "/srv/docs/" + name
    return open(path).read()
`, wantFires: true},
		policyRuleCase{name: "MCP-215 fires on a GitPython call", ruleID: "MCP-215", kind: models.KindMCPTool, src: `
def log(repo, ref: str) -> str:
    """Log."""
    return repo.git.log(ref)
`, wantFires: true},
		policyRuleCase{name: "MCP-215 silent when the path is resolved", ruleID: "MCP-215", kind: models.KindMCPTool, src: `
def read_doc(name: str) -> str:
    """Read a doc."""
    p = Path("/srv/docs/" + name).resolve()
    return open(p).read()
`, wantFires: false},
		policyRuleCase{name: "MCP-215 silent on a literal command", ruleID: "MCP-215", kind: models.KindMCPTool, src: `
def status(ref: str) -> str:
    """Status."""
    print(ref)
    return subprocess.check_output(["git", "status"]).decode()
`, wantFires: false},
		policyRuleCase{name: "MCP-215 silent behind an allowlist guard", ruleID: "MCP-215", kind: models.KindMCPTool, src: `
def run_git(ref: str) -> str:
    """Show a ref."""
    if ref not in ALLOWED_REFS:
        raise ValueError(ref)
    return subprocess.check_output(["git", "show", ref]).decode()
`, wantFires: false},
		policyRuleCase{name: "MCP-215 silent when the name only appears in a comment", ruleID: "MCP-215", kind: models.KindMCPTool, src: `
def run_git(ref: str) -> str:
    """Show a ref."""
    # subprocess.run(ref)
    return "ok"
`, wantFires: false},
		policyRuleCase{name: "MCP-215 silent on a helper call", ruleID: "MCP-215", kind: models.KindMCPTool, src: `
def run_git(ref: str) -> str:
    """Show a ref."""
    return helper(ref)
`, wantFires: false},

		// MCP-216
		policyRuleCase{name: "MCP-216 fires on a parameter in eval", ruleID: "MCP-216", kind: models.KindMCPTool, src: `
def calc(expr: str) -> str:
    """Calculate."""
    return str(eval(expr))
`, wantFires: true},
		policyRuleCase{name: "MCP-216 fires on exec of a built string", ruleID: "MCP-216", kind: models.KindMCPTool, src: `
def run(code: str) -> str:
    """Run."""
    src = "x = 1\n" + code
    exec(src)
    return "ok"
`, wantFires: true},
		policyRuleCase{name: "MCP-216 silent on literal_eval", ruleID: "MCP-216", kind: models.KindMCPTool, src: `
def parse(expr: str):
    """Parse."""
    return ast.literal_eval(expr)
`, wantFires: false},
		policyRuleCase{name: "MCP-216 silent when the parameter is cast to int", ruleID: "MCP-216", kind: models.KindMCPTool, src: `
def calc(n: str) -> str:
    """Calculate."""
    k = int(n)
    return str(eval(f"{k} + 1"))
`, wantFires: false},
		policyRuleCase{name: "MCP-216 silent on a literal eval", ruleID: "MCP-216", kind: models.KindMCPTool, src: `
def calc(expr: str) -> str:
    """Calculate."""
    return str(eval("1 + 1"))
`, wantFires: false},

		// MCP-217
		policyRuleCase{name: "MCP-217 fires on a joined scope key", ruleID: "MCP-217", kind: models.KindMCPTool, src: `
memory = Memory()
def remember(org: str, uid: str, text: str) -> str:
    """Remember."""
    memory.add(text, user_id=f"{org}:{uid}")
    return "ok"
`, wantFires: true},
		policyRuleCase{name: "MCP-217 silent on a parameter passed alone", ruleID: "MCP-217", kind: models.KindMCPTool, src: `
memory = Memory()
def remember(uid: str, text: str) -> str:
    """Remember."""
    memory.add(text, user_id=uid)
    return "ok"
`, wantFires: false},
		policyRuleCase{name: "MCP-217 silent on a joined key from a constant", ruleID: "MCP-217", kind: models.KindMCPTool, src: `
memory = Memory()
def remember(text: str) -> str:
    """Remember."""
    memory.add(text, user_id=f"{TENANT}:{USER}")
    return "ok"
`, wantFires: false},

		// MCP-219 (TypeScript)
		policyRuleCase{name: "MCP-219 fires on a parameter in execSync", ruleID: "MCP-219", kind: models.KindMCPTool, lang: ts, src: tsMCPHead + `
server.tool('git_show', { ref: z.string() }, async ({ ref }) => {
  const out = execSync('git show ' + ref).toString();
  return { content: [{ type: 'text', text: out }] };
});`, wantFires: true},
		policyRuleCase{name: "MCP-219 fires on a parameter in readFile", ruleID: "MCP-219", kind: models.KindMCPTool, lang: ts, src: tsMCPHead + `
server.registerTool('read_doc', { inputSchema: { name: z.string() } }, async ({ name }) => {
  const p = ` + "`/srv/docs/${name}`" + `;
  const text = await fs.promises.readFile(p, 'utf8');
  return { content: [{ type: 'text', text }] };
});`, wantFires: true},
		policyRuleCase{name: "MCP-219 silent when the path is resolved", ruleID: "MCP-219", kind: models.KindMCPTool, lang: ts, src: tsMCPHead + `
server.tool('read_doc', { name: z.string() }, async ({ name }) => {
  const p = path.resolve('/srv/docs', name);
  const text = fs.readFileSync(p, 'utf8');
  return { content: [{ type: 'text', text }] };
});`, wantFires: false},
		policyRuleCase{name: "MCP-219 silent on a numeric parameter", ruleID: "MCP-219", kind: models.KindMCPTool, lang: ts, src: tsMCPHead + `
server.tool('wait', { n: z.number() }, async ({ n }) => {
  execSync('sleep ' + n);
  return { content: [] };
});`, wantFires: false},
		policyRuleCase{name: "MCP-219 silent on a literal command", ruleID: "MCP-219", kind: models.KindMCPTool, lang: ts, src: tsMCPHead + `
server.tool('status', { ref: z.string() }, async ({ ref }) => {
  // execSync(ref)
  const out = execSync('git status').toString();
  return { content: [{ type: 'text', text: out }] };
});`, wantFires: false},

		// MCP-220 (TypeScript)
		policyRuleCase{name: "MCP-220 fires on a parameter in eval", ruleID: "MCP-220", kind: models.KindMCPTool, lang: ts, src: tsMCPHead + `
server.tool('calc', { expr: z.string() }, async ({ expr }) => {
  return { content: [{ type: 'text', text: String(eval(expr)) }] };
});`, wantFires: true},
		policyRuleCase{name: "MCP-220 fires on new Function", ruleID: "MCP-220", kind: models.KindMCPTool, lang: ts, src: tsMCPHead + `
server.tool('calc', { expr: z.string() }, async ({ expr }) => {
  const f = new Function('return ' + expr);
  return { content: [{ type: 'text', text: String(f()) }] };
});`, wantFires: true},
		policyRuleCase{name: "MCP-220 silent on JSON.parse", ruleID: "MCP-220", kind: models.KindMCPTool, lang: ts, src: tsMCPHead + `
server.tool('parse', { text: z.string() }, async ({ text }) => {
  return { content: [{ type: 'text', text: String(JSON.parse(text)) }] };
});`, wantFires: false},
		policyRuleCase{name: "MCP-220 silent on a literal eval", ruleID: "MCP-220", kind: models.KindMCPTool, lang: ts, src: tsMCPHead + `
server.tool('calc', { expr: z.string() }, async ({ expr }) => {
  return { content: [{ type: 'text', text: String(eval('1 + 1')) }] };
});`, wantFires: false},

		// LC-204
		policyRuleCase{name: "LC-204 fires on a parameter in a filter dict", ruleID: "LC-204", kind: models.KindLangChainTool, src: `
def search_docs(query: str, tenant: str) -> str:
    """Search."""
    docs = vectorstore.similarity_search(query, filter={"tenant": tenant})
    return str(docs)
`, wantFires: true},
		policyRuleCase{name: "LC-204 fires on a parameter built into a filter string", ruleID: "LC-204", kind: models.KindLangChainTool, src: `
def search_docs(query: str, tenant: str) -> str:
    """Search."""
    expr = f"tenant == '{tenant}'"
    return str(vectorstore.similarity_search(query, filter=expr))
`, wantFires: true},
		policyRuleCase{name: "LC-204 silent when the filter value is a constant", ruleID: "LC-204", kind: models.KindLangChainTool, src: `
def search_docs(query: str) -> str:
    """Search."""
    return str(vectorstore.similarity_search(query, filter={"tenant": "acme"}))
`, wantFires: false},
		policyRuleCase{name: "LC-204 silent when the parameter is only the query", ruleID: "LC-204", kind: models.KindLangChainTool, src: `
def search_docs(query: str, tenant: str) -> str:
    """Search."""
    return str(vectorstore.similarity_search(query, filter={"tenant": TENANT}))
`, wantFires: false},

		// LC-205
		policyRuleCase{name: "LC-205 fires on a parameter in a store namespace", ruleID: "LC-205", kind: models.KindLangChainTool, src: `
def recall(user: str, key: str) -> str:
    """Recall."""
    item = store.get(("memories", user), key)
    return str(item)
`, wantFires: true},
		policyRuleCase{name: "LC-205 silent when the namespace is constant", ruleID: "LC-205", kind: models.KindLangChainTool, src: `
def recall(key: str) -> str:
    """Recall."""
    item = store.get(("memories", "shared"), key)
    return str(item)
`, wantFires: false},
		policyRuleCase{name: "LC-205 silent on a receiver that is not a store", ruleID: "LC-205", kind: models.KindLangChainTool, src: `
def recall(user: str, key: str) -> str:
    """Recall."""
    return str(cache.get(("memories", user), key))
`, wantFires: false},
	)
}

func taintRuleFS(sources, sinks string) fstest.MapFS {
	return fstest.MapFS{"mcp/x.yaml": &fstest.MapFile{Data: []byte(`policy:
  id: p
  name: P
  category: mcp
  description: d
rules:
  - id: X-001
    title: t
    scope: tool
    severity: low
    confidence: 0.5
    applies_to: [mcp_tool]
    match:
      tool_source_reaches_sink:
        sources: ` + sources + `
        sinks: ` + sinks + `
    explanation: e
    fix: f
`)}}
}

func TestToolSourceReachesSink_LoaderChecksValues(t *testing.T) {
	if _, err := rules.Load(taintRuleFS("[param]", "[shell, code_exec]")); err != nil {
		t.Fatalf("valid rule rejected: %v", err)
	}
	if _, err := rules.Load(taintRuleFS("[secret_env]", "[shell]")); err == nil || !strings.Contains(err.Error(), "source") {
		t.Errorf("unknown source accepted: %v", err)
	}
	if _, err := rules.Load(taintRuleFS("[param]", "[shel]")); err == nil || !strings.Contains(err.Error(), "sink") {
		t.Errorf("unknown sink accepted: %v", err)
	}
	if _, err := rules.Load(taintRuleFS("[]", "[shell]")); err == nil {
		t.Errorf("empty sources accepted")
	}
}

func TestToolSourceReachesSink_FindingOnSinkLine(t *testing.T) {
	d := loadToolRule(t, "MCP-216")
	tool, pf := parsePy(t, "\ndef calc(expr: str) -> str:\n    \"\"\"Calculate.\"\"\"\n    v = expr\n    return str(eval(v))\n", models.KindMCPTool)
	fs := d.Detect(tool, pf, models.RepoInventory{})
	if len(fs) != 1 {
		t.Fatalf("want 1 finding, got %d", len(fs))
	}
	if fs[0].StartLine != 5 || fs[0].EndLine != 5 {
		t.Errorf("finding on lines %d-%d, want the sink line 5", fs[0].StartLine, fs[0].EndLine)
	}
	if !strings.Contains(fs[0].Explanation, "parameter `expr`") {
		t.Errorf("explanation does not name the parameter: %q", fs[0].Explanation)
	}
}
