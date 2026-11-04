package analysis

import (
	"encoding/json"
	"path"
	"regexp"
	"strings"

	"github.com/trustabl/trustabl/internal/models"
	"github.com/trustabl/trustabl/internal/pathclass"
)

// AGCFG-015: a script committed to the repo starts a coding-agent command line
// with a flag its vendor documents as turning off approval prompts or the
// sandbox. Three surfaces are read: package.json lifecycle scripts, .husky hook
// files, and JS/TS process-spawn calls. Named package.json scripts, Makefiles,
// shell scripts and workflow run steps are not read.
//
// Secrecy contract: a hit's Reason names the surface, the binary and the flag.
// No other script text is stored or printed.

// scriptLifecycleKeys are the package.json script names that run without the
// developer typing the script's name.
var scriptLifecycleKeys = []string{"preinstall", "install", "postinstall", "prepare"}

// agentBypassPackages maps an npx / bunx / dlx package to the CLI it installs.
var agentBypassPackages = map[string]string{
	"@anthropic-ai/claude-code": "claude",
	"@google/gemini-cli":        "gemini",
	"@openai/codex":             "codex",
}

// agentBypassFlag reports the documented bypass flag among rest for bin. bin is
// claude, gemini, codex, aider or "q". Matching is token-exact: a flag that is
// merely a prefix of another token ("--allow-dangerously-skip-permissions") never
// matches. "q" counts only as "q chat ... --trust-all-tools".
func agentBypassFlag(bin string, rest []string) (string, bool) {
	has := func(t string) bool {
		for _, r := range rest {
			if r == t {
				return true
			}
		}
		return false
	}
	valueOf := func(flag, want string) bool {
		for i, r := range rest {
			if r == flag+"="+want || (r == flag && i+1 < len(rest) && rest[i+1] == want) {
				return true
			}
		}
		return false
	}
	switch bin {
	case "claude":
		if has("--dangerously-skip-permissions") {
			return "--dangerously-skip-permissions", true
		}
		if valueOf("--permission-mode", "bypassPermissions") {
			return "--permission-mode bypassPermissions", true
		}
	case "gemini":
		if has("--yolo") {
			return "--yolo", true
		}
		if valueOf("--approval-mode", "yolo") {
			return "--approval-mode yolo", true
		}
	case "codex":
		if has("--dangerously-bypass-approvals-and-sandbox") {
			return "--dangerously-bypass-approvals-and-sandbox", true
		}
		if has("--yolo") {
			return "--yolo", true
		}
	case "aider":
		if has("--yes-always") {
			return "--yes-always", true
		}
	case "q":
		if len(rest) > 0 && rest[0] == "chat" && has("--trust-all-tools") {
			return "--trust-all-tools", true
		}
	}
	return "", false
}

// agentBypassInShell returns the binary and flag when one shell segment starts
// a listed agent CLI with a bypass flag. The command word must be the first word
// of the segment, after env assignments, env, exec, time, sudo or a package
// runner, so `echo claude --yolo` is not a match. "q" is reported as "q chat".
func agentBypassInShell(seg string) (bin, flag string, ok bool) {
	f := shellFields(seg)
	i := 0
	for i < len(f) && ((strings.Contains(f[i], "=") && !strings.HasPrefix(f[i], "-")) ||
		f[i] == "env" || f[i] == "exec" || f[i] == "time" || f[i] == "sudo") {
		i++
	}
	if i >= len(f) {
		return "", "", false
	}
	word := path.Base(f[i])
	switch word {
	case "npx", "bunx", "pnpm", "yarn", "npm":
		j := i + 1
		for j < len(f) && (strings.HasPrefix(f[j], "-") || f[j] == "dlx" || f[j] == "exec") {
			j++
		}
		if j >= len(f) {
			return "", "", false
		}
		pkg := f[j]
		if k := strings.LastIndex(pkg, "@"); k > 0 {
			pkg = pkg[:k]
		}
		cli, known := agentBypassPackages[pkg]
		if !known {
			return "", "", false
		}
		word, i = cli, j
	}
	flagFound, found := agentBypassFlag(word, f[i+1:])
	if !found {
		return "", "", false
	}
	if word == "q" {
		word = "q chat"
	}
	return word, flagFound, true
}

