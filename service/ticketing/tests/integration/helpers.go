//go:build integration

package integration

// helpers.go — HTTP helpers, synthesized identities, the fake payment client,
// event fixtures, and the Kafka producer that stands in for the payment
// service's outbox.

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/nedo/TicketSaas/pkg/dto"
)

// ── HTTP helpers ──

func doJSON(
	method, url string,
	body interface{},
	headers map[string]string,
) (*http.Response, []byte, error) {
	var r io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		r = strings.NewReader(string(b))
	}
	req, err := http.NewRequest(method, url, r)
	if err != nil {
		return nil, nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, nil, err
	}
	defer resp.Body.Close()
	respBody, err := io.ReadAll(resp.Body)
	return resp, respBody, err
}

// pollFor calls fn every interval until it returns true or the timeout expires.
func pollFor(t *testing.T, timeout, interval time.Duration, fn func() bool, msg string) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if fn() {
			return
		}
		time.Sleep(interval)
	}
	t.Fatalf("%s timed out after %v", msg, timeout)
}

// ── Identity ──

// actor is a synthetic caller. WithUserContext reads the X-Authenticated-*
// headers the API gateway injects and never verifies a signature, so no auth
// service call is needed to act as any role.
//
// The id is a bare UUID because organizer_id and bookings.user_id are UUID.
type actor struct {
	id      string
	headers map[string]string
}

func newActor(role dto.Role) actor {
	id := uuid.NewString()
	return actor{
		id: id,
		headers: map[string]string{
			"X-Authenticated-User-ID":    id,
			"X-Authenticated-User-Role":  string(role),
			"X-Authenticated-User-Email": id + "@test.local",
		},
	}
}

func newCustomer() actor { return newActor(dto.RoleCustomer) }

func newEO() actor { return newActor(dto.RoleEO) }

func adminActor() actor { return newActor(dto.RoleAdmin) }

// ── Fake payment client ──

// fakePaymentClient stands in for the payment service. Reserve only needs a
// transaction to be created for the booking; the calls are recorded so tests can
// assert the ticketing side handed off correctly.
type fakePaymentClient struct {
	mu    sync.Mutex
	calls []string
}

func (f *fakePaymentClient) InitiateTxnForBooking(
	ctx context.Context,
	bookingID, eventID, userID, email string,
	amountRupiah int,
	expiresAt time.Time,
) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, bookingID)
	return nil
}

func (f *fakePaymentClient) calledFor(bookingID string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, id := range f.calls {
		if id == bookingID {
			return true
		}
	}
	return false
}

// ── Response shapes ──

type eventResp struct {
	ID            string `json:"id"`
	OrganizerID   string `json:"organizer_id"`
	Title         string `json:"title"`
	Status        string `json:"status"`
	VenueCapacity int    `json:"venue_capacity"`
}

type eventDetailResp struct {
	Event struct {
		ID    string `json:"id"`
		Title string `json:"title"`
	} `json:"event"`
	TicketTypes []struct {
		ID          string `json:"id"`
		Name        string `json:"name"`
		Available   int    `json:"available"`
		PriceRupiah int    `json:"price_rupiah"`
	} `json:"ticket_types"`
}

type reserveResp struct {
	Data struct {
		BookingID   string `json:"booking_id"`
		EventID     string `json:"event_id"`
		Status      string `json:"status"`
		TotalRupiah int    `json:"total_rupiah"`
	} `json:"data"`
}

// decodeDataPayload decodes the standard API envelope and unmarshals only its
// "data" field into target. target must be an INNER payload struct, not the
// envelope-wrapped variant. An error envelope returns a non-nil error.
func decodeDataPayload(respBody []byte, target interface{}) error {
	var envelope struct {
		Data  json.RawMessage `json:"data"`
		Error string          `json:"error"`
	}
	if err := json.Unmarshal(respBody, &envelope); err != nil {
		return err
	}
	if envelope.Error != "" {
		return assert.AnError
	}
	return json.Unmarshal(envelope.Data, target)
}

// ── Event fixtures ──

// createEventRaw creates a pending event as the given organizer.
func createEventRaw(t *testing.T, env *TestEnv, organizer actor) eventResp {
	t.Helper()
	future := time.Now().Add(30 * 24 * time.Hour).Format(time.RFC3339)
	futureEnd := time.Now().Add(30*24*time.Hour + 3*time.Hour).Format(time.RFC3339)

	resp, body, err := doJSON(http.MethodPost, env.apiURL+"/api/events", map[string]interface{}{
		"title":          "Integration E2E Event",
		"description":    "End-to-end test event",
		"venue_name":     "Test Stadium",
		"venue_address":  "123 Test St",
		"venue_capacity": 100,
		"start_at":       future,
		"end_at":         futureEnd,
		"ticket_types": []map[string]interface{}{
			{"name": "VIP", "price_rupiah": 10000, "quantity": 20, "max_per_order": 5},
			{"name": "GA", "price_rupiah": 5000, "quantity": 80, "max_per_order": 10},
		},
	}, organizer.headers)
	require.NoError(t, err)
	require.Equal(t, http.StatusCreated, resp.StatusCode, "create event: %s", body)

	var ev eventResp
	require.NoError(t, decodeDataPayload(body, &ev), "decode create response: %s", body)
	return ev
}

