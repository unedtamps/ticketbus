package telemetry

import (
	"context"
	"os"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
)

// Config identifies this process in the trace data it produces.
type Config struct {
	ServiceName    string
	ServiceVersion string
	Environment    string
}

// Init installs the global tracer provider and text map propagator.
//
// Everything is driven by the standard OTEL_* environment variables, so no
// vendor-specific configuration is baked into the code. When
// OTEL_EXPORTER_OTLP_ENDPOINT is unset no provider is installed at all and
// otel.Tracer() keeps returning the global no-op tracer. That keeps unit tests
// and the testcontainers-based integration suites completely free of telemetry
// overhead and export-failure noise, without them needing to know about this
// package.
//
// Init must run before anything that captures the global providers by
// reference — notably pgx's tracer, which is constructed in db.NewPool.
//
// The returned function flushes buffered spans and should be deferred.
func Init(ctx context.Context, cfg Config) (func(context.Context) error, error) {
	noop := func(context.Context) error { return nil }

	// The propagator is installed even without an exporter: it is pure in-process
	// plumbing, so injecting and extracting context costs nothing and keeps
	// behaviour identical whether or not anything is being exported.
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{},
		propagation.Baggage{},
	))

	if os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT") == "" {
		return noop, nil
	}

	// Schemaless rather than semconv-tagged: resource.Merge rejects two
	// resources whose schema URLs differ, and the SDK default carries its own.
	res := resource.NewSchemaless(
		attribute.String("service.name", cfg.ServiceName),
		attribute.String("service.version", cfg.ServiceVersion),
		attribute.String("deployment.environment.name", cfg.Environment),
	)

	exporter, err := otlptracehttp.New(ctx)
	if err != nil {
		return noop, err
	}

	provider := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(exporter),
		sdktrace.WithResource(res),
		// Local development traces everything. A real deployment should switch
		// to a ratio sampler, since always-on sampling is priced per span.
		sdktrace.WithSampler(sdktrace.ParentBased(sdktrace.AlwaysSample())),
	)
	otel.SetTracerProvider(provider)

	return provider.Shutdown, nil
}
