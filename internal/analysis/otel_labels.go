package analysis

import (
	"sort"
	"strings"

	sitter "github.com/smacker/go-tree-sitter"

	"github.com/trustabl/trustabl/internal/analysis/astutil"
	"github.com/trustabl/trustabl/internal/models"
	"github.com/trustabl/trustabl/internal/pathclass"
)

// otelLabelsPyModule and otelLabelsTSModule are the import names of the
// agent-reliability-otel-labels bindings the pass inspects.
const (
	otelLabelsPyModule = "agent_reliability_otel_labels"
	otelLabelsTSModule = "@trustabl/agent-reliability-otel-labels"
)

// otelLabelsCallee maps the last segment of a callee to the rung it proves.
// NeedsReceiver is set for a method: Run.mark_start only ever appears as
// `<run>.mark_start(...)`, so a receiver-less call of that name is some other
// function.
type otelLabelsCallee struct {
	Name          string
	Kind          models.OtelLabelsSignalKind
	NeedsReceiver bool
}

var otelLabelsPyCallees = []otelLabelsCallee{
	{"mark_start", models.OtelLabelsRunStart, true},
	{"mark_tool_span", models.OtelLabelsToolSpan, false},
	{"mark_run_end", models.OtelLabelsRunEnd, false},
}

var otelLabelsTSCallees = []otelLabelsCallee{
	{"markStart", models.OtelLabelsRunStart, true},
	{"markToolSpan", models.OtelLabelsToolSpan, false},
	{"markRunEnd", models.OtelLabelsRunEnd, false},
}

// DiscoverOtelLabels extracts every agent-reliability-otel-labels signal from
// the parsed files: where the library is imported, where a run is started,
// where a tool span is labelled, and where a run's end is recorded.
//
// The call names are plausible names for a project's own helpers, so they
// count only when the library is imported somewhere in the repo. The gate is
// repo-wide rather than per-file because a Run is routinely built in one module
// and handed to another, which then calls run.mark_start(root) without
// importing anything. Each file's own imports are still followed, so an aliased
// name (`mark_tool_span as mts`) resolves.
//
// Like DiscoverObservability, this inspects Python and TypeScript/JavaScript
// only. The Go binding is not inspected.
func DiscoverOtelLabels(parsed []ParsedFile) []models.OtelLabelsSignal {
	type labelledFile struct {
		pf      ParsedFile
		lang    models.Language
		test    bool
		aliases map[string]string
	}
	var (
		out      []models.OtelLabelsSignal
		files    []labelledFile
		imported bool
	)
	for _, pf := range parsed {
		lang, ok := otelLabelsLanguage(pf)
		if !ok {
			continue
		}
		f := labelledFile{pf: pf, lang: lang, test: pathclass.Classify(pf.RelPath) == models.OriginTest}
		var imports bool
		f.aliases, imports = otelLabelsImports(pf, lang)
		if imports {
			imported = true
			out = append(out, models.OtelLabelsSignal{
				Kind:       models.OtelLabelsImport,
				File:       pf.RelPath,
				StartLine:  1,
				EndLine:    1,
				Language:   lang,
				TestOrigin: f.test,
			})
		}
		out = append(out, otelLabelsAttrSignals(pf, lang, f.test)...)
		files = append(files, f)
	}
	if imported {
		for _, f := range files {
			out = append(out, otelLabelsCallSignals(f.pf, f.lang, f.test, f.aliases)...)
		}
	}
	if len(out) == 0 {
		return nil
	}
	sortOtelLabels(out)
	return out
}

// otelLabelsLanguage reports the language a parsed file is inspected as.
// Dispatches on extension explicitly, for the reason DiscoverObservability
// does: the parsed set also carries Go/C#/PHP/Rust trees.
func otelLabelsLanguage(pf ParsedFile) (models.Language, bool) {
	switch {
	case pf.Tree == nil:
		return "", false
	case astutil.ParserKindForExtension(pf.RelPath) != "":
		return models.LanguageTypeScript, true
	case strings.HasSuffix(pf.RelPath, ".py"):
		return models.LanguagePython, true
	}
	return "", false
}

// sortOtelLabels orders signals by file, line, kind and key. The sort is
// stable, so two signals equal on all four keep the deterministic order the
// tree walk produced.
func sortOtelLabels(out []models.OtelLabelsSignal) {
	sort.SliceStable(out, func(i, j int) bool {
		a, b := out[i], out[j]
		switch {
		case a.File != b.File:
			return a.File < b.File
		case a.StartLine != b.StartLine:
			return a.StartLine < b.StartLine
		case a.Kind != b.Kind:
			return a.Kind < b.Kind
		default:
			return a.Key < b.Key
		}
	})
}

