# Source: original fixture

Authored for the trustabl test corpus. Synthetic Claude Code settings files
pairing WebFetch allow rules for user-content hosts with a non-listed host, a
single-account site and a same-folder deny, for AGCFG-011. Every host outside
the rule's own list is a placeholder under `example.invalid`. No external code.
Public-domain.

A .gitignore ignores .claude/settings.local.json so AGCFG-018 (settings.local.json not git-ignored) stays silent here; the settings file is added to git with git add -f.
