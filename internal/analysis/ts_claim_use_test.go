package analysis_test

import (
	"testing"

	"github.com/trustabl/trustabl/internal/analysis"
	"github.com/trustabl/trustabl/internal/models"
)

// tsOpenAIClaimUse discovers the single TS OpenAI Agents tool whose options
// object is opts and returns its claim_use fact.
func tsOpenAIClaimUse(t *testing.T, opts string) string {
	t.Helper()
	src := `import { tool } from "@openai/agents";
import { z } from "zod";

export const sendEmail = tool(` + opts + `);
`
	pf := parseTSForTest(t, "src/tools.ts", src)
	tools := analysis.DiscoverTSOpenAITools([]analysis.ParsedFile{pf}, nil)
	if len(tools) != 1 {
		t.Fatalf("expected 1 tool, got %d: %+v", len(tools), tools)
	}
	return tools[0].Facts[models.FactClaimUse]
}

func TestClaimUse_TSOpenAI(t *testing.T) {
	const params = `parameters: z.object({ to: z.string(), claim: z.any() }),`
	cases := []struct {
		name string
		opts string
		want string
	}{
		{"destructured, never read", `{
  name: "send_email", description: "Send an email.", ` + params + `
  execute: async ({ to, claim }) => mailer.send(to),
}`, models.ClaimUseUnused},
		{"destructured, only logged", `{
  name: "send_email", description: "Send an email.", ` + params + `
  execute: async ({ to, claim }) => {
    console.log("claim", claim);
    return mailer.send(to);
  },
}`, models.ClaimUsePassthrough},
		// Review Focus 4: a renamed destructuring binds the claim to c.
		{"renamed destructuring, checked", `{
  name: "send_email", description: "Send an email.", ` + params + `
  execute: async ({ to, claim: c }) => {
    if (!verify(c)) throw new Error("claim does not match route");
    return mailer.send(to);
  },
}`, models.ClaimUseChecked},
		{"input.claim returned", `{
  name: "send_email", description: "Send an email.", ` + params + `
  execute: async (input) => {
    mailer.send(input.to);
    return { sent: true, claim: input.claim };
  },
}`, models.ClaimUsePassthrough},
		{"input[\"claim\"] serialized", `{
  name: "send_email", description: "Send an email.", ` + params + `
  execute: async (input) => {
    logger.info(JSON.stringify(input["claim"]));
    return mailer.send(input.to);
  },
}`, models.ClaimUsePassthrough},
		{"typed input, claim passed to a verifier", `{
  name: "send_email", description: "Send an email.", ` + params + `
  execute: async (input: { to: string; claim: unknown }, ctx) => {
    await mediate(input.claim, ctx);
    return mailer.send(input.to);
  },
}`, models.ClaimUseChecked},
		{"whole input handed to a call", `{
  name: "send_email", description: "Send an email.", ` + params + `
  execute: async (input) => {
    await mediate(input);
    return mailer.send(input.to);
  },
}`, models.ClaimUseChecked},
		{"only other keys read", `{
  name: "send_email", description: "Send an email.", ` + params + `
  execute: async (input) => mailer.send(input.to),
}`, models.ClaimUseUnused},
		{"method shorthand execute", `{
  name: "send_email", description: "Send an email.", ` + params + `
  async execute({ to, claim }) {
    console.info(claim);
    return mailer.send(to);
  },
}`, models.ClaimUsePassthrough},
		{"input guardrail attached", `{
  name: "send_email", description: "Send an email.", ` + params + `
  inputGuardrails: [checkClaim],
  execute: async ({ to, claim }) => mailer.send(to),
}`, models.ClaimUseWrapped},
		{"no claim key: fact not stamped", `{
  name: "send_email", description: "Send an email.",
  parameters: z.object({ to: z.string() }),
  execute: async ({ to }) => mailer.send(to),
}`, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tsOpenAIClaimUse(t, tc.opts); got != tc.want {
				t.Errorf("claim_use = %q, want %q", got, tc.want)
			}
		})
	}
}
