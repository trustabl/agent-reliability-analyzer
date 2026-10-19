package analysis

import (
	"encoding/json"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/trustabl/trustabl/internal/models"
)

// CI workflow discovery (category ci): coding agents run from GitHub Actions
// workflows on events any GitHub user can cause. DiscoverCIWorkflows parses the
// repo-root .github/workflows files into typed CIHit facts that the repo-scope
// predicates read; detectors never re-parse a workflow.
//
// What is and is not modelled is deliberate and documented in ARCHITECTURE.md:
//   - `${{ }}` expressions are never evaluated; an `if:` is matched textually
//     against a small grammar of actor gates (ciHasActorGate).
//   - matrix / strategy is ignored.
//   - a job-level `uses: ./.github/workflows/x.yml` is resolved ONE hop to the
//     callee in the scan set; remote reusable workflows and composite actions
//     are not resolved.
//
// Secrecy contract: a hit carries event names, action names, job ids and secret
// NAMES only. No `with`, `env` or `run` value is copied into a hit or a reason.

const (
	ciMaxAliasDepth = 32
	ciNodeBudget    = 100000
)

// ciUntrustedEvents are the events any GitHub user can cause.
var ciUntrustedEvents = map[string]bool{
	"pull_request_target":         true,
	"issue_comment":               true,
	"issues":                      true,
	"discussion":                  true,
	"discussion_comment":          true,
	"pull_request_review_comment": true,
}

// Agent kinds.
const (
	ciClaudeAction = "claude_code_action"
	ciClaudeBase   = "claude_code_base_action"
	ciGeminiAction = "gemini_cli_action"
	ciCodexAction  = "codex_action"
)

var ciActionKinds = map[string]string{
	"anthropics/claude-code-action":             ciClaudeAction,
	"anthropics/claude-code-action/base-action": ciClaudeBase,
	"anthropics/claude-code-base-action":        ciClaudeBase,
	"google-github-actions/run-gemini-cli":      ciGeminiAction,
	"openai/codex-action":                       ciCodexAction,
}

// ciCLIPackages maps an npx / bunx / dlx package to the CLI it installs.
var ciCLIPackages = map[string]string{
	"@anthropic-ai/claude-code": "claude",
	"@google/gemini-cli":        "gemini",
	"@openai/codex":             "codex",
	"cline":                     "cline",
}

var ciCLICommands = map[string]bool{"claude": true, "gemini": true, "codex": true, "cline": true, "aider": true}

// ciModelWithKeys are the `with:` inputs that carry the agent's model credential.
var ciModelWithKeys = map[string]map[string]bool{
	ciClaudeAction: {"anthropic_api_key": true, "claude_code_oauth_token": true},
	ciClaudeBase:   {"anthropic_api_key": true, "claude_code_oauth_token": true},
	ciGeminiAction: {"gemini_api_key": true, "google_api_key": true},
	ciCodexAction:  {"openai-api-key": true},
}

// ciModelEnvKeys are env var names that carry a model credential.
var ciModelEnvKeys = map[string]bool{
	"ANTHROPIC_API_KEY": true, "CLAUDE_CODE_OAUTH_TOKEN": true,
	"GEMINI_API_KEY": true, "GOOGLE_API_KEY": true, "OPENAI_API_KEY": true,
}

// ciExecPrimitives are download / network commands that make a Bash grant
// exec-capable alongside launcherShells (project_config.go) — the same words
// hookExecRe flags for AGCFG-005.
var ciExecPrimitives = map[string]bool{"curl": true, "wget": true, "nc": true, "ncat": true}

// ── parsed shape (unexported; detectors only see CIHit) ─────────────────────

type ciPerms struct {
	declared bool
	writeAll bool
	scopes   map[string]string
}

type ciStep struct {
	line   int
	uses   string // normalized owner/repo[/path], no @ref, lowercased; "" when absent or an expression
	ifExpr string
	with   map[string]string
	env    map[string]string
	run    string
}

type ciJob struct {
	id             string
	line           int
	ifExpr         string
	needs          []string
	perms          ciPerms
	usesWorkflow   string
	secretsInherit bool
	env            map[string]string
	steps          []ciStep
}

type ciWorkflow struct {
	path     string
	triggers map[string][]string // event -> types
	perms    ciPerms
	env      map[string]string
	jobs     []ciJob
}

// ── YAML node helpers ───────────────────────────────────────────────────────

type ciParser struct {
	budget int
	err    error
}

func (p *ciParser) spend() bool {
	p.budget--
	if p.budget < 0 {
		if p.err == nil {
			p.err = fmt.Errorf("workflow exceeds the node budget")
		}
		return false
	}
	return true
}

