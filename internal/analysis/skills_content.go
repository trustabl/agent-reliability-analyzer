package analysis

import (
	"bytes"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/trustabl/trustabl/internal/models"
)

// This file holds the content facts behind CSKILL-088 (decode and execute),
// CSKILL-089 (destructive command), CSKILL-090 (opaque bundled file) and
// CSKILL-091 (unpinned install). Each fact is read from a bundled file, never
// stored as text; the inventory stays an index of facts.

// ── comment stripping ───────────────────────────────────────────────────────

var (
	// hashCommentRe matches a "#" comment that starts a line or follows
	// whitespace, so "$#", "${#x}" and a URL fragment are left alone.
	hashCommentRe = regexp.MustCompile(`(?m)(?:^|[ \t])#.*$`)
	// slashCommentRe matches a "//" comment that starts a line or follows
	// whitespace, so "https://" is left alone.
	slashCommentRe = regexp.MustCompile(`(?m)(?:^|[ \t])//.*$`)
	blockCommentRe = regexp.MustCompile(`(?s)/\*.*?\*/`)
)

// stripScriptComments blanks the comment text of a bundled script so a command
// named only in a comment is not read as a call. Python, shell, Ruby, Perl and
// PowerShell use "#"; the JavaScript and TypeScript family uses "//" and "/* */".
func stripScriptComments(name string, content []byte) []byte {
	switch strings.ToLower(filepath.Ext(name)) {
	case ".js", ".mjs", ".cjs", ".ts", ".php":
		content = blockCommentRe.ReplaceAll(content, nil)
		return slashCommentRe.ReplaceAll(content, nil)
	case ".bat", ".cmd", ".lua":
		return content
	}
	return hashCommentRe.ReplaceAll(content, nil)
}

// ── CSKILL-088: decode a payload and run it ─────────────────────────────────

var (
	decodePipeShellRe = regexp.MustCompile(`\bbase64\b[^\n|;&]*?\s(?:--decode|-[a-zA-Z]*[dD][a-zA-Z]*)\b[^\n|;&]*\|\s*(?:sudo\s+)?(?:/usr/bin/|/bin/)?(?:env\s+)?(?:(?:ba|z|da|k)?sh|python[0-9.]*|perl|ruby|node|php|pwsh)\b`)
	decodeEvalShellRe = regexp.MustCompile(`\b(?:eval|source)\b[^\n]*\bbase64\b[^\n]*\s(?:--decode|-[a-zA-Z]*[dD][a-zA-Z]*)\b|\b(?:ba|z|da|k)?sh\s+<\(\s*base64\b[^\n)]*\s(?:--decode|-[a-zA-Z]*[dD][a-zA-Z]*)\b`)
	decodeEvalCodeRe  = regexp.MustCompile(`\b(?:eval|exec)\s*\([^\n]*?\b(?:b64decode|b32decode|b85decode|a85decode|decodebytes|decompress|fromhex|unhexlify|atob|codecs\.decode)\b|\bnew\s+Function\s*\([^\n]*?\batob\b|\b(?:eval|exec)\s*\([^\n]*?Buffer\.from\([^\n]*?base64`)
)

func scriptDecodesAndExecutes(code []byte) bool {
	return decodePipeShellRe.Match(code) || decodeEvalShellRe.Match(code) || decodeEvalCodeRe.Match(code)
}

// ── CSKILL-089: destructive host command ────────────────────────────────────

var (
	// destructiveRmRe matches rm with a recursive flag whose target is exactly
	// the filesystem root or the home directory (/, /*, ~, ~/, ~/*, $HOME,
	// ${HOME}, with an optional /*). A relative or nested target never matches.
	destructiveRmRe   = regexp.MustCompile(`\brm\b[^\n;&|]*?\s(?:-[a-zA-Z]*[rR][a-zA-Z]*|--recursive)\b[^\n;&|]*?\s["']?(?:/|~|\$HOME|\$\{HOME\})(?:/?\*)?["']?(?:\s|$|[;&|])`)
	destructiveMkfsRe = regexp.MustCompile(`(?:^|[\s;&|(])(?:sudo\s+)?mkfs(?:\.[a-z0-9]+)?\s+\S`)
	destructiveDDRe   = regexp.MustCompile(`\bdd\b[^\n]*\bof=/dev/(?:sd|hd|vd|xvd|nvme|mmcblk|disk|rdisk)`)
	destructiveShred  = regexp.MustCompile(`\bshred\b[^\n]*\s/dev/(?:sd|hd|vd|xvd|nvme|mmcblk|disk|rdisk)`)
	forkBombRe        = regexp.MustCompile(`:\(\)\s*\{\s*:\s*\|\s*:\s*&\s*\}\s*;\s*:`)
)

