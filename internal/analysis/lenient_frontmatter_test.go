package analysis_test

import (
	"reflect"
	"strings"
	"testing"

	"github.com/trustabl/trustabl/internal/analysis"
	"github.com/trustabl/trustabl/internal/models"
)

// oneSkill writes a SKILL.md whose frontmatter is name plus extra and returns
// what discovery found (zero or one skill).
func oneSkill(t *testing.T, extra string) []models.SkillDef {
	t.Helper()
	dir := t.TempDir()
	writeFixture(t, dir, ".claude/skills/x/SKILL.md", "---\nname: x\n"+extra+"---\n\nbody\n")
	return analysis.DiscoverSkills(models.ScanManifest{
		RepoRoot:      dir,
		MarkdownFiles: []string{".claude/skills/x/SKILL.md"},
	})
}

func oneCommand(t *testing.T, fm string) models.SlashCommandDef {
	t.Helper()
	dir := t.TempDir()
	writeFixture(t, dir, ".claude/commands/x.md", "---\n"+fm+"---\n\nbody\n")
	got := analysis.DiscoverSlashCommands(models.ScanManifest{
		RepoRoot:   dir,
		Components: []models.AgentComponent{{Kind: models.ComponentSlashCommand, Path: ".claude/commands/x.md"}},
	})
	if len(got) != 1 {
		t.Fatalf("got %d commands, want 1", len(got))
	}
	return got[0]
}

// spellings is every scalar text the boolean reader knows. Each is tried
// bare, double-quoted, single-quoted, lower, Title and UPPER case.
var trueSpellings = []string{"true", "yes", "on", "y", "1"}
var falseSpellings = []string{"false", "no", "off", "n", "0"}

func forms(word string) []string {
	titled := strings.ToUpper(word[:1]) + word[1:]
	var out []string
	for _, w := range []string{word, titled, strings.ToUpper(word)} {
		out = append(out, w, `"`+w+`"`, `'`+w+`'`)
	}
	return out
}

func TestLenientBool_DisableModelInvocationSpellings(t *testing.T) {
	for _, w := range trueSpellings {
		for _, f := range forms(w) {
			t.Run("true/"+f, func(t *testing.T) {
				got := oneSkill(t, "disable-model-invocation: "+f+"\n")
				if len(got) != 1 {
					t.Fatalf("skill dropped for %s", f)
				}
				if !got[0].DisableModelInvocation {
					t.Errorf("%s read false, want true", f)
				}
			})
		}
	}
	for _, w := range falseSpellings {
		for _, f := range forms(w) {
			t.Run("false/"+f, func(t *testing.T) {
				got := oneSkill(t, "disable-model-invocation: "+f+"\n")
				if len(got) != 1 {
					t.Fatalf("skill dropped for %s", f)
				}
				if got[0].DisableModelInvocation {
					t.Errorf("%s read true, want false", f)
				}
			})
		}
	}
}

func TestLenientBool_DisableModelInvocationUnexpectedKeepsSkill(t *testing.T) {
	for _, v := range []string{"maybe", `"maybe"`, "[a]", "{a: b}", "", "2", "tru", "1.0"} {
		t.Run(v, func(t *testing.T) {
			got := oneSkill(t, "description: keep me\nallowed-tools: Read\ndisable-model-invocation: "+v+"\n")
			if len(got) != 1 {
				t.Fatalf("skill dropped for unexpected value %q", v)
			}
			if got[0].DisableModelInvocation {
				t.Errorf("unexpected value %q read true, want false", v)
			}
			if got[0].Description != "keep me" || !reflect.DeepEqual(got[0].AllowedTools, []string{"Read"}) {
				t.Errorf("other fields lost: %q %v", got[0].Description, got[0].AllowedTools)
			}
		})
	}
}

func TestLenientBool_UserInvocableTriState(t *testing.T) {
	for _, w := range trueSpellings {
		for _, f := range forms(w) {
			got := oneSkill(t, "user-invocable: "+f+"\n")
			if len(got) != 1 || got[0].UserInvocable == nil || !*got[0].UserInvocable {
				t.Errorf("user-invocable: %s want true, got %v", f, got)
			}
		}
	}
	for _, w := range falseSpellings {
		for _, f := range forms(w) {
			got := oneSkill(t, "user-invocable: "+f+"\n")
			if len(got) != 1 || got[0].UserInvocable == nil || *got[0].UserInvocable {
				t.Errorf("user-invocable: %s want false, got %v", f, got)
			}
		}
	}
	// Absent, null and unrecognised values stay unset: the default is true, so
	// a value we cannot read must not claim false.
	for _, extra := range []string{
		"", "user-invocable:\n", "user-invocable: ~\n", "user-invocable: maybe\n",
		"user-invocable: \"maybe\"\n", "user-invocable: [a]\n", "user-invocable: {a: b}\n",
	} {
		got := oneSkill(t, extra)
		if len(got) != 1 {
			t.Fatalf("skill dropped for %q", extra)
		}
		if got[0].UserInvocable != nil {
			t.Errorf("%q: UserInvocable = %v, want nil", extra, *got[0].UserInvocable)
		}
	}
}