func (p *ciParser) deref(n *yaml.Node) *yaml.Node {
	for i := 0; n != nil && n.Kind == yaml.AliasNode; i++ {
		if i >= ciMaxAliasDepth || !p.spend() {
			if p.err == nil {
				p.err = fmt.Errorf("alias chain too deep")
			}
			return nil
		}
		n = n.Alias
	}
	return n
}

type ciPair struct {
	key string
	val *yaml.Node
}

// pairs flattens a mapping node, expanding `<<` merge keys. Explicit keys win
// over merged ones.
func (p *ciParser) pairs(n *yaml.Node) []ciPair {
	n = p.deref(n)
	if n == nil || n.Kind != yaml.MappingNode || !p.spend() {
		return nil
	}
	var explicit, merged []ciPair
	seen := map[string]bool{}
	for i := 0; i+1 < len(n.Content); i += 2 {
		k := p.deref(n.Content[i])
		if k == nil {
			return nil
		}
		if k.Kind == yaml.ScalarNode && k.Value == "<<" {
			v := p.deref(n.Content[i+1])
			if v == nil {
				return nil
			}
			switch v.Kind {
			case yaml.MappingNode:
				merged = append(merged, p.pairs(v)...)
			case yaml.SequenceNode:
				for _, e := range v.Content {
					merged = append(merged, p.pairs(e)...)
				}
			}
			continue
		}
		if k.Kind != yaml.ScalarNode {
			continue
		}
		explicit = append(explicit, ciPair{k.Value, n.Content[i+1]})
		seen[k.Value] = true
	}
	for _, m := range merged {
		if !seen[m.key] {
			explicit = append(explicit, m)
			seen[m.key] = true
		}
	}
	return explicit
}

func (p *ciParser) get(n *yaml.Node, key string) *yaml.Node {
	for _, kv := range p.pairs(n) {
		if kv.key == key {
			return p.deref(kv.val)
		}
	}
	return nil
}

// scalar returns the text of a scalar node, "" for anything else.
func (p *ciParser) scalar(n *yaml.Node) string {
	n = p.deref(n)
	if n == nil || n.Kind != yaml.ScalarNode {
		return ""
	}
	return n.Value
}

func (p *ciParser) strList(n *yaml.Node) []string {
	n = p.deref(n)
	if n == nil {
		return nil
	}
	if n.Kind == yaml.ScalarNode {
		if n.Value == "" {
			return nil
		}
		return []string{n.Value}
	}
	var out []string
	if n.Kind == yaml.SequenceNode {
		for _, e := range n.Content {
			if s := p.scalar(e); s != "" {
				out = append(out, s)
			}
		}
	}
	return out
}

// scalarMap reads a mapping of scalar values (with:, env:). Non-scalar values
// are kept as "" so the key is still known.
func (p *ciParser) scalarMap(n *yaml.Node) map[string]string {
	out := map[string]string{}
	for _, kv := range p.pairs(n) {
		out[kv.key] = p.scalar(kv.val)
	}
	return out
}

func ciStripExpr(s string) string {
	s = strings.TrimSpace(s)
	if strings.HasPrefix(s, "${{") && strings.HasSuffix(s, "}}") {
		s = strings.TrimSpace(s[3 : len(s)-2])
	}
	return strings.Join(strings.Fields(s), " ")
}

func (p *ciParser) perms(n *yaml.Node) ciPerms {
	n = p.deref(n)
	if n == nil {
		return ciPerms{}
	}
	out := ciPerms{declared: true, scopes: map[string]string{}}
	switch n.Kind {
	case yaml.ScalarNode:
		if strings.EqualFold(strings.TrimSpace(n.Value), "write-all") {
			out.writeAll = true
		}
	case yaml.MappingNode:
		for _, kv := range p.pairs(n) {
			out.scopes[kv.key] = strings.ToLower(strings.TrimSpace(p.scalar(kv.val)))
		}
	}
	return out
}

