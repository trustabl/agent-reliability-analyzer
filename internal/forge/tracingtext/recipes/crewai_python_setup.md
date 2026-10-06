Install: `pip install openinference-instrumentation-crewai`

```python
from openinference.instrumentation.crewai import CrewAIInstrumentor

CrewAIInstrumentor().instrument(tracer_provider=provider)
```

A mismatched instrumentor version emits no spans and only prints a warning,
so run the confirm step.
