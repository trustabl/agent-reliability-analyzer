package analysis

import (
	"bytes"
	"encoding/json"
	"io"
	"net/netip"
	"path"
	"regexp"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/trustabl/trustabl/internal/models"
	"github.com/trustabl/trustabl/internal/pathclass"
)

// AGSRV-001 and AGSRV-002: how a server the repo runs for an agent is exposed.
//
// AGSRV-001 (any browser origin) reads four surfaces, each an explicit "*":
//   - the adk.web.cors.origins key of Spring application.properties / .yml / .yaml;
//   - the allow_origins list of a get_fast_api_app(...) call in a Python file that
//     imports google.adk;
//   - the allowOrigins option of an AdkApiServer(...) call in a TS/JS file that
//     imports @google/adk-devtools;
//   - an --allow_origins option of "adk web" / "adk api_server" in a Dockerfile,
//     Makefile, shell script, compose command / entrypoint or package.json script.
//
// AGSRV-002 (agent server exposed without a session key) reads Docker Compose
// services and docker run lines that run ghcr.io/openhands/agent-server, and fires
// only when all three hold: a port is published without a loopback host address,
// the server's --host is set to a non-loopback address literal, and the
// environment names no session-key variable the server reads. The server binds
// loopback when it is given no --host and no key is configured (v1.52.0
// __main__.py), so a published port alone is not judged.
//
// Secrecy contract: a hit's Reason is static text naming the surface. No origin
// list, port, host, URL or environment value is stored; environment keys are
// compared by name only.

// ── shared helpers ───────────────────────────────────────────────────────────

var composeBase = regexp.MustCompile(`^(?:docker-)?compose(?:[._-].*)?\.ya?ml$`)

func isComposeFile(rel string) bool {
	return composeBase.MatchString(strings.ToLower(path.Base(rel)))
}

// joinContinuations folds backslash-newline pairs so one command is one line.
func joinContinuations(s string) string {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	return strings.ReplaceAll(s, "\\\n", " ")
}

// shellTokens splits a command line into tokens. Quotes group text and are
// dropped. When jsonForm is true (a Dockerfile exec-form array), commas outside
// quotes and square brackets outside quotes separate tokens as well.
func shellTokens(line string, jsonForm bool) []string {
	var toks []string
	var cur strings.Builder
	var quote rune
	flush := func() {
		if cur.Len() > 0 {
			toks = append(toks, cur.String())
			cur.Reset()
		}
	}
	for _, r := range line {
		switch {
		case quote != 0:
			if r == quote {
				quote = 0
			} else {
				cur.WriteRune(r)
			}
		case r == '"' || r == '\'':
			quote = r
		case r == ' ' || r == '\t' || r == '\n':
			flush()
		case jsonForm && (r == ',' || r == '[' || r == ']'):
			flush()
		default:
			cur.WriteRune(r)
		}
	}
	flush()
	return toks
}

func isJSONForm(line string) bool {
	return strings.Contains(line, `["`) || strings.Contains(line, `['`)
}

// listHasWildcard reports whether a comma-separated origin list has a "*" entry.
func listHasWildcard(v string) bool {
	for _, e := range strings.Split(v, ",") {
		if strings.TrimSpace(e) == "*" {
			return true
		}
	}
	return false
}

// codeMask marks which bytes of src are code: false for a comment or the inside
// of a string literal (including Python docstrings and JS/TS template literals),
// so a call that appears in a comment or in quoted text is not judged. python
// selects # comments and triple-quoted strings; otherwise // and /* */ comments
// are used. A string's delimiters are marked as code, its contents are not.
// Template literal substitutions are treated as part of the string.
func codeMask(src string, python bool) []bool {
	mask := make([]bool, len(src))
	n := len(src)
	for i := 0; i < n; {
		c := src[i]
		switch {
		case python && c == '#', !python && c == '/' && i+1 < n && src[i+1] == '/':
			for i < n && src[i] != '\n' {
				i++
			}
		case !python && c == '/' && i+1 < n && src[i+1] == '*':
			i += 2
			for i+1 < n && !(src[i] == '*' && src[i+1] == '/') {
				i++
			}
			i += 2
		case c == '"' || c == '\'' || (!python && c == '`'):
			q := c
			mask[i] = true
			if python && i+2 < n && src[i+1] == q && src[i+2] == q {
				mask[i+1], mask[i+2] = true, true
				i += 3
				for i < n && !(src[i] == q && i+2 < n && src[i+1] == q && src[i+2] == q) {
					if src[i] == '\\' {
						i++
					}
					i++
				}
				for k := 0; k < 3 && i < n; k++ {
					mask[i] = true
					i++
				}
				continue
			}
			i++
			for i < n && src[i] != q && (src[i] != '\n' || q == '`') {
				if src[i] == '\\' {
					i++
				}
				i++
			}
			if i < n {
				mask[i] = true
				i++
			}
		default:
			mask[i] = true
			i++
		}
	}
	return mask
}

