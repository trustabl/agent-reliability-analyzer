package analysis_test

import (
	"testing"

	"github.com/trustabl/trustabl/internal/analysis"
	"github.com/trustabl/trustabl/internal/models"
)

func otelLabelsOfKind(sigs []models.OtelLabelsSignal, kind models.OtelLabelsSignalKind) []models.OtelLabelsSignal {
	var out []models.OtelLabelsSignal
	for _, s := range sigs {
		if s.Kind == kind {
			out = append(out, s)
		}
	}
	return out
}

func TestDiscoverOtelLabels_ReasoningCall(t *testing.T) {
	py := parsePyFile(t, "agent.py", `from agent_reliability_otel_labels import mark_reasoning as label


def call(span, text):
    label(span, text=text)
`)
	got := otelLabelsOfKind(analysis.DiscoverOtelLabels([]analysis.ParsedFile{py}), models.OtelLabelsReasoning)
	if len(got) != 1 || got[0].StartLine != 5 {
		t.Errorf("python: got %+v, want one reasoning signal at line 5", got)
	}

	ts := parseTSForTest(t, "agent.ts", `import { markReasoning } from "@trustabl/agent-reliability-otel-labels";

export function call(span, text) {
  markReasoning(span, { text });
}
`)
	got = otelLabelsOfKind(analysis.DiscoverOtelLabels([]analysis.ParsedFile{ts}), models.OtelLabelsReasoning)
	if len(got) != 1 || got[0].StartLine != 4 {
		t.Errorf("typescript: got %+v, want one reasoning signal at line 4", got)
	}
}

// mark_reasoning is a plausible name for a project's own helper, so it counts
// only when the library is imported somewhere in the repo.
func TestDiscoverOtelLabels_ReasoningCallWithoutAnImportIsIgnored(t *testing.T) {
	pf := parsePyFile(t, "helpers.py", `def mark_reasoning(span, text=None):
    return None


mark_reasoning(None, text="x")
`)
	if got := analysis.DiscoverOtelLabels([]analysis.ParsedFile{pf}); len(got) != 0 {
		t.Errorf("got %+v, want no signals without an import", got)
	}
}

const otelLabelsImportPy = "from agent_reliability_otel_labels import Run\n"

// A reasoning option turns the missing label into something worth saying, and
// only in a repo already using the labels: with none of them in sight, nothing
// is recorded and the report stays as it was.
func TestDiscoverOtelLabels_ReasoningConfigOnlyBesideLabels(t *testing.T) {
	src := `client.messages.create(model="m", thinking={"type": "enabled", "budget_tokens": 1024})
`
	pf := parsePyFile(t, "agent.py", src)
	if got := analysis.DiscoverOtelLabels([]analysis.ParsedFile{pf}); len(got) != 0 {
		t.Errorf("got %+v, want nothing without any label in the repo", got)
	}
	labels := parsePyFile(t, "labels.py", otelLabelsImportPy)
	got := otelLabelsOfKind(analysis.DiscoverOtelLabels([]analysis.ParsedFile{pf, labels}), models.OtelLabelsReasoningConfig)
	if len(got) != 1 || got[0].File != "agent.py" || got[0].StartLine != 1 || got[0].Key != "thinking" {
		t.Errorf("got %+v, want one thinking option at agent.py:1", got)
	}
}

