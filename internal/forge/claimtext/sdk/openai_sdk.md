Run the mediation step in a tool input guardrail (`@tool_input_guardrail`,
attached through the tool's `tool_input_guardrails`). It runs before the tool
with the call's arguments; reject the call from it on deny and review. Map
review to the tool's `needs_approval` gate.
