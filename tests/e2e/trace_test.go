//go:build e2e

package e2e

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"
)

// Trace assertions live against Tempo's HTTP query API, which is the same API
// Grafana uses. That keeps the check honest: if this test can find the spans,
// the Grafana datasource can too.
//
// The e2e stack runs Tempo and a traces-only collector (see
// observability/collector.e2e.yaml); the services only export spans, so this
// proves propagation rather than mocking it.

const defaultTempoURL = "http://localhost:13200"

func tempoURL() string {
	if u := os.Getenv("E2E_TEMPO_URL"); u != "" {
		return u
	}
	return defaultTempoURL
}

// tempoSpan is the subset of Tempo's OTLP JSON this test asserts on.
type tempoSpan struct {
	TraceID      string            `json:"traceId"`
	SpanID       string            `json:"spanId"`
	ParentSpanID string            `json:"parentSpanId"`
	Name         string            `json:"name"`
	Kind         string            `json:"kind"`
	Service      string            `json:"-"`
	Attributes   map[string]string `json:"-"`
}

type tempoTrace struct {
	Batches []struct {
		Resource struct {
			Attributes []struct {
				Key   string `json:"key"`
				Value struct {
					StringValue string `json:"stringValue"`
				} `json:"value"`
			} `json:"attributes"`
		} `json:"resource"`
		ScopeSpans []struct {
			Spans []struct {
				TraceID      string `json:"traceId"`
				SpanID       string `json:"spanId"`
				ParentSpanID string `json:"parentSpanId"`
				Name         string `json:"name"`
				Kind         string `json:"kind"`
				Attributes   []struct {
					Key   string `json:"key"`
					Value struct {
						StringValue string `json:"stringValue"`
						IntValue    string `json:"intValue"`
					} `json:"value"`
				} `json:"attributes"`
			} `json:"spans"`
		} `json:"scopeSpans"`
	} `json:"batches"`
}

func (tr tempoTrace) flatten() []tempoSpan {
	var out []tempoSpan
	for _, b := range tr.Batches {
		service := ""
		for _, a := range b.Resource.Attributes {
			if a.Key == "service.name" {
				service = a.Value.StringValue
			}
		}
		for _, ss := range b.ScopeSpans {
			for _, s := range ss.Spans {
				attrs := make(map[string]string, len(s.Attributes))
				for _, a := range s.Attributes {
					if a.Value.StringValue != "" {
						attrs[a.Key] = a.Value.StringValue
					} else if a.Value.IntValue != "" {
						attrs[a.Key] = a.Value.IntValue
					}
				}
				out = append(out, tempoSpan{
					TraceID: s.TraceID, SpanID: s.SpanID, ParentSpanID: s.ParentSpanID,
					Name: s.Name, Kind: s.Kind, Service: service, Attributes: attrs,
				})
			}
		}
	}
	return out
}

