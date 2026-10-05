package analysis_test

import (
	"reflect"
	"testing"

	"github.com/trustabl/trustabl/internal/analysis"
	"github.com/trustabl/trustabl/internal/models"
)

func epsOf(t *testing.T, files ...analysis.ParsedFile) []models.EntrypointDef {
	t.Helper()
	return analysis.DiscoverEntrypoints(files)
}

func TestDiscoverEntrypoints_FireAndSilent(t *testing.T) {
	cases := []struct {
		name          string
		src           string
		wantFramework string // "" = no entrypoint
		wantKind      models.EntrypointKind
		wantRoute     string
		wantFunc      string
	}{
		{"fastapi app", "from fastapi import FastAPI\napp = FastAPI()\n\n@app.get(\"/x\")\ndef h():\n    pass\n", "fastapi", models.EntrypointHTTP, "/x", "h"},
		{"fastapi router", "from fastapi import APIRouter\nrouter = APIRouter()\n\n@router.post(\"/y\")\nasync def h():\n    pass\n", "fastapi", models.EntrypointHTTP, "/y", "h"},
		{"fastapi websocket", "import fastapi\napp = fastapi.FastAPI()\n\n@app.websocket(\"/ws\")\nasync def h(ws):\n    pass\n", "fastapi", models.EntrypointHTTP, "/ws", "h"},
		{"fastapi api_route", "from fastapi import FastAPI\napp = FastAPI()\n\n@app.api_route(\"/z\", methods=[\"GET\"])\ndef h():\n    pass\n", "fastapi", models.EntrypointHTTP, "/z", "h"},
		{"fastapi imported recv", "from fastapi import APIRouter\nfrom app.main import router\n\n@router.get(\"/i\")\ndef h():\n    pass\n", "fastapi", models.EntrypointHTTP, "/i", "h"},
		{"fastapi recv bound to non-framework", "from fastapi import FastAPI\ncache = make_cache()\n\n@cache.get(\"/k\")\ndef h():\n    pass\n", "", "", "", ""},
		{"flask route", "from flask import Flask\napp = Flask(__name__)\n\n@app.route(\"/\")\ndef h():\n    pass\n", "flask", models.EntrypointHTTP, "/", "h"},
		{"flask blueprint", "from flask import Blueprint\nbp = Blueprint(\"b\", __name__)\n\n@bp.get(\"/b\")\ndef h():\n    pass\n", "flask", models.EntrypointHTTP, "/b", "h"},
		{"flask route in dual-import file stays flask", "from fastapi import FastAPI\nfrom flask import Flask\nfrom x import app\n\n@app.route(\"/r\")\ndef h():\n    pass\n", "flask", models.EntrypointHTTP, "/r", "h"},
		{"fastapi wins shared method in dual-import file", "from fastapi import FastAPI\nfrom flask import Flask\nfrom x import app\n\n@app.get(\"/g\")\ndef h():\n    pass\n", "fastapi", models.EntrypointHTTP, "/g", "h"},
		{"celery app.task", "from celery import Celery\napp = Celery()\n\n@app.task\ndef t():\n    pass\n", "celery", models.EntrypointWorker, "", "t"},
		{"celery task bind", "from celery import Celery\napp = Celery()\n\n@app.task(bind=True)\ndef t(self):\n    pass\n", "celery", models.EntrypointWorker, "", "t"},
		{"celery shared_task", "from celery import shared_task\n\n@shared_task\ndef t():\n    pass\n", "celery", models.EntrypointWorker, "", "t"},
		{"dramatiq actor", "import dramatiq\n\n@dramatiq.actor\ndef t():\n    pass\n", "dramatiq", models.EntrypointWorker, "", "t"},
		{"dramatiq actor call", "import dramatiq\n\n@dramatiq.actor(max_retries=3)\ndef t():\n    pass\n", "dramatiq", models.EntrypointWorker, "", "t"},
		{"no framework import .get", "app = make()\n\n@app.get(\"/x\")\ndef h():\n    pass\n", "", "", "", ""},
		{"app.task without celery import", "from foo import app\n\n@app.task\ndef t():\n    pass\n", "", "", "", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			eps := epsOf(t, parsePyFile(t, "svc/app.py", tc.src))
			if tc.wantFramework == "" {
				if len(eps) != 0 {
					t.Fatalf("want none, got %+v", eps)
				}
				return
			}
			if len(eps) != 1 {
				t.Fatalf("want 1 entrypoint, got %+v", eps)
			}
			e := eps[0]
			if e.Framework != tc.wantFramework || e.Kind != tc.wantKind || e.Route != tc.wantRoute || e.FuncName != tc.wantFunc {
				t.Errorf("got %+v", e)
			}
			if e.FilePath != "svc/app.py" || e.Line == 0 {
				t.Errorf("bad location %+v", e.Location)
			}
		})
	}
}

