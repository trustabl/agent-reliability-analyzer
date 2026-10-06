Install: `npm install @ai-sdk/otel`

```typescript
import { OpenTelemetry } from "@ai-sdk/otel";
import { generateText, registerTelemetry } from "ai";

registerTelemetry(new OpenTelemetry({ tracer }));

const result = await generateText({
  model, tools, prompt,
  telemetry: { isEnabled: true, functionId: "TravelBooker" },
});
```
