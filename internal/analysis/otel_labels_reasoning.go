package analysis

import (
	"regexp"
	"strings"
	"unicode"

	sitter "github.com/smacker/go-tree-sitter"

	"github.com/trustabl/trustabl/internal/analysis/astutil"
	"github.com/trustabl/trustabl/internal/models"
)

// otelLabelsCoTPrefix is the namespace of the reasoning labels. A hand-written
// key under it is recorded whether or not the library defines it, because
// trustabl.cot.text is the worst case and no table of known keys would name it.
const otelLabelsCoTPrefix = "trustabl.cot."

// otelLabelsCoTFlagKeys are the reasoning keys whose values are a flag or an
// enum, so a value there is not reasoning text in any shape a mistake produces.
var otelLabelsCoTFlagKeys = map[string]bool{
	"trustabl.cot.present": true,
	"trustabl.cot.mode":    true,
}

// otelLabelsHashCalls are the substrings that show a hand-written value went
// through a hash: the library's own canonical_fp / canonicalFp, or a hash the
// team computed itself. Matched against the lowercased value expression, and only
// ever used to decline to report. A bare "digest" is not here: a digest of the
// reasoning is usually a summary of it.
var otelLabelsHashCalls = []string{"canonical_fp", "canonicalfp", "fingerprint", "sha256", "sha-256", "hexdigest", ".digest(", "hashlib", "createhash", "hash("}

// otelLabelsCoTConstants are the library's own names for the reasoning keys. A
// hand-written label through one of them is the likeliest mistake of all, and it
// reads as more trustworthy than a string literal.
var otelLabelsCoTConstants = map[string]string{
	"COT_PRESENT": "trustabl.cot.present",
	"COT_TEXT_FP": "trustabl.cot.text_fp",
	"COT_MODE":    "trustabl.cot.mode",
}

// otelLabelsAttrKey returns the label key an expression names: a string literal,
// or one of the library's constants, bare or through a module (keys.COT_TEXT_FP).
// A plain object key is its own name, so only a computed one is read as a constant.
func otelLabelsAttrKey(n *sitter.Node, src []byte, lang models.Language) (string, bool) {
	if key, ok := otelLabelsStringLiteral(n, src, lang); ok {
		return key, true
	}
	if n == nil {
		return "", false
	}
	switch n.Type() {
	case "identifier", "attribute", "member_expression", "computed_property_name":
	default:
		return "", false
	}
	text := strings.Trim(astutil.NodeText(n, src), "[] ")
	key, ok := otelLabelsCoTConstants[text[strings.LastIndex(text, ".")+1:]]
	return key, ok
}

// otelLabelsLooksHashed reports whether a lowercased value expression shows a
// hash: a hash call, or a plain name that ends the way a variable holding one is
// named (text_fp, self.reasoning_hash).
func otelLabelsLooksHashed(lowered string) bool {
	for _, marker := range otelLabelsHashCalls {
		if strings.Contains(lowered, marker) {
			return true
		}
	}
	name := lowered[strings.LastIndex(lowered, ".")+1:]
	for _, r := range name {
		if r != '_' && !unicode.IsLetter(r) && !unicode.IsDigit(r) {
			return false
		}
	}
	return strings.HasSuffix(name, "fp") || strings.HasSuffix(name, "hash")
}

// otelLabelsValueUnhashed reports whether a hand-written trustabl.cot.* value
// could be the reasoning itself: a runtime value that is not visibly a hash.
// A literal is not runtime text. A missing value says nothing, so it is not
// reported either.
func otelLabelsValueUnhashed(key string, value *sitter.Node, src []byte, lang models.Language) bool {
	if !strings.HasPrefix(key, otelLabelsCoTPrefix) || otelLabelsCoTFlagKeys[key] || value == nil {
		return false
	}
	text := astutil.NodeText(value, src)
	switch value.Type() {
	case "integer", "float", "number", "true", "false", "none", "null", "undefined":
		return false
	case "string":
		// In Python an f-string is a string node too, and it can hold anything.
		if lang == models.LanguageTypeScript {
			return false
		}
		if _, plain := pyStringLiteral(text); plain {
			return false
		}
	}
	return !otelLabelsLooksHashed(strings.ToLower(text))
}

// otelLabelsSingleValue returns the value passed to set_attribute /
// setAttribute: the second positional argument, or Python's value= keyword.
func otelLabelsSingleValue(args *sitter.Node, src []byte) *sitter.Node {
	positional := 0
	for i := 0; i < int(args.NamedChildCount()); i++ {
		c := args.NamedChild(i)
		switch c.Type() {
		case "comment":
			continue
		case "keyword_argument":
			if astutil.NodeText(c.ChildByFieldName("name"), src) == "value" {
				return c.ChildByFieldName("value")
			}
			continue
		}
		if positional == 1 {
			return c
		}
		positional++
	}
	return nil
}

