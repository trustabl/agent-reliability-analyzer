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
| [Claude Directory](https://claude.ai/directory) | **Listed** | 29 Sep 2026 |
| [VS Code Marketplace](https://marketplace.visualstudio.com/items?itemName=trustabl.trustabl) | **Listed** | 11 Sep 2026 |
| [Cursor](https://cursor.directory/plugins/trustabl) | **Listed** | 11 Aug 2026 |
| [GitHub Marketplace](https://github.com/marketplace/actions/trustabl-fix-agent-reliability-issues) | **Listed** | — |
| [GitLab CI/CD Catalog](https://gitlab.com/explore/catalog/trustabl-ai/components) | **Listed** | — |
| [Bitbucket Pipes](https://bitbucket.org/hoolisoftware/trustabl-pipe) | **Listed** | 18 Aug 2026 |
| [in-toto](https://github.com/in-toto/friends/pull/122) | Submitted | 28 Sep 2026 |
| [Google ADK](https://github.com/google/adk-docs/pull/2290) | Submitted | 1 Oct 2026 |

---

## Agent frameworks

Every framework below is covered by the rule packs today. The listing column says
whether that ecosystem's own directory carries an entry for Trustabl.

| # | Ecosystem | What Trustabl checks | Listing |
|---|---|---|---|
| 1 | **Google ADK** | Agents, tools, skills, plugins and callbacks | [Submitted](https://github.com/google/adk-docs/pull/2290) |
| 2 | **Claude Agent SDK** | Agents, tools, skills and hooks — unsafe tool grants, missing turn limits, prompt-injectable shell tools | **Listed** |
| 3 | **Pydantic AI** | Typed tools, structured outputs, usage limits, idempotent mutations | Not listed |
| 4 | **OpenAI Agents SDK** | Agents, tools, handoffs and guardrails | Not listed |
| 5 | **Vercel AI SDK** | Untyped tools, missing step bounds, provider shell and file tools, fetch calls with no timeout | Not listed |

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
| **NVIDIA OpenShell** | Trustabl derives least-privilege policy from agent code, identity and required endpoints; OpenShell enforces it at runtime | Not listed |

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
