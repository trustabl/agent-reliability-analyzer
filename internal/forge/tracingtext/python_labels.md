Install: `pip install agent-reliability-otel-labels`

```python
import warnings

from opentelemetry import trace
from agent_reliability_otel_labels import Run, canonical_fp, mark_run_end, mark_tool_span


def run_agent(prompt):
    run = Run()        # one per run, never shared between runs
    attempts = {}      # calls so far in this run, per arguments fingerprint

    def search_flights(args: dict) -> dict:
        """Search flights between two airports on a date."""
        try:
            key = canonical_fp(args)
        except Exception:
            key = "search_flights (no fingerprint)"
        attempts[key] = attempts.get(key, 0) + 1
        step = run.next_step()
        span = trace.get_current_span()   # the framework's tool span
        result = None
        try:
            result = find_flights(args)
            return result
        finally:
            try:
                mark_tool_span(span, args=args, result=result,
                               attempt=attempts[key], side_effect="read",
                               step_index=step, run_id=run.run_id)
            except ValueError as err:
                warnings.warn(f"trustabl: could not label the tool span: {err}")

    with tracer.start_as_current_span("invoke_agent travel-booker") as root:
        root.set_attribute("gen_ai.operation.name", "invoke_agent")
        root.set_attribute("gen_ai.agent.name", "travel-booker")
        run.mark_start(root)
        try:
            answer = call_the_agent(prompt, tools=[search_flights])
            mark_run_end(root, exit_reason="final_answer")
            return answer
        except Exception:
            mark_run_end(root, exit_reason="error")
            raise
```

When the failure class is known, also pass `error_class`, for example
`error_class="timeout"`. For a delegated agent, call
`mark_handoff(span, handoff_id=...)` on the child agent's span. For signed
policies, call `bind_authority(root, bindings=[Binding(...)])` right after
`run.mark_start(root)`.
