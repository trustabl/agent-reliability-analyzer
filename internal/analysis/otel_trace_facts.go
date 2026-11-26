package analysis

import (
	"regexp"
	"sort"
	"strings"

	sitter "github.com/smacker/go-tree-sitter"

	"github.com/trustabl/trustabl/internal/analysis/astutil"
	"github.com/trustabl/trustabl/internal/models"
	"github.com/trustabl/trustabl/internal/pathclass"
)

// Trace facts for OBS-015 and OBS-016: which span attributes the code writes by
// hand, and which judge-style functions call a chat model by a floating alias.

// SpanAttrKeys are the span attribute names the rules OBS-015 and OBS-016 may
// ask about: the OpenTelemetry GenAI names (gen_ai.response.model,
// gen_ai.conversation.id, gen_ai.tool.call.id, as listed in the OpenTelemetry
// semantic-conventions attribute registry) and the trustabl.* keys that
// agent-reliability-otel-labels defines (spec/keys.json). The loader rejects a
// repo_otel_span_attrs_missing key outside this set.
func SpanAttrKeys() []string {
	keys := []string{"gen_ai.response.model", "gen_ai.request.model", "gen_ai.conversation.id", "gen_ai.tool.call.id"}
	keys = append(keys, OtelLabelsAttributeKeys()...)
	// The three keys spec/keys.json defines that the vendored 1.0.0 manifest behind
	// otelLabelsKeys does not carry yet.
	keys = append(keys, "trustabl.expected_tools", "trustabl.retrieval.result_count", "trustabl.progress_marker")
	sort.Strings(keys)
	return keys
}

var spanAttrKeySet = func() map[string]bool {
	m := map[string]bool{}
	for _, k := range SpanAttrKeys() {
		m[k] = true
	}
	return m
}()

// spanAttrConstants maps the last segment of an OpenTelemetry semconv constant
// to its key.
var spanAttrConstants = map[string]string{
	"GEN_AI_RESPONSE_MODEL":       "gen_ai.response.model",
	"GEN_AI_REQUEST_MODEL":        "gen_ai.request.model",
	"GEN_AI_CONVERSATION_ID":      "gen_ai.conversation.id",
	"GEN_AI_TOOL_CALL_ID":         "gen_ai.tool.call.id",
	"ATTR_GEN_AI_RESPONSE_MODEL":  "gen_ai.response.model",
	"ATTR_GEN_AI_REQUEST_MODEL":   "gen_ai.request.model",
	"ATTR_GEN_AI_CONVERSATION_ID": "gen_ai.conversation.id",
	"ATTR_GEN_AI_TOOL_CALL_ID":    "gen_ai.tool.call.id",
}

func spanAttrKey(n *sitter.Node, src []byte, lang models.Language) (string, bool) {
	if k, ok := otelLabelsStringLiteral(n, src, lang); ok {
		return k, spanAttrKeySet[k]
	}
	if n == nil {
		return "", false
	}
	switch n.Type() {
	case "identifier", "attribute", "member_expression":
		text := astutil.NodeText(n, src)
		k, ok := spanAttrConstants[text[strings.LastIndex(text, ".")+1:]]
		return k, ok
	}
	return "", false
}

// DiscoverSpanAttrWrites returns every span attribute write under a SpanAttrKeys
// key, in Python and TypeScript/JavaScript files.
func DiscoverSpanAttrWrites(parsed []ParsedFile) []models.SpanAttrWrite {
	var out []models.SpanAttrWrite
	for _, pf := range parsed {
		lang, ok := otelLabelsLanguage(pf)
		if !ok {
			continue
		}
		test := pathclass.Classify(pf.RelPath) == models.OriginTest
		callType, single, multi := "call", "set_attribute", "set_attributes"
		if lang == models.LanguageTypeScript {
			callType, single, multi = "call_expression", "setAttribute", "setAttributes"
		}
		emit := func(call *sitter.Node, key string) {
			out = append(out, models.SpanAttrWrite{Key: key, File: pf.RelPath, StartLine: astutil.NodeLine(call), Language: lang, TestOrigin: test})
		}
		astutil.Walk(pf.Tree.RootNode(), func(n *sitter.Node) bool {
			if n.Type() != callType {
				return true
			}
			callee := astutil.NodeText(n.ChildByFieldName("function"), pf.Source)
			name := callee[strings.LastIndex(callee, ".")+1:]
			if name != single && name != multi {
				return true
			}
			args := n.ChildByFieldName("arguments")
			if args == nil {
				return true
			}
			first := firstPositionalArg(args)
			if first == nil {
				return true
			}
			if name == single {
				if k, ok := spanAttrKey(first, pf.Source, lang); ok {
					emit(n, k)
				}
				return true
			}
			if first.Type() != "dictionary" && first.Type() != "object" {
				return true
			}
			for i := 0; i < int(first.NamedChildCount()); i++ {
				p := first.NamedChild(i)
				if p.Type() != "pair" {
					continue
				}
				if k, ok := spanAttrKey(p.ChildByFieldName("key"), pf.Source, lang); ok {
					emit(n, k)
				}
			}
			return true
		})
	}
	sort.SliceStable(out, func(i, j int) bool {
		a, b := out[i], out[j]
		if a.File != b.File {
			return a.File < b.File
		}
		if a.StartLine != b.StartLine {
			return a.StartLine < b.StartLine
		}
		return a.Key < b.Key
	})
	return out
}

