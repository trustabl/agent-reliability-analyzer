- OpenAI Agents SDK: its spans arrive through a bridge and may not be current
  while a tool runs. `trace.getActiveSpan()` inside the tool is then the
  application's root span, and the tool labels would land there. Check where
  they landed before relying on them.
