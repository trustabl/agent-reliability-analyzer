package analysis

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/trustabl/trustabl/internal/models"
)

// ciFacts writes workflow files under a temp repo's .github/workflows and runs
// the discovery. Keys of files are workflow file names.
func ciFacts(t *testing.T, files map[string]string) models.CIWorkflowFacts {
	t.Helper()
	dir := t.TempDir()
	m := models.ScanManifest{RepoRoot: dir}
	for name, body := range files {
		rel := ".github/workflows/" + name
		full := filepath.Join(dir, rel)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
		m.WorkflowFiles = append(m.WorkflowFiles, rel)
		m.Components = append(m.Components, models.AgentComponent{Kind: models.ComponentGitHubWorkflow, Path: rel})
	}
	return DiscoverCIWorkflows(m)
}

// letters renders which fact slices are non-empty: U ungated (CI-001),
// P privileged (CI-002), C checkout (CI-003), S shell (CI-004), E egress (CI-005).
func letters(f models.CIWorkflowFacts) string {
	var b strings.Builder
	for _, x := range []struct {
		c byte
		n int
	}{
		{'U', len(f.UntrustedUngated)}, {'P', len(f.UntrustedPrivileged)}, {'C', len(f.CheckoutPersists)},
		{'S', len(f.UnrestrictedShell)}, {'E', len(f.NoEgressRestriction)},
	} {
		if x.n > 0 {
			b.WriteByte(x.c)
		}
	}
	return b.String()
}

const (
	gateG1 = `contains(fromJSON('["OWNER","MEMBER","COLLABORATOR"]'), github.event.comment.author_association)`

	stClaude = "      - uses: anthropics/claude-code-action@v1\n        with:\n          anthropic_api_key: ${{ secrets.ANTHROPIC_API_KEY }}\n"
	stGemini = "      - uses: google-github-actions/run-gemini-cli@v0\n        with:\n          gemini_api_key: ${{ secrets.GEMINI_API_KEY }}\n"
	stCodex  = "      - uses: openai/codex-action@v1\n        with:\n          openai-api-key: ${{ secrets.OPENAI_API_KEY }}\n"
)

// ciWF builds a one-job workflow. ifLine is an optional job-level if:.
func ciWF(on, perms, ifLine, steps string) string {
	s := "name: t\non: " + on + "\n"
	if perms != "" {
		s += "permissions:\n" + perms
	}
	s += "jobs:\n  j:\n    runs-on: ubuntu-latest\n"
	if ifLine != "" {
		s += "    if: " + ifLine + "\n"
	}
	return s + "    steps:\n" + steps
}

