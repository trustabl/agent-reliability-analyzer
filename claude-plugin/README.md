# Trustabl for Claude

Find and fix reliability and safety gaps in AI-agent code, without leaving Claude.

Trustabl is a static analyzer for agent codebases. It inventories the agents, tools, subagents, skills and MCP servers in a repository, then evaluates each one against a versioned rule pack covering nine agent SDKs: Claude Agent SDK, OpenAI Agents SDK, Google ADK, MCP, LangChain, LangGraph, CrewAI, AutoGen AG2, Pydantic AI and Vercel AI.

It looks for the failure modes ordinary code review misses. A tool that shells out and can be prompt-injected. An agent session with no turn limit. A tool that fetches a caller-controlled URL. A network call with no timeout.

## What this plugin gives you

- **`trustabl-scan` skill** — scans a repository and reports findings with severity, exact file and line, and a suggested fix
- **`trustabl-enrich` skill** — takes findings further, proposing concrete replacements for the code it flags
- **`trustabl` agent** — a subagent that runs the scan, review and apply loop end to end
- **A pinned, checksum-verified CLI** — installed by the plugin itself, so Claude can scan on request

## What it does on your machine

This plugin is honest about what it runs, because you should not have to read the source to find out.

- On session start, it **downloads the pinned Trustabl CLI** from GitHub Releases
  (`github.com/trustabl/agent-reliability-analyzer`) into the plugin's own data
  directory. The download is checksum-verified against the release's
  `checksums.txt` and installed atomically. It is skipped when the binary is
  already present.
- **Scanning runs entirely on your machine.** There is no hosted scanner, no account, no source code upload, and no LLM in the analysis path.
- **Rules are fetched at scan time** from a signed channel served from
  `github.com/trustabl/agent-reliability-rules`, so new detections arrive
  without upgrading the binary.
- **Telemetry is opt-in and defaults to disabled.** When enabled, it sends the
  command name, operating system, architecture and flags used to
  `us.i.posthog.com`. It never sends source code, file paths, repository names,
  or finding details. Retention is covered by the
  [privacy policy](https://trustabl.ai/privacy).

Those three are every destination the plugin contacts. Nothing else leaves your
machine.

The pinned CLI version is set in the install block of `skills/trustabl-scan/SKILL.md`. Trustabl is Apache-2.0 licensed and its source is at
[trustabl/agent-reliability-analyzer](https://github.com/trustabl/agent-reliability-analyzer).

## Requirements

Nothing beyond what Claude Code already provides. The plugin ships no scripts:
the first scan runs a short install block (Git Bash on Windows) that downloads
the pinned CLI and verifies it against the release checksums, using `curl`,
`tar` or `unzip`, and `sha256sum`, into `~/.trustabl/cli/`.

**Automatic install covers macOS, Linux and Windows.** If you would rather
install the CLI yourself, `brew install trustabl/tap/trustabl` or
`scoop install trustabl` also work, and the plugin uses whatever it finds.

## Reading the results

Findings carry a severity from `critical` down to `info`, a confidence score, the exact file and line, and a written fix. A readiness score is computed across the surfaces found, so a repository with one bad tool out of fifty scores differently from one with one bad tool out of two.

A finding is a weakness, not a proven exploit, and severity reflects the shape of the risk rather than a demonstrated attack. An empty result is not a pass: check the inventory counts first, because a high score over an empty inventory means nothing was analysed.

## Privacy

[Privacy policy](https://trustabl.ai/privacy) · privacy@trustabl.ai

## License

Apache-2.0. See [LICENSE](../LICENSE).
