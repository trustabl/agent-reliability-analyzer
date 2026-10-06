Install: `pip install openinference-instrumentation-openai-agents`

```python
from openinference.instrumentation.openai_agents import OpenAIAgentsInstrumentor

OpenAIAgentsInstrumentor().instrument(tracer_provider=provider)
```