// otelLabelsReasoningOptions are the model options that turn on reasoning, as a
// keyword argument or an object key.
var otelLabelsReasoningOptions = map[string]bool{
	"thinking":                true, // Anthropic, LangChain ChatAnthropic, LiteLLM
	"reasoning":               true, // OpenAI Responses, OpenAI Agents ModelSettings, CrewAI
	"reasoning_effort":        true, // OpenAI chat completions, LangChain ChatOpenAI, LiteLLM
	"reasoningEffort":         true, // Vercel AI SDK openai provider options
	"max_thinking_tokens":     true, // Claude Agent SDK options (Python)
	"maxThinkingTokens":       true, // Claude Agent SDK options (TypeScript)
	"thinking_config":         true, // Google GenAI, ADK BuiltInPlanner
	"thinkingConfig":          true, // Google GenAI, Vercel AI SDK google provider options
	"anthropic_thinking":      true, // Pydantic AI
	"openai_reasoning_effort": true, // Pydantic AI
	"google_thinking_config":  true, // Pydantic AI
}

// otelLabelsReasoningAmbiguous are the option names that are also plausible
// names for a UI flag, a response field or a field of a project's own model
// (Verdict(reasoning=text)). They count only in the shape a provider's option
// takes: an object, or a config call as a keyword argument. A settings variable
// looks the same as a field, so it is not read.
var otelLabelsReasoningAmbiguous = map[string]bool{"thinking": true, "reasoning": true}

// otelLabelsReasoningOffForm matches an option value that leaves reasoning off,
// once otelLabelsReasoningOff has reduced it to one spelling: a type or effort
// that says so, or a budget of zero. A budget of -1 is Gemini's dynamic setting,
// which is on.
var otelLabelsReasoningOffForm = regexp.MustCompile(`type:(disabled|between_tools)|effort:none|budget:0([,)}]|$)`)

// otelLabelsReasoningOff reports whether an option's value leaves reasoning off.
func otelLabelsReasoningOff(value string) bool {
	v := strings.ToLower(value)
	for _, drop := range []string{" ", "\t", "\n", "\r", `"`, "'"} {
		v = strings.ReplaceAll(v, drop, "")
	}
	switch v {
	case "", "none", "null", "undefined", "false", "0", "off", "disabled":
		return true
	}
	return otelLabelsReasoningOffForm.MatchString(strings.ReplaceAll(v, "=", ":"))
}

// otelLabelsReasoningConfigSignals returns one reasoning_config signal per model
// option in the file that turns on reasoning. Options the source cannot show (a
// **settings unpack, an environment variable, a model that reasons by default)
// produce nothing, so a miss here keeps OBS-014 silent rather than wrong.
func otelLabelsReasoningConfigSignals(pf ParsedFile, lang models.Language, test bool) []models.OtelLabelsSignal {
	var out []models.OtelLabelsSignal
	emit := func(n *sitter.Node, key string) {
		out = append(out, models.OtelLabelsSignal{
			Kind:       models.OtelLabelsReasoningConfig,
			File:       pf.RelPath,
			StartLine:  astutil.NodeLine(n),
			EndLine:    astutil.NodeEndLine(n),
			Language:   lang,
			TestOrigin: test,
			Key:        key,
		})
	}
	astutil.Walk(pf.Tree.RootNode(), func(n *sitter.Node) bool {
		switch n.Type() {
		case "keyword_argument":
			name, value := n.ChildByFieldName("name"), n.ChildByFieldName("value")
			if name == nil || value == nil {
				return true
			}
			key := astutil.NodeText(name, pf.Source)
			if !otelLabelsReasoningOptions[key] {
				return true
			}
			// reasoning=True is CrewAI's documented switch. No provider takes a
			// bool for thinking, so thinking=True is a project's own flag.
			isConfig := value.Type() == "dictionary" || value.Type() == "call" || (key == "reasoning" && value.Type() == "true")
			if otelLabelsReasoningOff(astutil.NodeText(value, pf.Source)) || (otelLabelsReasoningAmbiguous[key] && !isConfig) {
				return true
			}
			emit(n, key)
		case "pair":
			key, value := n.ChildByFieldName("key"), n.ChildByFieldName("value")
			if key == nil || value == nil {
				return true
			}
			name := strings.Trim(astutil.NodeText(key, pf.Source), `"'`)
			if !otelLabelsReasoningOptions[name] {
				return true
			}
			isObject := value.Type() == "object" || value.Type() == "dictionary"
			if otelLabelsReasoningAmbiguous[name] && !isObject {
				return true
			}
			if otelLabelsReasoningOff(astutil.NodeText(value, pf.Source)) {
				return true
			}
			emit(n, name)
		}
		return true
	})
	return out
}