// nonCommentLines drops lines whose first non-space character is #.
func nonCommentLines(text string) []string {
	var out []string
	for _, l := range strings.Split(text, "\n") {
		if !strings.HasPrefix(strings.TrimSpace(l), "#") {
			out = append(out, l)
		}
	}
	return out
}

// ── AGSRV-001 ────────────────────────────────────────────────────────────────

var (
	propsOriginKey = regexp.MustCompile(`^\s*adk\.web\.cors\.origins\s*[=:]\s*(.*)$`)
	pyOriginsArg   = regexp.MustCompile(`allow_origins\s*=\s*[\[\(]([^\]\)]*)[\]\)]`)
	pyWildcardItem = regexp.MustCompile(`(?:^|,)\s*["']\*["']\s*(?:,|$)`)
	tsOriginsArg   = regexp.MustCompile("allowOrigins\\s*:\\s*[\"'`]([^\"'`]*)[\"'`]")
	getFastAPICall = regexp.MustCompile(`get_fast_api_app\s*\(`)
	adkServerCall  = regexp.MustCompile(`AdkApiServer\s*\(`)
)

const (
	reasonSpring = "ADK server origin list contains * (Spring configuration)"
	reasonPyCall = "ADK server origin list contains * (get_fast_api_app call)"
	reasonTSCall = "ADK server origin list contains * (AdkApiServer options)"
	reasonCLI    = "ADK server origin list contains * (--allow_origins option)"
)

// anyOriginServerHits returns one hit per offending location; the caller sorts.
func anyOriginServerHits(manifest models.ScanManifest) []models.ConfigHit {
	root := manifest.RepoRoot
	var out []models.ConfigHit

	for _, rel := range manifest.PropertiesFiles {
		raw := readConfig(root, rel)
		for n, l := range strings.Split(string(raw), "\n") {
			tl := strings.TrimSpace(l)
			if strings.HasPrefix(tl, "#") || strings.HasPrefix(tl, "!") {
				continue
			}
			if m := propsOriginKey.FindStringSubmatch(l); m != nil && listHasWildcard(m[1]) {
				out = append(out, hit(rel, n+1, reasonSpring))
			}
		}
	}
	for _, rel := range manifest.YAMLFiles {
		base := strings.ToLower(path.Base(rel))
		switch {
		case strings.HasPrefix(base, "application"):
			out = append(out, springYAMLOriginHits(rel, readConfig(root, rel))...)
		case isComposeFile(rel):
			out = append(out, composeCLIOriginHits(rel, readConfig(root, rel))...)
		}
	}
	for _, rel := range manifest.PythonFiles {
		if pathclass.Classify(rel) == models.OriginTest {
			continue
		}
		out = append(out, pyOriginHits(rel, readConfig(root, rel))...)
	}
	for _, rels := range [][]string{manifest.TypeScriptFiles, manifest.JavaScriptFiles} {
		for _, rel := range rels {
			if pathclass.Classify(rel) == models.OriginTest {
				continue
			}
			out = append(out, tsOriginHits(rel, readConfig(root, rel))...)
		}
	}
	for _, rel := range manifest.ServerScriptFiles {
		raw := readConfig(root, rel)
		text := joinContinuations(string(raw))
		for _, l := range nonCommentLines(text) {
			if adkCLIWildcard(l) {
				out = append(out, hit(rel, lineOfText(raw, firstAdkToken(l)), reasonCLI))
			}
		}
	}
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
		for _, v := range doc.Scripts {
			if s, ok := v.(string); ok && adkCLIWildcard(s) {
				out = append(out, hit(rel, lineOfText(raw, firstAdkToken(s)), reasonCLI))
			}
		}
	}
	return out
}

// firstAdkToken returns a short needle used to locate a command's line.
func firstAdkToken(l string) string {
	if i := strings.Index(l, "--allow_origins"); i >= 0 {
		return "--allow_origins"
	}
	return "adk"
}