func isOtelLabelsPyModule(mod string) bool {
	return mod == otelLabelsPyModule || strings.HasPrefix(mod, otelLabelsPyModule+".")
}

func isOtelLabelsTSModule(mod string) bool {
	return mod == otelLabelsTSModule || strings.HasPrefix(mod, otelLabelsTSModule+"/")
}

// otelLabelsImports reports whether the file imports the library, and returns
// the file's alias map: local name → library name, with "*" for a module or
// namespace alias (`import agent_reliability_otel_labels as otl`,
// `import * as otl from "..."`).
func otelLabelsImports(pf ParsedFile, lang models.Language) (map[string]string, bool) {
	if lang == models.LanguageTypeScript {
		aliases := astutil.TSImportAliasesMatch(pf.Tree.RootNode(), pf.Source, isOtelLabelsTSModule)
		return aliases, len(aliases) > 0
	}
	if !fileImportsModule(pf, isOtelLabelsPyModule) {
		return nil, false
	}
	return pyOtelLabelsAliases(pf), true
}

// pyOtelLabelsAliases maps the names a Python file binds from the library.
// `from agent_reliability_otel_labels import Run` binds Run → Run;
// `... import mark_tool_span as mts` binds mts → mark_tool_span;
// `import agent_reliability_otel_labels as otl` binds otl → "*".
func pyOtelLabelsAliases(pf ParsedFile) map[string]string {
	out := map[string]string{}
	astutil.Walk(pf.Tree.RootNode(), func(n *sitter.Node) bool {
		switch n.Type() {
		case "import_from_statement":
			if !isOtelLabelsPyModule(astutil.NodeText(n.ChildByFieldName("module_name"), pf.Source)) {
				return true
			}
			for i := 0; i < int(n.ChildCount()); i++ {
				if n.FieldNameForChild(i) != "name" {
					continue
				}
				c := n.Child(i)
				switch c.Type() {
				case "dotted_name":
					name := astutil.NodeText(c, pf.Source)
					out[name] = name
				case "aliased_import":
					out[astutil.NodeText(c.ChildByFieldName("alias"), pf.Source)] =
						astutil.NodeText(c.ChildByFieldName("name"), pf.Source)
				}
			}
		case "import_statement":
			for i := 0; i < int(n.NamedChildCount()); i++ {
				c := n.NamedChild(i)
				if c.Type() == "aliased_import" &&
					isOtelLabelsPyModule(astutil.NodeText(c.ChildByFieldName("name"), pf.Source)) {
					out[astutil.NodeText(c.ChildByFieldName("alias"), pf.Source)] = "*"
				}
			}
		}
		return true
	})
	return out
}

// otelLabelsCalleeName resolves a callee to the library name it calls, and
// reports whether it has a receiver. A bare name goes through the file's alias
// map, so `mts(...)` after `import mark_tool_span as mts` is mark_tool_span. A
// dotted callee keeps its last segment: `run.mark_start` and
// `otl.mark_tool_span` both resolve by name, as before aliases were followed.
func otelLabelsCalleeName(callee string, aliases map[string]string) (string, bool) {
	if i := strings.LastIndex(callee, "."); i >= 0 {
		return callee[i+1:], true
	}
	if orig, ok := aliases[callee]; ok && orig != "*" && orig != "default" {
		return orig, false
	}
	return callee, false
}

// otelLabelsCallSignals collects the run-start, tool-span and run-end calls in
// one file.
func otelLabelsCallSignals(pf ParsedFile, lang models.Language, test bool, aliases map[string]string) []models.OtelLabelsSignal {
	callType, callees := "call", otelLabelsPyCallees
	if lang == models.LanguageTypeScript {
		callType, callees = "call_expression", otelLabelsTSCallees
	}
	var out []models.OtelLabelsSignal
	astutil.Walk(pf.Tree.RootNode(), func(n *sitter.Node) bool {
		if sig, ok := otelLabelsRunSignal(n, pf, lang, test, aliases); ok {
			out = append(out, sig)
		}
		if n.Type() != callType {
			return true
		}
		fn := n.ChildByFieldName("function")
		if fn == nil {
			return true
		}
		name, hasReceiver := otelLabelsCalleeName(strings.TrimSpace(astutil.NodeText(fn, pf.Source)), aliases)
		for _, c := range callees {
			if name != c.Name || (c.NeedsReceiver && !hasReceiver) {
				continue
			}
			sig := models.OtelLabelsSignal{
				Kind:       c.Kind,
				File:       pf.RelPath,
				StartLine:  astutil.NodeLine(n),
				EndLine:    astutil.NodeEndLine(n),
				Language:   lang,
				TestOrigin: test,
			}
			otelLabelsCallFacts(&sig, n, pf.Source, lang)
			out = append(out, sig)
		}
		return true
	})
	return out
}
