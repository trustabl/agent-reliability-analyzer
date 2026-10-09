---
name: trustabl-scan
description: >-
  Use right after you write or modify AI agent, tool, subagent, or MCP-server
  code (Claude Agent SDK, OpenAI Agents SDK, Google ADK, Vercel AI SDK,
  Pydantic AI, LangChain, CrewAI, AutoGen, MCP) to audit it for reliability and
  safety defects before committing. Triggers on adding or editing an agent
  definition, a tool or @function_tool or @tool handler, a subagent markdown
  file, an MCP server registration, agent guardrails, or
  .claude/settings.json permissions. Runs the local `trustabl` CLI and guides
  remediation of what it finds.
version: "0.1.13"
license: Apache-2.0
metadata:
  author: Trustabl
  tags:
    - security
    - static-analysis
    - agents
    - mcp
    - reliability
---

# Audit agent code with Trustabl

Trustabl models the agents, tools, subagents, skills and MCP servers a
repository declares, then checks each against a versioned rule pack. Every
finding names a file and line, explains why it matters, and carries a
suggested fix and a confidence score.

The analysis is static and deterministic. Trustabl never executes the agent,
never writes to the scanned repository, and has no model in the analysis path.
You apply the fixes; re-scanning confirms them.

## Before you run anything

This skill drives the `trustabl` binary on the user's machine. It does not
bundle one.

Check first:

```bash
trustabl --help
```

If that fails, the CLI is not installed. **Do not install it silently.** Tell
the user it is missing, offer one of these, and wait for them to choose:

```bash
brew install trustabl/tap/trustabl                                  # macOS, Linux
scoop bucket add trustabl https://github.com/trustabl/scoop-bucket  # Windows
scoop install trustabl
```

If the user would rather install nothing, Docker runs the same scan:

```bash
docker run --rm -v "$PWD:/repo" ghcr.io/trustabl/trustabl:latest scan /repo
```

## Running the scan

Scan the repository, or the directory you just changed:

```bash
trustabl scan .
```

For a result you can read programmatically:

```bash
trustabl scan . --format json --no-progress -o scan.json
```

## Reading the result, in this order

**1. Read the inventory before the findings.** The scan reports how many
agents, tools, subagents, skills and MCP servers it found. If those counts look
wrong for the repository, the scan was pointed at the wrong directory and the
findings are not worth acting on yet. Fix the path and scan again.

**2. An empty result is not a pass.** A repository with no recognised agent
surfaces produces no findings. Zero findings plus a zero inventory means
nothing was analysed.

**3. Check whether rules were skipped.** When the rule pack is newer than the
installed binary, some rules do not run and the scan says so. A clean result
alongside skipped rules means the scan was incomplete, not that the code is
sound. Upgrade the CLI and scan again.

**4. Then work the findings**, highest severity first.

## Acting on a finding

Treat every finding as a claim to verify, not an instruction to obey.

1. Open the file and line it names.
2. Read enough of the surrounding code to confirm the problem is real. Static
   analysis sees shape, not intent, and some findings will not apply.
3. Fix the confirmed ones one at a time, smallest change that resolves the
   finding.
4. Re-scan to confirm the finding is gone.

A finding that disappears means the pattern is gone. It does not mean the code
is correct, so review your own diff.

Leave anything that needs a judgement about the user's system to the user. The
clearest example is which hosts a tool should be allowed to reach: Trustabl can
tell you the allow-list is missing, but not what belongs in it.

## What it looks for

Across the agent SDKs it recognises:

- Tools with no timeout, so a slow dependency stalls the agent
- Tool descriptions too vague for a model to know when to use them
- Untyped or loosely typed tool parameters
- Agents with shell or filesystem tools and no input guardrails
- Loops and delegation with no turn or step limit
- Subagents granted tools they never use
- MCP servers with caller-controlled URLs, missing annotations, or tools that
  shell out
- Permission settings wider than the agent needs

## Privacy

The scan runs entirely on the user's machine. There is no hosted scanner, no
account, and no code upload. Repository contents, file paths and findings are
not transmitted anywhere.

The only network calls the CLI makes are fetching the versioned rule pack at
scan time, and fetching a release if the user chooses to install or upgrade.
Neither sends workspace data. Telemetry is off by default and opt in.