func TestCIWorkflows_Matrix(t *testing.T) {
	readPerm := "  contents: read\n"
	cases := []struct {
		name string
		body string
		want string
	}{
		// ── CI-001: actor gate ───────────────────────────────────────────
		{"claude with bypass input is ungated", ciWF("issues", readPerm, "", stClaude+"          allowed_non_write_users: \"*\"\n"), "U"},
		{"claude built-in write-access check counts as a gate (the report's own fail example)", ciWF("issues", readPerm, "", stClaude), ""},
		{"claude allowed_bots star is ungated", ciWF("issues", readPerm, "", stClaude+"          allowed_bots: '*'\n"), "U"},
		{"claude named non-write user list stays gated", ciWF("issues", readPerm, "", stClaude+"          allowed_non_write_users: octocat\n"), ""},
		{"claude bypass as an expression is ungated", ciWF("issues", readPerm, "", stClaude+"          allowed_non_write_users: ${{ vars.USERS }}\n"), "U"},
		{"gemini on issues is ungated", ciWF("issues", readPerm, "", stGemini), "U"},
		{"gemini with G1 if is gated", ciWF("issue_comment", readPerm, gateG1, stGemini), ""},
		{"G1 with an || chain of contexts is a gate", ciWF("issue_comment", readPerm, `${{ contains(fromJSON('["OWNER", "MEMBER"]'), github.event.comment.author_association || github.event.review.author_association || github.event.issue.author_association) }}`, stGemini), ""},
		{"G2 equality is a gate", ciWF("issue_comment", readPerm, "github.event.comment.author_association == 'OWNER'", stGemini), ""},
		{"deny-list is not a gate", ciWF("issue_comment", readPerm, "github.event.comment.author_association != 'NONE'", stGemini), "U"},
		{"trigger phrase is not a gate", ciWF("issue_comment", readPerm, "contains(github.event.comment.body, '@gemini-cli')", stGemini), "U"},
		{"CONTRIBUTOR in the list is not a gate", ciWF("issue_comment", readPerm, `contains(fromJSON('["OWNER","CONTRIBUTOR"]'), github.event.comment.author_association)`, stGemini), "U"},
		{"negated allow-list is not a gate", ciWF("issue_comment", readPerm, `${{ !contains(fromJSON('["OWNER"]'), github.event.comment.author_association) }}`, stGemini), "U"},
		{"actor literal is a gate", ciWF("issue_comment", readPerm, "github.actor == 'octocat'", stGemini), ""},
		{"fork check gates pull_request_target", ciWF("pull_request_target", readPerm, "github.event.pull_request.head.repo.full_name == github.repository", stGemini), ""},
		{"fork check does not gate issues", ciWF("issues", readPerm, "github.event.pull_request.head.repo.full_name == github.repository", stGemini), "U"},
		{"step-level G1 if is a gate", ciWF("issue_comment", readPerm, "", "      - if: "+gateG1+"\n        uses: google-github-actions/run-gemini-cli@v0\n"), ""},
		{"codex allow-users star is ungated", ciWF("issues", readPerm, "", stCodex+"          allow-users: '*'\n"), "U"},
		{"codex built-in check counts as a gate", ciWF("issues", readPerm, "", stCodex), ""},
		{"base action has no built-in gate", ciWF("issues", readPerm, "", "      - uses: anthropics/claude-code-action/base-action@v1\n"), "U"},
		{"base action mirror has no built-in gate", ciWF("issues", readPerm, "", "      - uses: anthropics/claude-code-base-action@beta\n"), "U"},
		{"cli on issues is ungated", ciWF("issues", readPerm, "", "      - run: gemini -p \"summarize\"\n"), "U"},
		{"npx package is a cli agent", ciWF("issues", readPerm, "", "      - run: npx -y @google/gemini-cli@latest -p x\n"), "U"},
		{"echo of an agent name is not an agent", ciWF("issues", readPerm, "", "      - run: echo gemini\n"), ""},
		{"similar action name is not an agent", ciWF("issues", readPerm, "", "      - uses: acme/decline-action@v1\n"), ""},
		{"expression uses is not an agent", ciWF("issues", readPerm, "", "      - uses: ${{ matrix.action }}\n"), ""},
		{"pull_request is not an untrusted trigger", ciWF("pull_request", readPerm, "", stGemini), ""},
		{"workflow_run is not in the trigger set", ciWF("workflow_run", readPerm, "", stGemini), ""},
		{"list-form on", "name: t\non: [push, issues]\njobs:\n  j:\n    runs-on: ubuntu-latest\n    steps:\n" + stGemini, "U"},

		// ── CI-002: privileged ───────────────────────────────────────────
		{"contents write", ciWF("issue_comment", "  contents: write\n", "", stGemini), "UP"},
		{"write-all", ciWF("issue_comment", "", "", stGemini), "U"}, // replaced below by the scalar form
		{"pull-requests write", ciWF("issue_comment", "  pull-requests: write\n", "", stGemini), "UP"},
		{"issues write is not counted", ciWF("issue_comment", "  issues: write\n", "", stGemini), "U"},
		{"no permissions block is not flagged", ciWF("issue_comment", "", "", stGemini), "U"},
		{"extra secret in agent env", ciWF("issue_comment", readPerm, "", stGemini+"        env:\n          NPM_TOKEN: ${{ secrets.NPM_TOKEN }}\n"), "UP"},
		{"PAT as github_token", ciWF("issue_comment", readPerm, "", stGemini+"          github_token: ${{ secrets.MY_PAT }}\n"), "UP"},
		{"GITHUB_TOKEN is not extra", ciWF("issue_comment", readPerm, "", stGemini+"          github_token: ${{ secrets.GITHUB_TOKEN }}\n"), "U"},
		{"model key under a custom secret name is not extra", ciWF("issues", readPerm, "", stClaude+"          allowed_non_write_users: '*'\n"), "U"},
		{"model key env var is not extra", ciWF("issue_comment", readPerm, "", "      - run: gemini -p x\n        env:\n          GEMINI_API_KEY: ${{ secrets.WHATEVER }}\n"), "U"},
		{"actions/cache is not counted", ciWF("issue_comment", readPerm, "", "      - uses: actions/cache@v4\n"+stGemini), "U"},
		{"write token on a gated job is not CI-002", ciWF("issue_comment", "  contents: write\n", gateG1, stGemini), ""},

		// ── CI-003: checkout credentials (gated job isolates it) ────────
		{"default checkout persists", ciWF("issue_comment", readPerm, gateG1, "      - uses: actions/checkout@v4\n"+stGemini), "C"},
		{"persist-credentials false", ciWF("issue_comment", readPerm, gateG1, "      - uses: actions/checkout@v4\n        with:\n          persist-credentials: false\n"+stGemini), ""},
		{"persist-credentials quoted false", ciWF("issue_comment", readPerm, gateG1, "      - uses: actions/checkout@v4\n        with:\n          persist-credentials: 'false'\n"+stGemini), ""},
		{"persist-credentials expression persists", ciWF("issue_comment", readPerm, gateG1, "      - uses: actions/checkout@v4\n        with:\n          persist-credentials: ${{ inputs.p }}\n"+stGemini), "C"},
		{"no checkout", ciWF("issue_comment", readPerm, gateG1, stGemini), ""},
		{"checkout on a trusted trigger is not flagged", ciWF("push", readPerm, "", "      - uses: actions/checkout@v4\n"+stGemini), ""},

		// ── CI-004 / CI-005 (gated job isolates them) ────────────────────
		{"claude bare Bash", ciWF("issue_comment", readPerm, gateG1, stClaude+"          claude_args: --allowedTools \"Bash,Read\"\n"), "SE"},
		{"claude exact Bash command", ciWF("issue_comment", readPerm, gateG1, stClaude+"          claude_args: --allowedTools \"Bash(npm test),Read\"\n"), "E"},
		{"claude Bash star", ciWF("issue_comment", readPerm, gateG1, stClaude+"          claude_args: --allowedTools \"Bash(*)\"\n"), "SE"},
		{"claude Bash python prefix", ciWF("issue_comment", readPerm, gateG1, stClaude+"          claude_args: --allowedTools \"Bash(python:*)\"\n"), "SE"},
		{"claude Bash curl prefix", ciWF("issue_comment", readPerm, gateG1, stClaude+"          claude_args: --allowedTools \"Bash(curl:*)\"\n"), "SE"},
		{"claude skip permissions flag", ciWF("issue_comment", readPerm, gateG1, stClaude+"          claude_args: --dangerously-skip-permissions\n"), "SE"},
		{"claude permission mode bypass", ciWF("issue_comment", readPerm, gateG1, stClaude+"          claude_args: --permission-mode bypassPermissions\n"), "SE"},
		{"claude settings allow Bash", ciWF("issue_comment", readPerm, gateG1, stClaude+"          settings: '{\"permissions\": {\"allow\": [\"Bash\"]}}'\n"), "SE"},
		{"claude read-only tools", ciWF("issue_comment", readPerm, gateG1, stClaude+"          claude_args: --allowedTools \"Read,Grep\"\n"), ""},
		{"claude WebFetch alone", ciWF("issue_comment", readPerm, gateG1, stClaude+"          claude_args: --allowedTools \"WebFetch\"\n"), "E"},
		{"claude no tool configuration", ciWF("issue_comment", readPerm, gateG1, stClaude), ""},
		{"gemini bare run_shell_command", ciWF("issue_comment", readPerm, gateG1, stGemini+"          settings: '{\"tools\": {\"core\": [\"run_shell_command\"]}}'\n"), "SE"},
		{"gemini exact run_shell_command", ciWF("issue_comment", readPerm, gateG1, stGemini+"          settings: '{\"tools\": {\"core\": [\"run_shell_command(git diff)\"]}}'\n"), "E"},
		{"gemini empty core", ciWF("issue_comment", readPerm, gateG1, stGemini+"          settings: '{\"tools\": {\"core\": []}}'\n"), ""},
		{"gemini cli yolo", ciWF("issue_comment", readPerm, gateG1, "      - run: gemini --yolo -p x\n"), "SE"},
		{"gemini cli approval-mode yolo", ciWF("issue_comment", readPerm, gateG1, "      - run: gemini --approval-mode=yolo -p x\n"), "SE"},
		{"codex danger-full-access", ciWF("issue_comment", readPerm, gateG1, stCodex+"          sandbox: danger-full-access\n"), "SE"},
		{"codex unsafe strategy", ciWF("issue_comment", readPerm, gateG1, stCodex+"          safety-strategy: unsafe\n"), "SE"},
		{"codex bypass arg", ciWF("issue_comment", readPerm, gateG1, stCodex+"          codex-args: '[\"--dangerously-bypass-approvals-and-sandbox\"]'\n"), "SE"},
		{"codex default", ciWF("issue_comment", readPerm, gateG1, stCodex), ""},
		{"harden-runner block before agent", ciWF("issue_comment", readPerm, gateG1, "      - uses: step-security/harden-runner@v2\n        with:\n          egress-policy: block\n"+stClaude+"          claude_args: --allowedTools \"Bash(npm test),WebFetch\"\n"), ""},
		{"harden-runner audit", ciWF("issue_comment", readPerm, gateG1, "      - uses: step-security/harden-runner@v2\n        with:\n          egress-policy: audit\n"+stClaude+"          claude_args: --allowedTools \"Bash(npm test),WebFetch\"\n"), "E"},
		{"harden-runner after agent", ciWF("issue_comment", readPerm, gateG1, stClaude+"          claude_args: --allowedTools \"Bash(npm test),WebFetch\"\n      - uses: step-security/harden-runner@v2\n        with:\n          egress-policy: block\n"), "E"},
	}
	// The write-all case uses the scalar form, which ciWF's block form cannot express.
	for i := range cases {
		if cases[i].name == "write-all" {
			cases[i].body = "name: t\non: issue_comment\npermissions: write-all\njobs:\n  j:\n    runs-on: ubuntu-latest\n    steps:\n" + stGemini
			cases[i].want = "UP"
		}
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f := ciFacts(t, map[string]string{"w.yml": c.body})
			if got := letters(f); got != c.want {
				t.Fatalf("got %q want %q\nfacts: %+v", got, c.want, f)
			}
		})
	}
}

