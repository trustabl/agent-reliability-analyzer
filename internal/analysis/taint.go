package analysis

import (
	"bytes"
	"sort"

	sitter "github.com/smacker/go-tree-sitter"

	"github.com/trustabl/trustabl/internal/analysis/astutil"
	"github.com/trustabl/trustabl/internal/models"
)

// Taint engine, version 1: one file, one function, Python and TypeScript.
//
// The only source is a tool parameter. The pass walks the handler body in
// source order, keeps a set of tainted names, and records a flow each time a
// tainted value reaches an argument position of a sink call (taint_sinks.go).
// It follows no call into another function, reads no class or module state and
// tracks no container element beyond a literal; the Limits are written out in
// the rule explanations that depend on it.

const (
	// taintMaxVia caps the hop lines kept on a flow.
	taintMaxVia = 8
	// taintMaxExprDepth bounds expression recursion on adversarial input.
	taintMaxExprDepth = 64
	// taintMaxLoopDepth is how many nested loops are walked twice; deeper loops
	// are walked once so the cost stays linear in the function size.
	taintMaxLoopDepth = 3
	// taintStepBudget bounds the node visits spent on one function.
	taintStepBudget = 400000
)

// taintOrigin is the provenance of one tainted value.
type taintOrigin struct {
	param     string
	kind      models.SourceKind // empty means models.SourceParam
	srcLine   int
	via       []int
	elem      bool // the value is a literal container holding the parameter
	composite bool // the value was built by joining the parameter with other text
}

type taintState map[string][]taintOrigin

func (s taintState) clone() taintState {
	out := make(taintState, len(s))
	for k, v := range s {
		out[k] = v
	}
	return out
}

func originKey(o taintOrigin) string {
	k := o.param
	if o.kind != "" {
		k = string(o.kind) + ":" + k
	}
	if o.elem {
		k += "|e"
	}
	if o.composite {
		k += "|c"
	}
	return k
}

func mergeOrigins(a, b []taintOrigin) []taintOrigin {
	if len(b) == 0 {
		return a
	}
	if len(a) == 0 {
		return b
	}
	seen := make(map[string]bool, len(a)+len(b))
	out := make([]taintOrigin, 0, len(a)+len(b))
	for _, o := range a {
		if !seen[originKey(o)] {
			seen[originKey(o)] = true
			out = append(out, o)
		}
	}
	for _, o := range b {
		if !seen[originKey(o)] {
			seen[originKey(o)] = true
			out = append(out, o)
		}
	}
	sort.Slice(out, func(i, j int) bool { return originKey(out[i]) < originKey(out[j]) })
	return out
}

// unionState merges b into a copy of a.
func unionState(a, b taintState) taintState {
	out := a.clone()
	for k, v := range b {
		out[k] = mergeOrigins(out[k], v)
	}
	return out
}

func killNames(st taintState, names []string) {
	for _, n := range names {
		delete(st, n)
	}
}

// withVia returns origins with line appended to every via list.
func withVia(os []taintOrigin, line int, composite bool) []taintOrigin {
	out := make([]taintOrigin, 0, len(os))
	for _, o := range os {
		n := o
		n.via = append(append([]int(nil), o.via...), line)
		if len(n.via) > taintMaxVia {
			n.via = n.via[:taintMaxVia]
		}
		if composite {
			n.composite = true
		}
		out = append(out, n)
	}
	return out
}

func asElem(os []taintOrigin) []taintOrigin {
	out := make([]taintOrigin, 0, len(os))
	for _, o := range os {
		o.elem = true
		out = append(out, o)
	}
	return out
}

func asComposite(os []taintOrigin) []taintOrigin {
	out := make([]taintOrigin, 0, len(os))
	for _, o := range os {
		o.composite = true
		out = append(out, o)
	}
	return out
}

// dropElem removes container-held origins: a read of one element of a literal
// container is not followed.
func dropElem(os []taintOrigin) []taintOrigin {
	var out []taintOrigin
	for _, o := range os {
		if !o.elem {
			out = append(out, o)
		}
	}
	return out
}

type flowKey struct {
	param, class, callee string
	kind                 models.SourceKind
	line                 int
}

// taintMode selects which sources are read and which flows are kept. The zero value is the
// batch 6 tool pass; every other mode is a batch 7 addition and leaves it unchanged.
type taintMode int

const (
	// modeTool: tool handlers. Parameters reach the batch 6 sinks and memory_write; a
	// secret-named environment value reaches return_value.
	modeTool taintMode = iota
	// modeRepoLLM: any function; the result of a chat-model SDK call reaches shell or code_exec.
	modeRepoLLM
	// modeRepoResume: any function; a parameter reaches the resume keyword of ClaudeAgentOptions.
	modeRepoResume
	// modeSkill: a skill script; a secret-named environment value reaches print or a stdout write.
	modeSkill
)

func (m taintMode) readsEnvSecrets() bool { return m == modeTool || m == modeSkill }

// allowed reports whether a flow of this source kind into this sink class is kept in the mode.
func (m taintMode) allowed(kind models.SourceKind, class string) bool {
	switch kind {
	case "", models.SourceParam:
		if m != modeTool {
			return false
		}
		return class != models.SinkReturnValue && class != models.SinkLogPrint && class != models.SinkClaudeResume
	case models.SourceSecretEnv:
		return (m == modeTool && class == models.SinkReturnValue) || (m == modeSkill && class == models.SinkLogPrint)
	case models.SourceLLMOutput:
		return m == modeRepoLLM && (class == models.SinkShell || class == models.SinkCodeExec)
	case models.SourceHandlerParam:
		return m == modeRepoResume && class == models.SinkClaudeResume
	}
	return false
}