// ── judge model calls ───────────────────────────────────────────────────────

var judgeNameRe = regexp.MustCompile(`(?i)(judge|guardrail|moderat|monitor|grader|critic)`)

// floatingModelAlias reports whether a model name is an alias that names the
// newest version by its own wording: a name ending in -latest, with or without a
// provider prefix. A bare family name such as gpt-4o is not judged, because
// whether it is repointed is a fact about the provider, not about the name.
func floatingModelAlias(model string) bool {
	m := strings.ToLower(strings.TrimSpace(model))
	if i := strings.LastIndex(m, "/"); i >= 0 {
		m = m[i+1:]
	}
	return !strings.HasPrefix(m, "ft:") && strings.HasSuffix(m, "-latest")
}

var tsFuncTypes = map[string]bool{"function_declaration": true, "method_definition": true, "arrow_function": true, "function_expression": true, "function": true}

// judgeFuncName returns the name of a function node, or "".
func judgeFuncName(fn *sitter.Node, src []byte, lang models.Language) string {
	if nm := fn.ChildByFieldName("name"); nm != nil {
		return astutil.NodeText(nm, src)
	}
	if lang == models.LanguageTypeScript {
		if p := fn.Parent(); p != nil && p.Type() == "variable_declarator" {
			return astutil.NodeText(p.ChildByFieldName("name"), src)
		}
	}
	return ""
}

// DiscoverJudgeModelCalls finds, in each function named as a judge, guardrail,
// monitor, moderator, grader or critic, a model name written as a string literal:
// a model= keyword argument or model property, or an init_chat_model("...") first
// argument. Test paths are marked, not dropped.
func DiscoverJudgeModelCalls(parsed []ParsedFile) []models.JudgeModelCall {
	var out []models.JudgeModelCall
	for _, pf := range parsed {
		lang, ok := otelLabelsLanguage(pf)
		if !ok {
			continue
		}
		test := pathclass.Classify(pf.RelPath) == models.OriginTest
		isFunc := func(t string) bool { return t == "function_definition" }
		if lang == models.LanguageTypeScript {
			isFunc = func(t string) bool { return tsFuncTypes[t] }
		}
		astutil.Walk(pf.Tree.RootNode(), func(fn *sitter.Node) bool {
			if !isFunc(fn.Type()) {
				return true
			}
			name := judgeFuncName(fn, pf.Source, lang)
			if name == "" || !judgeNameRe.MatchString(name) {
				return true
			}
			body := fn.ChildByFieldName("body")
			if body == nil {
				return true
			}
			astutil.Walk(body, func(n *sitter.Node) bool {
				var val *sitter.Node
				switch n.Type() {
				case "keyword_argument":
					if astutil.NodeText(n.ChildByFieldName("name"), pf.Source) == "model" {
						val = n.ChildByFieldName("value")
					}
				case "pair":
					if strings.Trim(astutil.NodeText(n.ChildByFieldName("key"), pf.Source), `"'`) == "model" {
						val = n.ChildByFieldName("value")
					}
				case "call", "call_expression":
					callee := astutil.NodeText(n.ChildByFieldName("function"), pf.Source)
					if callee == "init_chat_model" || strings.HasSuffix(callee, ".init_chat_model") {
						if args := n.ChildByFieldName("arguments"); args != nil {
							val = firstPositionalArg(args)
						}
					}
				}
				if val == nil {
					return true
				}
				if m, ok := otelLabelsStringLiteral(val, pf.Source, lang); ok && m != "" {
					out = append(out, models.JudgeModelCall{
						Function: name, Model: m, Floating: floatingModelAlias(m),
						File: pf.RelPath, StartLine: astutil.NodeLine(n), Language: lang, TestOrigin: test,
					})
				}
				return true
			})
			return true
		})
	}
	sort.SliceStable(out, func(i, j int) bool {
		a, b := out[i], out[j]
		if a.File != b.File {
			return a.File < b.File
		}
		if a.StartLine != b.StartLine {
			return a.StartLine < b.StartLine
		}
		return a.Model < b.Model
	})
	return out
}
