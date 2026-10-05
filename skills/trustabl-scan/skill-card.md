## Description: <br>
Audit AI-agent code for reliability and safety weaknesses before it ships: unsafe tool grants, prompt-injectable shell calls, missing turn limits, network calls with no timeout, and weak tool contracts. <br>

This skill is ready for commercial/non-commercial use. <br>

## Owner
Trustabl <br>

### License/Terms of Use: <br>
Apache 2.0 <br>

## Use Case: <br>
Developers writing or reviewing agent code on the Claude Agent SDK, OpenAI Agents SDK, Google ADK, MCP, LangChain and LangGraph, CrewAI, AutoGen, Pydantic AI, or the Vercel AI SDK, who want defects caught at authoring time rather than in review or production. <br>

### Deployment Geography for Use: <br>
Global <br>

## Requirements / Dependencies: <br>
**Requires API Key or External Credential:** [No] <br>
**Credential Type(s):** [None] <br>

The scanner is a single static binary with no account and no hosted service. Analysis is deterministic with no model call in the path, and source code is never uploaded. The only network calls are fetching the scanner release on first use and fetching the versioned rule pack at scan time; `--no-rules-update` runs fully offline from cache. <br>

Do not include secrets in prompts/logs/output; use least-privilege credentials; rotate keys as appropriate. <br>

## Known Risks and Mitigations: <br>
Risk: A finding is a weakness, not a proven exploit. Treat the output as evidence for review rather than a verdict, and read the reported inventory first — if the tool and agent counts look wrong, the scan was pointed at the wrong directory and the findings are not yet worth acting on. <br>

Risk: An empty result is not a pass. A repository with no recognised agent surfaces produces no findings, which can be mistaken for a clean bill of health. The inventory counts distinguish the two cases. <br>

Risk: Suggested fixes are generated from each rule's own remediation text. Review the diff before applying, and leave anything requiring a judgement about the system, such as which hosts a tool should reach. <br>