func scriptDestructiveCommand(code []byte) bool {
	return destructiveRmRe.Match(code) || destructiveMkfsRe.Match(code) || destructiveDDRe.Match(code) ||
		destructiveShred.Match(code) || forkBombRe.Match(code)
}

// ── CSKILL-090: opaque bundled file ─────────────────────────────────────────

// opaqueHeadBytes is how much of a bundled file the magic-number check reads.
const opaqueHeadBytes = 512

// officeZipExts are zip containers that hold documents, not code. They are
// skipped so a skill that ships a .docx or .xlsx template is not read as
// shipping an archive.
var officeZipExts = map[string]bool{
	".docx": true, ".xlsx": true, ".pptx": true, ".dotx": true, ".xltx": true, ".potx": true,
	".odt": true, ".ods": true, ".odp": true, ".epub": true, ".vsix": true,
}

// opaqueKind returns the kind of opaque content a bundled file holds, or "".
// The check reads the file name and the first bytes only. A zip is judged by
// its magic number, apart from the document containers in officeZipExts.
func opaqueKind(name string, head []byte) string {
	ext := strings.ToLower(filepath.Ext(name))
	switch ext {
	case ".pyc", ".pyo":
		return "bytecode"
	case ".wasm":
		return "webassembly"
	case ".so", ".dll", ".dylib", ".exe", ".node", ".o", ".a", ".class", ".jar":
		return "native binary"
	case ".zip", ".tar", ".gz", ".tgz", ".bz2", ".xz", ".7z", ".rar", ".whl":
		return "archive"
	}
	switch {
	case bytes.HasPrefix(head, []byte{0x7f, 'E', 'L', 'F'}):
		return "native binary"
	case bytes.HasPrefix(head, []byte{0xfe, 0xed, 0xfa, 0xce}), bytes.HasPrefix(head, []byte{0xfe, 0xed, 0xfa, 0xcf}),
		bytes.HasPrefix(head, []byte{0xce, 0xfa, 0xed, 0xfe}), bytes.HasPrefix(head, []byte{0xcf, 0xfa, 0xed, 0xfe}):
		return "native binary"
	case bytes.HasPrefix(head, []byte{0xca, 0xfe, 0xba, 0xbe}):
		return "native binary"
	case bytes.HasPrefix(head, []byte{'M', 'Z'}) && peSignature(head):
		return "native binary"
	case bytes.HasPrefix(head, []byte{0x00, 'a', 's', 'm'}):
		return "webassembly"
	case bytes.HasPrefix(head, []byte{0x1f, 0x8b}), bytes.HasPrefix(head, []byte{'7', 'z', 0xbc, 0xaf, 0x27, 0x1c}),
		bytes.HasPrefix(head, []byte("Rar!\x1a\x07")), bytes.HasPrefix(head, []byte{0xfd, '7', 'z', 'X', 'Z', 0x00}),
		bytes.HasPrefix(head, []byte("BZh")) && len(head) > 3 && head[3] >= '1' && head[3] <= '9':
		return "archive"
	case len(head) >= 262 && bytes.Equal(head[257:262], []byte("ustar")):
		return "archive"
	case bytes.HasPrefix(head, []byte{'P', 'K', 0x03, 0x04}) && !officeZipExts[ext]:
		return "archive"
	}
	return ""
}

// peSignature reports whether head, which starts with "MZ", points at a
// "PE\0\0" header through the e_lfanew field at offset 0x3c.
func peSignature(head []byte) bool {
	if len(head) < 0x40 {
		return false
	}
	off := int(head[0x3c]) | int(head[0x3d])<<8
	return off+4 <= len(head) && bytes.Equal(head[off:off+4], []byte("PE\x00\x00"))
}

// ── CSKILL-091: install without an exact version ────────────────────────────

