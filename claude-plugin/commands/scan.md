---
description: Scan this repository for agent reliability and safety weaknesses
argument-hint: "[path or GitHub URL]"
---

Scan for agent reliability and safety weaknesses with Trustabl.

Target: `$ARGUMENTS` when given, otherwise the current working directory.

Get the CLI with the block in the `trustabl-scan` skill ("Get the CLI"), then run the scan with the Bash tool:
`"$TRUSTABL_BIN" scan <target> --format json`,
where the target is `.` unless the user named a specific directory or a GitHub
repository URL. On a big repository, write the JSON to a file and read that. If
no binary resolves, say so and point to the install steps in the
`trustabl-scan` skill.

Then report what came back:

1. **Inventory first.** State how many tools, agents, subagents, skills and MCP
   servers were found, and which SDKs were detected. If those counts look wrong
   the scan is pointed at the wrong place, and the findings are not worth
   reading until that is fixed. Say so rather than continuing.
2. **Findings by severity**, critical first. For each one give the rule id, the
   file and line, and what is actually wrong in a sentence. Do not paste the
   full explanation text for every finding.
3. **Where to start.** Name the two or three findings worth fixing first and
   why, rather than restating the list in severity order.

Be straight about what the result means. A finding is a weakness, not a proven
exploit. An empty result is not a pass: if nothing was found, check the
inventory counts before saying the repository is clean.

Do not modify any files. This command reports only. To apply fixes, use
`/agent-reliability-analyzer:fix`.