func TestDiscoverOtelLabels_ReasoningConfigShapes(t *testing.T) {
	py := []struct {
		name, line string
		want       bool
	}{
		{"anthropic thinking", `client.messages.create(thinking={"type": "enabled"})`, true},
		{"anthropic adaptive thinking", `client.messages.create(thinking={"type": "adaptive"})`, true},
		{"thinking from a config call", `client.messages.create(thinking=build_thinking())`, true},
		{"thinking from a variable, which a field looks like too", `client.messages.create(thinking=settings)`, false},
		{"a structured-output field named reasoning", `Verdict(reasoning=text, answer=a)`, false},
		{"a structured-output field named thinking", `Verdict(thinking=text)`, false},
		{"thinking off through between_tools", `client.messages.create(thinking={"type": "between_tools"})`, false},
		{"thinking disabled", `client.messages.create(thinking={"type": "disabled"})`, false},
		{"thinking as a project's own flag", `render(thinking=True)`, false},
		{"openai effort", `client.chat.completions.create(reasoning_effort="high")`, true},
		{"openai effort none", `client.chat.completions.create(reasoning_effort="none")`, false},
		{"responses api", `client.responses.create(reasoning={"effort": "low"})`, true},
		{"responses api effort none", `client.responses.create(reasoning={"effort": "none"})`, false},
		{"agents model settings", `ModelSettings(reasoning=Reasoning(effort="high"))`, true},
		{"agents model settings with effort none", `ModelSettings(reasoning=Reasoning(effort="none"))`, false},
		{"crewai", `Agent(role="r", reasoning=True)`, true},
		{"crewai off", `Agent(role="r", reasoning=False)`, false},
		{"reasoning none", `Agent(role="r", reasoning=None)`, false},
		{"claude sdk", `ClaudeAgentOptions(max_thinking_tokens=2000)`, true},
		{"claude sdk zero", `ClaudeAgentOptions(max_thinking_tokens=0)`, false},
		{"google", `GenerateContentConfig(thinking_config=ThinkingConfig(thinking_budget=100))`, true},
		{"google dynamic budget", `GenerateContentConfig(thinking_config=ThinkingConfig(thinking_budget=-1))`, true},
		{"google zero budget", `GenerateContentConfig(thinking_config=ThinkingConfig(thinking_budget=0))`, false},
		{"pydantic ai", `AnthropicModelSettings(anthropic_thinking={"type": "enabled"})`, true},
		{"pydantic ai openai", `OpenAIResponsesModelSettings(openai_reasoning_effort="low")`, true},
		{"a dict the call unpacks", `params = {"thinking": {"type": "enabled"}}`, true},
		{"a dict with a flag of that name", `state = {"thinking": True}`, false},
		{"an unrelated keyword", `client.messages.create(temperature=0)`, false},
	}
	for _, tc := range py {
		t.Run("python "+tc.name, func(t *testing.T) {
			sigs := analysis.DiscoverOtelLabels([]analysis.ParsedFile{
				parsePyFile(t, "agent.py", tc.line+"\n"),
				parsePyFile(t, "labels.py", otelLabelsImportPy),
			})
			got := len(otelLabelsOfKind(sigs, models.OtelLabelsReasoningConfig)) > 0
			if got != tc.want {
				t.Errorf("%s: got %v, want %v (%+v)", tc.line, got, tc.want, sigs)
			}
		})
	}

	ts := []struct {
		name, line string
		want       bool
	}{
		{"anthropic thinking", `anthropic.messages.create({ thinking: { type: "enabled" } });`, true},
		{"thinking disabled", `anthropic.messages.create({ thinking: { type: "disabled" } });`, false},
		{"thinking as ui state", `setState({ thinking: true });`, false},
		{"openai effort", `openai.chat.completions.create({ reasoning_effort: "high" });`, true},
		{"vercel openai", `generateText({ providerOptions: { openai: { reasoningEffort: "high" } } });`, true},
		{"vercel off", `generateText({ providerOptions: { openai: { reasoningEffort: "none" } } });`, false},
		{"google", `generateText({ providerOptions: { google: { thinkingConfig: { thinkingBudget: 100 } } } });`, true},
		{"agents sdk", `new Agent({ modelSettings: { reasoning: { effort: "low" } } });`, true},
		{"reasoning as text", `show({ reasoning: "because" });`, false},
		{"claude sdk", `query({ options: { maxThinkingTokens: 2000 } });`, true},
		{"quoted key", `client.create({ "reasoning_effort": "low" });`, true},
	}
	for _, tc := range ts {
		t.Run("typescript "+tc.name, func(t *testing.T) {
			sigs := analysis.DiscoverOtelLabels([]analysis.ParsedFile{
				parseTSForTest(t, "agent.ts", tc.line+"\n"),
				parsePyFile(t, "labels.py", otelLabelsImportPy),
			})
			got := len(otelLabelsOfKind(sigs, models.OtelLabelsReasoningConfig)) > 0
			if got != tc.want {
				t.Errorf("%s: got %v, want %v (%+v)", tc.line, got, tc.want, sigs)
			}
		})
	}
}

func TestDiscoverOtelLabels_ReasoningConfigInATestFileIsMarked(t *testing.T) {
	sigs := analysis.DiscoverOtelLabels([]analysis.ParsedFile{
		parsePyFile(t, "tests/test_agent.py", `client.messages.create(thinking={"type": "enabled"})`+"\n"),
		parsePyFile(t, "labels.py", otelLabelsImportPy),
	})
	got := otelLabelsOfKind(sigs, models.OtelLabelsReasoningConfig)
	if len(got) != 1 || !got[0].TestOrigin {
		t.Errorf("got %+v, want one signal marked TestOrigin", got)
	}
}

