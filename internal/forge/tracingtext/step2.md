| Span | Labels |
|---|---|
| The agent's root span (`invoke_agent`, or the instrumentor's agent span) | `trustabl.run_id`, `trustabl.exit_reason`, one `trustabl.policy.binding` event per policy |
| Each tool span (`execute_tool`, or a span whose kind is tool) | `trustabl.tool.input_fp`, `trustabl.tool.output_fp`, `trustabl.tool.attempt`, `trustabl.tool.side_effect`, `trustabl.tool.error_class`, `trustabl.step_index`, `trustabl.run_id` |
| A delegated agent's span | `trustabl.handoff_id` |

Arguments and results are fingerprinted, never copied onto the span.

Policy bindings: call the bind-authority function only for a signed policy
that really exists in the deployment. Never invent an id, a version or a
sha256. With no policies, leave the call out.

If the framework's own agent span cannot be reached where the run starts, wrap
the run in a root span the application creates, and set
`gen_ai.operation.name` to `invoke_agent` and `gen_ai.agent.name` on it.
