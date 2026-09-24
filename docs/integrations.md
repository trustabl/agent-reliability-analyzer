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

## Agent frameworks

Every framework below is covered by the rule packs today. The order is the order
we are pursuing an official listing in each ecosystem's own directory.

| # | Ecosystem | What Trustabl checks | Listing |
|---|---|---|---|
| 1 | **Google ADK** | Agents, tools, skills, plugins and callbacks | Not listed |
| 2 | **Claude Agent SDK** | Agents, tools, skills and hooks — unsafe tool grants, missing turn limits, prompt-injectable shell tools | Not listed |
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

Trustabl is published in the official MCP Registry as
`io.github.trustabl/agent-reliability-analyzer`:

```bash
curl "https://registry.modelcontextprotocol.io/v0.1/servers?search=trustabl"
```

---

## Policy and standards

| Ecosystem | Relationship | Listing |
|---|---|---|
| **in-toto** | Trustabl emits a signed scan attestation; in-toto makes it verifiable across the supply chain, so a verifier can prove an agent was checked against a known ruleset before it shipped | Not listed |
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
