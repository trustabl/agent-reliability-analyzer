# Source: original fixture

Authored for the trustabl test corpus. Synthetic repo for AGCFG-019, AGCFG-020, AGCFG-021,
OAI-210 and OAI-211: an MCP client that launches a stdio server (command from a variable) with the whole parent
environment in Python and in TypeScript, an SSRF guard called with warn_only=True and a
settings call with ssrf_protection_enabled=False, a model loaded with trust_remote_code=True,
an OpenAI Agents SDK file with an unguarded and a guarded agent that reach a shell-executing
agent through a handoff chain (one chain contains a cycle), the same patterns under a tests/
folder (not judged), and a safe/ folder with the same shapes written without the flaw. All
values are placeholders. No external code. Public-domain.
