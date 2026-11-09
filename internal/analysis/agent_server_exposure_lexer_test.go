package analysis

import "testing"

// Lexer cases for codeMask through the AGSRV-001 Python and TS readers. A call
// whose text sits inside a string, docstring or comment is silent; a real call is
// found even when its "*" argument is itself a string literal, and even when an
// earlier string holds a "#" or "//" that must not start a comment.
func TestAgentServerExposure_Lexer(t *testing.T) {
	call := `get_fast_api_app(allow_origins=["*"])`
	tsCall := `new AdkApiServer({allowOrigins: '*'});`
	py := func(body string) map[string]string { return map[string]string{"s.py": pyAdkImport + body} }
	ts := func(body string) map[string]string { return map[string]string{"s.ts": tsAdkImport + body} }
	cases := []struct {
		name  string
		files map[string]string
		want  bool
	}{
		// Python triple-quoted strings and docstrings, spanning lines.
		{"py triple double, multi-line", py("x = \"\"\"\nline\napp = " + call + "\nmore\n\"\"\"\n"), false},
		{"py triple single, multi-line", py("x = '''\nline\napp = " + call + "\nmore\n'''\n"), false},
		{"py module docstring", py("\"\"\"Usage:\n    " + call + "\n\"\"\"\n"), false},
		{"py function docstring", py("def f():\n    \"\"\"\n    " + call + "\n    \"\"\"\n    return 1\n"), false},
		{"py class docstring single", py("class A:\n    '''\n    " + call + "\n    '''\n"), false},
		{"py triple double holding a lone single quote", py("x = \"\"\"it's\n" + call + "\n\"\"\"\n"), false},
		{"py triple single holding double quotes", py("x = '''say \"hi\"\n" + call + "\n'''\n"), false},
		{"py real call after a multi-line docstring", py("\"\"\"\nabout\n\"\"\"\napp = " + call + "\n"), true},
		{"py real call between two docstrings", py("\"\"\"a\"\"\"\napp = " + call + "\n'''b'''\n"), true},
		// f-strings and raw strings.
		{"py f-string", py("s = f\"{x} " + call + "\"\n"), false},
		{"py f-string single quote", py("s = f'{x} " + call + "'\n"), false},
		{"py raw string", py("s = r\"path\\n " + call + "\"\n"), false},
		{"py raw string with escaped quote", py("s = r\"a\\\" " + call + "\"\n"), false},
		{"py f-triple-quoted", py("s = f\"\"\"\n{x}\n" + call + "\n\"\"\"\n"), false},
		{"py real call after an f-string", py("s = f\"{x}\"\napp = " + call + "\n"), true},
		{"py real call after a raw string", py("s = r\"\\d+\"\napp = " + call + "\n"), true},
		// "#" inside a Python string does not start a comment.
		{"py hash inside a string, call after on the line", py("u = \"a#b\"; app = " + call + "\n"), true},
		{"py hash in single-quoted string, call after", py("u = '#'; app = " + call + "\n"), true},
		{"py hash in string, call on next line", py("u = \"# not a comment\"\napp = " + call + "\n"), true},
		{"py call inside a string that holds a hash", py("u = \"# x " + call + "\"\n"), false},
		// Escaped quotes inside strings.
		{"py escaped double quote then call text in the string", py("s = \"say \\\" " + call + "\"\n"), false},
		{"py escaped single quote then call text in the string", py("s = 'it\\'s " + call + "'\n"), false},
		{"py real call after a string with escaped quotes", py("s = \"say \\\"hi\\\" # x\"\napp = " + call + "\n"), true},
		{"py real call after a string with escaped single quote", py("s = 'it\\'s'\napp = " + call + "\n"), true},
		// A comment holding quotes does not open a string for the next line.
		{"py comment with an apostrophe then real call", py("# it's here\napp = " + call + "\n"), true},
		{"py comment with a triple quote then real call", py("# \"\"\" not a docstring\napp = " + call + "\n"), true},
		// The real call fires with a string-literal "*" argument.
		{"py real call, double-quoted star", py("app = get_fast_api_app(allow_origins=[\"*\"])\n"), true},
		{"py real call, single-quoted star", py("app = get_fast_api_app(allow_origins=['*'])\n"), true},
		{"py real call, multi-line args", py("app = get_fast_api_app(\n    agents_dir=\".\",\n    allow_origins=[\"*\"],\n)\n"), true},

		// TS/JS: "//" inside a string does not start a comment.
		{"ts url string then call on the line", ts("const u = \"http://example.com\"; " + tsCall + "\n"), true},
		{"ts url single-quoted then call", ts("const u = 'http://example.com'; " + tsCall + "\n"), true},
		{"ts url in template then call", ts("const u = `http://example.com`; " + tsCall + "\n"), true},
		{"ts url string, call next line", ts("const u = \"http://example.com\";\n" + tsCall + "\n"), true},
		{"ts call inside a string holding a url", ts("const u = \"http://example.com " + tsCall + "\";\n"), false},
		// Template literals, including multi-line.
		{"ts template, single line", ts("const s = `" + tsCall + "`;\n"), false},
		{"ts template, multi-line", ts("const s = `\nline\n" + tsCall + "\nmore\n`;\n"), false},
		{"ts template with a substitution", ts("const s = `${x} " + tsCall + "`;\n"), false},
		{"ts template holding both quote kinds", ts("const s = `it's \"x\" " + tsCall + "`;\n"), false},
		{"ts real call after a multi-line template", ts("const s = `\na\n`;\n" + tsCall + "\n"), true},
		// Block comments.
		{"ts block comment, single line", ts("/* " + tsCall + " */\n"), false},
		{"ts block comment, multi-line", ts("/*\n * " + tsCall + "\n */\n"), false},
		{"ts block comment holding quotes", ts("/* it's \"x\" " + tsCall + " */\n"), false},
		{"ts block comment holding line-comment marker", ts("/* // " + tsCall + " */\n"), false},
		{"ts real call after a block comment", ts("/* note */\n" + tsCall + "\n"), true},
		{"ts real call after a block comment on the same line", ts("/* note */ " + tsCall + "\n"), true},
		{"ts line comment holding a block opener then real call", ts("// /* not a block\n" + tsCall + "\n"), true},
		{"ts line comment holding a backtick then real call", ts("// ` not a template\n" + tsCall + "\n"), true},
		{"ts comment with an apostrophe then real call", ts("// it's here\n" + tsCall + "\n"), true},
		// Escaped quotes inside strings.
		{"ts escaped double quote then call text", ts("const s = \"say \\\" " + tsCall + "\";\n"), false},
		{"ts escaped single quote then call text", ts("const s = 'it\\'s " + tsCall + "';\n"), false},
		{"ts escaped backtick then call text", ts("const s = `a\\` " + tsCall + "`;\n"), false},
		{"ts real call after a string with escaped quotes", ts("const s = \"say \\\"hi\\\" // x\";\n" + tsCall + "\n"), true},
		// The real call fires with a string-literal "*" argument.
		{"ts real call, single-quoted star", ts("new AdkApiServer({allowOrigins: '*'});\n"), true},
		{"ts real call, double-quoted star", ts("new AdkApiServer({allowOrigins: \"*\"});\n"), true},
		{"ts real call, template star", ts("new AdkApiServer({allowOrigins: `*`});\n"), true},
		{"ts real call, list value", ts("new AdkApiServer({allowOrigins: 'http://a.example,*'});\n"), true},
		{"ts real call, named origin silent", ts("new AdkApiServer({allowOrigins: 'http://a.example'});\n"), false},
	}
	for _, c := range cases {
		if got := len(anyOriginServerHits(serverFixture(t, c.files))) > 0; got != c.want {
			t.Errorf("%s: fire=%v, want %v", c.name, got, c.want)
		}
	}
}

// codeMask itself: delimiters are code, contents are not, and the mask length
// matches the input.
func TestCodeMask(t *testing.T) {
	src := "a = 'x#y' # c\n"
	m := codeMask(src, true)
	if len(m) != len(src) {
		t.Fatalf("mask length %d, want %d", len(m), len(src))
	}
	for i, want := range map[int]bool{0: true, 4: true, 5: false, 6: false, 7: false, 8: true, 10: false, 13: true} {
		if m[i] != want {
			t.Errorf("python mask[%d]=%v, want %v", i, m[i], want)
		}
	}
	src = "x = \"//\"; // c\n"
	m = codeMask(src, false)
	for i, want := range map[int]bool{0: true, 5: false, 6: false, 8: true, 11: false} {
		if m[i] != want {
			t.Errorf("ts mask[%d]=%v, want %v", i, m[i], want)
		}
	}
}
