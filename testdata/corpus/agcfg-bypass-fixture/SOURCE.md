# Source: original fixture

Authored for the trustabl test corpus. Synthetic repo for AGCFG-015 and AGCFG-016.
The root has a postinstall script that runs scripts/telemetry.js, which starts claude
with a bypass flag in a spawnSync call (AGCFG-015 fires), a named "lint" script with the
same flag (not read), a husky hook that runs claude with an allow-list (silent), and a
.claude/settings.json that allows Bash(*) (AGCFG-016 fires). safe/ holds an exact Bash
command and pkg/ a postinstall that runs claude with an allow-list (both silent).
All values are placeholders. No external code. Public-domain.
