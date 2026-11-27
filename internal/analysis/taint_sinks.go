package analysis

import (
	"strings"

	sitter "github.com/smacker/go-tree-sitter"

	"github.com/trustabl/trustabl/internal/analysis/astutil"
	"github.com/trustabl/trustabl/internal/models"
)

// Sink tables. The shell, code-exec and TS write callee lists are read by both
// the taint pass and the older has_shell_call / has_code_exec_call /
// has_write_call predicates and the TS handler facts, so there is one list.

// IsPyShellCallee reports whether a Python callee text is an OS shell primitive.
func IsPyShellCallee(c string) bool {
	return strings.HasPrefix(c, "subprocess.") || c == "os.system" || c == "os.popen" || strings.HasPrefix(c, "os.spawn")
}

// IsPyCodeExecCallee reports whether a Python callee text is eval, exec or compile.
func IsPyCodeExecCallee(c string) bool {
	return c == "eval" || c == "exec" || c == "compile"
}

// TSShellCallees are the child_process callee texts that run a command.
var TSShellCallees = map[string]bool{
	"execSync": true, "exec": true, "execFile": true, "execFileSync": true, "spawn": true, "spawnSync": true, "fork": true,
	"child_process.exec": true, "child_process.execSync": true, "child_process.spawn": true, "child_process.spawnSync": true,
	"child_process.execFile": true, "child_process.execFileSync": true, "child_process.fork": true,
}

// TSWriteCallees are the TypeScript filesystem write callee texts.
var TSWriteCallees = map[string]bool{
	"writeFile": true, "writeFileSync": true, "appendFile": true, "appendFileSync": true, "createWriteStream": true,
	"fs.writeFile": true, "fs.writeFileSync": true, "fs.appendFile": true, "fs.appendFileSync": true,
	"fs.createWriteStream": true, "fsPromises.writeFile": true, "fsPromises.appendFile": true,
}

var pyShellExtra = map[string]bool{
	"os.system": true, "asyncio.create_subprocess_shell": true, "asyncio.create_subprocess_exec": true, "pty.spawn": true,
	"commands.getoutput": true,
}

var pyOSPathIO = map[string]string{
	"os.listdir": models.SinkPathIO, "os.scandir": models.SinkPathIO, "os.walk": models.SinkPathIO, "os.stat": models.SinkPathIO,
	"os.lstat": models.SinkPathIO, "os.readlink": models.SinkPathIO, "os.chdir": models.SinkPathIO, "os.startfile": models.SinkPathIO,
	"os.path.exists": models.SinkPathIO, "os.path.isfile": models.SinkPathIO, "os.path.isdir": models.SinkPathIO,
	"os.path.islink": models.SinkPathIO, "os.path.getsize": models.SinkPathIO, "os.path.getmtime": models.SinkPathIO,
	"os.path.getatime": models.SinkPathIO, "os.path.getctime": models.SinkPathIO,
	"os.remove": models.SinkFileWrite, "os.unlink": models.SinkFileWrite, "os.rmdir": models.SinkFileWrite,
	"os.removedirs": models.SinkFileWrite, "os.rename": models.SinkFileWrite, "os.renames": models.SinkFileWrite,
	"os.replace": models.SinkFileWrite, "os.mkdir": models.SinkFileWrite, "os.makedirs": models.SinkFileWrite,
	"os.chmod": models.SinkFileWrite, "os.chown": models.SinkFileWrite, "os.symlink": models.SinkFileWrite,
	"os.link": models.SinkFileWrite, "os.truncate": models.SinkFileWrite, "os.utime": models.SinkFileWrite,
	"shutil.copy": models.SinkFileWrite, "shutil.copy2": models.SinkFileWrite, "shutil.copyfile": models.SinkFileWrite,
	"shutil.copytree": models.SinkFileWrite, "shutil.move": models.SinkFileWrite, "shutil.rmtree": models.SinkFileWrite,
	"shutil.make_archive": models.SinkFileWrite, "shutil.unpack_archive": models.SinkFileWrite,
}

