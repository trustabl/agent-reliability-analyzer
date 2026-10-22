This section binds every tool call that has an effect to a typed action claim
and a check that runs before the call has any effect. It follows Explanation-Bound Tool
Execution (EBTE, arXiv 2607.25364): the decision-relevant part of the model's
reasoning becomes a small typed object, and code outside the model compares
that object with facts the model cannot change.

A claim is untrusted input. It never grants authority, never widens what the
agent is already allowed to do, and never replaces an existing control. The
deterministic controls (allow and deny lists, schema validation, sandbox and
network policy, approval gates) stay the floor; the claim check can only add a
reason to stop.
