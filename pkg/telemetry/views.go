package telemetry

import (
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
)

// Bucket boundaries for the latency histograms.
//
// These are pinned with Views rather than relying on the instrument's own
// WithExplicitBucketBoundaries option, which the OpenTelemetry API documents as
// advisory: an implementation may ignore it and fall back to OTel's default
// boundaries of [0,5,10,25,...] seconds, which are useless for HTTP and query
// latency. A View is honoured by the SDK, so the quantiles mean something.
var (
	// Matches the OpenTelemetry HTTP semantic conventions.
	httpDurationBuckets = []float64{
		0.005, 0.01, 0.025, 0.05, 0.075, 0.1, 0.25, 0.5, 0.75, 1, 2.5, 5, 7.5, 10,
	}
	// Matches the boundaries otelpgx asks for on its DB duration histogram.
	dbDurationBuckets = []float64{0.001, 0.005, 0.01, 0.05, 0.1, 0.5, 1, 5, 10}
)

// metricViews returns the SDK views applied to every meter.
func metricViews() []sdkmetric.View {
	return []sdkmetric.View{
		// NoMinMax: min/max add two series per attribute combination and are of
		// no use for a latency SLO expressed in quantiles.
		bucketedView("http.server.request.duration", httpDurationBuckets),
		bucketedView("http.client.request.duration", httpDurationBuckets),
		bucketedView("db.client.operation.duration", dbDurationBuckets),

		// Body sizes are dropped on purpose. Every payload in this codebase is
		// fixed-shape JSON of a couple hundred bytes, so all these buckets would
		// collapse into the lowest one — stored series for no signal. The
		// instruments themselves still exist inside otelhttp; these views just
		// keep them out of the pipeline. Delete the entries to bring them back.
		dropView("http.server.request.body.size"),
		dropView("http.server.response.body.size"),
		dropView("http.client.request.body.size"),
	}
}

func bucketedView(name string, boundaries []float64) sdkmetric.View {
	return sdkmetric.NewView(
		sdkmetric.Instrument{Name: name},
		sdkmetric.Stream{
			Aggregation: sdkmetric.AggregationExplicitBucketHistogram{
				Boundaries: boundaries,
				NoMinMax:   true,
			},
		},
	)
}

func dropView(name string) sdkmetric.View {
	return sdkmetric.NewView(
		sdkmetric.Instrument{Name: name},
		sdkmetric.Stream{Aggregation: sdkmetric.AggregationDrop{}},
	)
}