var pyPathMethods = map[string]string{
	"read_text": models.SinkPathIO, "read_bytes": models.SinkPathIO, "iterdir": models.SinkPathIO, "glob": models.SinkPathIO,
	"rglob": models.SinkPathIO, "exists": models.SinkPathIO, "is_file": models.SinkPathIO, "is_dir": models.SinkPathIO,
	"stat": models.SinkPathIO, "lstat": models.SinkPathIO, "open": models.SinkPathIO,
	"write_text": models.SinkFileWrite, "write_bytes": models.SinkFileWrite, "unlink": models.SinkFileWrite,
	"rmdir": models.SinkFileWrite, "mkdir": models.SinkFileWrite, "touch": models.SinkFileWrite, "rename": models.SinkFileWrite,
	"symlink_to": models.SinkFileWrite, "chmod": models.SinkFileWrite,
}

var vectorMethods = func() map[string]bool {
	m := map[string]bool{}
	for _, s := range []string{"similarity_search", "similarity_search_with_score", "similarity_search_by_vector",
		"similarity_search_with_relevance_scores", "max_marginal_relevance_search", "as_retriever", "query", "search",
		"get", "delete", "asimilarity_search", "asimilarity_search_with_score", "amax_marginal_relevance_search",
		"similaritySearch", "similaritySearchWithScore", "maxMarginalRelevanceSearch", "asRetriever"} {
		m[s] = true
	}
	return m
}()

var vectorKwargs = map[string]bool{"filter": true, "filters": true, "where": true, "search_kwargs": true, "searchKwargs": true,
	"pre_filter": true, "expr": true, "where_document": true}

var storeMethods = map[string]bool{"put": true, "get": true, "search": true, "delete": true, "list_namespaces": true,
	"aput": true, "aget": true, "asearch": true, "adelete": true, "alist_namespaces": true, "listNamespaces": true}

var memoryMethods = map[string]bool{"add": true, "search": true, "get_all": true, "delete_all": true, "get": true,
	"update": true, "delete": true, "history": true, "reset": true}

var memoryScopeKeys = map[string]bool{"user_id": true, "agent_id": true, "run_id": true, "app_id": true, "namespace": true,
	"userId": true, "agentId": true, "runId": true}

var sqlMethods = map[string]bool{"execute": true, "executemany": true, "executescript": true, "exec_driver_sql": true,
	"raw": true, "query": true, "$queryRawUnsafe": true, "$executeRawUnsafe": true, "unsafe": true}

// pyCallArgs splits a call's arguments.
func pyCallArgs(call *sitter.Node, src []byte) (pos []*sitter.Node, kw map[string]*sitter.Node) {
	kw = map[string]*sitter.Node{}
	args := call.ChildByFieldName("arguments")
	if args == nil {
		return
	}
	for i := 0; i < int(args.NamedChildCount()); i++ {
		c := args.NamedChild(i)
		if c.Type() == "keyword_argument" {
			kw[astutil.NodeText(c.ChildByFieldName("name"), src)] = c.ChildByFieldName("value")
			continue
		}
		pos = append(pos, c)
	}
	return
}

func (e *taintEngine) hit(class, callee string, nodes []*sitter.Node, st taintState, needComposite bool, line int) {
	for _, n := range nodes {
		if n == nil {
			continue
		}
		for _, o := range e.expr(n, st) {
			if needComposite && !o.composite {
				continue
			}
			e.record(o, class, callee, line)
		}
	}
}

func kwNodes(kw map[string]*sitter.Node, names ...string) []*sitter.Node {
	var out []*sitter.Node
	for _, n := range names {
		if v, ok := kw[n]; ok {
			out = append(out, v)
		}
	}
	return out
}

func (e *taintEngine) receiverIsStore(recv string) bool {
	return strings.Contains(strings.ToLower(lastSegment(recv)), "store")
}

func (e *taintEngine) receiverIsMemory(recv string) bool {
	if e.file != nil && e.file.memoryVars[recv] {
		return true
	}
	l := strings.ToLower(lastSegment(recv))
	return strings.Contains(l, "mem0") || strings.Contains(l, "memory")
}