// agentBypassInScript scans a multi-line shell script text and returns the first
// bypass found. Lines that begin with # are comments.
func agentBypassInScript(text string) (bin, flag string, ok bool) {
	text = strings.ReplaceAll(text, "\\\n", " ")
	for _, line := range strings.Split(text, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "#") {
			continue
		}
		for _, seg := range ciSegmentSplitRe.Split(line, -1) {
			if b, fl, hit := agentBypassInShell(seg); hit {
				return b, fl, true
			}
		}
	}
	return "", "", false
}

// scriptAgentBypassHits reads package.json lifecycle scripts, .husky hook files
// and JS/TS spawn calls in the manifest and returns one hit per offending script.
func scriptAgentBypassHits(manifest models.ScanManifest) []models.ConfigHit {
	root := manifest.RepoRoot
	var out []models.ConfigHit
	for _, rel := range manifest.JSONFiles {
		if path.Base(rel) != "package.json" {
			continue
		}
		raw := readConfig(root, rel)
		var doc struct {
			Scripts map[string]any `json:"scripts"`
		}
		if len(raw) == 0 || json.Unmarshal(raw, &doc) != nil {
			continue
		}
		start := strings.Index(string(raw), `"scripts"`)
		for _, key := range scriptLifecycleKeys {
			s, ok := doc.Scripts[key].(string)
			if !ok {
				continue
			}
			bin, flag, hitOK := agentBypassInScript(s)
			if !hitOK {
				continue
			}
			line := 1
			if start >= 0 {
				line = lineOfText(raw[start:], `"`+key+`"`) + strings.Count(string(raw[:start]), "\n")
			}
			out = append(out, hit(rel, line,
				`package.json script "`+key+`" runs `+bin+` with `+flag))
		}
	}
	for _, rel := range manifest.HuskyFiles {
		raw := readConfig(root, rel)
		for n, l := range strings.Split(string(raw), "\n") {
			if bin, flag, ok := agentBypassInScript(l); ok {
				out = append(out, hit(rel, n+1, ".husky hook runs "+bin+" with "+flag))
			}
		}
	}
	for _, rels := range [][]string{manifest.JavaScriptFiles, manifest.TypeScriptFiles} {
		for _, rel := range rels {
			if pathclass.Classify(rel) == models.OriginTest {
				continue
			}
			out = append(out, spawnAgentBypassHits(rel, readConfig(root, rel))...)
		}
	}
	return sortHits(out)
}

// jsSpawnCall finds a process-spawn call: child_process spawn, spawnSync, exec,
// execSync, execFile, execFileSync, and Bun.spawn / Bun.spawnSync.
var jsSpawnCall = regexp.MustCompile(`(?:^|[^\w.$])((?:(?:child_process|cp|childProcess)\.)?` +
	`(?:spawnSync|spawn|execFileSync|execFile|execSync|exec)|Bun\.spawnSync|Bun\.spawn)\s*\(`)

// jsFlagNeedles are the strings a file must contain before it is parsed.
var jsFlagNeedles = []string{
	"--dangerously-skip-permissions", "bypassPermissions", "--yolo", "yolo",
	"--dangerously-bypass-approvals-and-sandbox", "--yes-always", "--trust-all-tools",
}

// spawnAgentBypassHits reports a spawn call in one JS/TS file whose binary and a
// bypass flag are both string literals in the same call: a command string, an
// args array after a literal binary, or a Bun.spawn command array. A binary or
// flag held in a variable is not seen.
func spawnAgentBypassHits(rel string, src []byte) []models.ConfigHit {
	text := string(src)
	found := false
	for _, n := range jsFlagNeedles {
		if strings.Contains(text, n) {
			found = true
			break
		}
	}
	if !found {
		return nil
	}
	var out []models.ConfigHit
	for _, m := range jsSpawnCall.FindAllStringSubmatchIndex(text, -1) {
		args, ok := balancedArgs(text, m[1]-1)
		if !ok {
			continue
		}
		bin, flag, hitOK := agentBypassInSpawnArgs(args)
		if !hitOK {
			continue
		}
		line := strings.Count(text[:m[2]], "\n") + 1
		out = append(out, hit(rel, line, "process-spawn call runs "+bin+" with "+flag))
	}
	return out
}