func TestCIWorkflows_OneHopReusableWorkflow(t *testing.T) {
	callee := "name: callee\non:\n  workflow_call:\npermissions:\n  contents: read\njobs:\n  invoke:\n    runs-on: ubuntu-latest\n    steps:\n" + stGemini
	caller := func(ifLine, extra string) string {
		s := "name: caller\non:\n  issue_comment:\n    types: [created]\njobs:\n  dispatch:\n"
		if ifLine != "" {
			s += "    if: " + ifLine + "\n"
		}
		return s + "    uses: ./.github/workflows/callee.yml\n" + extra
	}
	t.Run("ungated caller fires in the callee", func(t *testing.T) {
		f := ciFacts(t, map[string]string{"caller.yml": caller("", ""), "callee.yml": callee})
		if letters(f) != "U" || len(f.UntrustedUngated) != 1 {
			t.Fatalf("got %q %+v", letters(f), f)
		}
		h := f.UntrustedUngated[0]
		if h.FilePath != ".github/workflows/callee.yml" || h.Job != "caller.yml#dispatch>callee.yml#invoke" || h.Line < 1 {
			t.Fatalf("hit = %+v", h)
		}
	})
	t.Run("gated caller silences the callee", func(t *testing.T) {
		f := ciFacts(t, map[string]string{"caller.yml": caller(gateG1, ""), "callee.yml": callee})
		if letters(f) != "" {
			t.Fatalf("got %q %+v", letters(f), f)
		}
	})
	t.Run("secrets inherit makes the ungated callee privileged", func(t *testing.T) {
		f := ciFacts(t, map[string]string{"caller.yml": caller("", "    secrets: inherit\n"), "callee.yml": callee})
		if letters(f) != "UP" {
			t.Fatalf("got %q", letters(f))
		}
	})
	t.Run("callee alone has no untrusted trigger", func(t *testing.T) {
		f := ciFacts(t, map[string]string{"callee.yml": callee})
		if letters(f) != "" {
			t.Fatalf("got %q", letters(f))
		}
	})
	t.Run("remote reusable workflow is not resolved", func(t *testing.T) {
		remote := "name: c\non: issue_comment\njobs:\n  d:\n    uses: acme/repo/.github/workflows/x.yml@main\n"
		if got := letters(ciFacts(t, map[string]string{"c.yml": remote})); got != "" {
			t.Fatalf("got %q", got)
		}
	})
}