var (
	curlPipeShellRe = regexp.MustCompile(`\b(?:curl|wget)\b[^\n|;&]*\|\s*(?:sudo\s+(?:-[a-zA-Z]+\s+)*)?(?:(?:ba|z|da|k)?sh)\b|\b(?:ba|z|da|k)?sh\s+<\(\s*(?:curl|wget)\b|\bsource\s+<\(\s*(?:curl|wget)\b`)
	goInstallRe     = regexp.MustCompile(`\bgo\s+install\s+(?:-[a-zA-Z0-9=._/-]+\s+)*[^\s"'@]+@latest\b`)
	// installCmdRe finds the start of an install command anywhere on a line, so a
	// command inside os.system("...") or a backtick span is found too. Group 1 is
	// the runner words and group 2 the verb.
	installCmdRe = regexp.MustCompile(`(?:^|[\s"'(` + "`" + `;&|])(pip3?|python[0-9.]*\s+-m\s+pip|uv\s+pip|pipx|uv\s+tool|npm|yarn(?:\s+global)?|pnpm|bun)\s+(install|i|add)\b`)
	// runnerCmdRe finds a one-shot package runner (npx, bunx, pnpm dlx, uvx,
	// uv tool run, pipx run).
	runnerCmdRe = regexp.MustCompile(`(?:^|[\s"'(` + "`" + `;&|])(npx|bunx|pnpm\s+dlx|uvx|uv\s+tool\s+run|pipx\s+run)\s+`)
	lineEndRe   = regexp.MustCompile("&&|\\|\\||[;|)`\n]")
)

// pipValueFlags and npmInstallValueFlags are the install flags that consume the
// next argument; the checks skip it so it is not read as a package.
var (
	pipValueFlags = map[string]bool{
		"-r": true, "--requirement": true, "-c": true, "--constraint": true, "-e": true, "--editable": true,
		"-i": true, "--index-url": true, "--extra-index-url": true, "-f": true, "--find-links": true,
		"-t": true, "--target": true, "--prefix": true, "--root": true, "--python": true, "--platform": true,
		"--only-binary": true, "--no-binary": true, "--implementation": true, "--abi": true, "--python-version": true,
		"--from": true, "--with": true, "--spec": true,
	}
	npmInstallValueFlags = map[string]bool{
		"--prefix": true, "--registry": true, "--cache": true, "-w": true, "--workspace": true, "--userconfig": true,
	}
)

// installArgs returns the arguments that follow an install verb up to the end
// of the command, with quotes removed.
func installArgs(rest string) []string {
	if loc := lineEndRe.FindStringIndex(rest); loc != nil {
		rest = rest[:loc[0]]
	}
	rest = strings.NewReplacer(`"`, "", `'`, "").Replace(rest)
	return strings.Fields(rest)
}

// unpinnedInstallArgs reports whether some package named in args has no exact
// version, using judge for the ecosystem. A flag in valueFlags consumes the
// next argument. Anything that is not a registry name is not judged.
func unpinnedInstallArgs(args []string, valueFlags map[string]bool, judge func(string) (bool, bool)) bool {
	for i := 0; i < len(args); i++ {
		a := args[i]
		if strings.HasPrefix(a, "-") {
			if name, _, hasEq := strings.Cut(a, "="); valueFlags[name] && !hasEq {
				i++
			}
			continue
		}
		if pinned, judged := judge(a); judged && !pinned {
			return true
		}
	}
	return false
}

// execCallRe marks a line that hands a command string to a process API, so an
// install command inside its quotes is a command the script runs.
var execCallRe = regexp.MustCompile(`\b(?:os\.system|os\.popen|subprocess\.\w+|check_call|check_output|Popen|execSync|spawnSync|spawn|execFile|exec|system)\s*\(`)

// installLeadRe matches the start of a line that begins with a command that can
// carry an install: an optional "$ ", "sudo", "!" or "time", then the program.
var installLeadRe = regexp.MustCompile(`^(?:[$!]\s*)?(?:sudo\s+(?:-[a-zA-Z]+\s+)*)?(?:time\s+)?(?:pip3?|python[0-9.]*|uv|pipx|npm|yarn|pnpm|bun|npx|bunx|uvx|go|curl|wget|bash|sh|source)\b`)

