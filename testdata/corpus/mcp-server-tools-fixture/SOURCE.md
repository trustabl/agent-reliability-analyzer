# Source: original fixture

Authored for the trustabl test corpus. Synthetic MCP servers (Python and TypeScript) for MCP-036,
MCP-037 and MCP-203: tool descriptions that hold one run of invisible characters (a zero-width
space, or Unicode Tags-block characters), and HTTP servers started on the wildcard host 0.0.0.0
with no authentication or host allow-list, including a one-hop factory
function whose result is run (`factory_server.py`). `safe/` holds the same shapes with clean descriptions,
credited authentication or an allow-list, and a loopback host. `tests/` holds the unsafe shapes
under a test path. The invisible characters are written by a generator command, not typed; list
them with a hidden-character viewer. No external code. Public-domain.
