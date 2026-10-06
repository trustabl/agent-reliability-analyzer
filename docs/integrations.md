# Ecosystem integrations

Where Trustabl fits in each agent ecosystem it analyses, and where to find it
listed.

Trustabl is a static analyzer. It reads an agent repository, inventories the
agents, tools, subagents, skills and MCP servers in it, and evaluates each one
against a versioned rule pack. It does not run your agent, and scanning happens
entirely on your machine.

This page exists so that a developer arriving from one of these ecosystems can
tell, quickly, what Trustabl does for *their* framework and where the official
listing lives.

---

## Where Trustabl is listed

The directories Trustabl is published in, and the ones a submission is open
with. Anything not on this list has no entry yet.

| Directory | Status | Since |
|---|---|---|
| [MCP Registry](https://registry.modelcontextprotocol.io/?q=trustabl) | **Listed** | 24 Sep 2026 |
| [Claude Directory](claude-plugin.md) | **Listed** | 29 Sep 2026 |
| [VS Code Marketplace](https://marketplace.visualstudio.com/items?itemName=trustabl.trustabl) | **Listed** | 11 Sep 2026 |
| [Cursor](https://cursor.directory/plugins/trustabl) | **Listed** | 11 Aug 2026 |
| [GitHub Marketplace](https://github.com/marketplace/actions/trustabl-fix-agent-reliability-issues) | **Listed** | — |
| [GitLab CI/CD Catalog](https://gitlab.com/explore/catalog/trustabl-ai/components) | **Listed** | — |
| [Bitbucket Pipes](https://bitbucket.org/hoolisoftware/trustabl-pipe) | **Listed** | 18 Aug 2026 |
| [in-toto](https://github.com/in-toto/friends/pull/122) | Submitted | 28 Sep 2026 |
| [Google ADK](https://github.com/google/adk-docs/pull/2290) | Submitted | 1 Oct 2026 |
| [NVIDIA NemoClaw](https://github.com/NVIDIA/nemoclaw-community/issues/196) | Proposed | 5 Oct 2026 |
| [npm](https://www.npmjs.com/package/@trustabl/ai-sdk) | **Listed** | 2 Oct 2026 |

---

## Agent frameworks

Every framework below is covered by the rule packs today. The listing column says
whether that ecosystem's own directory carries an entry for Trustabl, in the
order we are working them.

| # | Ecosystem | What Trustabl checks | Listing |
|---|---|---|---|
| 1 | **Claude Agent SDK** | Agents, tools, skills and hooks — unsafe tool grants, missing turn limits, prompt-injectable shell tools | **Listed** |
| 2 | **Google ADK** | Agents, tools, skills, plugins and callbacks | [Submitted](https://github.com/google/adk-docs/pull/2290) |
| 3 | **Vercel AI SDK** | Untyped tools, missing step bounds, provider shell and file tools, fetch calls with no timeout | **In progress** — see below |
| 4 | **OpenAI Agents SDK** | Agents, tools, handoffs and guardrails | No route — tracing only |
| 5 | **Pydantic AI** | Typed tools, structured outputs, usage limits, idempotent mutations | Route open — see below |

**Vercel AI SDK** is in progress. Its registry
([`content/tools-registry/registry.ts`](https://github.com/vercel/ai/blob/main/content/tools-registry/registry.ts))
accepts an entry only for a published npm package that an agent calls at
runtime. That package now exists —
[`@trustabl/ai-sdk`](https://www.npmjs.com/package/@trustabl/ai-sdk), source at
[trustabl/ai-sdk-tool](https://github.com/trustabl/ai-sdk-tool) — so only the
registry pull request remains.

**OpenAI Agents SDK** has one listing mechanism, *tracing integration listings*,
and its criteria require implementing the Agents SDK tracing interface. The same
paragraph excludes "generic OpenTelemetry support, or a hooks-only or
guardrails-only integration". Trustabl never executes an agent, so there is
nothing for it to trace; this is not a submission we can write our way into.

**Pydantic AI** does publish a third-party directory, and an earlier version of
this page said otherwise. The mistake was reading `docs/third-party-tools.md`,
which covers MCP and LangChain tool *usage* and lists no vendors, and stopping
there. The vendor surface is two other places:

- [`docs/capabilities/third-party.md`](https://github.com/pydantic/pydantic-ai/blob/main/docs/capabilities/third-party.md)
  lists community capability packages, under headings that include **Guardrails
  & Safety** and **File Operations & Sandboxing**.
- [`src/pydantic_ai_harness/`](https://github.com/pydantic/pydantic-ai/tree/main/src/pydantic_ai_harness/pydantic_ai_harness)
  carries fifteen vendor integrations, among them StackOne, Pylon, Ordinal,
  PostHog and Macroscope.

[Macroscope](https://ai.pydantic.dev/docs/ai/harness/macroscope/) is the closest
precedent. It is a CLI code-review tool that otherwise ships as editor plugins,
wrapped as a capability that runs the CLI in the agent's workspace and returns
structured findings for the agent to validate and fix. That is the same design
as [`@trustabl/ai-sdk`](https://www.npmjs.com/package/@trustabl/ai-sdk).

Two routes exist. Pydantic's
[extensibility guide](https://github.com/pydantic/pydantic-ai/blob/main/docs/extensibility.md)
invites third parties to publish their own package under the `pydantic-ai-`
prefix, which needs no approval from anyone; upstreaming into the harness comes
later, in their words "once a capability has real users and a stable API".

**NVIDIA OpenShell** has no image catalogue to list in: `NVIDIA/OpenShell-Community`,
which accepted sandbox images and skills, carries the notice "This repository is
retired and will be archived" and OpenShell no longer depends on it. The live
[`NVIDIA/OpenShell`](https://github.com/NVIDIA/OpenShell) repository documents
extension points — drivers, gateway interceptors, isolation backends,
supervisor middleware — rather than a partner directory.

The relationship is real regardless, and it is the one target where the
integration already exists rather than needing to be built:
[Trustabl Probe](https://github.com/trustabl/trustabl-probe) runs an agent tool
in an OpenShell sandbox, observes the network destinations it actually reaches,
and generates a least-privilege OpenShell `network_policies` draft from that
evidence.

There is now a route for it. `NVIDIA/nemoclaw-community` carries partner recipes
from outside vendors under `examples/recipes/partners/`, Tavily and Telnyx among
them, and the contributing guide asks for location, name and provenance to be
agreed with maintainers before anything is written. That agreement is what
[issue #196](https://github.com/NVIDIA/nemoclaw-community/issues/196) asks for.

The issue proposes **one** recipe, covering Probe deriving a least-privilege
policy from an observation run. The analyzer is named in it only as a possible
second recipe later, matching how the existing partner recipes are each scoped
narrowly. So the analyzer has no open NVIDIA submission today.

Checked against the public repositories only. NVIDIA may run a partner
programme that is not on GitHub.

These three are analysed the same way, but their ecosystems publish no
integrations directory, so there is nowhere to list:

| Ecosystem | What Trustabl checks |
|---|---|
| **LangChain / LangGraph** | Tool contracts, unbounded graphs, missing checkpointers, human-in-the-loop gaps |
| **CrewAI** | Unsafe tools, unbounded delegation, missing iteration caps, weak tool contracts |
| **AutoGen / AG2** | Host-side code execution, missing human review, unbounded rounds, untyped tools |

Rules are versioned separately from the engine and fetched at scan time, so a
scan picks up new detections for these frameworks without upgrading the binary.

---

## MCP

Trustabl analyses MCP servers as a first-class scope: tool annotations, caller-
controlled URLs, missing titles, and tools that shell out.

Trustabl also ships an MCP server of its own, so an agent can run a scan as a
tool call. It is built into the CLI — `trustabl mcp` runs a stdio MCP server
exposing a `scan` tool backed by the same analysis as `trustabl scan`:

```json
{
  "mcpServers": {
    "trustabl": { "command": "trustabl", "args": ["mcp"] }
  }
}
```

Registry listing: **[io.github.trustabl/agent-reliability-analyzer](https://registry.modelcontextprotocol.io/?q=trustabl)**, live since 24 September 2026.

---

## Policy and standards

| Ecosystem | Relationship | Listing |
|---|---|---|
| **in-toto** | Trustabl emits a signed scan attestation; in-toto makes it verifiable across the supply chain, so a verifier can prove an agent was checked against a known ruleset before it shipped | [Submitted](https://github.com/in-toto/friends/pull/122) |
| **NVIDIA OpenShell** | Trustabl derives least-privilege policy from agent code, identity and required endpoints; OpenShell enforces it at runtime | [Proposed](https://github.com/NVIDIA/nemoclaw-community/issues/196) |

See [`attestation.md`](attestation.md) for the attestation format.

---

## Editors and CI

Trustabl already ships integrations for these surfaces. They are listed on their
own marketplaces rather than here:

| Surface | Repository |
|---|---|
| GitHub Actions | [trustabl/trustabl-action](https://github.com/trustabl/trustabl-action) |
| GitLab CI/CD | [trustabl-ai/components](https://gitlab.com/trustabl-ai/components) |
| Bitbucket Pipelines | [hoolisoftware/trustabl-pipe](https://bitbucket.org/hoolisoftware/trustabl-pipe) |
| VS Code | [trustabl/trustabl-vscode](https://github.com/trustabl/trustabl-vscode) |
| Cursor | [trustabl/trustabl-cursor](https://github.com/trustabl/trustabl-cursor) |
| AWS | [trustabl/trustabl-aws](https://github.com/trustabl/trustabl-aws) |

---

## A note on coverage

"Not listed" means there is no official directory entry, not that the framework
is unsupported — every framework in the tables above is covered by the rule
packs. Where a framework has no integrations directory, there is nowhere to list.

If you maintain one of these ecosystems and want an integration page, open an
issue.
