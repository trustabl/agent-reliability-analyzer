- Every tool takes a `claim` argument, and the mediation step removes it
  before the tool body runs.
- The mediation step runs on every call path, including retries, handoffs and
  sub-agents. A tool that can be reached without it is not covered.
- Every fact the step compares against comes from the server side. Nothing in
  the claim, the prompt or a tool result decides which facts are used.
- A missing claim is a deny, an unresolved fact is a review, and only allow
  runs the tool.
- The claim check is combined with the existing controls, and the most
  restrictive outcome wins. It never turns another control's deny or review
  into allow.
- The audit trail holds the schema version, the outcome, the check outcomes
  and digests — not raw untrusted content and not the `summary` text.