// adkCLIWildcard reports a command line that runs "adk web" or "adk api_server"
// with an --allow_origins value that has a * entry. Both the Python CLI (repeated
// option) and the TypeScript CLI (comma-separated value) use this option name.
func adkCLIWildcard(line string) bool {
	if !strings.Contains(line, "--allow_origins") || !strings.Contains(line, "adk") {
		return false
	}
	toks := shellTokens(line, isJSONForm(line))
	start := -1
	for i := 0; i+1 < len(toks); i++ {
		if path.Base(toks[i]) == "adk" && (toks[i+1] == "web" || toks[i+1] == "api_server") {
			start = i + 2
			break
		}
	}
	if start < 0 {
		return false
	}
	rest := toks[start:]
	for i, t := range rest {
		if v, ok := strings.CutPrefix(t, "--allow_origins="); ok && listHasWildcard(v) {
			return true
		}
		if t == "--allow_origins" && i+1 < len(rest) && listHasWildcard(rest[i+1]) {
			return true
		}
	}
	return false
}

func flattenYAML(v any, prefix string, emit func(key string, val any)) {
	switch x := v.(type) {
	case map[string]any:
		for k, vv := range x {
			flattenYAML(vv, prefix+k+".", emit)
		}
	default:
		emit(strings.TrimSuffix(prefix, "."), v)
	}
}

func springYAMLOriginHits(rel string, raw []byte) []models.ConfigHit {
	var out []models.ConfigHit
	dec := yaml.NewDecoder(bytes.NewReader(raw))
	for {
		var doc map[string]any
		if err := dec.Decode(&doc); err != nil {
			if err == io.EOF {
				break
			}
			return out // malformed: no hit
		}
		flattenYAML(doc, "", func(key string, val any) {
			if key != "adk.web.cors.origins" {
				return
			}
			switch x := val.(type) {
			case string:
				if listHasWildcard(x) {
					out = append(out, hit(rel, lineOfText(raw, "origins"), reasonSpring))
				}
			case []any:
				for _, e := range x {
					if s, ok := e.(string); ok && strings.TrimSpace(s) == "*" {
						out = append(out, hit(rel, lineOfText(raw, "origins"), reasonSpring))
						break
					}
				}
			}
		})
	}
	return out
}

// composeCLIOriginHits reads command and entrypoint of each compose service.
func composeCLIOriginHits(rel string, raw []byte) []models.ConfigHit {
	var doc struct {
		Services map[string]map[string]any `yaml:"services"`
	}
	if yaml.Unmarshal(raw, &doc) != nil {
		return nil
	}
	var out []models.ConfigHit
	for _, svc := range doc.Services {
		var parts []string
		for _, k := range []string{"entrypoint", "command"} {
			switch x := svc[k].(type) {
			case string:
				parts = append(parts, x)
			case []any:
				var ss []string
				for _, e := range x {
					if s, ok := e.(string); ok {
						ss = append(ss, `"`+s+`"`)
					}
				}
				parts = append(parts, strings.Join(ss, " "))
			}
		}
		if adkCLIWildcard(strings.Join(parts, " ")) {
			out = append(out, hit(rel, lineOfText(raw, "--allow_origins"), reasonCLI))
		}
	}
	return out
}

func pyOriginHits(rel string, raw []byte) []models.ConfigHit {
	text := string(raw)
	if !strings.Contains(text, "google.adk") || !strings.Contains(text, "get_fast_api_app") {
		return nil
	}
	var out []models.ConfigHit
	mask := codeMask(text, true)
	for _, m := range getFastAPICall.FindAllStringIndex(text, -1) {
		if !mask[m[0]] {
			continue // inside a comment, docstring or string literal
		}
		args, ok := balancedArgs(text, m[1]-1)
		if !ok {
			continue
		}
		if a := pyOriginsArg.FindStringSubmatch(args); a != nil && pyWildcardItem.MatchString(a[1]) {
			out = append(out, hit(rel, strings.Count(text[:m[0]], "\n")+1, reasonPyCall))
		}
	}
	return out
}

