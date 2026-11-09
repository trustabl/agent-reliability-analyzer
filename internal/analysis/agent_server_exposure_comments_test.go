package analysis

import "testing"

const (
	pyAdkImport = "from google.adk.cli.fast_api import get_fast_api_app\n"
	tsAdkImport = "import {AdkApiServer} from '@google/adk-devtools';\n"
	ohImage     = "ghcr.io/openhands/agent-server:latest-python"
)

// A call inside a comment, a docstring or a string literal is not judged; a real
// call next to one is.
func TestAgentServerExposure_CommentsAndStrings(t *testing.T) {
	cases := []struct {
		name  string
		files map[string]string
		want  bool
	}{
		{"python # comment", map[string]string{"s.py": pyAdkImport + "# app = get_fast_api_app(allow_origins=[\"*\"])\n"}, false},
		{"python indented # comment", map[string]string{"s.py": pyAdkImport + "def f():\n    # get_fast_api_app(allow_origins=['*'])\n    pass\n"}, false},
		{"python trailing comment after a real call", map[string]string{"s.py": pyAdkImport + "app = get_fast_api_app(allow_origins=[\"*\"])  # open\n"}, true},
		{"python docstring", map[string]string{"s.py": pyAdkImport + "\"\"\"\napp = get_fast_api_app(allow_origins=[\"*\"])\n\"\"\"\n"}, false},
		{"python single-quote docstring", map[string]string{"s.py": pyAdkImport + "'''\napp = get_fast_api_app(allow_origins=[\"*\"])\n'''\n"}, false},
		{"python string literal", map[string]string{"s.py": pyAdkImport + "s = 'get_fast_api_app(allow_origins=[\"*\"])'\n"}, false},
		{"python real call after a docstring", map[string]string{"s.py": pyAdkImport + "\"\"\"doc\"\"\"\napp = get_fast_api_app(allow_origins=[\"*\"])\n"}, true},
		{"python real call after a comment", map[string]string{"s.py": pyAdkImport + "# note\napp = get_fast_api_app(allow_origins=[\"*\"])\n"}, true},
		{"ts // comment", map[string]string{"s.ts": tsAdkImport + "// new AdkApiServer({allowOrigins: '*'});\n"}, false},
		{"ts block comment", map[string]string{"s.ts": tsAdkImport + "/* new AdkApiServer({allowOrigins: '*'}); */\n"}, false},
		{"ts multi-line block comment", map[string]string{"s.ts": tsAdkImport + "/*\n * new AdkApiServer({allowOrigins: '*'});\n */\n"}, false},
		{"ts template literal", map[string]string{"s.ts": tsAdkImport + "const s = `new AdkApiServer({allowOrigins: '*'})`;\n"}, false},
		{"ts double-quoted string", map[string]string{"s.ts": tsAdkImport + "const s = \"new AdkApiServer({allowOrigins: '*'})\";\n"}, false},
		{"ts real call after a comment", map[string]string{"s.ts": tsAdkImport + "// note\nnew AdkApiServer({allowOrigins: '*'});\n"}, true},
		{"ts real call after a block comment", map[string]string{"s.ts": tsAdkImport + "/* note */ new AdkApiServer({allowOrigins: '*'});\n"}, true},
		{"makefile # comment", map[string]string{"Makefile": "# adk web --allow_origins=*\n"}, false},
		{"shell # comment", map[string]string{"run.sh": "  # adk api_server --allow_origins=*\n"}, false},
		{"dockerfile # comment", map[string]string{"Dockerfile": "# CMD [\"adk\", \"web\", \"--allow_origins\", \"*\"]\n"}, false},
	}
	for _, c := range cases {
		if got := len(anyOriginServerHits(serverFixture(t, c.files))) > 0; got != c.want {
			t.Errorf("AGSRV-001 %s: fire=%v, want %v", c.name, got, c.want)
		}
	}
	for _, c := range []struct {
		name  string
		files map[string]string
		want  bool
	}{
		{"docker run in a # comment", map[string]string{"run.sh": "# docker run -d -p 8010:8000 " + ohImage + " --host 0.0.0.0\n"}, false},
		{"docker run after a # comment", map[string]string{"run.sh": "# start it\ndocker run -d -p 8010:8000 " + ohImage + " --host 0.0.0.0\n"}, true},
	} {
		if got := len(agentServerNoKeyHits(serverFixture(t, c.files))) > 0; got != c.want {
			t.Errorf("AGSRV-002 %s: fire=%v, want %v", c.name, got, c.want)
		}
	}
}