func (e *taintEngine) pySinks(call *sitter.Node, st taintState) {
	fn := call.ChildByFieldName("function")
	if fn == nil {
		return
	}
	raw := astutil.NodeText(fn, e.src)
	c := e.canon(raw)
	meth := lastSegment(raw)
	line := astutil.NodeLine(call)
	pos, kw := pyCallArgs(call, e.src)
	var recvNode *sitter.Node
	recvText := ""
	if fn.Type() == "attribute" {
		recvNode = fn.ChildByFieldName("object")
		recvText = astutil.NodeText(recvNode, e.src)
	}
	all := append(append([]*sitter.Node{}, pos...), kwNodes(kw, "args", "cwd", "executable", "env")...)
	first := func() []*sitter.Node {
		if len(pos) > 0 {
			return pos[:1]
		}
		return nil
	}

	switch {
	case IsPyShellCallee(c) || pyShellExtra[c]:
		e.hit(models.SinkShell, raw, all, st, false, line)
		return
	case c == "eval" || c == "exec" || c == "compile" || c == "builtins.eval" || c == "builtins.exec":
		e.hit(models.SinkCodeExec, raw, append(first(), kwNodes(kw, "source", "expression", "object")...), st, false, line)
		return
	case c == "sqlalchemy.text" || c == "sqlalchemy.sql.text" || (recvNode != nil && sqlMethods[meth] && meth != "query" && meth != "raw" && meth != "unsafe"):
		e.hit(models.SinkSQLExecute, raw, append(first(), kwNodes(kw, "sql", "query", "statement", "operation")...), st, false, line)
		return
	case c == "open" || c == "io.open" || c == "codecs.open":
		class := models.SinkPathIO
		mode := ""
		if len(pos) > 1 {
			mode = astutil.NodeText(pos[1], e.src)
		} else if m, ok := kw["mode"]; ok {
			mode = astutil.NodeText(m, e.src)
		}
		if strings.ContainsAny(mode, "wax+") {
			class = models.SinkFileWrite
		}
		e.hit(class, raw, append(first(), kwNodes(kw, "file")...), st, false, line)
		return
	case c == "Repo" || c == "git.Repo" || c == "Repo.clone_from" || c == "git.Repo.clone_from" || c == "Repo.init" || c == "git.Repo.init" ||
		(recvNode != nil && strings.HasSuffix(recvText, ".git")):
		e.hit(models.SinkPathIO, raw, append(append([]*sitter.Node{}, pos...), kwNodes(kw, "path", "to_path", "url")...), st, false, line)
		return
	}
	if class, ok := pyOSPathIO[c]; ok {
		e.hit(class, raw, pos, st, false, line)
		return
	}
	if recvNode != nil {
		if class, ok := pyPathMethods[meth]; ok && meth != "open" && meth != "get" {
			for _, o := range e.expr(recvNode, st) {
				if !o.elem {
					e.record(o, class, raw, line)
				}
			}
			return
		}
		if vectorMethods[meth] {
			var nodes []*sitter.Node
			for k, v := range kw {
				if vectorKwargs[k] {
					nodes = append(nodes, v)
				}
			}
			e.hit(models.SinkVectorFilter, raw, sortNodes(nodes), st, false, line)
		}
		if storeMethods[meth] && e.receiverIsStore(recvText) {
			e.hit(models.SinkStoreNamespace, raw, append(first(), kwNodes(kw, "namespace", "namespace_prefix", "prefix")...), st, false, line)
		}
		if memoryMethods[meth] && e.receiverIsMemory(recvText) {
			var nodes []*sitter.Node
			for k, v := range kw {
				if memoryScopeKeys[k] {
					nodes = append(nodes, v)
				}
			}
			e.hit(models.SinkMemoryScopeKey, raw, sortNodes(nodes), st, true, line)
		}
	}
}

func sortNodes(ns []*sitter.Node) []*sitter.Node {
	for i := 1; i < len(ns); i++ {
		for j := i; j > 0 && ns[j].StartByte() < ns[j-1].StartByte(); j-- {
			ns[j], ns[j-1] = ns[j-1], ns[j]
		}
	}
	return ns
}
