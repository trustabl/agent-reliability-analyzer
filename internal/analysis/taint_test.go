package analysis

import (
	"context"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/trustabl/trustabl/internal/analysis/astutil"
	"github.com/trustabl/trustabl/internal/models"
)

func pyTaint(t *testing.T, body string) []models.TaintFlow {
	t.Helper()
	src := "from mcp.server.fastmcp import FastMCP\nmcp = FastMCP('x')\n\n" + body
	tree, err := astutil.ParseCtxTimeout(context.Background(), astutil.NewPyParser(), []byte(src))
	if err != nil {
		t.Fatal(err)
	}
	pf := ParsedFile{RelPath: "a.py", Source: []byte(src), Tree: tree}
	tools := DiscoverToolsFromParsed([]ParsedFile{pf})
	if len(tools) == 0 {
		t.Fatalf("no tool discovered in:\n%s", body)
	}
	AttachTaintFlows(tools, []ParsedFile{pf})
	var out []models.TaintFlow
	for _, tl := range tools {
		if tl.Kind == models.KindMCPTool {
			out = append(out, tl.TaintFlows...)
		}
	}
	return out
}

func tsTaint(t *testing.T, body string) []models.TaintFlow {
	t.Helper()
	src := "import { McpServer } from '@modelcontextprotocol/sdk/server/mcp.js';\nimport { z } from 'zod';\nconst server = new McpServer({name:'x',version:'1'});\n" + body
	tree, err := astutil.ParseCtxTimeout(context.Background(), astutil.NewTSXParser(), []byte(src))
	if err != nil {
		t.Fatal(err)
	}
	pf := ParsedFile{RelPath: "a.ts", Source: []byte(src), Tree: tree}
	tools := DiscoverTSMCPProper([]ParsedFile{pf}, nil)
	if len(tools) == 0 {
		t.Fatalf("no tool discovered in:\n%s", body)
	}
	AttachTaintFlows(tools, []ParsedFile{pf})
	var out []models.TaintFlow
	for _, tl := range tools {
		out = append(out, tl.TaintFlows...)
	}
	return out
}

func classes(fs []models.TaintFlow) string {
	var parts []string
	for _, f := range fs {
		parts = append(parts, fmt.Sprintf("%s>%s", f.Param, f.SinkClass))
	}
	return strings.Join(parts, ",")
}

const pyHead = "@mcp.tool()\n"