// balancedArgs returns the text between the parenthesis at text[open] and its
// match, honouring quotes. It gives up after 4000 bytes.
func balancedArgs(text string, open int) (string, bool) {
	depth := 0
	var quote byte
	end := open + 4000
	if end > len(text) {
		end = len(text)
	}
	for j := open; j < end; j++ {
		c := text[j]
		switch {
		case quote != 0:
			if c == '\\' {
				j++
			} else if c == quote {
				quote = 0
			}
		case c == '"' || c == '\'' || c == '`':
			quote = c
		case c == '(' || c == '[' || c == '{':
			depth++
		case c == ')' || c == ']' || c == '}':
			depth--
			if depth == 0 {
				return text[open+1 : j], true
			}
		}
	}
	return "", false
}

// splitTopLevel splits s on commas that sit outside quotes and brackets.
func splitTopLevel(s string) []string {
	var parts []string
	depth := 0
	var quote byte
	start := 0
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case quote != 0:
			if c == '\\' {
				i++
			} else if c == quote {
				quote = 0
			}
		case c == '"' || c == '\'' || c == '`':
			quote = c
		case c == '(' || c == '[' || c == '{':
			depth++
		case c == ')' || c == ']' || c == '}':
			depth--
		case c == ',' && depth == 0:
			parts = append(parts, strings.TrimSpace(s[start:i]))
			start = i + 1
		}
	}
	return append(parts, strings.TrimSpace(s[start:]))
}

// jsLiteral returns the contents of s when s is one quoted string. interp keeps
// a template literal whose ${...} parts are replaced by a NUL byte.
func jsLiteral(s string, interp bool) (string, bool) {
	if len(s) < 2 || s[0] != s[len(s)-1] || (s[0] != '"' && s[0] != '\'' && s[0] != '`') {
		return "", false
	}
	body := s[1 : len(s)-1]
	if strings.Contains(body, "${") {
		if !interp || s[0] != '`' {
			return "", false
		}
		body = jsInterp.ReplaceAllString(body, "\x00")
	}
	return body, true
}

var jsInterp = regexp.MustCompile(`\$\{[^}]*\}`)

// agentBypassInSpawnArgs judges the argument text of one spawn call.
func agentBypassInSpawnArgs(args string) (bin, flag string, ok bool) {
	parts := splitTopLevel(args)
	if len(parts) == 0 {
		return "", "", false
	}
	var first string
	var rest []string
	if strings.HasPrefix(parts[0], "[") && strings.HasSuffix(parts[0], "]") {
		// Bun.spawn([cmd, ...args])
		elems := splitTopLevel(parts[0][1 : len(parts[0])-1])
		for i, e := range elems {
			v, lit := jsLiteral(e, false)
			if i == 0 {
				if !lit {
					return "", "", false
				}
				first = v
				continue
			}
			if lit {
				rest = append(rest, v)
			} else {
				rest = append(rest, "\x00")
			}
		}
	} else {
		v, lit := jsLiteral(parts[0], true)
		if !lit || strings.HasPrefix(v, "\x00") {
			return "", "", false
		}
		toks := strings.Fields(v)
		if len(toks) == 0 {
			return "", "", false
		}
		first = toks[0]
		if len(toks) > 1 || len(parts) == 1 {
			rest = toks[1:] // command-string form
		} else if len(parts) > 1 && strings.HasPrefix(parts[1], "[") && strings.HasSuffix(parts[1], "]") {
			for _, e := range splitTopLevel(parts[1][1 : len(parts[1])-1]) {
				if s, lit := jsLiteral(e, false); lit {
					rest = append(rest, s)
				} else {
					rest = append(rest, "\x00")
				}
			}
		}
	}
	b := path.Base(first)
	flagFound, found := agentBypassFlag(b, rest)
	if !found {
		return "", "", false
	}
	if b == "q" {
		b = "q chat"
	}
	return b, flagFound, true
}