func tsOriginHits(rel string, raw []byte) []models.ConfigHit {
	text := string(raw)
	if !strings.Contains(text, "@google/adk-devtools") || !strings.Contains(text, "AdkApiServer") {
		return nil
	}
	var out []models.ConfigHit
	mask := codeMask(text, false)
	for _, m := range adkServerCall.FindAllStringIndex(text, -1) {
		if !mask[m[0]] {
			continue // inside a comment or string literal
		}
		args, ok := balancedArgs(text, m[1]-1)
		if !ok {
			continue
		}
		if a := tsOriginsArg.FindStringSubmatch(args); a != nil && listHasWildcard(a[1]) {
			out = append(out, hit(rel, strings.Count(text[:m[0]], "\n")+1, reasonTSCall))
		}
	}
	return out
}

// ── AGSRV-002 ────────────────────────────────────────────────────────────────

const agentServerImage = "ghcr.io/openhands/agent-server"

var (
	imageDefault = regexp.MustCompile(`^\$\{[A-Za-z_][A-Za-z0-9_]*:?-(.+)\}$`)
	// sessionKeyEnv names the environment variables the OpenHands agent server and
	// its workspace launchers read for the session API key.
	sessionKeyPrefix = "OH_SESSION_API_KEYS_"
	sessionKeyLegacy = "SESSION_API_KEY"
)

const (
	reasonComposeNoKey = "compose service runs the OpenHands agent server image with a non-loopback " +
		"--host, publishes its port on all host interfaces and sets no session API key name"
	reasonRunNoKey = "docker run command starts the OpenHands agent server image with a non-loopback " +
		"--host, publishes its port on all host interfaces and sets no session API key name"
)

func isAgentServerImage(ref string) bool {
	ref = strings.TrimSpace(ref)
	if m := imageDefault.FindStringSubmatch(ref); m != nil {
		ref = m[1]
	}
	ref = strings.ToLower(ref)
	if !strings.HasPrefix(ref, agentServerImage) {
		return false
	}
	rest := ref[len(agentServerImage):]
	return rest == "" || rest[0] == ':' || rest[0] == '@'
}

// hostIsNonLoopbackLiteral reports whether v is an IP literal (IPv6 may be in
// brackets) that is not a loopback address: 0.0.0.0, ::, or another address.
// A $ reference, a name such as localhost and anything that is not an IP literal
// are not judged.
func hostIsNonLoopbackLiteral(v string) bool {
	v = strings.TrimSpace(v)
	if strings.Contains(v, "$") {
		return false
	}
	v = strings.TrimSuffix(strings.TrimPrefix(v, "["), "]")
	a, err := netip.ParseAddr(v)
	if err != nil {
		return false
	}
	return !a.IsLoopback()
}

// hostFlagNonLoopback reports a --host option (separate value or --host=value)
// among args whose value is a non-loopback IP literal. The server reads --host
// and no environment variable or config key for its bind address.
func hostFlagNonLoopback(args []string) bool {
	for i, t := range args {
		if v, ok := strings.CutPrefix(t, "--host="); ok && hostIsNonLoopbackLiteral(v) {
			return true
		}
		if t == "--host" && i+1 < len(args) && hostIsNonLoopbackLiteral(args[i+1]) {
			return true
		}
	}
	return false
}

// composeCommandTokens returns the entrypoint and command tokens of a service:
// a string is split like a shell line, a list is taken element by element.
func composeCommandTokens(svc map[string]any) []string {
	var toks []string
	for _, k := range []string{"entrypoint", "command"} {
		switch x := svc[k].(type) {
		case string:
			toks = append(toks, shellTokens(x, false)...)
		case []any:
			for _, e := range x {
				if s, ok := e.(string); ok {
					toks = append(toks, s)
				}
			}
		}
	}
	return toks
}

func isSessionKeyName(k string) bool {
	k = strings.TrimSpace(k)
	return k == sessionKeyLegacy || strings.HasPrefix(k, sessionKeyPrefix)
}

// publishState classifies one short-syntax port mapping ([IP:]HOST:CONTAINER or a
// bare CONTAINER): "all" publishes on all interfaces, "loopback" and "other" do
// not (other covers a $-reference, which is not judged).
func publishState(spec string) string {
	spec = strings.TrimSpace(spec)
	if i := strings.IndexByte(spec, '/'); i >= 0 {
		spec = spec[:i]
	}
	if strings.HasPrefix(spec, "[") { // [IPv6]:host:container
		end := strings.IndexByte(spec, ']')
		if end < 0 {
			return "other"
		}
		return ipState(spec[1:end], spec[end+1:] != "")
	}
	parts := strings.Split(spec, ":")
	if len(parts) >= 3 {
		return ipState(parts[0], true)
	}
	return "all"
}

