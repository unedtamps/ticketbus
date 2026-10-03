package http

import (
	"net/http"

	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
)

// OTelMiddleware starts a server span for each request, continuing any trace
// context the caller propagated in the traceparent header. Span names come out
// as "METHOD /route-pattern", because chi populates the standard
// http.Request.Pattern field and otelhttp derives the name from it.
//
// It must be registered *outside* RequestLogger so the span is already in the
// request context by the time RequestLogger emits its access log line — that is
// what gives those lines a trace_id. Registered the other way round, the access
// log would silently have no trace to correlate against.
//
// No operation name is passed: otelhttp's default span-name formatter discards
// it in favour of the method and route. service.name comes from the resource
// attributes set in pkg/telemetry, which is where a backend groups by service.
func OTelMiddleware() func(http.Handler) http.Handler {
	return otelhttp.NewMiddleware("")
}