// A trustabl.cot.* key written by hand is recorded whatever the key, because
// trustabl.cot.text is the worst case and no table of known keys would name it.
// ValueUnhashed says whether the value could be the reasoning itself.
func TestDiscoverOtelLabels_HandWrittenReasoningKeys(t *testing.T) {
	py := []struct {
		name, line string
		wantKey    string
		unhashed   bool
	}{
		{"raw text under the fingerprint key", `span.set_attribute("trustabl.cot.text_fp", thinking)`, "trustabl.cot.text_fp", true},
		{"raw text under an invented key", `span.set_attribute("trustabl.cot.text", thinking)`, "trustabl.cot.text", true},
		{"an f-string", `span.set_attribute("trustabl.cot.text_fp", f"{thinking}")`, "trustabl.cot.text_fp", true},
		{"a keyword value", `span.set_attribute("trustabl.cot.text_fp", value=thinking)`, "trustabl.cot.text_fp", true},
		{"a mapping", `span.set_attributes({"trustabl.cot.text_fp": thinking})`, "trustabl.cot.text_fp", true},
		{"the library's fingerprint", `span.set_attribute("trustabl.cot.text_fp", canonical_fp(thinking))`, "trustabl.cot.text_fp", false},
		{"the library's key constant", `span.set_attribute(keys.COT_TEXT_FP, thinking)`, "trustabl.cot.text_fp", true},
		{"the key constant imported bare", `span.set_attribute(COT_TEXT_FP, thinking)`, "trustabl.cot.text_fp", true},
		{"a mapping with the key constant", `span.set_attributes({keys.COT_TEXT_FP: thinking})`, "trustabl.cot.text_fp", true},
		{"a summary called a digest", `span.set_attribute("trustabl.cot.text", summarize_digest(thinking))`, "trustabl.cot.text", true},
		{"a field that only mentions a hash", `span.set_attribute("trustabl.cot.text", thought.hash_tags)`, "trustabl.cot.text", true},
		{"a hand-rolled hash", `span.set_attribute("trustabl.cot.text_fp", hashlib.sha256(raw).hexdigest()[:16])`, "trustabl.cot.text_fp", false},
		{"a variable named for what it holds", `span.set_attribute("trustabl.cot.text_fp", text_fp)`, "trustabl.cot.text_fp", false},
		{"an attribute named for what it holds", `span.set_attribute("trustabl.cot.text_fp", self.reasoning_hash)`, "trustabl.cot.text_fp", false},
		{"a literal", `span.set_attribute("trustabl.cot.text_fp", "abc123")`, "trustabl.cot.text_fp", false},
		{"a flag", `span.set_attribute("trustabl.cot.present", has_reasoning)`, "trustabl.cot.present", false},
		{"an enum", `span.set_attribute("trustabl.cot.mode", mode)`, "trustabl.cot.mode", false},
	}
	for _, tc := range py {
		t.Run("python "+tc.name, func(t *testing.T) {
			sigs := otelLabelsOfKind(analysis.DiscoverOtelLabels([]analysis.ParsedFile{parsePyFile(t, "agent.py", tc.line+"\n")}), models.OtelLabelsAttrLiteral)
			if len(sigs) != 1 || sigs[0].Key != tc.wantKey || sigs[0].ValueUnhashed != tc.unhashed {
				t.Errorf("got %+v, want key %s unhashed %v", sigs, tc.wantKey, tc.unhashed)
			}
		})
	}

	ts := []struct {
		name, line string
		unhashed   bool
	}{
		{"raw text", "span.setAttribute(\"trustabl.cot.text_fp\", thinking);", true},
		{"a template string", "span.setAttribute(\"trustabl.cot.text_fp\", `${thinking}`);", true},
		{"a mapping", "span.setAttributes({ \"trustabl.cot.text_fp\": thinking });", true},
		{"the library's key constant", "span.setAttribute(keys.COT_TEXT_FP, thinking);", true},
		{"a computed key", "span.setAttributes({ [keys.COT_TEXT_FP]: thinking });", true},
		{"a hand-rolled hash", "span.setAttribute(\"trustabl.cot.text_fp\", createHash(\"sha256\").update(t).digest(\"hex\"));", false},
		{"the library's fingerprint", "span.setAttribute(\"trustabl.cot.text_fp\", canonicalFp(thinking));", false},
		{"a literal", "span.setAttribute(\"trustabl.cot.text_fp\", \"abc123\");", false},
	}
	for _, tc := range ts {
		t.Run("typescript "+tc.name, func(t *testing.T) {
			sigs := otelLabelsOfKind(analysis.DiscoverOtelLabels([]analysis.ParsedFile{parseTSForTest(t, "agent.ts", tc.line+"\n")}), models.OtelLabelsAttrLiteral)
			if len(sigs) != 1 || sigs[0].ValueUnhashed != tc.unhashed {
				t.Errorf("got %+v, want unhashed %v", sigs, tc.unhashed)
			}
		})
	}
}

// An object key is its own name unless it is computed, so this writes a label
// called COT_TEXT_FP and not the library's key.
func TestDiscoverOtelLabels_PlainPropertyIsNotTheKeyConstant(t *testing.T) {
	sigs := analysis.DiscoverOtelLabels([]analysis.ParsedFile{parseTSForTest(t, "agent.ts", "span.setAttributes({ COT_TEXT_FP: thinking });\n")})
	if len(sigs) != 0 {
		t.Errorf("got %+v, want no signals", sigs)
	}
}

// The process keys carry the same shape and never set ValueUnhashed: their
// values are ids, enums and integers, and OBS-006..008 credit them as they are.
func TestDiscoverOtelLabels_ProcessKeysAreNeverUnhashed(t *testing.T) {
	sigs := analysis.DiscoverOtelLabels([]analysis.ParsedFile{
		parsePyFile(t, "agent.py", `span.set_attribute("trustabl.run_id", run_id)`+"\n"),
	})
	got := otelLabelsOfKind(sigs, models.OtelLabelsAttrLiteral)
	if len(got) != 1 || got[0].ValueUnhashed {
		t.Errorf("got %+v, want one signal not marked unhashed", got)
	}
}
