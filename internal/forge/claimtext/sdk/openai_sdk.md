Run the mediation step as the first statement of each `@function_tool`
function, before any side effect, through one shared wrapper so no tool is
left out. Map review to the tool's `needs_approval` gate.
