package analysis

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/trustabl/trustabl/internal/models"
)

// serverFixture writes files under a temp repo and lists them in the manifest
// the way the normalizer does.
func serverFixture(t *testing.T, files map[string]string) models.ScanManifest {
	t.Helper()
	dir := t.TempDir()
	m := models.ScanManifest{RepoRoot: dir}
	for rel, body := range files {
		full := filepath.Join(dir, rel)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
		lower := strings.ToLower(rel)
		base := strings.ToLower(filepath.Base(rel))
		switch {
		case strings.HasSuffix(lower, ".properties"):
			m.PropertiesFiles = append(m.PropertiesFiles, rel)
		case strings.HasSuffix(lower, ".yml"), strings.HasSuffix(lower, ".yaml"):
			m.YAMLFiles = append(m.YAMLFiles, rel)
		case strings.HasSuffix(lower, ".json"):
			m.JSONFiles = append(m.JSONFiles, rel)
		case strings.HasSuffix(lower, ".py"):
			m.PythonFiles = append(m.PythonFiles, rel)
		case strings.HasSuffix(lower, ".ts"):
			m.TypeScriptFiles = append(m.TypeScriptFiles, rel)
		case strings.HasPrefix(base, "dockerfile"), base == "makefile", strings.HasSuffix(lower, ".sh"):
			m.ServerScriptFiles = append(m.ServerScriptFiles, rel)
		}
	}
	return m
}

func TestAnyOriginServer(t *testing.T) {
	cases := []struct {
		name  string
		files map[string]string
		want  bool
	}{
		{"properties wildcard", map[string]string{"src/main/resources/application.properties": "adk.web.cors.origins=*\n"}, true},
		{"properties list with wildcard", map[string]string{"application.properties": "adk.web.cors.origins = http://a, *\n"}, true},
		{"properties named origin", map[string]string{"application.properties": "adk.web.cors.origins=http://localhost:8000\n"}, false},
		{"properties commented", map[string]string{"application.properties": "# adk.web.cors.origins=*\n"}, false},
		{"properties env reference", map[string]string{"application.properties": "adk.web.cors.origins=${ORIGINS}\n"}, false},
		{"yaml nested wildcard", map[string]string{"application.yml": "adk:\n  web:\n    cors:\n      origins: \"*\"\n"}, true},
		{"yaml list wildcard", map[string]string{"application-dev.yaml": "adk:\n  web:\n    cors:\n      origins:\n        - \"*\"\n"}, true},
		{"yaml named", map[string]string{"application.yml": "adk:\n  web:\n    cors:\n      origins: http://localhost:8000\n"}, false},
		{"yaml malformed", map[string]string{"application.yml": "adk: [unclosed\n"}, false},
		{"python wildcard", map[string]string{"serve.py": "from google.adk.cli.fast_api import get_fast_api_app\napp = get_fast_api_app(agents_dir='.', allow_origins=[\"*\"])\n"}, true},
		{"python named", map[string]string{"serve.py": "from google.adk.cli.fast_api import get_fast_api_app\napp = get_fast_api_app(allow_origins=[\"http://localhost:3000\"])\n"}, false},
		{"python test path", map[string]string{"tests/test_x.py": "from google.adk.cli.fast_api import get_fast_api_app\napp = get_fast_api_app(allow_origins=[\"*\"])\n"}, false},
		{"python no adk import", map[string]string{"serve.py": "app = get_fast_api_app(allow_origins=[\"*\"])\n"}, false},
		{"python method form", map[string]string{"s.py": "import google.adk\napp = srv.get_fast_api_app(\n    allow_origins=['*'],\n)\n"}, true},
		{"ts option wildcard", map[string]string{"s.ts": "import {AdkApiServer} from '@google/adk-devtools';\nnew AdkApiServer({allowOrigins: '*'});\n"}, true},
		{"ts option named", map[string]string{"s.ts": "import {AdkApiServer} from '@google/adk-devtools';\nnew AdkApiServer({allowOrigins: 'http://localhost:3000'});\n"}, false},
		{"ts no import", map[string]string{"s.ts": "new AdkApiServer({allowOrigins: '*'});\n"}, false},
		{"makefile equals quoted", map[string]string{"Makefile": "dev:\n\tuv run adk api_server app --allow_origins=\"*\"\n"}, true},
		{"makefile spaced", map[string]string{"Makefile": "dev:\n\tadk web --allow_origins '*'\n"}, true},
		{"makefile named", map[string]string{"Makefile": "dev:\n\tadk web --allow_origins=http://localhost:8000\n"}, false},
		{"dockerfile exec form", map[string]string{"Dockerfile": "CMD [\"adk\", \"web\", \"--allow_origins\", \"*\"]\n"}, true},
		{"dockerfile continuation", map[string]string{"Dockerfile": "CMD adk web \\\n  --allow_origins=*\n"}, true},
		{"ts comma list", map[string]string{"run.sh": "adk web --allow_origins \"http://a.example,*\"\n"}, true},
		{"other command", map[string]string{"run.sh": "other web --allow_origins=*\n"}, false},
		{"compose command", map[string]string{"docker-compose.yml": "services:\n  a:\n    image: x\n    command: adk api_server --allow_origins=*\n"}, true},
		{"compose command list", map[string]string{"compose.yaml": "services:\n  a:\n    command: [\"adk\", \"web\", \"--allow_origins\", \"*\"]\n"}, true},
		{"package.json script", map[string]string{"package.json": "{\"scripts\":{\"dev\":\"adk web --allow_origins=*\"}}"}, true},
		{"package.json malformed", map[string]string{"package.json": "{not json"}, false},
	}
	for _, c := range cases {
		m := serverFixture(t, c.files)
		got := anyOriginServerHits(m)
		if (len(got) > 0) != c.want {
			t.Errorf("%s: got %v, want fire=%v", c.name, got, c.want)
		}
		for _, h := range got {
			if strings.Contains(h.Reason, "http") || strings.Contains(h.Reason, "localhost") {
				t.Errorf("%s: Reason leaks a value: %q", c.name, h.Reason)
			}
		}
	}
}

