Every tool with an effect takes one extra argument, `claim`, typed as
`ToolInvocationExplanation`. A tool has an effect when it creates, changes,
deletes, sends, pays, deploys or runs anything, or sends data the model chose
to a destination outside the application. A tool that only reads data the
application already holds, for the current user, may skip the claim. Mark each
tool in the tool registry as needing a claim or not, so the mediation step can
tell a skipped claim from a missing one. Tools the application does not define
(built-in SDK tools, third-party MCP servers) cannot take the argument; they
keep the existing controls only.

Keep the enforced fields small, typed and categorical. Free text is advisory
only and no check reads it.

```
ToolInvocationExplanation
  schema_version     "ebte-0.1"
  intent_class       one value from a closed enum the application defines
                     (for example: read, search, create, update, delete, send)
  selected_tool      the tool's registered name
  expected_effect
    operation        one value from a closed enum (read, create, update, delete, send, execute)
    resource_type    a closed enum value, or null
    resource_ids     bounded list of identifiers, or empty
    max_items        integer upper bound on items touched
    destination      allowed destination identifier, or null
  risk_tier          low | medium | high
  uncertainty        low | medium | high
  untrusted_context  list of { source, digest } for each untrusted input the
                     call depends on (tool output, fetched page, file, email)
  evidence_refs      list of { type: intent | policy | payload, digest }
  summary            optional short text; advisory, never checked
```

Rules for the schema:

- Use enums and bounded lists, not prose. A field a check cannot compare is
  not an enforced field.
- `selected_tool` must equal the tool actually being called.
- `untrusted_context` must list every untrusted input the call depends on. An
  empty list is a claim too, and it is checked.
- Python uses the names above; TypeScript and Go may use camelCase, as long as
  one spelling is used across the application.