func ipState(ip string, hasPorts bool) string {
	switch {
	case strings.Contains(ip, "$"):
		return "other"
	case ip == "127.0.0.1" || ip == "::1" || ip == "localhost":
		return "loopback"
	case ip == "" || ip == "0.0.0.0" || ip == "::":
		return "all"
	}
	return "other"
}

func agentServerNoKeyHits(manifest models.ScanManifest) []models.ConfigHit {
	root := manifest.RepoRoot
	var out []models.ConfigHit
	for _, rel := range manifest.YAMLFiles {
		if !isComposeFile(rel) {
			continue
		}
		raw := readConfig(root, rel)
		var doc struct {
			Services map[string]map[string]any `yaml:"services"`
		}
		if yaml.Unmarshal(raw, &doc) != nil {
			continue
		}
		for _, svc := range doc.Services {
			img, _ := svc["image"].(string)
			if !isAgentServerImage(img) || svc["env_file"] != nil {
				continue
			}
			if composeHasSessionKey(svc["environment"]) || !composePublishesAll(svc["ports"]) ||
				!hostFlagNonLoopback(composeCommandTokens(svc)) {
				continue
			}
			out = append(out, hit(rel, lineOfText(raw, agentServerImage), reasonComposeNoKey))
		}
	}
	for _, rel := range manifest.ServerScriptFiles {
		raw := readConfig(root, rel)
		if !bytes.Contains(raw, []byte(agentServerImage)) {
			continue
		}
		for _, l := range nonCommentLines(joinContinuations(string(raw))) {
			if dockerRunNoKey(l) {
				out = append(out, hit(rel, lineOfText(raw, agentServerImage), reasonRunNoKey))
			}
		}
	}
	return out
}

func composeHasSessionKey(env any) bool {
	switch x := env.(type) {
	case map[string]any:
		for k := range x {
			if isSessionKeyName(k) {
				return true
			}
		}
	case []any:
		for _, e := range x {
			if s, ok := e.(string); ok && isSessionKeyName(strings.SplitN(s, "=", 2)[0]) {
				return true
			}
		}
	}
	return false
}

func composePublishesAll(ports any) bool {
	list, _ := ports.([]any)
	for _, p := range list {
		switch x := p.(type) {
		case string:
			if publishState(x) == "all" {
				return true
			}
		case int:
			return true // bare container port: published on all interfaces
		case map[string]any:
			hostIP, _ := x["host_ip"].(string)
			if ipState(hostIP, true) == "all" {
				return true
			}
		}
	}
	return false
}

// dockerRunNoKey reports one docker run line that names the agent server image,
// publishes a port on all interfaces, passes a non-loopback --host to the server,
// sets no session key name and passes no --env-file.
func dockerRunNoKey(line string) bool {
	if !strings.Contains(line, agentServerImage) {
		return false
	}
	toks := shellTokens(line, false)
	run := false
	for i := 0; i+1 < len(toks); i++ {
		if toks[i] == "docker" && (toks[i+1] == "run" || (toks[i+1] == "container" && i+2 < len(toks) && toks[i+2] == "run")) {
			run = true
			break
		}
	}
	if !run {
		return false
	}
	// Only tokens after the image are arguments to the server; a --host before the
	// image would be a Docker option.
	imgIdx := -1
	for i, t := range toks {
		if isAgentServerImage(t) {
			imgIdx = i
			break
		}
	}
	if imgIdx < 0 || !hostFlagNonLoopback(toks[imgIdx+1:]) {
		return false
	}
	published, keyed := false, false
	for i, t := range toks {
		var val string
		switch {
		case t == "-p" || t == "--publish":
			if i+1 < len(toks) {
				val = toks[i+1]
			}
			if val != "" && publishState(val) == "all" {
				published = true
			}
		case strings.HasPrefix(t, "--publish="):
			if publishState(strings.TrimPrefix(t, "--publish=")) == "all" {
				published = true
			}
		case t == "-e" || t == "--env":
			if i+1 < len(toks) && isSessionKeyName(strings.SplitN(toks[i+1], "=", 2)[0]) {
				keyed = true
			}
		case strings.HasPrefix(t, "--env="):
			if isSessionKeyName(strings.SplitN(strings.TrimPrefix(t, "--env="), "=", 2)[0]) {
				keyed = true
			}
		case t == "--env-file" || strings.HasPrefix(t, "--env-file="):
			keyed = true // not judged
		}
	}
	return published && !keyed
}
