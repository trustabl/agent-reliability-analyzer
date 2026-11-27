package scanner_test

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/trustabl/trustabl/internal/models"
)

func batch8Files(t *testing.T) map[string]string {
	t.Helper()
	files := map[string]string{}
	root := "../../testdata/corpus/static-batch8-fixture"
	err := filepath.Walk(root, func(p string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() || filepath.Base(p) == "SOURCE.md" {
			return err
		}
		rel, _ := filepath.Rel(root, p)
		b, rerr := os.ReadFile(p)
		files[filepath.ToSlash(rel)] = string(b)
		return rerr
	})
	if err != nil {
		t.Fatal(err)
	}
	return files
}

func batch8Sites(res models.ScanResult, id string) string {
	var out []string
	for _, f := range res.Findings {
		if f.RuleID == id {
			site := f.ToolName
			if site == "" {
				site = f.FilePath
			}
			out = append(out, site)
		}
	}
	sort.Strings(out)
	return strings.Join(out, ",")
}

// Static batch 8 over the corpus fixture: each rule fires at the agents or files that set no gate,
// and the gated agents, the neutral tool names, the test-path agent, the prose and the comment add nothing.
func TestScan_StaticBatch8Fixture(t *testing.T) {
	res := scanFiles(t, batch8Files(t))
	want := map[string]string{
		"OAI-212": "mailer",
		"ADK-204": "billing",
		"LC-206":  "notifier",
		"PYD-204": "texter",
		"CI-006":  ".claude/hooks/merge.sh",
		"CI-007":  ".github/workflows/agent.yml",
	}
	for id, w := range want {
		if got := batch8Sites(res, id); got != w {
			t.Errorf("%s: got %q, want %q", id, got, w)
		}
	}
}

// A CODEOWNERS file that names an owner for the workflow clears CI-007; one that leaves the workflow
// with no owner, or an agent-config file with none, does not.
func TestScan_StaticBatch8Codeowners(t *testing.T) {
	base := batch8Files(t)
	cases := []struct {
		name, codeowners string
		extra            map[string]string
		want             string
	}{
		{"owner for workflows", "/.github/workflows/ @acme/platform\n", nil, ""},
		{"owner for everything", "* @acme/platform\n", nil, ""},
		{"owner under .github", "/.github/ @acme/platform\n", nil, ""},
		{"no owner for workflows", "/src/ @acme/platform\n", nil, ".github/workflows/agent.yml"},
		{"ownership cleared later", "* @acme/platform\n/.github/workflows/\n", nil, ".github/workflows/agent.yml"},
		{"agent config unowned", "/.github/ @acme/platform\n", map[string]string{"CLAUDE.md": "# Notes\n"}, "CLAUDE.md"},
		{"agent config owned", "/.github/ @acme/platform\nCLAUDE.md @acme/platform\n", map[string]string{"CLAUDE.md": "# Notes\n"}, ""},
	}
	for _, c := range cases {
		files := map[string]string{}
		for k, v := range base {
			files[k] = v
		}
		files["CODEOWNERS"] = c.codeowners
		for k, v := range c.extra {
			files[k] = v
		}
		res := scanFiles(t, files)
		if got := batch8Sites(res, "CI-007"); got != c.want {
			t.Errorf("%s: got %q, want %q", c.name, got, c.want)
		}
	}
}

// A comment or a string that names an approval call is not a gate; a call is. A camel-case tool
// name is split before the name words are tested.
func TestScan_StaticBatch8BodyGatesReadCallsOnly(t *testing.T) {
	files := map[string]string{
		"requirements.txt": "langchain\nlanggraph\n",
		"app.py": `from langchain.agents import create_agent
from langchain_core.tools import tool
from langgraph.types import interrupt


@tool
def send_note(text: str) -> str:
    """Send a note."""
    # interrupt("ask first")
    msg = "interrupt(x)"
    return msg


@tool
def deleteRow(row: str) -> str:
    """Delete a row."""
    interrupt("ok?")
    return "gone"


@tool
def notify_team(text: str) -> str:
    """Notify the team."""
    return "sent"


commented = create_agent("openai:gpt-4o", name="commented", tools=[send_note])
camel = create_agent("openai:gpt-4o", name="camel", tools=[deleteRow])
plain = create_agent("openai:gpt-4o", name="plain", tools=[notify_team])
`,
	}
	res := scanFiles(t, files)
	if got := batch8Sites(res, "LC-206"); got != "commented,plain" {
		t.Errorf("LC-206: got %q, want commented,plain", got)
	}
}

// An approval or guardrail attribute set on the tool object after it is built counts as the kwarg.
func TestScan_StaticBatch8AssignedGateAttribute(t *testing.T) {
	files := map[string]string{
		"requirements.txt": "openai-agents\n",
		"app.py": `from agents import Agent, function_tool


@function_tool
def send_email(to: str) -> str:
    """Send an email."""
    return "sent"


@function_tool
def delete_row(row: str) -> str:
    """Delete a row."""
    return "gone"


def check(data):
    return None


send_email.tool_input_guardrails = [check]
delete_row.other = 1

gated = Agent(name="gated", instructions="x", tools=[send_email])
open_ = Agent(name="open_", instructions="x", tools=[delete_row])
`,
	}
	res := scanFiles(t, files)
	if got := batch8Sites(res, "OAI-212"); got != "open_" {
		t.Errorf("OAI-212: got %q, want open_", got)
	}
}
