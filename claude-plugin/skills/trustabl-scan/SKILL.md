---
name: trustabl-scan
description: >-
  Use right after you write or modify AI agent, tool, subagent, or MCP-server
  code (OpenAI Agents SDK, Claude Agent SDK, Google ADK, MCP) to self-audit it
  for security and reliability misconfigurations with Trustabl before
  committing. Triggers on adding or editing an agent definition, a tool /
  @function_tool / @tool / tool() handler, a subagent markdown file, an MCP
  server registration, agent guardrails, or .claude/settings.json permissions.
  Runs the plugin's pinned, checksum-verified Trustabl CLI and guides
  remediation of the findings.
---

# Self-audit agent code with Trustabl

Trustabl finds and fixes reliability defects in agent-SDK code. It models the tools, agents,
subagents, and MCP servers a repo declares and checks them against a rule
catalog, reporting reliability and safety weaknesses with an explanation, a
suggested fix, and a confidence score. Run it as a self-audit at generation
time, upstream of CI, so issues are caught and fixed before the commit.

Trustabl is read-only: it detects and reports, and never writes to or modifies
the scanned repo. You apply the fixes yourself, then re-scan to confirm.

## When to use this

Invoke after you have just written or changed any of these, before committing:

- An agent definition (`Agent(...)`, `SandboxAgent(...)`, `AgentDefinition`,
  `new Agent({...})`, ADK `LlmAgent` / `SequentialAgent` / etc., or a TS
  `query(...)` main-thread agent).
- A tool definition (a `@function_tool` / `@tool` / `@claude_tool` function, a
  TS `tool(...)` factory call, an ADK `FunctionTool` wrapper, or a shell-invoking
  function).
- A subagent markdown file (`.claude/agents/*.md`), a skill, a slash command, or
  a plugin manifest.
- An MCP server registration (`@server.tool` / `@mcp.tool` / `createSdkMcpServer`)
  or an MCP config.
- Agent guardrails (`@input_guardrail` / `@output_guardrail`) or
  `.claude/settings.json` permission settings.

Languages understood today: Python, TypeScript, and JavaScript (`.ts` / `.tsx` /
`.mts` / `.cts` / `.js` / `.jsx` / `.mjs` / `.cjs`) for tools and agents, plus
MCP-server tool discovery in Go, C#, PHP, and Rust. The dependency BOM (and
`--vuln-scan`) spans all of those package ecosystems — pip, npm, Go modules,
NuGet, Composer, and Cargo.

## How to run the scan

### Get the CLI

The plugin ships no scripts. Run this block with the Bash tool: it downloads the pinned release from GitHub,
checks it against the release's `checksums.txt` (a mismatch installs nothing), keeps it in `~/.trustabl/cli/`, and
prints `TRUSTABL_BIN=<path>`. Later runs only check the version, so run it at the start of each session that uses
Trustabl. Tell the user what it downloads the first time. If the user already has `trustabl` 0.1.16 or later on `PATH` (`trustabl version`), use that instead.

```bash
V=0.1.16; D="$HOME/.trustabl/cli/v$V"
case "$(uname -s)" in MINGW*|MSYS*|CYGWIN*) OS=windows; EXT=zip; EXE=trustabl.exe ;; Darwin) OS=darwin; EXT=tar.gz; EXE=trustabl ;; *) OS=linux; EXT=tar.gz; EXE=trustabl ;; esac
case "$(uname -m)" in arm64|aarch64) ARCH=arm64 ;; *) ARCH=amd64 ;; esac; [ "$OS" = windows ] && ARCH=amd64
TRUSTABL_BIN="$D/$EXE"
if ! "$TRUSTABL_BIN" version 2>/dev/null | head -n1 | grep -q "$V\$"; then
  A="trustabl_${V}_${OS}_${ARCH}.$EXT"; U="https://github.com/trustabl/agent-reliability-analyzer/releases/download/v$V"; T="$(mktemp -d)"
  curl -fsSL "$U/$A" -o "$T/$A" && curl -fsSL "$U/checksums.txt" -o "$T/checksums.txt" &&
  SUM=sha256sum; command -v sha256sum >/dev/null || SUM="shasum -a 256"
  (cd "$T" && L="$(grep " $A\$" checksums.txt)" && [ -n "$L" ] && echo "$L" | $SUM -c -) &&
  mkdir -p "$D" && if [ "$EXT" = zip ]; then unzip -oq "$T/$A" -d "$D"; else tar -xzf "$T/$A" -C "$D"; fi &&
  chmod +x "$TRUSTABL_BIN"; rm -rf "$T"
fi
if "$TRUSTABL_BIN" version 2>/dev/null | head -n1 | grep -q "$V\$"; then echo "TRUSTABL_BIN=$TRUSTABL_BIN"; else echo "install failed: see the message above"; fi
```

