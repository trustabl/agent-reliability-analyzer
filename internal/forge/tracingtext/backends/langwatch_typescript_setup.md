This project sends its traces to LangWatch. Keep its `setupObservability`
call, and do not add another tracer provider or exporter. If the call is
missing, add it once, at startup, before the agent runs. It reads the key from
`LANGWATCH_API_KEY`.

Install: `npm install langwatch @opentelemetry/api`

```typescript
import { trace } from "@opentelemetry/api";
import { setupObservability } from "langwatch/observability/node";

setupObservability({ serviceName: "agent" });
const provider = trace.getTracerProvider();
const tracer = provider.getTracer("agent");
```

If the project registers its own tracer provider before this call, pass
`advanced: { attachToExistingProvider: true }` to `setupObservability`.
Without it, LangWatch receives no spans.

LangWatch's spans are OpenTelemetry spans, so the labels in Step 2 attach to
them.

If a part of the project uses OpenTelemetry without the LangWatch SDK, keep
that provider and point its exporter at LangWatch over HTTP, with protobuf or
JSON bodies. LangWatch does not accept gRPC. Set
`OTEL_EXPORTER_OTLP_TRACES_ENDPOINT` in the deployment's environment to the
LangWatch trace endpoint: scheme `https`, host `app.langwatch.ai`, path
`/api/otel/v1/traces`. The exporter reads it from there.

```typescript
import { OTLPTraceExporter } from "@opentelemetry/exporter-trace-otlp-http";

const exporter = new OTLPTraceExporter({
  headers: { Authorization: `Bearer ${process.env.LANGWATCH_API_KEY}` },
});
```
