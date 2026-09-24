package pathclass_test

import (
	"testing"

	"github.com/trustabl/trustabl/internal/models"
	"github.com/trustabl/trustabl/internal/pathclass"
)

func TestClassify(t *testing.T) {
	cases := []struct {
		name string
		path string
		want models.SurfaceOrigin
	}{
		// --- the reported case ---
		{"reported gap", "tests/agentcheck/test_openai_adapter.py", models.OriginTest},

		// --- directory segments, matched at any depth ---
		{"top-level tests dir", "tests/agent.py", models.OriginTest},
		{"top-level test dir singular", "test/agent.py", models.OriginTest},
		{"testing dir", "testing/agent.py", models.OriginTest},
		{"testdata dir", "testdata/agent.py", models.OriginTest},
		{"nested tests dir", "pkg/tests/fixtures/agent.py", models.OriginTest},
		{"jest __tests__ dir", "src/__tests__/tool.spec.ts", models.OriginTest},
		{"__mocks__ dir", "src/__mocks__/client.ts", models.OriginTest},
		{"__fixtures__ dir", "src/__fixtures__/payload.json", models.OriginTest},
		{"fixtures dir", "app/fixtures/agent.py", models.OriginTest},
		{"mocks dir", "internal/mocks/client.go", models.OriginTest},
		{"e2e dir", "e2e/agent_flow.ts", models.OriginTest},
		{"segment case-insensitive", "Tests/Agent.py", models.OriginTest},

		// --- filename patterns, any directory ---
		{"pytest test_ prefix", "src/test_agent.py", models.OriginTest},
		{"pytest _test suffix", "src/agent_test.py", models.OriginTest},
		{"conftest.py", "src/conftest.py", models.OriginTest},
		{"tests.py module", "pkg/tests.py", models.OriginTest},
		{"go _test.go", "pkg/agent_test.go", models.OriginTest},
		{"ts .test.ts", "src/ai/text-splitter.test.ts", models.OriginTest},
		{"ts .test.tsx", "src/component.test.tsx", models.OriginTest},
		{"js .test.js", "src/tool.test.js", models.OriginTest},
		{"ts .spec.ts", "src/tool.spec.ts", models.OriginTest},
		{"tsx .spec.tsx", "src/widget.spec.tsx", models.OriginTest},
		{"mjs .test.mjs", "src/tool.test.mjs", models.OriginTest},
		{"cjs .spec.cjs", "src/tool.spec.cjs", models.OriginTest},
		{"csharp Test.cs", "src/AgentTest.cs", models.OriginTest},
		{"csharp Tests.cs", "src/AgentTests.cs", models.OriginTest},
		{"php Test.php", "src/AgentTest.php", models.OriginTest},
		{"filename case-insensitive", "src/AGENT_TEST.PY", models.OriginTest},

		// --- production paths that must NOT match ---
		{"plain production python", "src/agent.py", ""},
		{"plain production ts", "src/agent.ts", ""},
		{"repo root file", "agent.py", ""},

		// --- deliberately excluded directory names ---
		{"spec dir (not filename)", "spec/openapi.yaml", ""},
		{"integration dir", "integration/payments/adapter.py", ""},
		{"qa dir", "qa/checklist.py", ""},
		{"examples dir", "examples/agent.py", ""},
		{"demo dir", "demo/agent.py", ""},
		{"samples dir", "samples/agent.py", ""},

		// --- near-misses: substring of a test segment, not an exact segment ---
		{"test-utils dir is not exact 'test'", "test-utils/helpers.py", ""},
		{"latest_test_results dir is not exact 'tests'", "latest_test_results/agent.py", ""},
		{"our own fixture-repo corpus dir", "testdata/corpus/claude-settings-fixture/agent.py", models.OriginTest}, // testdata/ itself still matches
		{"fixture repo name alone, no testdata/tests segment", "corpus/claude-settings-fixture/agent.py", ""},

		// --- near-misses: substring of a test filename pattern, not a real match ---
		{"contest.py is not test_*.py or *_test.py", "src/contest.py", ""},
		{"testosterone lacks the underscore test_ prefix needs", "src/testosterone_levels.py", ""},
		{"latest.py does not end in _test.py", "src/latest.py", ""},
		{"protest.py does not end in _test.py", "src/protest.py", ""},
		{"bestquest.go does not end in _test.go", "src/bestquest.go", ""},

		// --- edge cases ---
		{"empty path", "", ""},
		{"no extension in test dir still matches on segment", "tests/README", models.OriginTest},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := pathclass.Classify(tc.path)
			if got != tc.want {
				t.Errorf("Classify(%q) = %q, want %q", tc.path, got, tc.want)
			}
		})
	}
}
