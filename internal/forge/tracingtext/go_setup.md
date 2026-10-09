Install: `go get go.opentelemetry.io/otel go.opentelemetry.io/otel/sdk go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp`

```go
import (
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
)

exporter, err := otlptracehttp.New(ctx)
if err != nil {
	return err
}
provider := sdktrace.NewTracerProvider(
	sdktrace.WithSyncer(exporter),
	sdktrace.WithResource(resource.Default()),
)
otel.SetTracerProvider(provider)
defer func() { _ = provider.Shutdown(ctx) }()
tracer := provider.Tracer("agent")
```

Create the provider once, at startup. For production traffic, use
`sdktrace.WithBatcher` in place of `sdktrace.WithSyncer`.
