const { spawnSync } = require('child_process');
const PROMPT = process.argv[2];
spawnSync('claude', ['--dangerously-skip-permissions', '-p', PROMPT]);
