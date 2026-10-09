Install: `go get github.com/trustabl/agent-reliability-otel-labels/go`

```go
import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sync"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"

	otellabels "github.com/trustabl/agent-reliability-otel-labels/go"
)

func runAgent(ctx context.Context, prompt string) (string, error) {
	// One per run, never shared between runs.
	run := otellabels.NewRun("")
	var mu sync.Mutex
	attempts := map[string]int{}

	searchFlights := func(ctx context.Context, a searchArgs) (result searchResult, err error) {
		// Structs are refused: pass the JSON form.
		rawArgs, _ := json.Marshal(a)
		key, fpErr := otellabels.CanonicalFP(json.RawMessage(rawArgs))
		if fpErr != nil {
			key = "search_flights (no fingerprint)"
		}
		mu.Lock()
		attempts[key]++
		attempt := attempts[key]
		mu.Unlock()
		step := run.NextStep()
		span := trace.SpanFromContext(ctx) // the framework's tool span

		defer func() {
			call := otellabels.ToolCall{
				Args: json.RawMessage(rawArgs), Attempt: attempt,
				SideEffect: otellabels.Read, StepIndex: &step,
				RunID: run.RunID(), Name: "search_flights",
			}
			if err == nil {
				rawResult, _ := json.Marshal(result)
				call.Result = json.RawMessage(rawResult)
			}
			if mErr := otellabels.MarkToolSpan(span, call); mErr != nil {
				fmt.Fprintln(os.Stderr, "trustabl: could not label the tool span:", mErr)
			}
		}()
		return findFlights(a)
	}

	ctx, root := tracer.Start(ctx, "invoke_agent travel-booker")
	defer root.End()
	root.SetAttributes(
		attribute.String("gen_ai.operation.name", "invoke_agent"),
		attribute.String("gen_ai.agent.name", "travel-booker"),
	)
	run.MarkStart(root)

	answer, err := callTheAgent(ctx, prompt, searchFlights)
	reason := otellabels.FinalAnswer
	if err != nil {
		reason = otellabels.Error
	}
	// Still before the root span ends: the deferred End runs after this.
	if mErr := otellabels.MarkRunEnd(root, reason); mErr != nil {
		fmt.Fprintln(os.Stderr, "trustabl: could not mark the run's end:", mErr)
	}
	return answer, err
}
```

When the failure class is known, also set `ErrorClass`, for example
`otellabels.Timeout`; the values `4xx` and `5xx` are `otellabels.Class4xx`
and `otellabels.Class5xx`. For a delegated agent, call
`otellabels.MarkHandoff(span, handoffID)` on the child agent's span. For
signed policies, call `otellabels.BindAuthority(root, bindings)` right after
`run.MarkStart(root)`.