// taintEngine holds the per-function walk state.
type taintEngine struct {
	src   []byte
	py    bool
	file  *taintFileCtx
	flows map[flowKey]models.TaintFlow
	steps int

	// pyDict / pyDictKeys: a Claude Agent SDK handler reads its inputs from one
	// args dict; a read of a declared key is a source.
	dictName string
	dictKeys map[string]bool
	// tsArgs: the handler's first parameter name when it is a plain identifier;
	// a property read of a declared parameter name is a source.
	tsArgs      string
	tsArgsKeys  map[string]bool // nil means any property
	tsArgsLine  int
	loopDepth   int
	cb          map[uint32][]taintOrigin
	killedKeys  map[string]bool // args.key reads ended by a guard
	excludedSet map[string]bool
	mode        taintMode
	fnDepth     int  // nesting of function literals inside the handler (TypeScript)
	envOK       bool // the function mentions the environment, so return_value is worth computing
}

func (e *taintEngine) over() bool {
	e.steps++
	return e.steps > taintStepBudget
}

func (e *taintEngine) record(o taintOrigin, class, callee string, line int) {
	kind := o.kind
	if kind == "" {
		kind = models.SourceParam
	}
	if !e.mode.allowed(kind, class) {
		return
	}
	k := flowKey{o.param, class, callee, kind, line}
	if _, ok := e.flows[k]; ok {
		return
	}
	via := append([]int(nil), o.via...)
	if len(via) > taintMaxVia {
		via = via[:taintMaxVia]
	}
	e.flows[k] = models.TaintFlow{
		Param:      o.param,
		SourceKind: kind,
		SourceLine: o.srcLine,
		SinkClass:  class,
		SinkCallee: callee,
		SinkLine:   line,
		Via:        via,
	}
}

func (e *taintEngine) sortedFlows() []models.TaintFlow {
	out := make([]models.TaintFlow, 0, len(e.flows))
	for _, f := range e.flows {
		out = append(out, f)
	}
	sort.Slice(out, func(i, j int) bool {
		a, b := out[i], out[j]
		if a.SinkLine != b.SinkLine {
			return a.SinkLine < b.SinkLine
		}
		if a.Param != b.Param {
			return a.Param < b.Param
		}
		if a.SourceKind != b.SourceKind {
			return a.SourceKind < b.SourceKind
		}
		if a.SinkClass != b.SinkClass {
			return a.SinkClass < b.SinkClass
		}
		return a.SinkCallee < b.SinkCallee
	})
	return out
}

// taintFileCtx is the per-file context shared by every tool in the file.
type taintFileCtx struct {
	pf         ParsedFile
	pyAliases  map[string]string // local name -> canonical dotted prefix
	memoryVars map[string]bool   // names bound to a mem0-style client
	enums      map[string]bool   // classes deriving from an Enum
	tsFuncs    map[string]*sitter.Node
}

// AttachTaintFlows runs the taint pass over every tool whose function can be
// resolved and stores the result in ToolDef.TaintFlows. Tools are edited in
// place; a tool with no flow is left untouched.
func AttachTaintFlows(tools []models.ToolDef, files []ParsedFile) {
	byPath := make(map[string]ParsedFile, len(files))
	for _, pf := range files {
		byPath[pf.RelPath] = pf
	}
	ctxs := map[string]*taintFileCtx{}
	for i := range tools {
		t := &tools[i]
		pf, ok := byPath[t.FilePath]
		if !ok || pf.Tree == nil {
			continue
		}
		if len(t.ParamNames) == 0 && len(t.InputKeys) == 0 && !bytes.Contains(pf.Source, []byte("environ")) &&
			!bytes.Contains(pf.Source, []byte("getenv")) && !bytes.Contains(pf.Source, []byte("process.env")) {
			continue
		}
		fc := ctxs[t.FilePath]
		if fc == nil {
			fc = newTaintFileCtx(pf)
			ctxs[t.FilePath] = fc
		}
		var flows []models.TaintFlow
		switch {
		case models.IsTSOrJS(t.Language):
			flows = fc.tsToolFlows(*t)
		case t.Language == models.LanguagePython:
			flows = fc.pyToolFlows(*t)
		}
		if len(flows) > 0 {
			t.TaintFlows = flows
		}
	}
}

func newTaintFileCtx(pf ParsedFile) *taintFileCtx {
	fc := &taintFileCtx{pf: pf}
	root := pf.Tree.RootNode()
	if astutil.IsJavaScriptExtension(pf.RelPath) || isTSPath(pf.RelPath) {
		fc.tsFuncs = collectTSFuncs(root, pf.Source)
		fc.memoryVars = collectMemoryVars(root, pf.Source, false)
		return fc
	}
	fc.pyAliases = collectPyAliases(root, pf.Source)
	fc.memoryVars = collectMemoryVars(root, pf.Source, true)
	fc.enums = collectPyEnums(root, pf.Source)
	return fc
}

func isTSPath(p string) bool {
	n := len(p)
	return n > 3 && (p[n-3:] == ".ts" || (n > 4 && (p[n-4:] == ".tsx" || p[n-4:] == ".mts" || p[n-4:] == ".cts")))
}
