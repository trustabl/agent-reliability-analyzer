Put one mediation step between the model's tool call and the tool's effect.
It is a function the model cannot call or skip, and it runs on every call.

1. Read `claim` from the call's arguments. When the step runs in a wrapper
   that can change the arguments, remove `claim` before the tool function is
   called. A hook that can only allow or reject leaves it in place. Either
   way, nothing after the step acts on the claim.
2. Validate the claim against the schema. A missing or malformed claim is a
   deny.
3. Resolve the facts from the server side, never from the claim or the
   conversation: the tool registry entry, the task's authorized intent, the
   current policy, the route the call is allowed to take, and the context-risk
   state for this run.
4. Compare. Each check gives allow, review or deny:
   - `selected_tool` differs from the called tool, or the tool is not on the
     authorized route → deny.
   - `intent_class` or `expected_effect.operation` is outside the authorized
     intent → deny.
   - The arguments go beyond `expected_effect` (more items, another
     destination, other resources), or the claim goes beyond the intent → deny.
     Required order: arguments ≤ claim ≤ authorized intent.
   - `untrusted_context` leaves out an untrusted input the run did use → deny.
   - A digest in `evidence_refs` is stale or does not match → deny.
   - A fact cannot be resolved → review. Unknown is never a match.
   - `uncertainty` is high, or `risk_tier` is higher than the server's risk
     for the tool → review. A declared risk can raise the server's risk, never
     lower it.
   - The tool is high risk on the server side → review, even when every
     check matches.
5. Combine: the result is the most restrictive outcome of every check, then
   the most restrictive of that result and the existing authorization and
   approval controls. Deny beats review, review beats allow.
6. Act on it. Only allow runs the tool. Review runs nothing: it returns a
   draft or a confirmation request through the SDK's approval mechanism, and
   an approval is a new decision that re-resolves the facts and runs the
   checks again. Deny returns an error to the model.
7. Tell the model only a coarse reason (for example "claim does not match
   route"). Keep the per-check detail in the audit trail, so the reasons
   cannot be used to probe the checks.
