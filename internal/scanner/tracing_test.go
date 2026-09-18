package scanner

import (
	"testing"

	"github.com/trustabl/trustabl/internal/analysis"
	"github.com/trustabl/trustabl/internal/analysis/astutil"
)

func parsePy(t *testing.T, src string) analysis.ParsedFile {
	t.Helper()
	tree, err := astutil.Parse([]byte(src))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	return analysis.ParsedFile{RelPath: "x.py", Source: []byte(src), Tree: tree}
}

func TestComputeUsesDefaultTracing(t *testing.T) {
	cases := []struct {
		name string
		src  string
		want bool
	}{
		{
			name: "plain agent code uses default tracing",
			src:  "from agents import Agent\nagent = Agent(name='a')\n",
			want: true,
		},
		{
			name: "add_trace_processor call disables default",
			src:  "from agents.tracing import add_trace_processor\nadd_trace_processor(MyProcessor())\n",
			want: false,
		},
		{
			name: "attribute-form processor call disables default",
			src:  "import agents\nagents.set_trace_processors([p])\n",
			want: false,
		},
		{
			name: "env-var string literal disables default",
			src:  "import os\nos.environ['OPENAI_AGENTS_DISABLE_TRACING'] = '1'\n",
			want: false,
		},
		{
			// The whole point of the AST fix: a mention in a comment must NOT
			// flip the result the way the old substring scan did.
			name: "mention in a comment does not disable default",
			src:  "# do not call add_trace_processor here\nx = 1\n",
			want: true,
		},
		{
			// An unrelated identifier that merely contains the substring must
			// not match either.
			name: "lookalike identifier does not disable default",
			src:  "my_add_trace_processor_helper = None\n",
			want: true,
		},
		{
			name: "set_tracing_disabled(True) disables default",
			src:  "from agents import set_tracing_disabled\nset_tracing_disabled(True)\n",
			want: false,
		},
		{
			name: "set_tracing_disabled(False) does not disable default",
			src:  "from agents import set_tracing_disabled\nset_tracing_disabled(False)\n",
			want: true,
		},
		{
			name: "attribute-form set_tracing_disabled(True) disables default",
			src:  "import agents\nagents.set_tracing_disabled(True)\n",
			want: false,
		},
		{
			name: "RunConfig(tracing_disabled=True) disables default",
			src:  "from agents import RunConfig\ncfg = RunConfig(tracing_disabled=True)\n",
			want: false,
		},
		{
			name: "RunConfig(tracing_disabled=False) does not disable default",
			src:  "from agents import RunConfig\ncfg = RunConfig(tracing_disabled=False)\n",
			want: true,
		},
		{
			name: "inline run_config=RunConfig(tracing_disabled=True) disables default",
			src: "from agents import Agent, Runner, RunConfig\n" +
				"agent = Agent(name='a')\n" +
				"Runner.run(agent, run_config=RunConfig(tracing_disabled=True))\n",
			want: false,
		},
		{
			name: "RunConfig with an unrelated kwarg does not disable default",
			src:  "from agents import RunConfig\ncfg = RunConfig(workflow_name='wf')\n",
			want: true,
		},
		{
			name: "lookalike RunConfig-suffixed identifier does not disable default",
			src:  "MyRunConfig(tracing_disabled=True)\n",
			want: true,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			pf := parsePy(t, c.src)
			defer pf.Tree.Close()
			if got := computeUsesDefaultTracing([]analysis.ParsedFile{pf}); got != c.want {
				t.Errorf("computeUsesDefaultTracing = %v, want %v", got, c.want)
			}
		})
	}
}