func TestDiscoverEntrypoints_StackedDecoratorsEmitOneEach(t *testing.T) {
	src := "from fastapi import FastAPI\napp = FastAPI()\n\n@app.get(\"/a\")\n@app.post(\"/a\")\ndef h():\n    pass\n"
	if eps := epsOf(t, parsePyFile(t, "a.py", src)); len(eps) != 2 {
		t.Fatalf("want 2, got %+v", eps)
	}
}

func TestDiscoverEntrypoints_SkipsExamplePaths(t *testing.T) {
	src := "from fastapi import FastAPI\napp = FastAPI()\n\n@app.get(\"/x\")\ndef h():\n    pass\n"
	for _, p := range []string{"examples/a.py", "x/docs/a.py", "Demo/a.py", "cookbook/n/a.py", "tutorials/a.py"} {
		if eps := epsOf(t, parsePyFile(t, p, src)); len(eps) != 0 {
			t.Errorf("%s: want skipped, got %+v", p, eps)
		}
	}
	// The filename alone is never a skip.
	if eps := epsOf(t, parsePyFile(t, "src/example_utils.py", src)); len(eps) != 1 {
		t.Errorf("filename must not skip, got %+v", eps)
	}
}

// reach runs the same three steps as the scanner, in the same order.
func reach(files []analysis.ParsedFile) models.RepoInventory {
	inv := models.RepoInventory{
		AgentRunCalls: analysis.DiscoverAgentRunCalls(files),
		Entrypoints:   analysis.DiscoverEntrypoints(files),
	}
	analysis.ApplyEntrypointReachability(&inv, files)
	return inv
}

const reachAgentPrelude = "from agents import Agent, Runner\nagent = Agent(name=\"x\")\n"

func TestApplyEntrypointReachability(t *testing.T) {
	route := "from fastapi import FastAPI\napp = FastAPI()\n"
	cases := []struct {
		name    string
		files   map[string]string
		wantVia string // "" = not stamped
	}{
		{"A direct", map[string]string{"app.py": route + reachAgentPrelude + "\n@app.post(\"/\")\nasync def h():\n    return await Runner.run(agent, \"x\")\n"}, "direct"},
		{"B same-file helper", map[string]string{"app.py": route + reachAgentPrelude + "\nasync def helper():\n    return await Runner.run(agent, \"x\")\n\n@app.post(\"/\")\nasync def h():\n    result = await helper()\n    return result\n"}, "same_file"},
		{"B nested in args", map[string]string{"app.py": route + reachAgentPrelude + "\ndef helper():\n    return Runner.run_sync(agent, \"x\")\n\n@app.post(\"/\")\ndef h():\n    return {\"r\": str(helper())}\n"}, "same_file"},
		{"C absolute import", map[string]string{
			"app.py":      route + "from pkg.svc import run_it\n\n@app.post(\"/\")\ndef h():\n    return run_it()\n",
			"pkg/svc.py":  reachAgentPrelude + "\ndef run_it():\n    return Runner.run_sync(agent, \"x\")\n",
			"pkg/init.py": "",
		}, "import"},
		{"C relative .mod", map[string]string{
			"app/api.py": route + "from .svc import run_it\n\n@app.post(\"/\")\ndef h():\n    return run_it()\n",
			"app/svc.py": reachAgentPrelude + "\ndef run_it():\n    return Runner.run_sync(agent, \"x\")\n",
		}, "import"},
		{"C relative ..mod", map[string]string{
			"app/routes/api.py": route + "from ..svc import run_it\n\n@app.post(\"/\")\ndef h():\n    return run_it()\n",
			"app/svc.py":        reachAgentPrelude + "\ndef run_it():\n    return Runner.run_sync(agent, \"x\")\n",
		}, "import"},
		{"C relative from . import name via __init__", map[string]string{
			"app/api.py":      route + "from . import run_it\n\n@app.post(\"/\")\ndef h():\n    return run_it()\n",
			"app/__init__.py": reachAgentPrelude + "\ndef run_it():\n    return Runner.run_sync(agent, \"x\")\n",
		}, "import"},
		{"relative import to a file outside the scan set", map[string]string{
			"app/api.py": route + "from .missing import run_it\n\n@app.post(\"/\")\ndef h():\n    return run_it()\n",
			"other.py":   reachAgentPrelude + "\ndef run_it():\n    return Runner.run_sync(agent, \"x\")\n",
		}, ""},
		{"relative import climbing above the root", map[string]string{
			"api.py": route + "from ..svc import run_it\n\n@app.post(\"/\")\ndef h():\n    return run_it()\n",
			"svc.py": reachAgentPrelude + "\ndef run_it():\n    return Runner.run_sync(agent, \"x\")\n",
		}, ""},
		{"two hops is a documented limit", map[string]string{"app.py": route + reachAgentPrelude + "\ndef inner():\n    return Runner.run_sync(agent, \"x\")\n\ndef helper():\n    return inner()\n\n@app.post(\"/\")\ndef h():\n    return helper()\n"}, ""},
		{"run call outside any entrypoint", map[string]string{"app.py": route + reachAgentPrelude + "\ndef script():\n    return Runner.run_sync(agent, \"x\")\n\n@app.get(\"/\")\ndef h():\n    return 1\n"}, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var files []analysis.ParsedFile
			for _, p := range sortedKeys(tc.files) {
				files = append(files, parsePyFile(t, p, tc.files[p]))
			}
			inv := reach(files)
			if len(inv.AgentRunCalls) != 1 {
				t.Fatalf("want 1 run call, got %+v", inv.AgentRunCalls)
			}
			got := inv.AgentRunCalls[0].ServerReachable
			if tc.wantVia == "" {
				if got != nil {
					t.Fatalf("want unstamped, got %+v", got)
				}
				return
			}
			if got == nil || got.Via != tc.wantVia || got.FuncName != "h" || got.Framework != "fastapi" {
				t.Fatalf("want via %q from h, got %+v", tc.wantVia, got)
			}
		})
	}
}