func TestTaintPythonFound(t *testing.T) {
	cases := []struct{ name, body, want string }{
		{"direct shell", "def run(cmd: str):\n    subprocess.run(cmd, shell=True)\n", "cmd>shell"},
		{"alias import", "import subprocess as sp\n@mcp.tool()\ndef run(cmd: str):\n    sp.run(cmd, shell=True)\n", "cmd>shell"},
		{"from import", "from subprocess import check_output\n@mcp.tool()\ndef run(cmd: str):\n    check_output(cmd)\n", "cmd>shell"},
		{"os.system fstring", "def run(name: str):\n    os.system(f'ls {name}')\n", "name>shell"},
		{"sql one hop", "def q(name: str, cur):\n    q = f\"select * from t where n='{name}'\"\n    cur.execute(q)\n", "name>sql_execute"},
		{"path concat", "def r(p: str):\n    path = '/base/' + p\n    return open(path).read()\n", "p>path_io"},
		{"open str()", "def r(p: str):\n    open(str(p))\n", "p>path_io"},
		{"os.path.join", "def r(p: str):\n    open(os.path.join('/b', p))\n", "p>path_io"},
		{"Path wrap", "def r(p: str):\n    Path(p).read_text()\n", "p>path_io"},
		{"format", "def r(p: str):\n    os.system('ls {}'.format(p))\n", "p>shell"},
		{"percent", "def r(p: str):\n    os.system('ls %s' % p)\n", "p>shell"},
		{"tuple unpack", "def r(p: str):\n    a, b = p, 'x'\n    os.system(a)\n", "p>shell"},
		{"self strip", "def r(p: str):\n    p = p.strip()\n    os.system(p)\n", "p>shell"},
		{"vector dict", "def s(q: str, tenant: str, store):\n    return store.similarity_search(q, filter={'tenant': tenant})\n", "tenant>vector_filter"},
		{"vector fstring", "def s(q: str, tenant: str, vs):\n    return vs.similarity_search(q, filter=f\"tenant == '{tenant}'\")\n", "tenant>vector_filter"},
		{"store ns", "def s(user: str, p: str, store):\n    store.put((user, p), 'k', {})\n", "p>store_namespace,user>store_namespace"},
		{"async method", "async def r(self, p: str):\n    subprocess.run(['git', 'log', p])\n", "p>shell"},
		{"cwd kwarg", "def r(p: str):\n    subprocess.run(['ls'], cwd=p)\n", "p>shell"},
		{"eval", "def r(expr: str):\n    return eval(expr)\n", "expr>code_exec"},
		{"exec fstring", "def r(x: str):\n    exec(f'v = {x}')\n", "x>code_exec"},
		{"git", "def r(p: str, repo):\n    repo.git.log(p)\n", "p>path_io"},
		{"write mode", "def r(p: str, c: str):\n    open(p, 'w').write(c)\n", "p>file_write"},
		{"memory composite", "m = Memory()\n@mcp.tool()\ndef r(org: str, uid: str):\n    m.add('x', user_id=f'{org}:{uid}')\n", "org>memory_scope_key,uid>memory_scope_key"},
		{"loop late taint", "def r(p: str):\n    c = ''\n    for i in range(2):\n        if i:\n            os.system(c)\n        c = p\n", "p>shell"},
		{"if branch union", "def r(p: str, f: bool):\n    if f:\n        c = p\n    else:\n        c = 'x'\n    os.system(c)\n", "p>shell"},
		{"try", "def r(p: str):\n    try:\n        os.system(p)\n    except Exception:\n        pass\n", "p>shell"},
		{"comprehension sink", "def r(ps: list):\n    [os.system(x) for x in ps]\n", "ps>shell"},
	}
	for _, c := range cases {
		got := classes(pyTaint(t, pyHead+c.body))
		if strings.HasPrefix(c.body, "import") || strings.HasPrefix(c.body, "from") || strings.HasPrefix(c.body, "m = ") {
			got = classes(pyTaint(t, c.body))
		}
		if got != c.want {
			t.Errorf("%s: got %q want %q", c.name, got, c.want)
		}
	}
}

func TestTaintPythonNotFound(t *testing.T) {
	cases := []struct{ name, body string }{
		{"comment", "def r(p: str):\n    # os.system(p)\n    return 1\n"},
		{"docstring", "def r(p: str):\n    \"\"\"os.system(p)\"\"\"\n    return 1\n"},
		{"string literal", "def r(p: str):\n    os.system('p')\n"},
		{"fstring no braces", "def r(p: str):\n    os.system(f'ls p')\n"},
		{"local var", "def r(p: str):\n    path = 'x'\n    open(path)\n"},
		{"loop var shadow", "def r(path: str):\n    items = [1]\n    for path in items:\n        open(path)\n"},
		{"comp var", "def r(path: str):\n    items = [1]\n    return [open(path) for path in items]\n"},
		{"lambda param", "def r(path: str):\n    f = lambda path: open(path)\n    return f\n"},
		{"nested def", "def r(path: str):\n    def inner(path):\n        return open(path)\n    return inner\n"},
		{"attribute", "def r(p: str):\n    obj = Foo()\n    open(obj.p)\n"},
		{"keyword name", "def r(path: str):\n    subprocess.run(['ls'], path='x')\n"},
		{"dict key", "def r(p: str):\n    d = {p: 1}\n    return d\n"},
		{"overwrite literal", "def r(p: str):\n    p = 'ls'\n    os.system(p)\n"},
		{"int cast", "def r(p: str):\n    n = int(p)\n    os.system(f'sleep {n}')\n"},
		{"int annotation", "def r(n: int):\n    os.system(f'sleep {n}')\n"},
		{"literal annotation", "def r(n: Literal['a', 'b']):\n    os.system(f'echo {n}')\n"},
		{"default int", "def r(n=5):\n    os.system(f'echo {n}')\n"},
		{"resolve", "def r(p: str):\n    q = Path(p).resolve()\n    open(q)\n"},
		{"resolve then use", "def r(p: str):\n    Path(p).resolve()\n    open(p)\n"},
		{"shlex quote", "def r(p: str):\n    os.system('ls ' + shlex.quote(p))\n"},
		{"allowlist guard", "def r(p: str):\n    if p not in ALLOWED:\n        raise ValueError(p)\n    os.system(p)\n"},
		{"regex guard", "def r(p: str):\n    if not re.fullmatch(r'[a-z]+', p):\n        raise ValueError()\n    os.system(p)\n"},
		{"positive membership branch", "def r(p: str):\n    if p in ALLOWED:\n        os.system(p)\n"},
		{"print only", "def r(p: str):\n    print(p)\n    logging.info(p)\n    return p\n"},
		{"sql params arg", "def r(name: str, cur):\n    cur.execute('select 1 where n=?', (name,))\n"},
		{"literal args", "def r(p: str):\n    os.system('ls')\n"},
		{"helper same file", "def helper(x):\n    os.system(x)\n@mcp.tool()\ndef r(p: str):\n    helper(p)\n"},
		{"sink in other fn", "def other(p):\n    os.system(p)\n@mcp.tool()\ndef r(p: str):\n    return 1\n"},
		{"ctx param", "def r(ctx: Context):\n    os.system(ctx.x)\n"},
		{"memory plain param", "m = Memory()\n@mcp.tool()\ndef r(uid: str):\n    m.add('x', user_id=uid)\n"},
		{"dict element read", "def r(p: str):\n    d = {'a': p}\n    os.system(d['a'])\n"},
	}
	for _, c := range cases {
		body := pyHead + c.body
		if strings.HasPrefix(c.body, "m = ") || strings.HasPrefix(c.body, "def helper") || strings.HasPrefix(c.body, "def other") {
			body = c.body
		}
		if got := classes(pyTaint(t, body)); got != "" {
			t.Errorf("%s: unexpected flows %q", c.name, got)
		}
	}
}