func TestCIWorkflows_NeedsChainGate(t *testing.T) {
	wf := func(agentIf string) string {
		return "name: t\non: issue_comment\njobs:\n  gate:\n    if: " + gateG1 + "\n    runs-on: ubuntu-latest\n    steps:\n      - run: echo ok\n" +
			"  agent:\n    needs: gate\n" + agentIf + "    runs-on: ubuntu-latest\n    steps:\n" + stGemini
	}
	if got := letters(ciFacts(t, map[string]string{"w.yml": wf("")})); got != "" {
		t.Fatalf("needs-inherited gate: got %q", got)
	}
	if got := letters(ciFacts(t, map[string]string{"w.yml": wf("    if: always()\n")})); got != "U" {
		t.Fatalf("always() defeats the inherited gate: got %q", got)
	}
}

func TestCIWorkflows_YAMLForms(t *testing.T) {
	anchors := `name: t
on:
  issues:
    types: [opened]
x-step: &agent
  uses: google-github-actions/run-gemini-cli@v0
jobs:
  a:
    runs-on: ubuntu-latest
    steps:
      - *agent
`
	if got := letters(ciFacts(t, map[string]string{"w.yml": anchors})); got != "U" {
		t.Fatalf("alias step: got %q", got)
	}
	merge := `name: t
on: issues
defaults: &d
  runs-on: ubuntu-latest
  steps:
    - uses: google-github-actions/run-gemini-cli@v0
jobs:
  a:
    <<: *d
`
	if got := letters(ciFacts(t, map[string]string{"w.yml": merge})); got != "U" {
		t.Fatalf("merge key: got %q", got)
	}
	f := ciFacts(t, map[string]string{"bad.yml": "name: [unclosed\n  - x", "scalar.yml": "just text"})
	if len(f.Unparsed) != 2 || letters(f) != "" {
		t.Fatalf("unparsed = %v, facts %+v", f.Unparsed, f)
	}
}

