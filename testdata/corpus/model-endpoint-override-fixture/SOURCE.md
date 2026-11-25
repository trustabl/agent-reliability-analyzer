# Source: original fixture

Authored for the trustabl test corpus. Synthetic Claude Code settings files whose
env block sets ANTHROPIC_BASE_URL: an off-vendor placeholder host (fires), the
vendor host, a loopback host and a variable reference (silent), for AGCFG-014.
All values are placeholders. No external code. Public-domain.

A .gitignore ignores .claude/settings.local.json so AGCFG-018 (settings.local.json not git-ignored) stays silent here; the settings file is added to git with git add -f.
