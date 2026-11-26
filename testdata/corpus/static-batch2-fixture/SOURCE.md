# Source: original fixture

Authored for the trustabl test corpus. Synthetic files for MCP-204 to MCP-210, OAI-204, OAI-205,
LC-203, CREW-203, ADK-203 and PYD-203: a TypeScript file-server helper that tests containment
with startsWith and a root with no separator, a Python client that builds a stdio launch from a
configuration value and opens an authorization URL with webbrowser, a Python server with bearer
settings and no audience check, a Python server that turns DNS-rebinding protection off, a
TypeScript HTTP server with a module-scope server connected in a handler, a TypeScript tool that
runs pnpm add with a built specifier, an OpenAI Agents SDK file (a shell-tool handoff target with
no input filter, and an agent holding an approval-requesting tool and a tool that takes
authorized_by), and the same approval pair for LangChain, CrewAI, Google ADK and Pydantic AI.
`safe/` holds the same shapes written with a separator, a literal command, a validated audience,
protection left on, per-request servers, a version check and an input filter. `tests/` holds a
stdio launch under a test path. No external code. Public-domain.