// ciNormalizeUses lowercases an action reference and drops its @ref. A value
// that is an expression, a docker:// image or a local path yields "".
func ciNormalizeUses(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" || strings.Contains(raw, "${{") || strings.HasPrefix(raw, "docker://") || strings.HasPrefix(raw, "./") {
		return ""
	}
	if i := strings.Index(raw, "@"); i >= 0 {
		raw = raw[:i]
	}
	return strings.ToLower(raw)
}

func (p *ciParser) parseWorkflow(rel string, raw []byte) (*ciWorkflow, error) {
	var doc yaml.Node
	if err := yaml.Unmarshal(raw, &doc); err != nil {
		return nil, err
	}
	if doc.Kind != yaml.DocumentNode || len(doc.Content) == 0 {
		return nil, fmt.Errorf("empty document")
	}
	root := p.deref(doc.Content[0])
	if root == nil || root.Kind != yaml.MappingNode {
		return nil, fmt.Errorf("workflow is not a mapping")
	}
	wf := &ciWorkflow{path: rel, triggers: map[string][]string{}, env: map[string]string{}}
	for _, kv := range p.pairs(root) {
		switch kv.key {
		case "on", "true":
			wf.triggers = p.triggers(kv.val)
		case "permissions":
			wf.perms = p.perms(kv.val)
		case "env":
			wf.env = p.scalarMap(kv.val)
		case "jobs":
			for _, jp := range p.pairs(kv.val) {
				wf.jobs = append(wf.jobs, p.job(jp))
			}
		}
	}
	if p.err != nil {
		return nil, p.err
	}
	return wf, nil
}

func (p *ciParser) triggers(n *yaml.Node) map[string][]string {
	out := map[string][]string{}
	n = p.deref(n)
	if n == nil {
		return out
	}
	switch n.Kind {
	case yaml.ScalarNode:
		if n.Value != "" {
			out[n.Value] = nil
		}
	case yaml.SequenceNode:
		for _, e := range n.Content {
			if s := p.scalar(e); s != "" {
				out[s] = nil
			}
		}
	case yaml.MappingNode:
		for _, kv := range p.pairs(n) {
			out[kv.key] = p.strList(p.get(kv.val, "types"))
		}
	}
	return out
}

func (p *ciParser) job(jp ciPair) ciJob {
	n := p.deref(jp.val)
	j := ciJob{id: jp.key, env: map[string]string{}}
	if n != nil {
		j.line = n.Line
	}
	for _, kv := range p.pairs(n) {
		switch kv.key {
		case "if":
			j.ifExpr = ciStripExpr(p.scalar(kv.val))
		case "needs":
			j.needs = p.strList(kv.val)
		case "permissions":
			j.perms = p.perms(kv.val)
		case "uses":
			j.usesWorkflow = strings.TrimSpace(p.scalar(kv.val))
		case "secrets":
			if v := p.deref(kv.val); v != nil && v.Kind == yaml.ScalarNode && strings.EqualFold(strings.TrimSpace(v.Value), "inherit") {
				j.secretsInherit = true
			}
		case "env":
			j.env = p.scalarMap(kv.val)
		case "steps":
			sn := p.deref(kv.val)
			if sn == nil || sn.Kind != yaml.SequenceNode {
				continue
			}
			for _, e := range sn.Content {
				j.steps = append(j.steps, p.step(e))
			}
		}
	}
	return j
}

func (p *ciParser) step(n *yaml.Node) ciStep {
	n = p.deref(n)
	s := ciStep{with: map[string]string{}, env: map[string]string{}}
	if n == nil {
		return s
	}
	s.line = n.Line
	for _, kv := range p.pairs(n) {
		switch kv.key {
		case "uses":
			s.uses = ciNormalizeUses(p.scalar(kv.val))
		case "if":
			s.ifExpr = ciStripExpr(p.scalar(kv.val))
		case "with":
			s.with = p.scalarMap(kv.val)
		case "env":
			s.env = p.scalarMap(kv.val)
		case "run":
			s.run = p.scalar(kv.val)
		}
	}
	return s
}

// ── agent recognition ───────────────────────────────────────────────────────

type ciAgent struct {
	kind string
	step int    // index into job.steps
	args string // CLI agents: the command segment text after the command word
}

var ciSecretRefRe = regexp.MustCompile(`secrets\.([A-Za-z_][A-Za-z0-9_]*)|secrets\[\s*['"]([^'"]+)['"]\s*\]`)

func ciSecretNames(text string) []string {
	var out []string
	for _, m := range ciSecretRefRe.FindAllStringSubmatch(text, -1) {
		name := m[1]
		if name == "" {
			name = m[2]
		}
		out = append(out, name)
	}
	return out
}

var ciSegmentSplitRe = regexp.MustCompile(`&&|\|\||;|\||\$\(|` + "`")

// ciCLISegments returns the agent CLI invocations in a run: script. The command
// word must be the first word of a command (after env assignments, env, exec,
// time or a package runner), which excludes `echo gemini`.
func ciCLISegments(run string) []ciAgent {
	run = strings.ReplaceAll(run, "\\\n", " ")
	var out []ciAgent
	for _, line := range strings.Split(run, "\n") {
		for _, seg := range ciSegmentSplitRe.Split(line, -1) {
			f := shellFields(seg)
			i := 0
			for i < len(f) && (strings.Contains(f[i], "=") && !strings.HasPrefix(f[i], "-") || f[i] == "env" || f[i] == "exec" || f[i] == "time" || f[i] == "sudo") {
				i++
			}
			if i >= len(f) {
				continue
			}
			word := path.Base(f[i])
			switch word {
			case "npx", "bunx", "pnpm", "yarn", "npm":
				j := i + 1
				for j < len(f) && (strings.HasPrefix(f[j], "-") || f[j] == "dlx" || f[j] == "exec") {
					j++
				}
				if j < len(f) {
					pkg := f[j]
					if k := strings.LastIndex(pkg, "@"); k > 0 {
						pkg = pkg[:k]
					}
					if cli, ok := ciCLIPackages[pkg]; ok {
						out = append(out, ciAgent{kind: "cli:" + cli, args: strings.Join(f[j+1:], " ")})
					}
				}
			default:
				if ciCLICommands[word] {
					out = append(out, ciAgent{kind: "cli:" + word, args: strings.Join(f[i+1:], " ")})
				}
			}
		}
	}
	return out
}

// shellFields splits s on whitespace, honouring single and double quotes.
func shellFields(s string) []string {
	var out []string
	var b strings.Builder
	var quote rune
	has := false
	flush := func() {
		if has {
			out = append(out, b.String())
		}
		b.Reset()
		has = false
	}
	for _, r := range s {
		switch {
		case quote != 0:
			if r == quote {
				quote = 0
			} else {
				b.WriteRune(r)
			}
		case r == '\'' || r == '"':
			quote = r
			has = true
		case r == ' ' || r == '\t' || r == '\n' || r == '\r':
			flush()
		default:
			b.WriteRune(r)
			has = true
		}
	}
	flush()
	return out
}

func ciAgentsInJob(j ciJob) []ciAgent {
	var out []ciAgent
	for i, st := range j.steps {
		if kind, ok := ciActionKinds[st.uses]; ok {
			out = append(out, ciAgent{kind: kind, step: i})
		}
		if st.run != "" {
			for _, a := range ciCLISegments(st.run) {
				a.step = i
				out = append(out, a)
			}
		}
	}
	return out
}

// ── actor gate grammar ──────────────────────────────────────────────────────

var (
	ciAssocWord = `[\w.\-\[\]'"]*author_association`
	// ciAssocArg is one author_association context or an || chain of them, the
	// form Google's gemini-dispatch example passes to contains().
	ciAssocArg    = `(?:` + ciAssocWord + `)(?:\s*\|\|\s*` + ciAssocWord + `)*`
	ciGateListRe  = regexp.MustCompile(`(?i)(?:^|[^!\w])contains\(\s*fromjson\(\s*'(\[[^']*\])'\s*\)\s*,\s*` + ciAssocArg + `\s*\)`)
	ciGateStrRe   = regexp.MustCompile(`(?i)(?:^|[^!\w])contains\(\s*'([^']*)'\s*,\s*` + ciAssocArg + `\s*\)`)
	ciGateEqRe    = regexp.MustCompile(`(?i)` + ciAssocWord + `\s*==\s*'(OWNER|MEMBER|COLLABORATOR)'|'(OWNER|MEMBER|COLLABORATOR)'\s*==\s*` + ciAssocWord)
	ciActorWord   = `github\.(?:actor|triggering_actor|event\.sender\.login)`
	ciActorEqRe   = regexp.MustCompile(`(?i)` + ciActorWord + `\s*==\s*'[^']+'|'[^']+'\s*==\s*` + ciActorWord)
	ciActorListRe = regexp.MustCompile(`(?i)(?:^|[^!\w])contains\(\s*fromjson\(\s*'(\[[^']*\])'\s*\)\s*,\s*` + ciActorWord + `\s*\)`)
	ciForkRe      = regexp.MustCompile(`(?i)github\.event\.pull_request\.head\.repo\.full_name\s*==\s*github\.repository|github\.event\.pull_request\.head\.repo\.fork\s*==\s*false|!\s*github\.event\.pull_request\.head\.repo\.fork\b`)
	ciStatusFnRe  = regexp.MustCompile(`(?i)\b(?:always|failure|cancelled)\(\)`)
	ciQuotedRe    = regexp.MustCompile(`['"]([^'"]+)['"]`)
)

var ciTrustedAssoc = map[string]bool{"OWNER": true, "MEMBER": true, "COLLABORATOR": true}

// ciHasActorGate reports whether expr contains an actor gate (G1..G4). Boolean
// structure is not evaluated: `A || G1` counts as gated.
func ciHasActorGate(expr string, onlyPRTarget bool) bool {
	if expr == "" {
		return false
	}
	for _, m := range ciGateListRe.FindAllStringSubmatch(expr, -1) {
		items := ciQuotedRe.FindAllStringSubmatch(m[1], -1)
		ok := len(items) > 0
		for _, it := range items {
			if !ciTrustedAssoc[strings.ToUpper(it[1])] {
				ok = false
			}
		}
		if ok {
			return true
		}
	}
	for _, m := range ciGateStrRe.FindAllStringSubmatch(expr, -1) {
		toks := strings.FieldsFunc(m[1], func(r rune) bool { return r == ' ' || r == ',' || r == '|' })
		ok := len(toks) > 0
		for _, t := range toks {
			if !ciTrustedAssoc[strings.ToUpper(t)] {
				ok = false
			}
		}
		if ok {
			return true
		}
	}
	if ciGateEqRe.MatchString(expr) || ciActorEqRe.MatchString(expr) {
		return true
	}
	for _, m := range ciActorListRe.FindAllStringSubmatch(expr, -1) {
		if len(ciQuotedRe.FindAllStringSubmatch(m[1], -1)) > 0 {
			return true
		}
	}
	return onlyPRTarget && ciForkRe.MatchString(expr)
}

// ── evaluation ──────────────────────────────────────────────────────────────

// ciUnit is one job evaluated under a set of triggers. For a one-hop callee the
// triggers, gate, permissions and secrets come from the calling job.
type ciUnit struct {
	wf        *ciWorkflow
	job       ciJob
	label     string
	triggers  []string // sorted untrusted events
	onlyPRT   bool
	callerWf  *ciWorkflow
	callerJob *ciJob
}

func ciGateFromJob(wf *ciWorkflow, j ciJob, onlyPRT bool, seen map[string]bool) bool {
	if seen[j.id] {
		return false
	}
	seen[j.id] = true
	if ciHasActorGate(j.ifExpr, onlyPRT) {
		return true
	}
	if ciStatusFnRe.MatchString(j.ifExpr) {
		return false
	}
	for _, n := range j.needs {
		for _, o := range wf.jobs {
			if o.id == n && ciGateFromJob(wf, o, onlyPRT, seen) {
				return true
			}
		}
	}
	return false
}

// ciBuiltinGate reports whether the action itself refuses actors without write
// access. It does for claude-code-action and codex-action unless the bypass
// input is set to '*' (or to an expression, which is unknown).
func ciBuiltinGate(kind string, with map[string]string) bool {
	star := func(v string) bool {
		v = strings.TrimSpace(v)
		if strings.Contains(v, "${{") {
			return true
		}
		for _, t := range strings.FieldsFunc(v, func(r rune) bool { return r == ',' || r == ' ' || r == '\n' }) {
			if t == "*" {
				return true
			}
		}
		return false
	}
	switch kind {
	case ciClaudeAction:
		return !star(with["allowed_non_write_users"]) && !star(with["allowed_bots"])
	case ciCodexAction:
		return !star(with["allow-users"])
	}
	return false
}

func (u ciUnit) gated(a ciAgent) bool {
	st := u.job.steps[a.step]
	if a.kind == ciClaudeAction || a.kind == ciCodexAction {
		if ciBuiltinGate(a.kind, st.with) {
			return true
		}
	}
	if ciHasActorGate(st.ifExpr, u.onlyPRT) {
		return true
	}
	if ciGateFromJob(u.wf, u.job, u.onlyPRT, map[string]bool{}) {
		return true
	}
	if u.callerWf != nil && ciGateFromJob(u.callerWf, *u.callerJob, u.onlyPRT, map[string]bool{}) {
		return true
	}
	return false
}

func (u ciUnit) effectivePerms() ciPerms {
	for _, p := range []ciPerms{u.job.perms, u.wf.perms} {
		if p.declared {
			return p
		}
	}
	if u.callerJob != nil {
		if u.callerJob.perms.declared {
			return u.callerJob.perms
		}
		if u.callerWf.perms.declared {
			return u.callerWf.perms
		}
	}
	return ciPerms{}
}

var ciWriteScopes = []string{"actions", "contents", "packages", "pull-requests"}

func (p ciPerms) writeReason() string {
	if p.writeAll {
		return "permissions write-all"
	}
	var w []string
	for _, s := range ciWriteScopes {
		if p.scopes[s] == "write" {
			w = append(w, s)
		}
	}
	if len(w) == 0 {
		return ""
	}
	return "write permission " + strings.Join(w, ", ")
}

func (u ciUnit) extraSecrets(agentKinds map[int]string) []string {
	set := map[string]bool{}
	add := func(text string) {
		for _, n := range ciSecretNames(text) {
			if n != "GITHUB_TOKEN" {
				set[n] = true
			}
		}
	}
	envAdd := func(env map[string]string) {
		for k, v := range env {
			if ciModelEnvKeys[k] {
				continue
			}
			add(v)
		}
	}
	envAdd(u.wf.env)
	envAdd(u.job.env)
	for i, st := range u.job.steps {
		kind, isAgent := agentKinds[i]
		for k, v := range st.with {
			if isAgent && ciModelWithKeys[kind][k] {
				continue
			}
			add(v)
		}
		for k, v := range st.env {
			if isAgent && ciModelEnvKeys[k] {
				continue
			}
			add(v)
		}
		add(st.run)
		add(st.ifExpr)
	}
	out := make([]string, 0, len(set))
	for n := range set {
		out = append(out, n)
	}
	sort.Strings(out)
	return out
}

// ── shell / egress grants ───────────────────────────────────────────────────

type ciGrants struct {
	bash        []string // raw Bash grants
	other       []string // raw non-Bash grants (WebFetch, WebSearch, run_shell_command(...), ...)
	bypassFlags []string // fixed phrases
}

func ciBashUnrestricted(raw string) (string, bool) {
	pr := ParsePermissionRule(raw)
	if pr.Tool != "Bash" {
		return "", false
	}
	pat := strings.TrimSpace(pr.Pattern)
	if strings.Trim(pat, "*:") == "" {
		return "Bash granted without a command restriction", true
	}
	word := strings.ToLower(pat)
	if i := strings.IndexAny(word, " :"); i >= 0 {
		word = word[:i]
	}
	if launcherShells[word] || ciExecPrimitives[word] {
		return "Bash grant starts with " + word, true
	}
	return "", false
}

func ciShellToolUnrestricted(raw string) (string, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "run_shell_command" {
		return "run_shell_command granted without a command restriction", true
	}
	if strings.HasPrefix(raw, "run_shell_command(") && strings.HasSuffix(raw, ")") {
		inner := strings.ToLower(strings.TrimSpace(raw[len("run_shell_command(") : len(raw)-1]))
		word := inner
		if i := strings.IndexAny(word, " :"); i >= 0 {
			word = word[:i]
		}
		if launcherShells[word] || ciExecPrimitives[word] {
			return "run_shell_command grant starts with " + word, true
		}
	}
	return "", false
}

func ciClaudeArgGrants(args string, g *ciGrants) {
	f := shellFields(args)
	for i := 0; i < len(f); i++ {
		t := f[i]
		switch {
		case t == "--dangerously-skip-permissions":
			g.bypassFlags = append(g.bypassFlags, "--dangerously-skip-permissions")
		case t == "--permission-mode" && i+1 < len(f) && f[i+1] == "bypassPermissions",
			t == "--permission-mode=bypassPermissions":
			g.bypassFlags = append(g.bypassFlags, "--permission-mode bypassPermissions")
		case t == "--allowedTools" || t == "--allowed-tools" || strings.HasPrefix(t, "--allowedTools=") || strings.HasPrefix(t, "--allowed-tools="):
			var vals []string
			if k := strings.Index(t, "="); k >= 0 {
				vals = append(vals, t[k+1:])
			}
			for j := i + 1; j < len(f) && !strings.HasPrefix(f[j], "-"); j++ {
				vals = append(vals, f[j])
				i = j
			}
			for _, v := range vals {
				ciAddToolGrants(splitToolGrants(v), g)
			}
		}
	}
}

func ciAddToolGrants(tokens []string, g *ciGrants) {
	for _, tok := range tokens {
		if ParsePermissionRule(tok).Tool == "Bash" {
			g.bash = append(g.bash, tok)
		} else {
			g.other = append(g.other, tok)
		}
	}
}

func ciSettingsJSON(raw string) map[string]any {
	raw = strings.TrimSpace(raw)
	if !strings.HasPrefix(raw, "{") {
		return nil
	}
	var m map[string]any
	if json.Unmarshal([]byte(raw), &m) != nil {
		return nil
	}
	return m
}

func ciStringList(v any) []string {
	arr, _ := v.([]any)
	var out []string
	for _, e := range arr {
		if s, ok := e.(string); ok {
			out = append(out, s)
		}
	}
	return out
}

func ciGeminiFlags(args string, g *ciGrants) {
	f := shellFields(args)
	for i, t := range f {
		if t == "--yolo" || t == "--approval-mode=yolo" || (t == "--approval-mode" && i+1 < len(f) && f[i+1] == "yolo") {
			g.bypassFlags = append(g.bypassFlags, "Gemini yolo mode")
		}
	}
}

func ciCodexFlags(args string, g *ciGrants) {
	if strings.Contains(args, "--dangerously-bypass-approvals-and-sandbox") {
		g.bypassFlags = append(g.bypassFlags, "--dangerously-bypass-approvals-and-sandbox")
	}
	if strings.Contains(args, "danger-full-access") {
		g.bypassFlags = append(g.bypassFlags, "danger-full-access sandbox")
	}
}

func (u ciUnit) grants(a ciAgent) ciGrants {
	var g ciGrants
	st := u.job.steps[a.step]
	switch a.kind {
	case ciClaudeAction, ciClaudeBase:
		ciClaudeArgGrants(st.with["claude_args"], &g)
		if s := ciSettingsJSON(st.with["settings"]); s != nil {
			if perm, ok := s["permissions"].(map[string]any); ok {
				ciAddToolGrants(ciStringList(perm["allow"]), &g)
				if perm["defaultMode"] == "bypassPermissions" {
					g.bypassFlags = append(g.bypassFlags, "permission mode bypassPermissions")
				}
			}
		}
	case ciGeminiAction:
		if s := ciSettingsJSON(st.with["settings"]); s != nil {
			if tools, ok := s["tools"].(map[string]any); ok {
				for _, k := range []string{"core", "allowed"} {
					ciAddToolGrants(ciStringList(tools[k]), &g)
				}
			}
		}
	case ciCodexAction:
		if strings.TrimSpace(st.with["sandbox"]) == "danger-full-access" {
			g.bypassFlags = append(g.bypassFlags, "danger-full-access sandbox")
		}
		if strings.TrimSpace(st.with["safety-strategy"]) == "unsafe" {
			g.bypassFlags = append(g.bypassFlags, "safety-strategy unsafe")
		}
		ciCodexFlags(st.with["codex-args"], &g)
	case "cli:claude":
		ciClaudeArgGrants(a.args, &g)
	case "cli:gemini":
		ciGeminiFlags(a.args, &g)
	case "cli:codex":
		ciCodexFlags(a.args, &g)
	}
	return g
}

// unrestrictedReasons returns fixed phrases for each unrestricted-shell signal.
func (g ciGrants) unrestrictedReasons() []string {
	var out []string
	for _, b := range g.bash {
		if r, ok := ciBashUnrestricted(b); ok {
			out = append(out, r)
		}
	}
	for _, o := range g.other {
		if r, ok := ciShellToolUnrestricted(o); ok {
			out = append(out, r)
		}
	}
	for _, f := range g.bypassFlags {
		out = append(out, "approvals or sandbox bypassed ("+f+")")
	}
	return ciSortUnique(out)
}

// hasShellOrFetch reports any shell or fetch capability, restricted or not.
func (g ciGrants) hasShellOrFetch() bool {
	if len(g.bash) > 0 || len(g.bypassFlags) > 0 {
		return true
	}
	for _, o := range g.other {
		w := strings.TrimSpace(o)
		if w == "WebFetch" || w == "WebSearch" || strings.HasPrefix(w, "WebFetch(") || strings.HasPrefix(w, "WebSearch(") ||
			w == "run_shell_command" || strings.HasPrefix(w, "run_shell_command(") || w == "web_fetch" || w == "google_web_search" {
			return true
		}
	}
	return false
}

func ciSortUnique(in []string) []string {
	sort.Strings(in)
	out := in[:0]
	for i, s := range in {
		if i == 0 || s != in[i-1] {
			out = append(out, s)
		}
	}
	return out
}

// ── discovery ───────────────────────────────────────────────────────────────

// DiscoverCIWorkflows parses every workflow file in the manifest and returns the
// sorted, deduped CI facts.
func DiscoverCIWorkflows(manifest models.ScanManifest) models.CIWorkflowFacts {
	var f models.CIWorkflowFacts
	parsed := map[string]*ciWorkflow{}
	var order []string
	for _, c := range manifest.Components {
		if c.Kind != models.ComponentGitHubWorkflow {
			continue
		}
		raw, err := os.ReadFile(filepath.Join(manifest.RepoRoot, c.Path))
		if err != nil {
			f.Unparsed = append(f.Unparsed, c.Path)
			continue
		}
		p := &ciParser{budget: ciNodeBudget}
		wf, err := p.parseWorkflow(filepath.ToSlash(c.Path), raw)
		if err != nil || wf == nil {
			f.Unparsed = append(f.Unparsed, c.Path)
			continue
		}
		parsed[wf.path] = wf
		order = append(order, wf.path)
	}
	sort.Strings(order)
	sort.Strings(f.Unparsed)

	for _, rel := range order {
		wf := parsed[rel]
		trig, onlyPRT := ciUntrusted(wf)
		for i := range wf.jobs {
			j := wf.jobs[i]
			if local := ciLocalCallee(j.usesWorkflow); local != "" {
				callee := parsed[local]
				if callee == nil || len(trig) == 0 {
					continue
				}
				for _, k := range callee.jobs {
					u := ciUnit{wf: callee, job: k, triggers: trig, onlyPRT: onlyPRT, callerWf: wf, callerJob: &wf.jobs[i],
						label: path.Base(wf.path) + "#" + j.id + ">" + path.Base(callee.path) + "#" + k.id}
					ciEvaluate(&f, u, j.secretsInherit)
				}
				continue
			}
			if len(trig) == 0 {
				continue
			}
			ciEvaluate(&f, ciUnit{wf: wf, job: j, triggers: trig, onlyPRT: onlyPRT, label: j.id}, false)
		}
	}
	f.UntrustedUngated = ciSortHits(f.UntrustedUngated)
	f.UntrustedPrivileged = ciSortHits(f.UntrustedPrivileged)
	f.CheckoutPersists = ciSortHits(f.CheckoutPersists)
	f.UnrestrictedShell = ciSortHits(f.UnrestrictedShell)
	f.NoEgressRestriction = ciSortHits(f.NoEgressRestriction)
	return f
}

func ciUntrusted(wf *ciWorkflow) ([]string, bool) {
	var out []string
	for ev := range wf.triggers {
		if ciUntrustedEvents[ev] {
			out = append(out, ev)
		}
	}
	sort.Strings(out)
	only := len(out) > 0
	for _, ev := range out {
		if ev != "pull_request_target" {
			only = false
		}
	}
	return out, only
}

func ciLocalCallee(uses string) string {
	uses = strings.TrimSpace(uses)
	if !strings.HasPrefix(uses, "./") {
		return ""
	}
	return path.Clean(strings.TrimPrefix(uses, "./"))
}

func ciEvaluate(f *models.CIWorkflowFacts, u ciUnit, secretsInherit bool) {
	agents := ciAgentsInJob(u.job)
	if len(agents) == 0 {
		return
	}
	kinds := map[int]string{}
	for _, a := range agents {
		kinds[a.step] = a.kind
	}
	events := strings.Join(u.triggers, ", ")
	extra := u.extraSecrets(kinds)
	write := u.effectivePerms().writeReason()
	mk := func(a ciAgent, line int, reason string) models.CIHit {
		return models.CIHit{
			Location: models.Location{FilePath: u.wf.path, Line: ciLine(line), EndLine: ciLine(line)},
			Job:      u.label, Agent: a.kind, Reason: reason,
		}
	}
	for _, a := range agents {
		st := u.job.steps[a.step]
		gated := u.gated(a)
		if !gated {
			f.UntrustedUngated = append(f.UntrustedUngated, mk(a, st.line,
				"agent runs on "+events+" with no actor gate"))
			var why []string
			if write != "" {
				why = append(why, write)
			}
			if len(extra) > 0 {
				why = append(why, "secrets other than the model key: "+strings.Join(extra, ", "))
			}
			if secretsInherit {
				why = append(why, "secrets: inherit")
			}
			if len(why) > 0 {
				f.UntrustedPrivileged = append(f.UntrustedPrivileged, mk(a, st.line,
					"agent runs on "+events+" with no actor gate and "+strings.Join(why, "; ")))
			}
		}
		for _, cs := range u.job.steps {
			if cs.uses == "actions/checkout" && !strings.EqualFold(strings.TrimSpace(cs.with["persist-credentials"]), "false") {
				f.CheckoutPersists = append(f.CheckoutPersists, mk(a, cs.line,
					"actions/checkout does not set persist-credentials: false in a job that runs an agent on "+events))
			}
		}
		g := u.grants(a)
		if rs := g.unrestrictedReasons(); len(rs) > 0 {
			f.UnrestrictedShell = append(f.UnrestrictedShell, mk(a, st.line,
				"agent on "+events+": "+strings.Join(rs, "; ")))
		}
		if g.hasShellOrFetch() && !ciEgressBlocked(u.job, a.step) {
			f.NoEgressRestriction = append(f.NoEgressRestriction, mk(a, st.line,
				"agent on "+events+" has shell or fetch access and no earlier harden-runner step with egress-policy: block"))
		}
	}
}

func ciEgressBlocked(j ciJob, agentStep int) bool {
	for i := 0; i < agentStep && i < len(j.steps); i++ {
		st := j.steps[i]
		if st.uses == "step-security/harden-runner" && strings.EqualFold(strings.TrimSpace(st.with["egress-policy"]), "block") {
			return true
		}
	}
	return false
}

func ciLine(l int) int {
	if l < 1 {
		return 1
	}
	return l
}

func ciSortHits(h []models.CIHit) []models.CIHit {
	if len(h) == 0 {
		return nil
	}
	sort.Slice(h, func(i, j int) bool {
		a, b := h[i], h[j]
		switch {
		case a.FilePath != b.FilePath:
			return a.FilePath < b.FilePath
		case a.Line != b.Line:
			return a.Line < b.Line
		case a.Job != b.Job:
			return a.Job < b.Job
		}
		return a.Reason < b.Reason
	})
	out := h[:1]
	for _, x := range h[1:] {
		l := out[len(out)-1]
		if x.FilePath != l.FilePath || x.Line != l.Line || x.Job != l.Job || x.Reason != l.Reason {
			out = append(out, x)
		}
	}
	return out
}
