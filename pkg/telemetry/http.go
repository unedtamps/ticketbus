package telemetry

import (
	"context"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

// instrumentationName is the meter name for instruments this package defines.
const instrumentationName = "github.com/nedo/TicketSaas/pkg/telemetry"

// HTTPMetrics are the RED instruments the HTTP middleware records: request rate,
// error rate (via the status code attribute), and in-flight concurrency.
//
// otelhttp already emits the request duration and DB histograms, but its v0.72
// instrumentation has no counter and no active-requests gauge, so these two are
// written here rather than inherited.
//
// Identifiers such as booking_id, event_id or trace_id must never be added as
// attributes: they are unbounded and would blow up the series count. The route
// attribute carries the chi pattern, so /api/bookings/{id} stays a single series.
type HTTPMetrics struct {
	requests   metric.Int64Counter
	inFlight   metric.Int64UpDownCounter
	haveInFl   bool
	haveMetric bool
}

// NewHTTPMetrics creates the instruments. Safe to call before or after Init:
// when no meter provider is installed the returned instruments are no-ops, so
// the test suites pay nothing.
func NewHTTPMetrics() *HTTPMetrics {
	m := otel.Meter(instrumentationName)
	h := &HTTPMetrics{}

	// Construction errors are programmer errors (bad instrument definition), not
	// runtime conditions, so they surface through OTel's error handler rather
	// than being swallowed here. On error the instrument is left nil and the
	// recording helpers below become no-ops.
	requests, err := m.Int64Counter(
		"http.server.request.count",
		metric.WithDescription("Number of HTTP requests handled, by method, route and response status."),
		metric.WithUnit("{request}"),
	)
	if err == nil {
		h.requests = requests
		h.haveMetric = true
	} else {
		otel.Handle(err)
	}

	inFlight, err := m.Int64UpDownCounter(
		"http.server.active_requests",
		metric.WithDescription("Number of HTTP requests currently being served."),
		metric.WithUnit("{request}"),
	)
	if err == nil {
		h.inFlight = inFlight
		h.haveInFl = true
	} else {
		otel.Handle(err)
	}

	return h
}

// Started records the beginning of a request. Defer Unstarted with the same ctx
// so the in-flight gauge returns to zero.
func (h *HTTPMetrics) Started(ctx context.Context) {
	if h == nil || !h.haveInFl {
		return
	}
	h.inFlight.Add(ctx, 1)
}

// Unstarted records the end of a request.
func (h *HTTPMetrics) Unstarted(ctx context.Context) {
	if h == nil || !h.haveInFl {
		return
	}
	h.inFlight.Add(ctx, -1)
}

// Observed counts one completed request. route is the chi route pattern and
// status is the numeric HTTP status.
func (h *HTTPMetrics) Observed(ctx context.Context, method, route string, status int) {
	if h == nil || !h.haveMetric {
		return
	}
	h.requests.Add(ctx, 1, metric.WithAttributes(
		attribute.String("http.request.method", method),
		attribute.String("http.route", route),
		attribute.Int("http.response.status_code", status),
	))
}