func TestTaintTypeScriptFound(t *testing.T) {
	cases := []struct{ name, body, want string }{
		{"template exec", "server.tool('a', {cmd: z.string()}, async ({cmd}) => { exec(`ls ${cmd}`); return {}; });", "cmd>shell"},
		{"concat execSync", "server.tool('a', {cmd: z.string()}, async (args) => { execSync('ls ' + args.cmd); return {}; });", "cmd>shell"},
		{"writeFile zod", "server.tool('a', {path: z.string()}, async ({path}) => { await fs.promises.writeFile(path, 'x'); return {}; });", "path>file_write"},
		{"destructured readFile", "server.tool('a', {path: z.string()}, async ({ path }) => { return fs.readFile(path, 'utf8'); });", "path>path_io"},
		{"arrow in map", "server.tool('a', {paths: z.array(z.string())}, async ({ paths }) => { paths.map(p => fs.readFileSync(p)); return {}; });", "paths>path_io"},
		{"registerTool", "server.registerTool('a', {inputSchema: {cmd: z.string()}}, async ({cmd}) => { child_process.exec(cmd); return {}; });", "cmd>shell"},
		{"eval", "server.tool('a', {e: z.string()}, async ({e}) => { return eval(e); });", "e>code_exec"},
		{"new Function", "server.tool('a', {e: z.string()}, async ({e}) => { return new Function(e)(); });", "e>code_exec"},
		{"one hop", "server.tool('a', {n: z.string()}, async ({n}) => { const c = `rm ${n}`; exec(c); return {}; });", "n>shell"},
	}
	for _, c := range cases {
		if got := classes(tsTaint(t, c.body)); got != c.want {
			t.Errorf("%s: got %q want %q", c.name, got, c.want)
		}
	}
}