func TestAgentServerNoKey(t *testing.T) {
	img := "ghcr.io/openhands/agent-server:latest-python"
	// svc builds a compose file: the agent server image, then extra service lines.
	svc := func(extra string) map[string]string {
		return map[string]string{"docker-compose.yml": "services:\n  agent-server:\n    image: " + img + "\n" + extra}
	}
	const wide = "    command: [\"--host\", \"0.0.0.0\"]\n"
	const pub = "    ports:\n      - \"8010:8000\"\n"
	cases := []struct {
		name  string
		files map[string]string
		want  bool
	}{
		// (a) published + (b) wide --host + (c) no key: fires.
		{"all three, list command", svc(wide + pub), true},
		{"all three, host=value form", svc("    command: [\"--host=0.0.0.0\"]\n" + pub), true},
		{"all three, string command", svc("    command: --host 0.0.0.0 --port 8000\n" + pub), true},
		{"all three, entrypoint", svc("    entrypoint: [\"python\", \"-m\", \"openhands.agent_server\", \"--host\", \"0.0.0.0\"]\n" + pub), true},
		{"ipv6 wildcard", svc("    command: [\"--host\", \"::\"]\n" + pub), true},
		{"ipv6 wildcard in brackets", svc("    command: [\"--host\", \"[::]\"]\n" + pub), true},
		{"other non-loopback literal", svc("    command: [\"--host\", \"192.168.1.20\"]\n" + pub), true},
		{"explicit all interfaces mapping", svc(wide + "    ports:\n      - \"0.0.0.0:8010:8000\"\n"), true},
		{"bare container port", svc(wide + "    ports:\n      - 8000\n"), true},
		{"long syntax no host_ip", svc(wide + "    ports:\n      - target: 8000\n        published: 8010\n"), true},
		{"unrelated env", svc(wide + pub + "    environment:\n      - ANTHROPIC_API_KEY\n"), true},
		{"image default form", map[string]string{"compose.yml": "services:\n  s:\n    image: ${IMG:-" + img + "}\n" + wide + pub}, true},
		// (b) fails: no host, loopback host, variable, or a name.
		{"no host set (server binds loopback by default)", svc(pub), false},
		{"command without --host", svc("    command: [\"--port\", \"8000\"]\n" + pub), false},
		{"host loopback", svc("    command: [\"--host\", \"127.0.0.1\"]\n" + pub), false},
		{"host loopback v6", svc("    command: [\"--host\", \"::1\"]\n" + pub), false},
		{"host localhost name", svc("    command: [\"--host\", \"localhost\"]\n" + pub), false},
		{"host from variable", svc("    command: [\"--host\", \"${BIND_HOST}\"]\n" + pub), false},
		{"host variable, equals form", svc("    command: [\"--host=${BIND_HOST}\"]\n" + pub), false},
		{"host other name", svc("    command: [\"--host\", \"example.internal\"]\n" + pub), false},
		{"--host with no value", svc("    command: [\"--host\"]\n" + pub), false},
		{"a different flag with a wide value", svc("    command: [\"--bind\", \"0.0.0.0\"]\n" + pub), false},
		// (a) fails.
		{"loopback mapping", svc(wide + "    ports:\n      - \"127.0.0.1:8010:8000\"\n"), false},
		{"loopback v6 mapping", svc(wide + "    ports:\n      - \"[::1]:8010:8000\"\n"), false},
		{"mapping ip from variable", svc(wide + "    ports:\n      - \"${BIND}:8010:8000\"\n"), false},
		{"long syntax loopback", svc(wide + "    ports:\n      - target: 8000\n        published: 8010\n        host_ip: 127.0.0.1\n"), false},
		{"no ports", svc(wide), false},
		// (c) fails.
		{"key in map env", svc(wide + pub + "    environment:\n      OH_SESSION_API_KEYS_0: ${KEY}\n"), false},
		{"legacy key name in list env", svc(wide + pub + "    environment:\n      - SESSION_API_KEY\n"), false},
		{"env_file not judged", svc(wide + pub + "    env_file: .env\n"), false},
		// Image matching, malformed and unrelated files.
		{"local image", map[string]string{"compose.yml": "services:\n  s:\n    image: agent-server:custom\n" + wide + pub}, false},
		{"lookalike path", map[string]string{"compose.yml": "services:\n  s:\n    image: ghcr.io/openhands/agent-server-extra:1\n" + wide + pub}, false},
		{"malformed yaml", map[string]string{"compose.yml": "services: [unclosed\n"}, false},
		{"not a compose name", map[string]string{"other.yml": "services:\n  s:\n    image: " + img + "\n" + wide + pub}, false},
		// docker run: the host flag must follow the image.
		{"docker run wide host", map[string]string{"run.sh": "docker run -d -p 8010:8000 " + img + " --host 0.0.0.0\n"}, true},
		{"docker run wide host, equals form", map[string]string{"run.sh": "docker run -d -p 8010:8000 " + img + " --host=0.0.0.0 --port 8000\n"}, true},
		{"docker run no host", map[string]string{"run.sh": "docker run -d -p 8010:8000 " + img + "\n"}, false},
		{"docker run loopback host", map[string]string{"run.sh": "docker run -d -p 8010:8000 " + img + " --host 127.0.0.1\n"}, false},
		{"docker run --host before the image is a docker option", map[string]string{"run.sh": "docker --host 0.0.0.0 run -d -p 8010:8000 " + img + "\n"}, false},
		{"docker run loopback mapping", map[string]string{"run.sh": "docker run -d -p 127.0.0.1:8010:8000 " + img + " --host 0.0.0.0\n"}, false},
		{"docker run with key", map[string]string{"run.sh": "docker run -d -p 8010:8000 -e OH_SESSION_API_KEYS_0=$K " + img + " --host 0.0.0.0\n"}, false},
		{"docker run env-file", map[string]string{"run.sh": "docker run -d -p 8010:8000 --env-file .env " + img + " --host 0.0.0.0\n"}, false},
		{"docker run continuation", map[string]string{"run.sh": "docker run -d \\\n  --publish 8010:8000 \\\n  " + img + " \\\n  --host 0.0.0.0\n"}, true},
	}
	for _, c := range cases {
		m := serverFixture(t, c.files)
		got := agentServerNoKeyHits(m)
		if (len(got) > 0) != c.want {
			t.Errorf("%s: got %v, want fire=%v", c.name, got, c.want)
		}
		for _, h := range got {
			if strings.Contains(h.Reason, "8010") || strings.Contains(h.Reason, "KEY=") ||
				strings.Contains(h.Reason, "192.168") {
				t.Errorf("%s: Reason leaks a value: %q", c.name, h.Reason)
			}
		}
	}
}

