package analysis

import (
	"bytes"
	"context"
	"sort"
	"strings"

	sitter "github.com/smacker/go-tree-sitter"

	"github.com/trustabl/trustabl/internal/analysis/astutil"
	"github.com/trustabl/trustabl/internal/models"
	"github.com/trustabl/trustabl/internal/pathclass"
)

// Taint flows outside tool handlers (static batch 7, schema 53), Python only. The same
// one-function engine as the tool pass runs over every function of a file that passes a text
// prefilter, with one source and a fixed set of sinks per mode (taintMode.allowed):
//
//   - modeRepoLLM (RAW-003): the result of a chat-model SDK call (isPyLLMCall) reaching a shell
//     or code-execution call (eval, exec, compile).
//   - modeRepoResume (CSDK-209): a function parameter reaching the resume keyword of
//     ClaudeAgentOptions.
//   - modeSkill (CSKILL-094): a secret-named environment value reaching print or a stdout write
//     in a skill script. See SkillScriptPrintsSecret.
//
// Not followed: a call into another function, a value held in an attribute or a class field, an
// element read from a container other than a literal. Functions under a test path are not walked.

var (
	repoLLMMarkers  = []string{".completions.create", ".messages.create", ".responses.create", "generate_content", "litellm", "ollama", ".invoke(", ".ainvoke(", "ChatCompletion"}
	repoSinkMarkers = []string{"subprocess", "os.system", "os.popen", "os.spawn", "eval(", "exec(", "compile(", "create_subprocess", "pty.spawn"}
)

func containsAnyBytes(b []byte, words []string) bool {
	for _, w := range words {
		if bytes.Contains(b, []byte(w)) {
			return true
		}
	}
	return false
}

// DiscoverRepoFlows returns the flows of the two repo-scope modes over the Python files, sorted
// by path and sink line.
func DiscoverRepoFlows(py []ParsedFile) []models.RepoFlow {
	var out []models.RepoFlow
	for _, pf := range py {
		if pf.Tree == nil || pathclass.Classify(pf.RelPath) == models.OriginTest {
			continue
		}
		llm := containsAnyBytes(pf.Source, repoLLMMarkers) && containsAnyBytes(pf.Source, repoSinkMarkers)
		resume := bytes.Contains(pf.Source, []byte("ClaudeAgentOptions")) && bytes.Contains(pf.Source, []byte("resume"))
		if !llm && !resume {
			continue
		}
		fc := newTaintFileCtx(pf)
		astutil.Walk(pf.Tree.RootNode(), func(n *sitter.Node) bool {
			if n.Type() != "function_definition" {
				return true
			}
			if llm {
				out = append(out, fc.pyFuncFlows(n, modeRepoLLM)...)
			}
			if resume {
				out = append(out, fc.pyFuncFlows(n, modeRepoResume)...)
			}
			return true
		})
	}
	sort.SliceStable(out, func(i, j int) bool {
		a, b := out[i], out[j]
		if a.FilePath != b.FilePath {
			return a.FilePath < b.FilePath
		}
		if a.SinkLine != b.SinkLine {
			return a.SinkLine < b.SinkLine
		}
		return a.SinkClass < b.SinkClass
	})
	return out
}

// pyFuncFlows runs one repo-scope mode over one function.
func (fc *taintFileCtx) pyFuncFlows(fn *sitter.Node, mode taintMode) []models.RepoFlow {
	body := fn.ChildByFieldName("body")
	if body == nil {
		return nil
	}
	e := &taintEngine{src: fc.pf.Source, py: true, file: fc, flows: map[flowKey]models.TaintFlow{}, mode: mode}
	st := taintState{}
	if mode == modeRepoResume {
		if params := fn.ChildByFieldName("parameters"); params != nil {
			for i := 0; i < int(params.NamedChildCount()); i++ {
				p := params.NamedChild(i)
				name, ann, def := pyParam(p, e.src)
				if name == "" || name == "self" || name == "cls" || pyParamExcluded(name, ann, def, fc) {
					continue
				}
				st[name] = []taintOrigin{{param: name, kind: models.SourceHandlerParam, srcLine: astutil.NodeLine(p)}}
			}
		}
		if len(st) == 0 {
			return nil
		}
	}
	e.pyBlock(body, st)
	var out []models.RepoFlow
	name := astutil.FunctionName(fn, fc.pf.Source)
	for _, f := range e.sortedFlows() {
		out = append(out, models.RepoFlow{FilePath: fc.pf.RelPath, Func: name, Source: f.SourceKind, Param: f.Param,
			SourceLine: f.SourceLine, SinkClass: f.SinkClass, SinkCallee: f.SinkCallee, SinkLine: f.SinkLine})
	}
	return out
}

// SkillScriptPrintsSecret reports the 1-indexed line of the first print or stdout write that
// receives a secret-named environment value in a Python skill script, or 0. Module-level
// assignments are visible inside the functions of the same file, so
// `KEY = os.environ["API_KEY"]` followed by `print(KEY)` in a function is a flow. Any other
// file type, and a script that does not read the environment, give 0.
func SkillScriptPrintsSecret(name string, content []byte) int {
	if !strings.HasSuffix(strings.ToLower(name), ".py") {
		return 0
	}
	if !bytes.Contains(content, []byte("environ")) && !bytes.Contains(content, []byte("getenv")) {
		return 0
	}
	if !bytes.Contains(content, []byte("print")) && !bytes.Contains(content, []byte("stdout")) {
		return 0
	}
	tree, err := astutil.ParseCtxTimeout(context.Background(), astutil.NewPyParser(), content)
	if err != nil || tree == nil {
		return 0
	}
	root := tree.RootNode()
	pf := ParsedFile{RelPath: name, Source: content, Tree: tree}
	fc := newTaintFileCtx(pf)
	e := &taintEngine{src: content, py: true, file: fc, flows: map[flowKey]models.TaintFlow{}, mode: modeSkill}
	mod := taintState{}
	for i := 0; i < int(root.NamedChildCount()); i++ {
		mod = e.pyStmt(root.NamedChild(i), mod)
	}
	astutil.Walk(root, func(n *sitter.Node) bool {
		if n.Type() != "function_definition" {
			return true
		}
		body := n.ChildByFieldName("body")
		if body == nil {
			return true
		}
		st := mod.clone()
		if params := n.ChildByFieldName("parameters"); params != nil {
			for i := 0; i < int(params.NamedChildCount()); i++ {
				pn, _, _ := pyParam(params.NamedChild(i), content)
				delete(st, pn)
			}
		}
		e.pyBlock(body, st)
		return true
	})
	fl := e.sortedFlows()
	if len(fl) == 0 {
		return 0
	}
	return fl[0].SinkLine
}
