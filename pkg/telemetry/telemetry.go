package telemetry

import (
	"context"
	"errors"
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

// Init installs the global propagator, tracer provider and meter provider.
//
// Everything is driven by the standard OTEL_* environment variables, so no
// vendor-specific configuration is baked into the code. When
// OTEL_EXPORTER_OTLP_ENDPOINT is unset no provider is installed at all:
// otel.Tracer() and otel.Meter() keep returning the global no-op
// implementations. That keeps unit tests and the testcontainers-based
// integration suites completely free of telemetry overhead and
// export-failure noise, without them needing to know about this package.
//
// Init must run before anything that captures the global providers by
// reference — notably pgx's tracer, which is constructed in db.NewPool.
//
// The returned shutdown function flushes buffered spans and metrics and should
// be deferred. httpMetrics may be nil, in which case RequestLogger simply skips
// recording.
func Init(ctx context.Context, cfg Config) (func(context.Context) error, *HTTPMetrics, error) {
	noop := func(context.Context) error { return nil }

	// The propagator is installed even without an exporter: it is pure in-process
	// plumbing, so injecting and extracting context costs nothing and keeps
	// behaviour identical whether or not anything is being exported.
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{},
		propagation.Baggage{},
	))

	if !tracesEnabled() {
		// Metrics can still be enabled independently, but today they share the
		// endpoint switch, so an empty endpoint means neither signal is exported.
		return noop, nil, nil
	}

	// Schemaless rather than semconv-tagged: resource.Merge rejects two
	// resources whose schema URLs differ, and the SDK default carries its own.
	// Shared by both providers so service.name is identical on traces and metrics.
	res := resource.NewSchemaless(
		attribute.String("service.name", cfg.ServiceName),
		attribute.String("service.version", cfg.ServiceVersion),
		attribute.String("deployment.environment.name", cfg.Environment),
	)

	exporter, err := otlptracehttp.New(ctx)
	if err != nil {
		return noop, nil, err
	}

	provider := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(exporter),
		sdktrace.WithResource(res),
		// Local development traces everything. A real deployment should switch
		// to a ratio sampler, since always-on sampling is priced per span.
		sdktrace.WithSampler(sdktrace.ParentBased(sdktrace.AlwaysSample())),
	)
	otel.SetTracerProvider(provider)

	meterProvider, err := newMeterProvider(ctx, res)
	if err != nil {
		// Traces are already live; keep them and report the metrics failure
		// rather than taking the process down over the second signal.
		otel.Handle(err)
		return provider.Shutdown, nil, nil
	}
	if meterProvider != nil {
		otel.SetMeterProvider(meterProvider)
	}

	shutdown := func(shutdownCtx context.Context) error {
		// Metrics first: a flush failure there should not prevent spans being
		// flushed, and neither should stop the other from being attempted.
		var metricErr error
		if meterProvider != nil {
			metricErr = meterProvider.Shutdown(shutdownCtx)
		}
		return errors.Join(provider.Shutdown(shutdownCtx), metricErr)
	}

	return shutdown, NewHTTPMetrics(), nil
}

// tracesEnabled reports whether an exporter should be installed. The endpoint
// variable is the single opt-in switch for both signals.
func tracesEnabled() bool {
	return os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT") != ""
}
