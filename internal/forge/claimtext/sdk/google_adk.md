Run the mediation step in the agent's `before_tool_callback`. Returning a
result from the callback stops the tool; return the coarse reason on deny and
review.
