# Trustabl for wshobson/agents

This directory is the review-scoped external plugin payload for the
`wshobson/agents` marketplace. It is deliberately smaller than the repository
root: the installed payload carries the portable scanning skill and its plugin
metadata, and nothing else.

## Maintainer disclosure

Trustabl maintains this payload and also maintains the open-source
[`agent-reliability-analyzer`](https://github.com/trustabl/agent-reliability-analyzer)
that the skill drives, published as the `trustabl` CLI.

The default workflow requires no account, no API key, no hosted Trustabl
service, no metered API and no paid tier. There is no paid tier. The scanner is
Apache-2.0 and the rule packs are published separately under the same licence.

The skill does not route repository contents, file paths, package names, URLs,
prompts or findings through any Trustabl service. All analysis happens on the
user's machine.

## High-privilege surface disclosure

The installed `git-subdir` payload contains exactly:

- `skills/trustabl-scan/SKILL.md`
- `.claude-plugin/plugin.json`
- this README
- the Apache-2.0 licence

It contains **no**:

- `hooks/` directory or automatic lifecycle hooks
- `.mcp.json` or any other MCP server auto-registration manifest
- `scripts/` directory, executable helper, install script, `preinstall`,
  `postinstall` or other package lifecycle script
- background daemon, telemetry setup, OAuth flow, hosted-service endpoint or
  credential collection

This is worth stating plainly because the full
[Trustabl Claude plugin](https://github.com/trustabl/claude-plugin) does ship a
hooks file and an MCP manifest. That plugin is distributed through the
Anthropic directory, where those surfaces are reviewed on their own terms. They
are **not** part of this payload, and this payload is not built from it.

## What the skill actually does

It checks whether the `trustabl` binary is present, and if it is not, it names
the install options and stops. Installation only happens when the user asks for
it or approves it.

Once the binary is present the skill runs `trustabl scan` against the user's
working directory and reads the result back. The scanner is read-only: it never
writes to or modifies the repository it scans. Applying fixes is left to the
agent and the user.

## Network behaviour

Two calls, neither carrying workspace data:

1. The CLI fetches the versioned rule pack at scan time.
2. If the user chooses to install or upgrade, the CLI fetches a release
   archive and verifies it against the release checksums before running it.

Telemetry is off by default and opt in.

## Version pinning

The skill is written against the `trustabl` CLI and does not pin an exact
version, because the user installs it through their own package manager. The
skill reads the scanner's own report of skipped rules and tells the user to
upgrade when the rule pack has moved ahead of their binary, rather than
silently producing an incomplete result.

The payload's declared version tracks the scanner release it was written
against: `0.1.13`.

## Source

- Scanner and rule packs: https://github.com/trustabl/agent-reliability-analyzer
- Homebrew tap: https://github.com/trustabl/homebrew-tap
- Scoop bucket: https://github.com/trustabl/scoop-bucket
- Container image: `ghcr.io/trustabl/trustabl`