// A nested-alias file (10^8 logical nodes if expanded) must neither hang nor
// produce facts: the traversal never expands aliased sequences.
func TestCIWorkflows_NestedAliasesDoNotExpand(t *testing.T) {
	var b strings.Builder
	b.WriteString("name: t\non: issues\nl0: &l0 [x, x, x, x, x, x, x, x, x, x]\n")
	prev := "l0"
	for i := 1; i <= 8; i++ {
		cur := "l" + string(rune('0'+i))
		b.WriteString(cur + ": &" + cur + " [")
		for k := 0; k < 10; k++ {
			if k > 0 {
				b.WriteString(", ")
			}
			b.WriteString("*" + prev)
		}
		b.WriteString("]\n")
		prev = cur
	}
	b.WriteString("jobs:\n  a:\n    runs-on: ubuntu-latest\n    needs: *" + prev + "\n    steps:\n      - run: echo hi\n")
	f := ciFacts(t, map[string]string{"bomb.yml": b.String()})
	if letters(f) != "" || len(f.Unparsed) != 0 {
		t.Fatalf("nested aliases: facts %+v", f)
	}
}

func TestCIWorkflows_HitsCarryNoValues(t *testing.T) {
	body := ciWF("issues", "  contents: write\n", "", stGemini+"        env:\n          NPM_TOKEN: ${{ secrets.NPM_TOKEN }}\n          LITERAL: hunter2-fake-value\n")
	f := ciFacts(t, map[string]string{"w.yml": body})
	for _, h := range append(append([]models.CIHit{}, f.UntrustedUngated...), f.UntrustedPrivileged...) {
		if strings.Contains(h.Reason, "hunter2-fake-value") || strings.Contains(h.Reason, "GEMINI_API_KEY}") {
			t.Fatalf("reason leaked a value: %q", h.Reason)
		}
	}
	if len(f.UntrustedPrivileged) != 1 || !strings.Contains(f.UntrustedPrivileged[0].Reason, "NPM_TOKEN") {
		t.Fatalf("privileged = %+v", f.UntrustedPrivileged)
	}
}

func TestCIWorkflows_Deterministic(t *testing.T) {
	files := map[string]string{
		"b.yml": ciWF("issues", "", "", stGemini),
		"a.yml": ciWF("issue_comment", "", "", stClaude+"          allowed_non_write_users: '*'\n"),
	}
	first := ciFacts(t, files)
	for i := 0; i < 5; i++ {
		got := ciFacts(t, files)
		if len(got.UntrustedUngated) != 2 || got.UntrustedUngated[0].FilePath != first.UntrustedUngated[0].FilePath ||
			got.UntrustedUngated[0].FilePath != ".github/workflows/a.yml" {
			t.Fatalf("order changed: %+v", got.UntrustedUngated)
		}
	}
}

func TestCIWorkflows_NodeBudgetExceededIsAnError(t *testing.T) {
	p := &ciParser{budget: 3}
	if _, err := p.parseWorkflow("w.yml", []byte(ciWF("issues", "", "", stGemini))); err == nil {
		t.Fatal("want an error when the node budget is exhausted")
	}
}