func TestHostIsNonLoopbackLiteral(t *testing.T) {
	for v, want := range map[string]bool{
		"0.0.0.0": true, "::": true, "[::]": true, "10.0.0.5": true, "192.168.1.20": true,
		"2001:db8::1": true, "127.0.0.1": false, "127.0.0.2": false, "::1": false, "[::1]": false,
		"localhost": false, "": false, "example.internal": false, "${HOST}": false, "$HOST": false,
		"0.0.0.0.0": false,
	} {
		if got := hostIsNonLoopbackLiteral(v); got != want {
			t.Errorf("hostIsNonLoopbackLiteral(%q)=%v, want %v", v, got, want)
		}
	}
}

func TestAgentServerExposure_LineAndDeterminism(t *testing.T) {
	m := serverFixture(t, map[string]string{
		"application.properties": "a=b\nc=d\nadk.web.cors.origins=*\n",
		"docker-compose.yml":     "services:\n  s:\n    image: ghcr.io/openhands/agent-server:x\n    command: [\"--host\", \"0.0.0.0\"]\n    ports: [\"1:2\"]\n",
	})
	f := DiscoverProjectConfig(m, nil)
	if len(f.AnyOriginServer) != 1 || f.AnyOriginServer[0].Line != 3 {
		t.Fatalf("want one hit on line 3, got %+v", f.AnyOriginServer)
	}
	if len(f.AgentServerNoKey) != 1 || f.AgentServerNoKey[0].Line != 3 {
		t.Fatalf("want one compose hit on line 3, got %+v", f.AgentServerNoKey)
	}
	g := DiscoverProjectConfig(m, nil)
	if len(g.AnyOriginServer) != len(f.AnyOriginServer) || g.AnyOriginServer[0] != f.AnyOriginServer[0] {
		t.Fatal("not deterministic")
	}
}

// One compose file with an ADK service and an OpenHands service: one finding each.
func TestAgentServerExposure_BothRulesOneFile(t *testing.T) {
	m := serverFixture(t, map[string]string{"compose.yml": "services:\n" +
		"  adk:\n    image: x\n    command: adk web --allow_origins=*\n" +
		"  oh:\n    image: ghcr.io/openhands/agent-server:x\n    command: [\"--host\", \"0.0.0.0\"]\n    ports: [\"8010:8000\"]\n"})
	f := DiscoverProjectConfig(m, nil)
	if len(f.AnyOriginServer) != 1 || len(f.AgentServerNoKey) != 1 {
		t.Fatalf("want one hit per list, got %+v / %+v", f.AnyOriginServer, f.AgentServerNoKey)
	}
}
