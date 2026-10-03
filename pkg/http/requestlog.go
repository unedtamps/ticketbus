package http

import (
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
)

// RequestLogger emits one structured JSON line per HTTP request through slog.
//
// It replaces chi's middleware.Logger, which writes plain text that a log
// aggregator cannot index, and the Prometheus instrumentation this stack no
// longer uses. Because the line is emitted with InfoContext, a trace_id is
// attached automatically once distributed tracing is enabled, with no change
// here.
//
// The logged path comes from chi's route pattern rather than the raw URL, so a
// per-entity route such as /api/bookings/{id} stays one low-cardinality value
// instead of one per identifier.
func RequestLogger(logger *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			// NewWrapResponseWriter reports Status and BytesWritten and keeps
			// http.Flusher and http.Hijacker intact, which a hand-rolled
			// ResponseWriter wrapper silently breaks.
			ww := middleware.NewWrapResponseWriter(w, r.ProtoMajor)

			next.ServeHTTP(ww, r)

			// The pattern has to be read here rather than before next.ServeHTTP:
			// chi resolves the route inside the handler chain, so during a
			// r.Use() middleware the pattern is still empty. Reading it earlier
			// silently fell back to the raw URL and gave every /api/bookings/{id}
			// its own series.
			path := chi.RouteContext(r.Context()).RoutePattern()
			if path == "" {
				path = r.URL.Path
			}

			// Container healthchecks poll /health every few seconds; logging
			// those would bury real traffic.
			if strings.HasSuffix(path, "/health") {
				return
			}

			logger.InfoContext(r.Context(), "http request",
				"method", r.Method,
				"path", path,
				"status", ww.Status(),
				"bytes", ww.BytesWritten(),
				"duration_ms", time.Since(start).Milliseconds(),
				"request_id", middleware.GetReqID(r.Context()),
			)
		})
	}
}