func sortedKeys(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	for i := range out { // tiny insertion sort; keeps this file import-light
		for j := i; j > 0 && out[j] < out[j-1]; j-- {
			out[j], out[j-1] = out[j-1], out[j]
		}
	}
	return out
}

func TestEntrypointReachability_Deterministic(t *testing.T) {
	build := func() []analysis.ParsedFile {
		return []analysis.ParsedFile{
			parsePyFile(t, "app/api.py", "from fastapi import FastAPI\napp = FastAPI()\nfrom .svc import run_it\n\n@app.post(\"/b\")\ndef b():\n    return run_it()\n\n@app.post(\"/a\")\ndef a():\n    return run_it()\n"),
			parsePyFile(t, "app/svc.py", reachAgentPrelude+"\ndef run_it():\n    return Runner.run_sync(agent, \"x\")\n"),
			parsePyFile(t, "w.py", "import dramatiq\n\n@dramatiq.actor\ndef t():\n    pass\n"),
		}
	}
	first := reach(build())
	if len(first.Entrypoints) != 3 || first.AgentRunCalls[0].ServerReachable == nil {
		t.Fatalf("unexpected result: %+v", first)
	}
	second := reach(build())
	rev := build()
	for i, j := 0, len(rev)-1; i < j; i, j = i+1, j-1 {
		rev[i], rev[j] = rev[j], rev[i]
	}
	third := reach(rev)
	for name, other := range map[string]models.RepoInventory{"rerun": second, "reversed": third} {
		if !reflect.DeepEqual(first.Entrypoints, other.Entrypoints) {
			t.Errorf("%s: entrypoints differ:\n%+v\n%+v", name, first.Entrypoints, other.Entrypoints)
		}
		if !reflect.DeepEqual(first.AgentRunCalls[0].ServerReachable, other.AgentRunCalls[0].ServerReachable) {
			t.Errorf("%s: stamp differs: %+v vs %+v", name, first.AgentRunCalls[0].ServerReachable, other.AgentRunCalls[0].ServerReachable)
		}
	}
}

// TestEntrypointPipeline_SyntheticRepoStamp mirrors the scanner e2e repo
// (FastAPI route -> helper -> Runner.run) and asserts the stamp, which the JSON
// report does not carry.
func TestEntrypointPipeline_SyntheticRepoStamp(t *testing.T) {
	src := "from fastapi import FastAPI\nfrom agents import Agent, Runner\n\napp = FastAPI()\nagent = Agent(name=\"x\")\n\n\nasync def helper(q):\n    return await Runner.run(agent, q)\n\n\n@app.post(\"/chat\")\nasync def chat(q: str):\n    result = await helper(q)\n    return {\"out\": str(result)}\n"
	inv := reach([]analysis.ParsedFile{parsePyFile(t, "app.py", src)})
	if len(inv.Entrypoints) != 1 || len(inv.AgentRunCalls) != 1 {
		t.Fatalf("unexpected inventory: %+v", inv)
	}
	got := inv.AgentRunCalls[0].ServerReachable
	if got == nil || got.Via != "same_file" || got.FuncName != "chat" || got.Kind != models.EntrypointHTTP {
		t.Fatalf("unexpected stamp %+v", got)
	}
}
