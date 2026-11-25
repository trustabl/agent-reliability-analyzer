# Source: original fixture

Authored for the trustabl test corpus. Synthetic files for CSKILL-092, CSKILL-093, MCP-211 to
MCP-214, OAI-206, OAI-208 and OAI-209: two skills (a description that argues for the skill, and a body
that tells the agent to decode text and follow it), an MCP server in Python and in TypeScript
(a tool description with an instruction block aimed at the model, and a tool that removes a file after it asks for
elicitation and does not check the answer), and an OpenAI Agents SDK file (a handoff target that
holds a shell tool, a shell tool with a model-settable
confirm flag, and an options order tool with no limit). `safe/` holds the same shapes with a
task-scoped preference, decode and follow in separate sentences, a plain description, an
accept check, filters, needs_approval and a size limit. `tests/` holds a directive description
under a test path. No external code. Public-domain.
