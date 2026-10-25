package analysis

import (
	"strings"
	"testing"

	"github.com/trustabl/trustabl/internal/models"
)

// All inputs are built from escape sequences, never from literal invisible
// characters, so this file stays reviewable.
func TestHiddenText_Flagged(t *testing.T) {
	cases := []struct {
		name, body, reason string
	}{
		{"zero-width space inside ASCII", "Run the tests\u200b before committing.\n", hiddenZeroWidthReason},
		{"byte order mark in the middle", "# Title\nbody \ufeff more\n", hiddenZeroWidthReason},
		{"zero-width joiner inside ASCII", "ab\u200dcd\n", hiddenZeroWidthReason},
		{"run of zero-width characters", "\u0645\u200b\u200c\u200b\u200c\u0645\n", hiddenZeroWidthReason},
		{"tags-block smuggled text", "Be helpful.\U000E0049\U000E0067\U000E006E\U000E006F\U000E0072\U000E0065\n", hiddenTagsReason},
		{"tags with no flag base", "x\U000E0067\U000E007F\n", hiddenTagsReason},
		{"bidi override", "access \u202egnirts\u202c here\n", hiddenBidiReason},
		{"bidi isolate", "a \u2066b\u2069 c\n", hiddenBidiReason},
	}
	for _, c := range cases {
		got := hiddenTextHits("AGENTS.md", []byte(c.body))
		var found bool
		for _, h := range got {
			if h.Reason == c.reason {
				found = true
			}
		}
		if !found {
			t.Errorf("%s: want reason %q, got %+v", c.name, c.reason, got)
		}
	}
}

func TestHiddenText_BenignInputsAreSilent(t *testing.T) {
	cases := map[string]string{
		"plain":                      "# Build\n\nRun `make test` before committing.\n",
		"leading BOM":                "\ufeff# Build\nplain text\n",
		"emoji ZWJ sequence":         "owner \U0001F469\u200d\U0001F4BB wrote this\n",
		"family emoji":               "\U0001F468\u200d\U0001F469\u200d\U0001F467\n",
		"persian ZWNJ between":       "\u0645\u06cc\u200c\u062e\u0648\u0627\u0647\u0645\n",
		"subdivision flag emoji":     "England \U0001F3F4\U000E0067\U000E0062\U000E0065\U000E006E\U000E0067\U000E007F\n",
		"ordinary unicode text":      "Caf\u00e9 \u2014 na\u00efve \u65e5\u672c\u8a9e\n",
		"empty":                      "",
		"only a BOM":                 "\ufeff",
		"zero-width char at the end": "text\u200b",
	}
	for name, body := range cases {
		got := hiddenTextHits("AGENTS.md", []byte(body))
		if name == "zero-width char at the end" {
			// A trailing invisible character is not a joiner use: it must be flagged.
			if len(got) == 0 {
				t.Errorf("%s: want a hit", name)
			}
			continue
		}
		if len(got) != 0 {
			t.Errorf("%s: want no hit, got %+v", name, got)
		}
	}
}

func TestHiddenText_LineNumberIsTheFirstOccurrence(t *testing.T) {
	body := "line one\nline two\nline \u200bthree\nline four \u200b\n"
	got := hiddenTextHits("AGENTS.md", []byte(body))
	if len(got) != 1 || got[0].Line != 3 {
		t.Fatalf("want one zero-width hit at line 3, got %+v", got)
	}
	if got[0].FilePath != "AGENTS.md" {
		t.Errorf("file = %q", got[0].FilePath)
	}
}

func TestHiddenText_NeverEchoesTheText(t *testing.T) {
	body := "secret \U000E0049\U000E0067 payload\n"
	for _, h := range hiddenTextHits("AGENTS.md", []byte(body)) {
		if strings.Contains(h.Reason, "payload") || strings.ContainsRune(h.Reason, 0xE0049) {
			t.Errorf("reason echoes content: %q", h.Reason)
		}
	}
}

// DiscoverProjectConfig wiring: AGENTS.md feeds HiddenAgentText, and no other
// project-config list.
func TestProjectConfig_AgentsMdFeedsOnlyHiddenAgentText(t *testing.T) {
	f := discoverWith(t, "AGENTS.md", "plain \u200b text\n", models.ComponentAgentsMd)
	if len(f.HiddenAgentText) != 1 {
		t.Fatalf("want one hidden-text hit, got %+v", f.HiddenAgentText)
	}
	if len(f.RiskyHooks)+len(f.CodexRiskyHooks)+len(f.TrustOverrideEnv)+len(f.LocalMCPLaunch) != 0 {
		t.Errorf("AGENTS.md leaked into another list: %+v", f)
	}
	clean := discoverWith(t, "AGENTS.md", "# plain guidance\n", models.ComponentAgentsMd)
	if len(clean.HiddenAgentText) != 0 {
		t.Errorf("plain AGENTS.md must be silent, got %+v", clean.HiddenAgentText)
	}
}
