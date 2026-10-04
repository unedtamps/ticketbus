package telemetry

import (
	"context"
	"os"
	"strconv"
	"time"

	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetrichttp"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/resource"
)

// defaultExportInterval is how often metrics are pushed. Long enough to be
// cheap, short enough that a Grafana refresh is not mostly empty.
const defaultExportInterval = 60 * time.Second

// newMeterProvider builds the metric pipeline, or nil when metrics are
// disabled.
//
// The exporter uses the same OTEL_EXPORTER_OTLP_* variables as the trace
// exporter, so pointing at a backend enables both signals at once. Like traces,
// the endpoint variable is the switch: with it unset no provider is created and
// every instrument degrades to a no-op.
func newMeterProvider(ctx context.Context, res *resource.Resource) (*sdkmetric.MeterProvider, error) {
	if !tracesEnabled() {
		return nil, nil
	}

	exporter, err := otlpmetrichttp.New(ctx)
	if err != nil {
		return nil, err
	}

	return sdkmetric.NewMeterProvider(
		sdkmetric.WithResource(res),
		sdkmetric.WithReader(sdkmetric.NewPeriodicReader(exporter,
			sdkmetric.WithInterval(exportInterval()))),
		sdkmetric.WithView(metricViews()...),
	), nil
}

// exportInterval honours OTEL_METRIC_EXPORT_INTERVAL (milliseconds). The Go SDK
// does not read that variable itself, so it is parsed here; anything
// unparseable or non-positive falls back to the default rather than failing
// startup over a typo.
func exportInterval() time.Duration {
	raw := os.Getenv("OTEL_METRIC_EXPORT_INTERVAL")
	if raw == "" {
		return defaultExportInterval
	}
	ms, err := strconv.Atoi(raw)
	if err != nil || ms <= 0 {
		return defaultExportInterval
	}
	return time.Duration(ms) * time.Millisecond
}
