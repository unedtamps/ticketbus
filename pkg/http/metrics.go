package http

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (r *statusRecorder) WriteHeader(code int) {
	r.status = code
	r.ResponseWriter.WriteHeader(code)
}

func (r *statusRecorder) Status() int {
	if r.status == 0 {
		return http.StatusOK
	}
	return r.status
}

func NewMetricsMiddleware(service string) func(http.Handler) http.Handler {
	requestsTotal := promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name:        "http_requests_total",
			Help:        "Total number of HTTP requests.",
			ConstLabels: prometheus.Labels{"service": service},
		},
		[]string{"method", "path", "status"},
	)

	requestsDuration := promauto.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:        "http_request_duration_seconds",
			Help:        "HTTP request latency in seconds.",
			ConstLabels: prometheus.Labels{"service": service},
			Buckets:     []float64{.005, .01, .025, .05, .1, .25, .5, 1, 2.5, 5, 10},
		},
		[]string{"method", "path"},
	)

	requestsInFlight := promauto.NewGauge(prometheus.GaugeOpts{
		Name:        "http_requests_in_flight",
		Help:        "Current number of HTTP requests being served.",
		ConstLabels: prometheus.Labels{"service": service},
	})

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			path := chi.RouteContext(r.Context()).RoutePattern()
			if path == "" {
				path = r.URL.Path
			}

			if strings.HasSuffix(path, "/health") || strings.HasSuffix(path, "/metrics") {
				next.ServeHTTP(w, r)
				return
			}

			requestsInFlight.Inc()
			defer requestsInFlight.Dec()

			recorder := &statusRecorder{ResponseWriter: w}
			start := time.Now()

			next.ServeHTTP(recorder, r)

			requestsTotal.WithLabelValues(
				r.Method, path, strconv.Itoa(recorder.Status()),
			).Inc()
			requestsDuration.WithLabelValues(
				r.Method, path,
			).Observe(time.Since(start).Seconds())
		})
	}
}
