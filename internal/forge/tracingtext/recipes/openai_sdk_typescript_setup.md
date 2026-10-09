Install: `npm install @arizeai/openinference-instrumentation-openai-agents`

```typescript
import { OpenInferenceTracingProcessor } from "@arizeai/openinference-instrumentation-openai-agents";
import { addTraceProcessor } from "@openai/agents";

addTraceProcessor(new OpenInferenceTracingProcessor({ tracerProvider: provider }));
```
