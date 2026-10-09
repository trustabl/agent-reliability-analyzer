Install: `npm install @trustabl/agent-reliability-otel-labels`

```typescript
import { trace } from "@opentelemetry/api";
import { Run, canonicalFp, markRunEnd, markToolSpan } from "@trustabl/agent-reliability-otel-labels";

// Built once per run, so two runs never share counters.
function buildRun() {
  const run = new Run();
  const attempts = new Map<string, number>();

  async function searchFlights(args: { from: string; to: string; date: string }) {
    let key: string;
    try {
      key = canonicalFp(args);
    } catch {
      key = "search_flights (no fingerprint)";
    }
    const attempt = (attempts.get(key) ?? 0) + 1;
    attempts.set(key, attempt);
    const stepIndex = run.nextStep();
    const span = trace.getActiveSpan(); // the framework's tool span

    let result: unknown;
    try {
      result = await findFlights(args);
      return result;
    } finally {
      try {
        markToolSpan(span, {
          args, result, attempt, sideEffect: "read",
          stepIndex, runId: run.runId, name: "search_flights",
        });
      } catch (err) {
        console.error(`trustabl: could not label the tool span: ${err}`);
      }
    }
  }

  return { run, searchFlights };
}

async function runAgent(prompt: string) {
  const { run, searchFlights } = buildRun();
  return tracer.startActiveSpan("travel-booker", async (root) => {
    root.setAttribute("gen_ai.operation.name", "invoke_agent");
    root.setAttribute("gen_ai.agent.name", "travel-booker");
    run.markStart(root);
    try {
      const answer = await callTheAgent(prompt, { searchFlights });
      markRunEnd(root, "final_answer");
      return answer;
    } catch (err) {
      markRunEnd(root, "error");
      throw err;
    } finally {
      root.end();
    }
  });
}
```

When the failure class is known, also pass `errorClass`, for example
`errorClass: "timeout"`. For a delegated agent, call
`markHandoff(span, handoffId)` on the child agent's span. For signed policies,
call `bindAuthority(root, bindings)` right after `run.markStart(root)`.