// setupApprovedEvent creates an event, approves it, and waits for its seats to
// be initialised. Returns the event id, its ticket type ids, and the organizer
// that owns it — cancellation can only be performed by that owner.
func setupApprovedEvent(t *testing.T, env *TestEnv) (eventID string, ticketTypeIDs []string, organizer actor) {
	t.Helper()

	organizer = newEO()
	ev := createEventRaw(t, env, organizer)
	eventID = ev.ID

	resp, body, err := doJSON(http.MethodPost, env.apiURL+"/api/events/"+eventID+"/approve", nil, adminActor().headers)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, resp.StatusCode, "approve: %s", body)

	cust := newCustomer()
	var detail eventDetailResp
	pollFor(t, 15*time.Second, 500*time.Millisecond, func() bool {
		_, b, _ := doJSON(http.MethodGet, env.apiURL+"/api/events/"+eventID, nil, cust.headers)
		if decodeDataPayload(b, &detail) != nil {
			return false
		}
		return len(detail.TicketTypes) > 0 && detail.TicketTypes[0].Available > 0
	}, "seat init after approval")

	for _, tt := range detail.TicketTypes {
		ticketTypeIDs = append(ticketTypeIDs, tt.ID)
	}
	return eventID, ticketTypeIDs, organizer
}

// ticketPrice reads the authoritative price so reserve payloads never hardcode
// a price that could drift from the created event.
func (env *TestEnv) ticketPrice(t *testing.T, ttID string) int {
	t.Helper()
	var price int
	require.NoError(t, env.pool.QueryRow(
		context.Background(), `SELECT price_rupiah FROM ticket_types WHERE id = $1`, ttID).Scan(&price))
	return price
}

// reserve reserves qty tickets of ttID for the given customer and returns the
// decoded response.
func (env *TestEnv) reserve(t *testing.T, cust actor, eventID, ttID string, qty, unitPrice int) (*http.Response, []byte, reserveResp) {
	t.Helper()
	resp, body, err := doJSON(http.MethodPost, env.apiURL+"/api/bookings/reserve", map[string]interface{}{
		"event_id": eventID,
		"items": []map[string]interface{}{
			{"ticket_type_id": ttID, "quantity": qty, "unit_price_rupiah": unitPrice},
		},
	}, cust.headers)
	require.NoError(t, err)

	var rr reserveResp
	_ = json.Unmarshal(body, &rr)
	return resp, body, rr
}

// ── Upstream events (stand-in for the payment service) ──

// emitPaymentCompleted publishes payment.completed the way the payment service
// does through its outbox.
func (env *TestEnv) emitPaymentCompleted(t *testing.T, txnID, bookingID, eventID, userID string) {
	t.Helper()
	require.NoError(t, env.infra.Producer.Produce(
		context.Background(),
		"payment.completed",
		txnID,
		dto.PaymentCompleted{
			TransactionID: txnID,
			BookingID:     bookingID,
			EventID:       eventID,
			UserID:        userID,
			At:            time.Now(),
		},
	))
}

// emitPaymentExpired publishes payment.expired the way the payment service's
// expiry sweeper does.
func (env *TestEnv) emitPaymentExpired(t *testing.T, txnID, bookingID, eventID, userID string) {
	t.Helper()
	require.NoError(t, env.infra.Producer.Produce(
		context.Background(),
		"payment.expired",
		txnID,
		dto.PaymentExpired{
			TransactionID: txnID,
			BookingID:     bookingID,
			EventID:       eventID,
			UserID:        userID,
			Reason:        "gateway_expired",
			At:            time.Now(),
		},
	))
}

// emitEventCancelled publishes event.cancelled the way the ticketing outbox does
// on cancellation.
func (env *TestEnv) emitEventCancelled(t *testing.T, eventID string) {
	t.Helper()
	require.NoError(t, env.infra.Producer.Produce(
		context.Background(),
		"event.cancelled",
		eventID,
		dto.EventCancelled{EventID: eventID, At: time.Now()},
	))
}

// ── Assertions ──

func (env *TestEnv) bookingStatus(t *testing.T, bookingID string) string {
	t.Helper()
	var status string
	require.NoError(t, env.pool.QueryRow(
		context.Background(), `SELECT status FROM bookings WHERE id = $1`, bookingID).Scan(&status))
	return status
}

func (env *TestEnv) availableSeats(t *testing.T, ttID string) int {
	t.Helper()
	var n int
	require.NoError(t, env.pool.QueryRow(
		context.Background(), `SELECT available_seat FROM ticket_types WHERE id = $1`, ttID).Scan(&n))
	return n
}