func TestLenientBool_QuotedFlagSkillIsDiscovered(t *testing.T) {
	// The reported bug: a quoted boolean dropped the whole skill, so no CSKILL
	// rule judged its allowed-tools.
	got := oneSkill(t, "description: d\nallowed-tools: Bash\ndisable-model-invocation: \"true\"\n")
	if len(got) != 1 {
		t.Fatal("skill with a quoted boolean was not discovered")
	}
	if !got[0].DisableModelInvocation || len(got[0].ToolGrants) != 1 || got[0].ToolGrants[0].Tool != "Bash" {
		t.Errorf("got dmi=%v grants=%+v", got[0].DisableModelInvocation, got[0].ToolGrants)
	}
}

func TestSkills_ArgumentHintStringOrList(t *testing.T) {
	cases := []struct {
		fm, want string
	}{
		{`argument-hint: "[env]"` + "\n", "[env]"},
		{"argument-hint: plain words\n", "plain words"},
		// Claude Code's own docs example: YAML reads the brackets as a list.
		{"argument-hint: [issue-number]\n", "issue-number"},
		{"argument-hint: [filename, format]\n", "filename format"},
		{"argument-hint: [\"[path] [--full]\"]\n", "[path] [--full]"},
		{"argument-hint: {a: b}\n", ""},
		{"argument-hint: [[a], b]\n", "b"},
		{"argument-hint:\n", ""},
	}
	for _, c := range cases {
		got := oneSkill(t, "description: d\n"+c.fm)
		if len(got) != 1 {
			t.Errorf("%q: skill dropped", c.fm)
			continue
		}
		if got[0].ArgumentHint != c.want {
			t.Errorf("%q: ArgumentHint = %q, want %q", c.fm, got[0].ArgumentHint, c.want)
		}
		if got[0].Description != "d" {
			t.Errorf("%q: description lost", c.fm)
		}
	}
}

func TestSkills_OtherInvalidFieldsStillSkipThem(t *testing.T) {
	// Out of scope for the tolerant types: a list or map in a string or list
	// field still fails the decode and the skill is skipped, as before. The
	// YAML-syntax case is a separate follow-up.
	for _, extra := range []string{
		"name: [z]\n", "description: [a, b]\n", "context: [x]\n", "allowed-tools: {a: b}\n",
		"description: has: colon\n",
	} {
		if got := oneSkill(t, extra); len(got) != 0 {
			t.Errorf("%q: skill kept, want skipped", extra)
		}
	}
}

func TestSlashCommands_TolerantFields(t *testing.T) {
	for _, w := range trueSpellings {
		for _, f := range forms(w) {
			c := oneCommand(t, "description: d\nallowed-tools: Bash\ndisable-model-invocation: "+f+"\n")
			if !c.DisableModelInvocation || c.Description != "d" || !reflect.DeepEqual(c.AllowedTools, []string{"Bash"}) {
				t.Errorf("%s: dmi=%v desc=%q tools=%v", f, c.DisableModelInvocation, c.Description, c.AllowedTools)
			}
		}
	}
	for _, f := range []string{"false", `"false"`, "0", "maybe", "[a]"} {
		c := oneCommand(t, "description: d\nallowed-tools: Bash\ndisable-model-invocation: "+f+"\n")
		if c.DisableModelInvocation || c.Description != "d" || !reflect.DeepEqual(c.AllowedTools, []string{"Bash"}) {
			t.Errorf("%s: dmi=%v desc=%q tools=%v", f, c.DisableModelInvocation, c.Description, c.AllowedTools)
		}
	}
	c := oneCommand(t, "description: d\nallowed-tools: Bash\nargument-hint: [issue-number]\n")
	if c.ArgumentHint != "issue-number" || c.Description != "d" || len(c.AllowedTools) != 1 {
		t.Errorf("list argument-hint dropped fields: %+v", c)
	}
}