Shell state does not carry between Bash calls: start each later command with `TRUSTABL_BIN="<the printed path>";`.
If the block prints `install failed`, show the user the message above it and stop.

### Scan

```bash
"$TRUSTABL_BIN" scan . --format json          # full result as JSON (the ScanResult shape)
"$TRUSTABL_BIN" scan . --strict               # exit 1 on low+ findings (info/META never fail)
"$TRUSTABL_BIN" scan . --detectors claude_sdk # claude_sdk|openai_sdk|google_adk|openshell|mcp|langchain|crewai|pydantic_ai|vercel_ai|autogen|claude_skill
"$TRUSTABL_BIN" scan . --vuln-scan            # also match declared deps against OSV → CVE findings
"$TRUSTABL_BIN" scan . --bom-out bom.json     # write a CycloneDX SBOM (+ VEX vulnerabilities under --vuln-scan)
```

Pass the repo you just edited (`.` for the current directory), or a GitHub
repository URL when the user names one. `--format json` prints the full scan
result with a `findings` array to reason over; on a big repository, write it to
a file (`--format json > /tmp/trustabl-scan.json`) and read that, rather than
flooding the conversation.

The exit code is the gate: `0` = no findings of medium severity or higher,
`1` = at least one, `2` = scanner/I-O error or no usable rules (run
`"$TRUSTABL_BIN" rules pull` once to pre-populate the rules cache).
The scanner fetches and caches the `agent-reliability-rules` pack itself,
falling back to its local cache when offline.

## How to read findings

Each finding carries:

- **severity** (`info` / `low` / `medium` / `high` / `critical`) how bad it is.
  Treat `medium` and above as commit-blockers; `info` / `META` never block. (In
  the CLI fallback these map to the exit code: `medium`+ → exit 1, or `low`+
  under `--strict`.)
- **confidence** a heuristic score for how sure the rule is. It is not
  LLM-judged and not yet calibrated, so treat findings as signal to investigate,
  not ground truth. Look at higher-confidence findings first.
- **explanation** why the pattern is a problem.
- **suggested_fix** the concrete remediation to apply.
- **location** where it fired, attributed to one of five scopes:
  - **tool** a specific tool definition (file and line). Example: an HTTP call
    with no timeout, untyped parameters, a missing docstring.
  - **agent** a specific agent at its constructor call site, with its resolved
    tool / handoff / guardrail edges. Example: an agent wired with shell tools
    but no input guardrails.
  - **subagent** a `.claude/agents/*.md` declaration. Example: a subagent
    granted `Bash` despite a read-only description.
  - **skill** a `SKILL.md` declaration (a Claude Code skill). Example: a skill
    that auto-approves unrestricted `Bash`, or runs a dynamic-context command
    that reads secrets before the model sees it.
  - **repo** the whole repo, once per scan. Example: an SDK present with no
    custom trace processor configured.

`META` findings (for example "this repo uses an SDK Trustabl does not currently
audit", or "this dep is declared but unused") are honest coverage signals, not
defects, and never fail the build. Two agents in the same repo can be in very
different postures, so always fix the agent the finding names, not the repo as a
whole.

**Dependency vulnerabilities** (only when the scan ran with `--vuln-scan` or the
`--vuln-scan` flag): each known CVE in a declared, concretely-pinned
dependency is a finding whose `rule_id` is the advisory id (CVE / GHSA / PYSEC),
located at the dependency's line in its manifest, with the fixed version in
`suggested_fix`. They flow through the same severity gate as rule findings — fix
by upgrading the dependency to the noted version.

## Remediation loop

1. Run `"$TRUSTABL_BIN" scan . --format json` and read the `findings` array.
2. For each finding of `medium` or higher, read its `explanation` and
   `suggested_fix` and apply the fix to the code (Trustabl will not edit files
   for you).
3. Scan again and confirm the finding is gone from `findings`.
4. Repeat until only `info` / `META` signals remain. For a stricter bar before
   committing, treat `low` findings as blockers too (or use `--strict`).

## If the CLI cannot be installed

The "Get the CLI" block is the normal path. It fails only when it cannot run:
offline on first use, or no `curl` and `tar` / `unzip`. Then offer to install
Trustabl system-wide, **asking the user before you run any install command;
never install silently**:

```bash
# macOS / Linux (Homebrew)
brew install trustabl/tap/trustabl

# Windows (Scoop)
scoop bucket add trustabl https://github.com/trustabl/scoop-bucket
scoop install trustabl

# Docker (no local install; mount the repo at /repo)
docker run --rm -v "$PWD:/repo" ghcr.io/trustabl/trustabl:latest scan /repo
```

Prebuilt archives for each platform are on the GitHub Releases page
(https://github.com/trustabl/agent-reliability-analyzer/releases). Confirm the
install with `trustabl version`, then use `trustabl` in place of `"$TRUSTABL_BIN"`.
