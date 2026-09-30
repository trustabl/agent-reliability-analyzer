package scanner_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/trustabl/trustabl/internal/models"
	"github.com/trustabl/trustabl/internal/scanner"
)

func sdkPresent(sdks []models.SDK, want models.SDK) bool {
	for _, s := range sdks {
		if s == want {
			return true
		}
	}
	return false
}

// TestReportedSDKsExcludeTestPaths asserts that the SDK list a scan reports
// describes the repository itself, not the sample code the repository vendors
// as scanner test data.
//
// The regression this guards is concrete: scanning the analyzer's own repo — a
// Go repo — reported sdks: [claude_agent_sdk, google_adk, langchain,
// openai_agents], every one of the last three coming from Python fixtures under
// testdata/. That matters beyond tidiness, because /trustabl:fix tells the user
// to sanity-check the inventory before applying any fix; an inventory invented
// from fixtures looks plausible, so the check cannot fire.
//
// The fixture here is a .claude/settings.json that exists ONLY under testdata/,
// so the repo has no production Claude surface at all. --include-test-paths must
// still see it: that direction is what keeps this test from passing vacuously if
// discovery stops finding the file altogether.
func TestReportedSDKsExcludeTestPaths(t *testing.T) {
	repo := t.TempDir()
	settingsDir := filepath.Join(repo, "testdata", "sample-repo", ".claude")
	if err := os.MkdirAll(settingsDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	settings := []byte(`{"permissions":{"allow":["Bash"]},"defaultMode":"bypassPermissions"}`)
	if err := os.WriteFile(filepath.Join(settingsDir, "settings.json"), settings, 0o644); err != nil {
		t.Fatalf("write settings: %v", err)
	}

	cfg := scanner.Config{Target: repo, RulesFS: rulesFixtureFS(t), RulesVersion: "fixedsha"}

	withTests := cfg
	withTests.IncludeTestPaths = true
	inclusive, err := scanner.Run(withTests)
	if err != nil {
		t.Fatalf("run with --include-test-paths: %v", err)
	}
	if !sdkPresent(inclusive.SDKs, models.SDKClaudeAgentSDK) {
		t.Fatalf("--include-test-paths: SDKs = %v, want it to include %q (fixture not discovered at all; the default-mode assertion below would pass for the wrong reason)", inclusive.SDKs, models.SDKClaudeAgentSDK)
	}

	def, err := scanner.Run(cfg)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if sdkPresent(def.SDKs, models.SDKClaudeAgentSDK) {
		t.Errorf("default scan: SDKs = %v, want %q excluded — its only evidence is a settings file under testdata/", def.SDKs, models.SDKClaudeAgentSDK)
	}
}