// textUnpinnedInstall reports whether the (comment-stripped) text holds an
// install command that names a package with no exact version, a one-shot
// package runner on such a package, a go install of @latest, or a remote script
// piped to a shell. With script set, a line counts when it starts with the
// command or passes it to a process API, so an install command that only appears
// in a message, a log line, an assertion or a list item is not judged.
func textUnpinnedInstall(code []byte, script bool) bool {
	for _, line := range strings.Split(string(code), "\n") {
		if script {
			t := strings.TrimSpace(line)
			if !installLeadRe.MatchString(t) && !execCallRe.MatchString(t) {
				continue
			}
		}
		if lineUnpinnedInstall(line) {
			return true
		}
	}
	return false
}

func lineUnpinnedInstall(line string) bool {
	if curlPipeShellRe.MatchString(line) || goInstallRe.MatchString(line) {
		return true
	}
	for _, m := range installCmdRe.FindAllStringSubmatchIndex(line, -1) {
		runner := strings.Join(strings.Fields(line[m[2]:m[3]]), " ")
		args := installArgs(line[m[1]:])
		switch {
		case strings.HasPrefix(runner, "pip"), strings.HasPrefix(runner, "python"), runner == "uv pip", runner == "pipx":
			if unpinnedInstallArgs(args, pipValueFlags, func(sp string) (bool, bool) { return pySpecPinned(sp, false) }) {
				return true
			}
		case runner == "uv tool":
			if unpinnedInstallArgs(args, pipValueFlags, func(sp string) (bool, bool) { return pySpecPinned(sp, true) }) {
				return true
			}
		default: // npm, yarn, pnpm, bun
			if unpinnedInstallArgs(args, npmInstallValueFlags, npmSpecPinned) {
				return true
			}
		}
	}
	for _, m := range runnerCmdRe.FindAllStringSubmatchIndex(line, -1) {
		words := strings.Fields(line[m[2]:m[3]])
		rest := strings.Fields(strings.NewReplacer(`"`, "", `'`, "").Replace(trimToCommandEnd(line[m[1]:])))
		if classifyMCPPin(mcpServer{Command: words[0], Args: append(append([]string{}, words[1:]...), rest...)}) != "" {
			return true
		}
	}
	return false
}

func trimToCommandEnd(rest string) string {
	if loc := lineEndRe.FindStringIndex(rest); loc != nil {
		return rest[:loc[0]]
	}
	return rest
}

// ── SKILL.md body: fenced shell blocks ──────────────────────────────────────

var fenceOpenRe = regexp.MustCompile("^\\s*(?:```|~~~)\\s*([A-Za-z0-9_+-]*)")

// shellFenceLangs are the fence tags whose content is read as shell commands.
var shellFenceLangs = map[string]bool{"": true, "bash": true, "sh": true, "shell": true, "zsh": true, "console": true, "terminal": true}

// fencedShellText returns the text of the shell code blocks in a markdown body,
// with a leading "$ " prompt removed from each line.
func fencedShellText(body string) []byte {
	var out bytes.Buffer
	in := false
	keep := false
	for _, line := range strings.Split(body, "\n") {
		if m := fenceOpenRe.FindStringSubmatch(line); m != nil {
			if in {
				in = false
			} else {
				in = true
				keep = shellFenceLangs[strings.ToLower(m[1])]
			}
			continue
		}
		if in && keep {
			l := strings.TrimSpace(line)
			l = strings.TrimPrefix(strings.TrimPrefix(l, "$ "), "> ")
			out.WriteString(l)
			out.WriteByte('\n')
		}
	}
	return out.Bytes()
}

// bodyUnpinnedInstall judges the shell code blocks of a SKILL.md body. Prose
// outside a code block is not judged.
func bodyUnpinnedInstall(body string) bool {
	return textUnpinnedInstall(hashCommentRe.ReplaceAll(fencedShellText(body), nil), false)
}

// scriptFacts fills the content facts of one bundled script. Files under a test
// path are left unjudged.
func scriptFacts(bf *models.BundledFile, name string, content []byte) {
	code := stripScriptComments(name, content)
	bf.DecodesAndExecutes = scriptDecodesAndExecutes(code)
	bf.DestructiveCommand = scriptDestructiveCommand(code)
	bf.UnpinnedInstall = textUnpinnedInstall(code, true)
}