func TestTaintTypeScriptNotFound(t *testing.T) {
	cases := []struct{ name, body string }{
		{"comment", "server.tool('a', {cmd: z.string()}, async ({cmd}) => { // exec(cmd)\n return {}; });"},
		{"template no subst", "server.tool('a', {cmd: z.string()}, async ({cmd}) => { exec(`ls`); return {}; });"},
		{"zod number", "server.tool('a', {n: z.number()}, async ({n}) => { exec(`sleep ${n}`); return {}; });"},
		{"number cast", "server.tool('a', {n: z.string()}, async ({n}) => { const k = Number(n); exec(`sleep ${k}`); return {}; });"},
		{"shadow arrow param", "server.tool('a', {path: z.string()}, async ({path}) => { [1].map(path => fs.readFileSync(path)); return {}; });"},
		{"overwrite", "server.tool('a', {cmd: z.string()}, async ({cmd}) => { let c = cmd; c = 'ls'; exec(c); return {}; });"},
		{"guard throw", "server.tool('a', {cmd: z.string()}, async ({cmd}) => { if (!ALLOWED.includes(cmd)) { throw new Error('no'); } exec(cmd); return {}; });"},
		{"path.resolve", "server.tool('a', {p: z.string()}, async ({p}) => { const q = path.resolve(p); fs.readFileSync(q); return {}; });"},
		{"zod parse", "server.tool('a', {cmd: z.string()}, async ({cmd}) => { const v = S.parse(cmd); exec(v); return {}; });"},
		{"log only", "server.tool('a', {cmd: z.string()}, async ({cmd}) => { console.log(cmd); return {content: cmd}; });"},
		{"literal args", "server.tool('a', {cmd: z.string()}, async ({cmd}) => { exec('ls'); return {}; });"},
		{"guard on args.key", "server.tool('a', {cmd: z.string()}, async (args) => { if (!SAFE.test(args.cmd)) throw new Error('bad'); exec(args.cmd); return {}; });"},
		{"undeclared", "server.tool('a', {cmd: z.string()}, async ({other}) => { exec(other); return {}; });"},
	}
	for _, c := range cases {
		if got := classes(tsTaint(t, c.body)); got != "" {
			t.Errorf("%s: unexpected flows %q", c.name, got)
		}
	}
}

func TestTaintDeterministicAndFast(t *testing.T) {
	body := pyHead + "def r(p: str, q: str):\n    a = p + q\n    os.system(a)\n    open(q)\n"
	first := pyTaint(t, body)
	for i := 0; i < 20; i++ {
		if got := pyTaint(t, body); !reflect.DeepEqual(got, first) {
			t.Fatalf("run %d differs: %v vs %v", i, got, first)
		}
	}
	var b strings.Builder
	b.WriteString(pyHead + "def big(p: str):\n    x = p\n")
	for i := 0; i < 5000; i++ {
		fmt.Fprintf(&b, "    y%d = x + 'a'\n", i)
	}
	b.WriteString("    os.system(y4999)\n")
	src := "from mcp.server.fastmcp import FastMCP\nmcp = FastMCP('x')\n\n" + b.String()
	tree, _ := astutil.ParseCtxTimeout(context.Background(), astutil.NewPyParser(), []byte(src))
	pf := ParsedFile{RelPath: "a.py", Source: []byte(src), Tree: tree}
	tools := DiscoverToolsFromParsed([]ParsedFile{pf})
	fc := newTaintFileCtx(pf)
	var big *models.ToolDef
	for i := range tools {
		if tools[i].Kind == models.KindMCPTool {
			big = &tools[i]
		}
	}
	best := time.Hour
	for i := 0; i < 5; i++ {
		start := time.Now()
		big.TaintFlows = fc.pyToolFlows(*big)
		if d := time.Since(start); d < best {
			best = d
		}
	}
	if !raceEnabled && best > 50*time.Millisecond {
		t.Errorf("5000-line function took %v (best of 5)", best)
	}
	if classes(big.TaintFlows) != "p>shell" {
		t.Errorf("big function flow: %q", classes(big.TaintFlows))
	}
}

func TestTaintLoopNestingCap(t *testing.T) {
	var b strings.Builder
	b.WriteString(pyHead + "def r(p: str):\n    c = p\n")
	ind := "    "
	for i := 0; i < 30; i++ {
		fmt.Fprintf(&b, "%sfor i%d in range(2):\n", ind, i)
		ind += "    "
	}
	fmt.Fprintf(&b, "%sos.system(c)\n", ind)
	start := time.Now()
	fl := pyTaint(t, b.String())
	if time.Since(start) > time.Second {
		t.Fatal("deeply nested loops are too slow")
	}
	if classes(fl) != "p>shell" {
		t.Errorf("got %q", classes(fl))
	}
}
