Install: `npm install @opentelemetry/api @opentelemetry/sdk-trace-base @opentelemetry/context-async-hooks @opentelemetry/exporter-trace-otlp-http`

```typescript
import { context } from "@opentelemetry/api";
import { AsyncLocalStorageContextManager } from "@opentelemetry/context-async-hooks";
import { BasicTracerProvider, SimpleSpanProcessor } from "@opentelemetry/sdk-trace-base";
import { OTLPTraceExporter } from "@opentelemetry/exporter-trace-otlp-http";

context.setGlobalContextManager(new AsyncLocalStorageContextManager().enable());

const provider = new BasicTracerProvider({
  spanProcessors: [new SimpleSpanProcessor(new OTLPTraceExporter())],
});
const tracer = provider.getTracer("agent");
```

Create the provider once, at startup. For production traffic, use
`BatchSpanProcessor` in place of `SimpleSpanProcessor`.
