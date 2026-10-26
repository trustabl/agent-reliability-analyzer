# ci-agent-workflow-fixture

Synthetic GitHub Actions workflows written for the CI-001..CI-005 rule tests.
Shapes follow the rule table in the Trustabl Agentic Security Baseline (AGS-07)
and the claude-code-action step excerpt published in Adnan Khan's Clinejection
post. No file is copied from a real repository. Secret names are placeholders.

- `issue-triage.yml` fires CI-001..CI-005.
- `gated-comment.yml` has an actor gate: CI-003, CI-004 and CI-005 fire,
  CI-001 and CI-002 stay silent.
