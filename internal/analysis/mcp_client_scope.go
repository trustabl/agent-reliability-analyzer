package analysis

import (
	"net"
	"net/url"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

// MCPCFG-004 (unscoped credential) and MCPCFG-005 (plain http remote URL):
// classifiers over one MCP client config server entry. Both run from mcpHits in
// project_config.go, so they read the same parse as MCPCFG-002 and MCPCFG-003.
//
// Secrecy: see the contract in the project_config.go header. Env keys are read
// by name. The value of GITHUB_READ_ONLY and GITHUB_TOOLSETS is the only value
// inspected, and only to derive a valState; it is never stored or echoed.

const (
	githubMCPBinary   = "github-mcp-server"
	githubMCPImage    = "ghcr.io/github/github-mcp-server"
	githubOldPackage  = "@modelcontextprotocol/server-github"
	githubTokenKey    = "GITHUB_PERSONAL_ACCESS_TOKEN"
	githubReadOnlyKey = "GITHUB_READ_ONLY"
	githubToolsetsKey = "GITHUB_TOOLSETS"
	githubToolsKey    = "GITHUB_TOOLS"
	supabasePackage   = "@supabase/mcp-server-supabase"
	supabaseBinary    = "mcp-server-supabase"
)

// valState is what an inspected option value reduces to.
type valState int

const (
	stAbsent   valState = iota // not set
	stTrue                     // read-only: parsed true; toolsets: names a restricting list
	stFalse                    // parsed false, or empty
	stWidening                 // toolsets list names all or default
	stUnknown                  // a variable reference, or a value the file does not show
)

// dockerEnvArg is one -e / --env argument of a docker run. val is meaningful
// only when hasVal; it is read for the allowlisted keys and nowhere else.
type dockerEnvArg struct {
	key    string
	val    string
	hasVal bool
}

// mcpEnvView is the env a server entry passes: its env map and, for docker, the
// -e / --env arguments before the image.
type mcpEnvView struct {
	env    map[string]any
	docker []dockerEnvArg
}

func newEnvView(s mcpServer, dockerOpts []string) mcpEnvView {
	v := mcpEnvView{}
	if m, ok := s.Env.(map[string]any); ok {
		v.env = m
	}
	for i := 0; i < len(dockerOpts); i++ {
		a := dockerOpts[i]
		var kv string
		switch {
		case (a == "-e" || a == "--env") && i+1 < len(dockerOpts):
			kv = dockerOpts[i+1]
			i++
		case strings.HasPrefix(a, "--env="):
			kv = strings.TrimPrefix(a, "--env=")
		default:
			continue
		}
		k, val, hasVal := strings.Cut(kv, "=")
		if k != "" {
			v.docker = append(v.docker, dockerEnvArg{key: k, val: val, hasVal: hasVal})
		}
	}
	return v
}

// keys returns the key names the entry passes, sorted and deduped.
func (v mcpEnvView) keys() []string {
	seen := map[string]bool{}
	for k := range v.env {
		seen[k] = true
	}
	for _, d := range v.docker {
		seen[d.key] = true
	}
	out := make([]string, 0, len(seen))
	for k := range seen {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func (v mcpEnvView) has(key string) bool {
	if _, ok := v.env[key]; ok {
		return true
	}
	for _, d := range v.docker {
		if d.key == key {
			return true
		}
	}
	return false
}

// lookup finds key. known is false when the key is passed but its value is not
// visible (a bare docker -e KEY with no env map entry, or a non-string value).
// A value given on the docker command line wins over the env map, since it is
// what the container receives.
func (v mcpEnvView) lookup(key string) (val string, present, known bool) {
	for _, d := range v.docker {
		if d.key == key && d.hasVal {
			return d.val, true, true
		}
	}
	if raw, ok := v.env[key]; ok {
		s, isStr := raw.(string)
		return s, true, isStr
	}
	for _, d := range v.docker {
		if d.key == key {
			return "", true, false
		}
	}
	return "", false, false
}

func isVarRef(val string) bool { return strings.Contains(val, "$") }

// readOnlyState reduces key's value to the state github-mcp-server gives it.
// The server binds GITHUB_READ_ONLY through viper's GetBool, which parses with
// strconv.ParseBool and reads any other string as false.
func (v mcpEnvView) readOnlyState(key string) valState {
	val, present, known := v.lookup(key)
	switch {
	case !present:
		return stAbsent
	case !known || isVarRef(val):
		return stUnknown
	}
	if b, err := strconv.ParseBool(val); err == nil && b {
		return stTrue
	}
	return stFalse
}

// toolsetsState reduces a comma-separated toolset list to a state: widening when
// it names "all" or "default" (the server's own keywords), restricting when it
// names any other toolset, absent when it is empty.
func toolsetsState(list string) valState {
	if isVarRef(list) {
		return stUnknown
	}
	state := stAbsent
	for _, id := range strings.Split(list, ",") {
		switch id = strings.TrimSpace(id); id {
		case "":
		case "all", "default":
			return stWidening
		default:
			state = stTrue
		}
	}
	return state
}

func (v mcpEnvView) toolsetsEnvState(key string) valState {
	val, present, known := v.lookup(key)
	switch {
	case !present:
		return stAbsent
	case !known:
		return stUnknown
	}
	return toolsetsState(val)
}

// flagNameValue parses one program argument as a long flag. pflag normalizes
// an underscore in a flag name to a hyphen, which github-mcp-server relies on.
func flagNameValue(arg string) (name, val string, hasVal, ok bool) {
	if !strings.HasPrefix(arg, "--") || len(arg) == 2 {
		return "", "", false, false
	}
	name, val, hasVal = strings.Cut(arg[2:], "=")
	return strings.ReplaceAll(name, "_", "-"), val, hasVal, true
}

// boolFlagState reads a pflag bool flag: bare means true, =value is parsed with
// ParseBool, and the last occurrence wins.
func boolFlagState(args []string, name string) valState {
	state := stAbsent
	for _, a := range args {
		n, val, hasVal, ok := flagNameValue(a)
		if !ok || n != name {
			continue
		}
		if !hasVal {
			state = stTrue
			continue
		}
		if b, err := strconv.ParseBool(val); err == nil && b {
			state = stTrue
		} else {
			state = stFalse
		}
	}
	return state
}

// sliceFlagValues returns the values of a pflag StringSlice flag, from both the
// --name=v and the --name v forms.
func sliceFlagValues(args []string, name string) []string {
	var out []string
	for i, a := range args {
		n, val, hasVal, ok := flagNameValue(a)
		if !ok || n != name {
			continue
		}
		switch {
		case hasVal:
			out = append(out, val)
		case i+1 < len(args):
			out = append(out, args[i+1])
		}
	}
	return out
}

func mcpCommandBase(cmd string) string {
	base := strings.ToLower(filepath.Base(strings.ReplaceAll(cmd, `\`, "/")))
	return strings.TrimSuffix(base, ".exe")
}

// imageName strips a tag or digest from a docker image reference.
func imageName(ref string) string {
	if i := strings.IndexByte(ref, '@'); i >= 0 {
		ref = ref[:i]
	}
	if i := strings.LastIndexByte(ref, ':'); i > strings.LastIndexByte(ref, '/') {
		ref = ref[:i]
	}
	return ref
}

// npmSpecName strips the version from an npm package spec.
func npmSpecName(spec string) string {
	spec = strings.TrimSpace(spec)
	body := strings.TrimPrefix(spec, "@")
	i := strings.IndexByte(body, '@')
	if i < 0 {
		return spec
	}
	return spec[:len(spec)-len(body)] + body[:i]
}

// npmRunnerPackages returns the package specs an npx, bunx or pnpm dlx command
// runs, read the same way MCPCFG-003 reads them.
func npmRunnerPackages(s mcpServer) []string {
	a := s.Args
	var r runnerArgs
	switch base := mcpCommandBase(s.Command); {
	case base == "npx":
		r = scanRunnerArgs(a, npmPackageFlags, npxValueFlags, npmShellFlags)
	case base == "bunx":
		r = scanRunnerArgs(a, npmPackageFlags, bunxValueFlags, npmShellFlags)
	case base == "pnpm" && len(a) > 0 && a[0] == "dlx":
		r = scanRunnerArgs(a[1:], npmPackageFlags, bunxValueFlags, pnpmShellFlags)
	default:
		return nil
	}
	if len(r.flagSpecs) > 0 {
		return r.flagSpecs
	}
	if r.shellCmd || r.positional == "" {
		return nil
	}
	return []string{r.positional}
}

// githubInvocation recognizes a github-mcp-server launch and returns the
// server's own arguments and the env it receives. For docker, arguments before
// the image belong to docker (its --read-only flag is not the server's), so
// they feed the env view only.
func githubInvocation(s mcpServer) (progArgs []string, env mcpEnvView, ok bool) {
	switch base := mcpCommandBase(s.Command); {
	case base == githubMCPBinary:
		return s.Args, newEnvView(s, nil), true
	case base == "docker" && len(s.Args) > 1 && s.Args[0] == "run":
		for i, a := range s.Args {
			if imageName(a) == githubMCPImage {
				return s.Args[i+1:], newEnvView(s, s.Args[:i]), true
			}
		}
	}
	return nil, mcpEnvView{}, false
}

// classifyGitHubScope returns a reason when a github-mcp-server launch carries
// GITHUB_PERSONAL_ACCESS_TOKEN and sets no read-only or toolset option.
//
// Read-only is credited the way the server parses it: a --read-only flag, or
// GITHUB_READ_ONLY with a value strconv.ParseBool accepts as true; a flag given
// on the command line wins over the env var. A toolset option restricts when it
// names a toolset other than the keywords all and default. --tools alone
// restricts (the server then registers only those tools) but is additive when a
// toolset list widens. A variable reference is credited, since its value is not
// in the file.
func classifyGitHubScope(s mcpServer) string {
	prog, env, ok := githubInvocation(s)
	if !ok || !env.has(githubTokenKey) {
		return ""
	}
	ro := boolFlagState(prog, "read-only")
	if ro == stAbsent {
		ro = env.readOnlyState(githubReadOnlyKey)
	}
	if ro == stTrue || ro == stUnknown {
		return ""
	}
	var restricting, widening, unknown bool
	states := []valState{env.toolsetsEnvState(githubToolsetsKey)}
	for _, list := range sliceFlagValues(prog, "toolsets") {
		states = append(states, toolsetsState(list))
	}
	for _, st := range states {
		switch st {
		case stTrue:
			restricting = true
		case stWidening:
			widening = true
		case stUnknown:
			unknown = true
		}
	}
	toolsSet := env.has(githubToolsKey)
	for _, list := range sliceFlagValues(prog, "tools") {
		if strings.TrimSpace(list) != "" {
			toolsSet = true
		}
	}
	if restricting || unknown || (toolsSet && !widening) {
		return ""
	}
	return "runs with " + githubTokenKey + " and sets no read-only or toolset option"
}

// classifyOldGitHubPackage returns a reason for the deprecated npm package
// @modelcontextprotocol/server-github given a token. Its published source reads
// no option or env var besides the token and registers its tool list, write
// tools included, unconditionally.
func classifyOldGitHubPackage(s mcpServer) string {
	env := newEnvView(s, nil)
	if !env.has(githubTokenKey) {
		return ""
	}
	for _, spec := range npmRunnerPackages(s) {
		if npmSpecName(spec) == githubOldPackage {
			return "runs the deprecated npm GitHub MCP package with " + githubTokenKey +
				", which has no read-only or toolset option"
		}
	}
	return ""
}

// classifySupabaseScope returns a reason when the Supabase MCP server is
// launched with neither --read-only nor --project-ref. A token is not required:
// the server reads SUPABASE_ACCESS_TOKEN from the developer's own environment
// when the file omits it. The CLI parses its arguments with Node's parseArgs,
// where --read-only is a bare boolean and --project-ref takes a value.
func classifySupabaseScope(s mcpServer) string {
	match := mcpCommandBase(s.Command) == supabaseBinary
	for _, spec := range npmRunnerPackages(s) {
		if npmSpecName(spec) == supabasePackage {
			match = true
		}
	}
	if !match {
		return ""
	}
	for _, a := range s.Args {
		if a == "--read-only" || a == "--project-ref" || strings.HasPrefix(a, "--project-ref=") {
			return ""
		}
	}
	return "runs the Supabase MCP server with neither --read-only nor --project-ref"
}

// serviceRoleReasons returns one reason per env key whose name contains
// SERVICE_ROLE. Key names only.
func serviceRoleReasons(env mcpEnvView) []string {
	var out []string
	for _, k := range env.keys() {
		if strings.Contains(strings.ToUpper(k), "SERVICE_ROLE") {
			out = append(out, "passes env key "+k+", named for a service_role credential")
		}
	}
	return out
}

// classifyMCPScope returns the fixed-phrase reasons for MCPCFG-004. A command
// inside the repo is MCPCFG-002's, so the entry is not judged here.
func classifyMCPScope(s mcpServer) []string {
	if s.Command != "" && isRepoRelativePath(s.Command) {
		return nil
	}
	var out []string
	for _, why := range []string{classifyGitHubScope(s), classifyOldGitHubPackage(s), classifySupabaseScope(s)} {
		if why != "" {
			out = append(out, why)
		}
	}
	var dockerOpts []string
	if mcpCommandBase(s.Command) == "docker" {
		dockerOpts = s.Args
	}
	return append(out, serviceRoleReasons(newEnvView(s, dockerOpts))...)
}

// classifyMCPRemoteURL returns a fixed-phrase reason when url or httpUrl is a
// plain http URL whose host is not loopback. A value with a variable reference
// is not judged, since its scheme or host is not in the file.
func classifyMCPRemoteURL(s mcpServer) string {
	for _, raw := range []string{s.URL, s.HTTPURL} {
		if plainHTTPRemote(raw) {
			return "uses a remote URL over plain http"
		}
	}
	return ""
}

func plainHTTPRemote(raw string) bool {
	raw = strings.TrimSpace(raw)
	if raw == "" || strings.Contains(raw, "$") {
		return false
	}
	u, err := url.Parse(raw)
	if err != nil || !strings.EqualFold(u.Scheme, "http") {
		return false
	}
	host := strings.TrimSuffix(u.Hostname(), ".")
	if host == "" || strings.EqualFold(host, "localhost") {
		return false
	}
	if ip := net.ParseIP(host); ip != nil && ip.IsLoopback() {
		return false
	}
	return true
}