func tempoGet(t *testing.T, path string, out interface{}) {
	t.Helper()
	resp, err := http.Get(tempoURL() + path)
	if err != nil {
		t.Fatalf("tempo unreachable at %s: %v", tempoURL(), err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("tempo read failed: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("tempo %s -> %d: %s", path, resp.StatusCode, truncate(string(body), 200))
	}
	if err := json.Unmarshal(body, out); err != nil {
		t.Fatalf("tempo %s returned unparseable JSON: %v", path, err)
	}
}

type tempoSearchResult struct {
	TraceID           string `json:"traceID"`
	RootTraceName     string `json:"rootTraceName"`
	StartTimeUnixNano string `json:"startTimeUnixNano"`
}

// startedAfter reports the trace start time and whether it is at or after since.
func (r tempoSearchResult) startedAfter(since time.Time) (int64, bool) {
	if r.StartTimeUnixNano == "" {
		return 0, false
	}
	n, err := strconv.ParseInt(r.StartTimeUnixNano, 10, 64)
	if err != nil {
		return 0, false
	}
	return n, n >= since.UnixNano()
}

type tempoSearch struct {
	Traces []tempoSearchResult `json:"traces"`
}

// findTraceSince polls Tempo for a trace with the given root span name that
// started after `since`.
//
// Both conditions matter. Matching on the name alone would happily return a
// trace left behind by an earlier test — several tests POST to /webhook and
// /reserve — which turns the assertions below into false passes. Filtering the
// results client-side by start time avoids passing start/end to the API, since
// doing that restricts Tempo's block search and can return a partial trace.
//
// Spans land in the WAL before they are searchable, hence the polling.
func findTraceSince(t *testing.T, rootName string, since time.Time) []tempoSpan {
	t.Helper()

	var searched int
	pollFor(t, 90*time.Second, 2*time.Second, func() bool {
		var search tempoSearch
		tempoGet(t, "/api/search?limit=500", &search)
		searched = len(search.Traces)

		for _, candidate := range search.Traces {
			if candidate.RootTraceName != rootName {
				continue
			}
			if _, ok := candidate.startedAfter(since); !ok {
				continue // belongs to an earlier test
			}
			var trace tempoTrace
			tempoGet(t, "/api/traces/"+candidate.TraceID, &trace)
			return len(trace.flatten()) > 0
		}
		return false
	}, fmt.Sprintf("trace %q started after %s to appear (%d traces searched)",
		rootName, since.Format(time.RFC3339), searched))

	var search tempoSearch
	tempoGet(t, "/api/search?limit=500", &search)
	for _, candidate := range search.Traces {
		if candidate.RootTraceName != rootName {
			continue
		}
		if _, ok := candidate.startedAfter(since); !ok {
			continue
		}
		var trace tempoTrace
		tempoGet(t, "/api/traces/"+candidate.TraceID, &trace)
		if spans := trace.flatten(); len(spans) > 0 {
			return spans
		}
	}
	t.Fatalf("trace %q vanished between search and fetch", rootName)
	return nil
}

func spanByName(spans []tempoSpan, name string) (tempoSpan, bool) {
	for _, s := range spans {
		if s.Name == name {
			return s, true
		}
	}
	return tempoSpan{}, false
}

func hasService(spans []tempoSpan, service string) bool {
	for _, s := range spans {
		if s.Service == service {
			return true
		}
	}
	return false
}

func kindOf(s tempoSpan) string { return strings.TrimPrefix(s.Kind, "SPAN_KIND_") }

// TestE2E_Trace_ReserveSpansBothServices covers the synchronous hop: one trace
// must contain the ticketing server span, the client span that called payment,
// and payment's server span as its child. If traceparent were not forwarded,
// payment would appear as a second unrelated root.
func TestE2E_Trace_ReserveSpansBothServices(t *testing.T) {
	env := getTestEnv()
	admin := env.loginAdmin(t)
	eventID, ticketTypes := env.approvedEvent(t, admin)
	cust := env.registerCustomer(t)
	tt := ticketTypes[0]

	since := time.Now()
	env.reserve(t, cust, eventID, tt.ID, 1, tt.PriceRupiah)

	spans := findTraceSince(t, "POST /api/bookings/reserve", since)

	if !hasService(spans, "ticketing-service") {
		t.Fatalf("trace is missing ticketing-service spans: %s", describe(spans))
	}
	if !hasService(spans, "payment-service") {
		t.Fatalf("reserve should reach payment-service in the same trace: %s", describe(spans))
	}

	server, ok := spanByName(spans, "POST /api/payments/internal")
	if !ok {
		t.Fatalf("no payment server span in trace: %s", describe(spans))
	}
	if kindOf(server) != "SERVER" {
		t.Errorf("payment internal span kind = %s, want SERVER", kindOf(server))
	}

	// Cross-service parentage is the whole point: payment's server span must be
	// a child of a ticketing span, not a root of its own.
	if server.ParentSpanID == "" {
		t.Fatalf("payment server span is a root — traceparent was not forwarded: %s", describe(spans))
	}
	parent, ok := findSpan(spans, server.ParentSpanID)
	if !ok {
		t.Fatalf("payment server span parent %s not present in trace: %s", server.ParentSpanID, describe(spans))
	}
	if parent.Service != "ticketing-service" {
		t.Errorf("payment span parented to %s, want ticketing-service", parent.Service)
	}
	if kindOf(parent) != "CLIENT" {
		t.Errorf("payment parent kind = %s, want CLIENT (the outbound HTTP call)", kindOf(parent))
	}

	// DB spans prove pkg/db instrumentation is live.
	assertDBSpans(t, spans)
}

// TestE2E_Trace_KafkaHopStaysInOneTrace covers the asynchronous hop, which is
// the fragile one. The webhook request ends long before the outbox worker
// publishes, so the producer span only shares the trace if the worker restored
// the context persisted in the outbox row. The consumer span must then be a
// child of that producer span across the Kafka hop.
func TestE2E_Trace_KafkaHopStaysInOneTrace(t *testing.T) {
	env := getTestEnv()
	admin := env.loginAdmin(t)
	eventID, ticketTypes := env.approvedEvent(t, admin)
	cust := env.registerCustomer(t)
	tt := ticketTypes[0]

	since := time.Now()
	bookingID := env.reserve(t, cust, eventID, tt.ID, 1, tt.PriceRupiah)
	sessionID := env.payBooking(t, cust, bookingID)
	env.deliverWebhook(t, "payment_session.completed", bookingID, sessionID)
	env.waitBookingStatus(t, cust, bookingID, "confirmed", settleTimeout)

	spans := findTraceSince(t, "POST /api/payments/webhook", since)

	if !hasService(spans, "payment-service") || !hasService(spans, "ticketing-service") {
		t.Fatalf("webhook trace should span both services: %s", describe(spans))
	}

	webhook, ok := spanByName(spans, "POST /api/payments/webhook")
	if !ok || kindOf(webhook) != "SERVER" {
		t.Fatalf("webhook server span missing or wrong kind: %s", describe(spans))
	}

	producer, ok := spanByName(spans, "payment.completed publish")
	if !ok {
		t.Fatalf("no producer span for payment.completed: %s", describe(spans))
	}
	if kindOf(producer) != "PRODUCER" {
		t.Errorf("producer span kind = %s, want PRODUCER", kindOf(producer))
	}
	if producer.Service != "payment-service" {
		t.Errorf("producer span service = %s, want payment-service", producer.Service)
	}

	// The decisive assertion: the outbox worker ran on its own goroutine, so the
	// only way this parent link exists is via outbox.trace_context.
	if producer.ParentSpanID != webhook.SpanID {
		t.Errorf("producer span parent = %q, want the webhook server span %q — "+
			"the outbox did not carry trace context", producer.ParentSpanID, webhook.SpanID)
	}

	consumer, ok := spanByName(spans, "payment.completed process")
	if !ok {
		t.Fatalf("no consumer span for payment.completed: %s", describe(spans))
	}
	if kindOf(consumer) != "CONSUMER" {
		t.Errorf("consumer span kind = %s, want CONSUMER", kindOf(consumer))
	}
	if consumer.Service != "ticketing-service" {
		t.Errorf("consumer span service = %s, want ticketing-service", consumer.Service)
	}
	if consumer.ParentSpanID != producer.SpanID {
		t.Errorf("consumer span parent = %q, want the producer span %q — "+
			"trace context was not propagated through the Kafka headers", consumer.ParentSpanID, producer.SpanID)
	}

	// One trace means one trace ID throughout.
	ids := map[string]bool{}
	for _, s := range spans {
		ids[s.TraceID] = true
	}
	if len(ids) != 1 {
		t.Errorf("expected a single trace ID across %d spans, got %d", len(spans), len(ids))
	}
}

func findSpan(spans []tempoSpan, spanID string) (tempoSpan, bool) {
	for _, s := range spans {
		if s.SpanID == spanID {
			return s, true
		}
	}
	return tempoSpan{}, false
}

func assertDBSpans(t *testing.T, spans []tempoSpan) {
	t.Helper()
	for _, s := range spans {
		if kindOf(s) == "CLIENT" && strings.EqualFold(s.Name, "SELECT") {
			if _, ok := s.Attributes["db.query.text"]; !ok {
				t.Errorf("db span %q has no db.query.text attribute", s.Name)
			}
			return
		}
	}
	t.Errorf("no database spans found: %s", describe(spans))
}

func describe(spans []tempoSpan) string {
	var b strings.Builder
	b.WriteString("\nspans:\n")
	for _, s := range spans {
		fmt.Fprintf(&b, "  %-9s %-18s %-34s parent=%s\n",
			kindOf(s), s.Service, s.Name, orDash(s.ParentSpanID))
	}
	return b.String()
}

func orDash(s string) string {
	if s == "" {
		return "<root>"
	}
	return s
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
