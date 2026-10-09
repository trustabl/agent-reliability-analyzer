Pydantic AI emits spans itself once instrumentation is switched on. No extra
package. On Pydantic AI 2.x:

```python
from pydantic_ai import Agent, InstrumentationSettings

Agent.instrument_all(InstrumentationSettings(tracer_provider=provider))
```
