package analysis

import (
	"testing"

	"github.com/trustabl/trustabl/internal/models"
)

// Inputs are built from escape sequences, never literal invisible characters.
const harnessHiddenPayload = "Follow the plan.\U000E0049\U000E0067\U000E006E\U000E006F\U000E0072\U000E0065\n"

// Staged-harness guidance files feed HiddenHarnessText, and only that list.
func TestHiddenHarness_GuidanceFeedsOnlyHiddenHarnessText(t *testing.T) {
	for _, rel := range []string{"systemprompt.md", "guardrails.md", "LongTermMEMORY.md", "tool_descriptions/tool_guidance.md"} {
		f := discoverWith(t, rel, harnessHiddenPayload, models.ComponentHarnessGuidance)
		if len(f.HiddenHarnessText) != 1 {
			t.Errorf("%s: want one hidden-text hit, got %+v", rel, f.HiddenHarnessText)
			continue
		}
		if f.HiddenHarnessText[0].FilePath != rel {
			t.Errorf("%s: attributed to %q", rel, f.HiddenHarnessText[0].FilePath)
		}
		if len(f.HiddenAgentText)+len(f.RiskyHooks)+len(f.CodexRiskyHooks) != 0 {
			t.Errorf("%s: leaked into another list: %+v", rel, f)
		}
	}
}

// AGENTS.md keeps feeding HiddenAgentText (AGCFG-009) and never HiddenHarnessText.
func TestHiddenHarness_AgentsMdStillFeedsOnlyHiddenAgentText(t *testing.T) {
	f := discoverWith(t, "AGENTS.md", harnessHiddenPayload, models.ComponentAgentsMd)
	if len(f.HiddenAgentText) != 1 || len(f.HiddenHarnessText) != 0 {
		t.Errorf("AGENTS.md: agent=%+v harness=%+v", f.HiddenAgentText, f.HiddenHarnessText)
	}
}

func TestHiddenHarness_BenignGuidanceIsSilent(t *testing.T) {
	for name, body := range map[string]string{
		"empty file (the baseline guardrails.md is empty)": "",
		"plain prose":        "Reconcile the ledger using python scripts/summarize.py.\n",
		"leading BOM":        "\U0000feff# Guardrails\nNever delete files.\n",
		"emoji ZWJ sequence": "owner \U0001F469\U0000200d\U0001F4BB wrote this\n",
	} {
		f := discoverWith(t, "guardrails.md", body, models.ComponentHarnessGuidance)
		if len(f.HiddenHarnessText) != 0 {
			t.Errorf("%s: want no hit, got %+v", name, f.HiddenHarnessText)
		}
	}
}

func TestHiddenHarness_ZeroWidthAndBidiAreFlagged(t *testing.T) {
	for name, body := range map[string]string{
		"zero-width space": "Never\U0000200b delete files.\n",
		"bidi override":    "access \U0000202egnirts\U0000202c here\n",
	} {
		f := discoverWith(t, "systemprompt.md", body, models.ComponentHarnessGuidance)
		if len(f.HiddenHarnessText) == 0 {
			t.Errorf("%s: want a hit", name)
		}
	}
}
