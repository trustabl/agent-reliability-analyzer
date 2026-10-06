Before adding anything, look for tracing that is already set up in this repo.
If a tracer provider is already created, reuse it; never create a second one.
Still turn on the framework's instrumentation below if it is not on yet, and
run the confirm step either way.

Each language block below is followed by framework sections where a verified
setup exists. For a framework with no section, turn on its own OpenTelemetry
instrumentation, or add an OpenTelemetry-based instrumentor for it, then run
the confirm step.
