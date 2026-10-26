package models

// CIHit is one finding site for a CI-* rule: an agent step (or, for CI-003, the
// checkout step) in a GitHub Actions workflow. Reason is a fixed phrase built
// from event names, action names and secret NAMES only; no `with`, `env` or
// `run` value is ever copied into a hit.
type CIHit struct {
	Location
	// Job is the job id, or "caller.yml#job>callee.yml#job" when the agent job
	// was reached through a one-hop local reusable workflow call.
	Job string `json:"job,omitempty"`
	// Agent is the recognised agent kind, e.g. "claude_code_action" or "cli:gemini".
	Agent  string `json:"agent,omitempty"`
	Reason string `json:"reason"`
}

// CIWorkflowFacts are the typed facts behind the CI-* rules (category ci).
// Each slice is sorted by path, line, job, reason and deduped.
type CIWorkflowFacts struct {
	UntrustedUngated    []CIHit `json:"untrusted_ungated,omitempty"`     // CI-001
	UntrustedPrivileged []CIHit `json:"untrusted_privileged,omitempty"`  // CI-002
	CheckoutPersists    []CIHit `json:"checkout_persists,omitempty"`     // CI-003
	UnrestrictedShell   []CIHit `json:"unrestricted_shell,omitempty"`    // CI-004
	NoEgressRestriction []CIHit `json:"no_egress_restriction,omitempty"` // CI-005
	// Unparsed lists workflow files that could not be parsed (invalid YAML, not
	// a mapping, or over the alias/node budget). They produce no finding.
	Unparsed []string `json:"unparsed,omitempty"`
}
