package analysis

import "testing"

func TestCodeownersOwned(t *testing.T) {
	cases := []struct {
		file, path string
		want       bool
	}{
		{"* @a", ".github/workflows/x.yml", true},
		{"/.github/workflows/ @a", ".github/workflows/x.yml", true},
		{".github/workflows/ @a", ".github/workflows/x.yml", true},
		{"/.github/ @a", ".github/workflows/x.yml", true},
		{"*.yml @a", ".github/workflows/x.yml", true},
		{"*.md @a", ".github/workflows/x.yml", false},
		{"/docs/ @a", ".github/workflows/x.yml", false},
		{"/CLAUDE.md @a", "CLAUDE.md", true},
		{"/CLAUDE.md @a", "sub/CLAUDE.md", false},
		{"CLAUDE.md @a", "sub/CLAUDE.md", true},
		{"/.github/**/*.yml @a", ".github/workflows/x.yml", true},
		{"* @a\n/.github/workflows/\n", ".github/workflows/x.yml", false},
		{"* @a\n/.github/workflows/ @b\n/src/ @c\n", ".github/workflows/x.yml", true},
		{"# only a comment\n", "CLAUDE.md", false},
		{"[Section]\n* @a # trailing\n", "CLAUDE.md", true},
		{"* a@example.com", "CLAUDE.md", true},
	}
	for _, c := range cases {
		if got := codeownersOwned(parseCodeowners(c.file), c.path); got != c.want {
			t.Errorf("%q on %s: got %v, want %v", c.file, c.path, got, c.want)
		}
	}
}

func TestCodeownersFileOrder(t *testing.T) {
	if got := codeownersFile([]string{"CODEOWNERS", "docs/CODEOWNERS", ".github/CODEOWNERS"}); got != ".github/CODEOWNERS" {
		t.Errorf("got %q", got)
	}
	if got := codeownersFile([]string{"docs/CODEOWNERS", "CODEOWNERS"}); got != "CODEOWNERS" {
		t.Errorf("got %q", got)
	}
	if got := codeownersFile(nil); got != "" {
		t.Errorf("got %q", got)
	}
}
