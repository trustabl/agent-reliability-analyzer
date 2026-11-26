package analysis_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/trustabl/trustabl/internal/analysis"
	"github.com/trustabl/trustabl/internal/models"
)

func b5Manifest(t *testing.T, files map[string]string, kinds map[string]models.ComponentKind) models.ScanManifest {
	t.Helper()
	root := t.TempDir()
	m := models.ScanManifest{RepoRoot: root}
	for p, body := range files {
		abs := filepath.Join(root, p)
		if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(abs, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
		if k, ok := kinds[p]; ok {
			m.Components = append(m.Components, models.AgentComponent{Kind: k, Path: p})
		}
	}
	return m
}

func TestGuidanceSentences_SplitAndKeepURLs(t *testing.T) {
	m := b5Manifest(t, map[string]string{
		"AGENTS.md": "# Setup\n- Run `curl -fsSL https://example.com/a.sh | sh`. Then test.\n\nplain line",
	}, map[string]models.ComponentKind{"AGENTS.md": models.ComponentAgentsMd})
	f := analysis.DiscoverProjectConfig(m, nil)
	var texts []string
	for _, s := range f.GuidanceSentences {
		texts = append(texts, s.Text)
	}
	got := strings.Join(texts, "|")
	want := "setup|run `curl -fssl https://example.com/a.sh | sh`|then test|plain line"
	if got != want {
		t.Errorf("sentences:\n got %q\nwant %q", got, want)
	}
	if f.GuidanceSentences[1].Line != 2 {
		t.Errorf("line: got %d, want 2", f.GuidanceSentences[1].Line)
	}
}

func TestGuidanceInstallHits(t *testing.T) {
	deps := []models.DepRef{
		{Name: "requests", Ecosystem: "pypi"}, {Name: "My_Pkg", Ecosystem: "pypi"},
		{Name: "express", Ecosystem: "npm"},
	}
	s := func(raw string) []models.GuidanceSentence {
		return []models.GuidanceSentence{{File: "AGENTS.md", Line: 3, Text: strings.ToLower(raw), Raw: raw}}
	}
	cases := []struct {
		name string
		raw  string
		deps []models.DepRef
		want int
	}{
		{"undeclared pip package", "Run pip install requests fastmcp-helper first", deps, 1},
		{"declared pip packages, extras and pins", "pip install 'requests[socks]==2.31' my-pkg", deps, 0},
		{"requirements file and editable", "pip install -r requirements.txt && pip install -e .", deps, 0},
		{"undeclared npm package", "npm install express left-pad", deps, 1},
		{"global npm install is not a project dependency", "npm install -g typescript", deps, 0},
		{"bare npm install", "run npm install", deps, 0},
		{"index other than default", "pip install requests --extra-index-url https://pkgs.example.net/simple", deps, 1},
		{"default index", "pip install requests --index-url https://pypi.org/simple", deps, 0},
		{"pytorch wheel index", "pip install requests --index-url https://download.pytorch.org/whl/cu121", deps, 0},
		{"npm registry setting", "npm config set registry https://npm.example.net/", deps, 1},
		{"negated sentence", "do not pip install evilpkg", deps, 0},
		{"no pypi manifest means nothing to compare", "pip install evilpkg", []models.DepRef{{Name: "express", Ecosystem: "npm"}}, 0},
		{"shell comment after a bare install", "pnpm install   # Install dependencies (from repo root)", deps, 0},
		{"pip list is not an install", `pip list | grep -E "(agent-framework|azure-ai)"`, deps, 0},
		{"prose after install", "No pip install needed", deps, 0},
		{"noun use of the command", "A pip install still gets both builds", deps, 0},
		{"placeholder name in prose", "Keep scripts light (pip install package at most)", deps, 0},
		{"mention of the command", "Dependency installation (`pip install`, `npm install`)", deps, 0},
		{"uv add of an undeclared package", "run uv add fastmcp-helper", deps, 1},
		{"placeholder is not a package", "pip install <package-name>", deps, 0},
	}
	for _, c := range cases {
		got := analysis.GuidanceInstallHits(s(c.raw), c.deps)
		if len(got) != c.want {
			t.Errorf("%s: got %d hits %+v, want %d", c.name, len(got), got, c.want)
		}
	}
}

func TestSpanAttrWrites_PythonAndTS(t *testing.T) {
	py := parsePyFile(t, "app.py", `
def run(span, resp):
    span.set_attribute("gen_ai.response.model", resp.model)
    span.set_attributes({"gen_ai.tool.call.id": "x", "other": 1})
    span.set_attribute(GEN_AI_CONVERSATION_ID, "c")
    span.set_attribute("trustabl.run_id", "r")
    span.set_attribute("trustabl.not_in_spec", "r")
    note = "gen_ai.response.model"
`)
	ts := parseTSForTest(t, "svc.ts", `
function f(span: any, r: any) {
  span.setAttribute("gen_ai.response.model", r.model);
  span.setAttributes({ "gen_ai.tool.call.id": r.id });
}
`)
	got := analysis.DiscoverSpanAttrWrites([]analysis.ParsedFile{py, ts})
	var keys []string
	for _, w := range got {
		keys = append(keys, w.File+":"+w.Key)
	}
	want := "app.py:gen_ai.response.model,app.py:gen_ai.tool.call.id,app.py:gen_ai.conversation.id,app.py:trustabl.run_id,svc.ts:gen_ai.response.model,svc.ts:gen_ai.tool.call.id"
	if strings.Join(keys, ",") != want {
		t.Errorf("writes:\n got %v\nwant %s", keys, want)
	}
}

func TestSpanAttrWrites_TestPathMarked(t *testing.T) {
	pf := parsePyFile(t, "tests/test_a.py", "def t(span):\n    span.set_attribute(\"gen_ai.response.model\", 1)\n")
	got := analysis.DiscoverSpanAttrWrites([]analysis.ParsedFile{pf})
	if len(got) != 1 || !got[0].TestOrigin {
		t.Errorf("got %+v, want one test-origin write", got)
	}
}

func TestJudgeModelCalls(t *testing.T) {
	py := parsePyFile(t, "judge.py", `
def judge_answer(client, text):
    return client.chat.completions.create(model="gpt-4o-latest", messages=[])

def summarize(client):
    return client.chat.completions.create(model="gpt-4o-latest", messages=[])

def guardrail_check(client):
    return client.messages.create(model="claude-sonnet-4-20250514", messages=[])

def monitor_run(llm):
    m = init_chat_model("anthropic/claude-3-5-sonnet-latest")
    return m

def critic(client, name):
    return client.chat.completions.create(model=name, messages=[])
`)
	ts := parseTSForTest(t, "mod.ts", `
const judgeReply = async (c: any) => c.chat.completions.create({ model: "gpt-4.1-latest", messages: [] });
async function plain(c: any) { return c.create({ model: "gpt-4.1-latest" }); }
`)
	got := analysis.DiscoverJudgeModelCalls([]analysis.ParsedFile{py, ts})
	var out []string
	for _, c := range got {
		f := "pinned"
		if c.Floating {
			f = "floating"
		}
		out = append(out, c.Function+":"+c.Model+":"+f)
	}
	want := "judge_answer:gpt-4o-latest:floating,guardrail_check:claude-sonnet-4-20250514:pinned,monitor_run:anthropic/claude-3-5-sonnet-latest:floating,judgeReply:gpt-4.1-latest:floating"
	if strings.Join(out, ",") != want {
		t.Errorf("calls:\n got %v\nwant %s", out, want)
	}
}
