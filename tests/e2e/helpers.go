//go:build e2e

package e2e

// helpers.go — gateway HTTP helpers, identity, event fixtures, and the payment
// automation (reserve → create session → deliver gateway webhook).
//
// Every request carries a real JWT. Traefik's forward-auth middleware calls the
// auth service's /api/auth/verify and copies X-Authenticated-User-{ID,Role,Email}
// onto the upstream request, which is what ticketing and payment read. So these
// helpers only ever send "Authorization: Bearer <token>" and never the
// X-Authenticated-* headers themselves.

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

const (
	adminEmail    = "admin@test.com"
	adminPassword = "Admin123!"
	testPassword  = "Test123!"

	// Must match WEBHOOK_CALLBACK_TOKEN in docker-compose.e2e.yml. The webhook
	// route is public at the gateway, so the shared secret is the only check.
	callbackToken = "e2e-callback-token"

	// txnTimeout bounds the transaction state transition, which is a direct
	// database write inside the webhook handler.
	txnTimeout = 30 * time.Second

	// settleTimeout bounds the full asynchronous leg: payment writes an outbox
	// row, an outbox worker publishes to Kafka, and the ticketing consumer
	// applies it. Every hop is a separate process in the e2e stack.
	settleTimeout = 60 * time.Second

	// pollInterval is how often asynchronous assertions re-check. The e2e stack
	// hops through Kafka and three containers, so polling is cheap relative to
	// the latency being waited on.
	pollInterval = 500 * time.Millisecond
)

// ── HTTP ──

// doJSON sends a JSON request through the gateway and returns status and body.
func (env *TestEnv) doJSON(method, path string, body interface{}, headers map[string]string) (int, []byte, error) {
	var r io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return 0, nil, err
		}
		r = bytes.NewReader(b)
	}
	req, err := http.NewRequest(method, env.url(path), r)
	if err != nil {
		return 0, nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := env.client.Do(req)
	if err != nil {
		return 0, nil, err
	}
	defer resp.Body.Close()
	out, err := io.ReadAll(resp.Body)
	return resp.StatusCode, out, err
}

// bearer builds the auth header for an account.
func bearer(a account) map[string]string {
	return map[string]string{"Authorization": "Bearer " + a.token}
}

// decodeData unwraps the standard {"data":…} / {"error":…} envelope.
func decodeData(body []byte, target interface{}) error {
	var envelope struct {
		Data  json.RawMessage `json:"data"`
		Error string          `json:"error"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil {
		return err
	}
	if envelope.Error != "" {
		return fmt.Errorf("api error: %s", envelope.Error)
	}
	return json.Unmarshal(envelope.Data, target)
}

// pollFor calls fn until it returns true or the timeout expires.
func pollFor(t *testing.T, timeout, interval time.Duration, fn func() bool, msg string) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if fn() {
			return
		}
		time.Sleep(interval)
	}
	t.Fatalf("%s: timed out after %v", msg, timeout)
}

// ── Identity ──

// account is a real user with a real signed token.
type account struct {
	id    string
	email string
	role  string
	token string
}

// register creates a customer through the public auth API.
func (env *TestEnv) registerCustomer(t *testing.T) account {
	t.Helper()
	email := uniqueEmail("cust")
	status, body, err := env.doJSON(http.MethodPost, "/api/auth/register", map[string]interface{}{
		"email": email, "password": testPassword, "name": "E2E Customer", "role": "customer",
	}, nil)
	require.NoError(t, err)
	require.Equal(t, http.StatusCreated, status, "register customer: %s", body)
	return env.login(t, email)
}

// registerEO creates an event organizer through the public auth API.
func (env *TestEnv) registerEO(t *testing.T) account {
	t.Helper()
	email := uniqueEmail("eo")
	status, body, err := env.doJSON(http.MethodPost, "/api/auth/register/organizer", map[string]interface{}{
		"email": email, "password": testPassword, "name": "E2E EO",
		"organizer_name": "E2E Org", "description": "e2e",
		"profile_link": "https://org.test", "contact_email": email,
	}, nil)
	require.NoError(t, err)
	require.Equal(t, http.StatusCreated, status, "register eo: %s", body)
	return env.login(t, email)
}

// loginAdmin logs in the admin seeded by ADMIN_SEEDS at service start.
func (env *TestEnv) loginAdmin(t *testing.T) account {
	t.Helper()
	return env.login(t, adminEmail)
}

func (env *TestEnv) login(t *testing.T, email string) account {
	t.Helper()
	status, body, err := env.doJSON(http.MethodPost, "/api/auth/login", map[string]string{
		"email": email, "password": testPassword,
	}, nil)
	// The admin seed uses its own password.
	if status == http.StatusUnauthorized {
		status, body, err = env.doJSON(http.MethodPost, "/api/auth/login", map[string]string{
			"email": email, "password": adminPassword,
		}, nil)
	}
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, status, "login %s: %s", email, body)

	var out struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
	}
	require.NoError(t, decodeData(body, &out), "login %s: %s", email, body)
	require.NotEmpty(t, out.AccessToken)

	// Read the identity back from /api/auth/me so tests assert against what the
	// services actually see.
	me := env.me(t, out.AccessToken)
	return account{id: me.id, email: me.email, role: me.role, token: out.AccessToken}
}

func (env *TestEnv) me(t *testing.T, token string) account {
	t.Helper()
	status, body, err := env.doJSON(http.MethodGet, "/api/auth/me", nil, map[string]string{
		"Authorization": "Bearer " + token,
	})
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, status, "me: %s", body)

	var out struct {
		User struct {
			ID    string `json:"id"`
			Email string `json:"email"`
			Role  string `json:"role"`
		} `json:"user"`
	}
	require.NoError(t, decodeData(body, &out), "me: %s", body)
	return account{id: out.User.ID, email: out.User.Email, role: out.User.Role, token: token}
}

func uniqueEmail(prefix string) string {
	return fmt.Sprintf("%s_%d@test.com", prefix, time.Now().UnixNano())
}

// ── Event fixtures ──

type ticketType struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Available   int    `json:"available"`
	PriceRupiah int    `json:"price_rupiah"`
}

type eventDetail struct {
	Event struct {
		ID     string `json:"id"`
		Title  string `json:"title"`
		Status string `json:"status"`
	} `json:"event"`
	TicketTypes []ticketType `json:"ticket_types"`
}

// createEvent creates a pending event as the given organizer.
func (env *TestEnv) createEvent(t *testing.T, eo account, capacity int) eventDetail {
	t.Helper()
	status, body, err := env.doJSON(http.MethodPost, "/api/events", map[string]interface{}{
		"title":          "E2E Event",
		"description":    "end-to-end test event",
		"venue_name":     "Test Stadium",
		"venue_address":  "123 Test St",
		"venue_capacity": capacity,
		"start_at":       time.Now().Add(30 * 24 * time.Hour).Format(time.RFC3339),
		"end_at":         time.Now().Add(33 * 24 * time.Hour).Format(time.RFC3339),
		"ticket_types": []map[string]interface{}{
			{"name": "VIP", "price_rupiah": 10000, "quantity": 20, "max_per_order": 5},
			{"name": "GA", "price_rupiah": 5000, "quantity": 80, "max_per_order": 10},
		},
	}, bearer(eo))
	require.NoError(t, err)
	require.Equal(t, http.StatusCreated, status, "create event: %s", body)

	var ev struct {
		ID     string `json:"id"`
		Status string `json:"status"`
	}
	require.NoError(t, decodeData(body, &ev), "create event: %s", body)
	require.Equal(t, "pending", ev.Status)

	return eventDetail{Event: struct {
		ID     string `json:"id"`
		Title  string `json:"title"`
		Status string `json:"status"`
	}{ID: ev.ID, Status: ev.Status}}
}

// approveEvent approves an event as the admin, exercising role propagation
// through forward-auth.
func (env *TestEnv) approveEvent(t *testing.T, eventID string, admin account) {
	t.Helper()
	status, body, err := env.doJSON(http.MethodPost, "/api/events/"+eventID+"/approve", nil, bearer(admin))
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, status, "approve: %s", body)
}

// fetchEvent reads an event with its ticket types and live availability.
func (env *TestEnv) fetchEvent(t *testing.T, eventID string) eventDetail {
	t.Helper()
	status, body, err := env.doJSON(http.MethodGet, "/api/events/"+eventID, nil, nil)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, status, "get event: %s", body)

	var detail eventDetail
	require.NoError(t, decodeData(body, &detail), "get event: %s", body)
	return detail
}

// approvedEvent creates and approves an event, then waits until its seats are
// initialised. Returns the event id and its ticket types.
func (env *TestEnv) approvedEvent(t *testing.T, admin account) (string, []ticketType) {
	t.Helper()
	ev := env.createEvent(t, env.registerEO(t), 100)
	env.approveEvent(t, ev.Event.ID, admin)

	var detail eventDetail
	pollFor(t, 30*time.Second, 500*time.Millisecond, func() bool {
		detail = env.fetchEvent(t, ev.Event.ID)
		return len(detail.TicketTypes) > 0 && detail.TicketTypes[0].Available > 0
	}, "seat init after approval")

	require.NotEmpty(t, detail.TicketTypes)
	return ev.Event.ID, detail.TicketTypes
}

// availableSeats reads the live available count for one ticket type.
func (env *TestEnv) availableSeats(t *testing.T, eventID, ttID string) int {
	t.Helper()
	for _, tt := range env.fetchEvent(t, eventID).TicketTypes {
		if tt.ID == ttID {
			return tt.Available
		}
	}
	t.Fatalf("ticket type %s not found on event %s", ttID, eventID)
	return 0
}

// ── Booking ──

type booking struct {
	ID     string `json:"id"`
	Status string `json:"status"`
	Items  []struct {
		TicketTypeID string `json:"ticket_type_id"`
		Quantity     int    `json:"quantity"`
	} `json:"items"`
}

// reserve creates a pending reservation and returns the booking id.
func (env *TestEnv) reserve(t *testing.T, cust account, eventID, ttID string, qty, unitPrice int) string {
	t.Helper()
	status, body, err := env.doJSON(http.MethodPost, "/api/bookings/reserve", map[string]interface{}{
		"event_id": eventID,
		"items": []map[string]interface{}{
			{"ticket_type_id": ttID, "quantity": qty, "unit_price_rupiah": unitPrice},
		},
	}, bearer(cust))
	require.NoError(t, err)
	require.Equal(t, http.StatusCreated, status, "reserve: %s", body)

	var out struct {
		BookingID string `json:"booking_id"`
		Status    string `json:"status"`
	}
	require.NoError(t, decodeData(body, &out), "reserve: %s", body)
	require.NotEmpty(t, out.BookingID)
	require.Equal(t, "pending", out.Status)
	return out.BookingID
}

// getBooking reads a booking the caller owns.
func (env *TestEnv) getBooking(t *testing.T, cust account, bookingID string) booking {
	t.Helper()
	status, body, err := env.doJSON(http.MethodGet, "/api/bookings/"+bookingID, nil, bearer(cust))
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, status, "get booking: %s", body)

	var b booking
	require.NoError(t, decodeData(body, &b), "get booking: %s", body)
	return b
}

// waitBookingStatus polls until the booking reaches the wanted status. The
// gateway is asynchronous here: payment writes the outbox row, an outbox worker
// publishes it to Kafka, and the ticketing consumer applies it.
func (env *TestEnv) waitBookingStatus(t *testing.T, cust account, bookingID, want string, timeout time.Duration) booking {
	t.Helper()
	var last string
	pollFor(t, timeout, 500*time.Millisecond, func() bool {
		b := env.getBooking(t, cust, bookingID)
		last = b.Status
		return last == want
	}, "booking "+bookingID+" to become "+want+" (last saw "+last+")")
	return env.getBooking(t, cust, bookingID)
}

// ── Payment automation ──
//
// This is the whole point of the e2e suite: drive a payment to completion with
// no browser and no database access, then assert the webhook moved the
// transaction and the booking.
//
// The customer-facing initiate call goes through the gateway, so it also proves
// the payments router and forward-auth work for that path.
//
// payBooking returns the gateway's payment_session_id. With PROVIDER=mock it is
// deterministic — "ps-mock-" + booking id — but it is parsed from the response so
// the helpers keep working if the provider is swapped for Xendit.
func (env *TestEnv) payBooking(t *testing.T, cust account, bookingID string) string {
	t.Helper()
	status, body, err := env.doJSON(
		http.MethodPost, "/api/payments/booking/"+bookingID, nil, bearer(cust))
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, status, "initiate payment: %s", body)

	var out struct {
		TransactionID    string `json:"transaction_id"`
		PaymentSessionID string `json:"payment_session_id"`
		PaymentLinkURL   string `json:"payment_link_url"`
		Status           string `json:"status"`
	}
	require.NoError(t, decodeData(body, &out), "initiate payment: %s", body)
	require.NotEmpty(t, out.PaymentSessionID, "initiate payment: %s", body)
	require.NotEmpty(t, out.PaymentLinkURL, "initiate payment: %s", body)

	// The initiate response reports "initiated", not "pending", even though the
	// row has just been transitioned. ProcessPayment holds the transaction it
	// loaded before calling TransitionIfInitiated, and the repo methods mutate
	// only the database, so the handler serialises the pre-transition snapshot.
	// GET /api/payments/booking/{id} re-reads the row and does report "pending".
	require.Equal(t, "initiated", out.Status,
		"initiate echoes the pre-transition status; the GET endpoint reports pending")
	return out.PaymentSessionID
}

// paymentStatus reads the transaction state for a booking through the gateway.
func (env *TestEnv) paymentStatus(t *testing.T, cust account, bookingID string) string {
	t.Helper()
	status, body, err := env.doJSON(
		http.MethodGet, "/api/payments/booking/"+bookingID, nil, bearer(cust))
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, status, "payment status: %s", body)

	var out struct {
		Status string `json:"status"`
	}
	require.NoError(t, decodeData(body, &out), "payment status: %s", body)
	return out.Status
}

// waitPaymentStatus polls until the transaction reaches the wanted status.
func (env *TestEnv) waitPaymentStatus(t *testing.T, cust account, bookingID, want string, timeout time.Duration) {
	t.Helper()
	var last string
	pollFor(t, timeout, 500*time.Millisecond, func() bool {
		last = env.paymentStatus(t, cust, bookingID)
		return last == want
	}, "transaction "+bookingID+" to become "+want+" (last saw "+last+")")
}

// deliverWebhook plays the gateway: POST a session webhook to the public webhook
// route. The handler matches on data.reference_id (the booking id); the session
// id is only checked when present, and a mismatch is acknowledged with 200 and
// no state change — so it is sent here to match what the service stored.
func (env *TestEnv) deliverWebhook(t *testing.T, event, bookingID, sessionID string) int {
	t.Helper()
	status, body, err := env.doJSON(http.MethodPost, "/api/payments/webhook", map[string]interface{}{
		"event": event,
		"data": map[string]string{
			"reference_id":       bookingID,
			"payment_session_id": sessionID,
			"status":             webhookStatus(event),
		},
	}, map[string]string{"x-callback-token": callbackToken})
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, status, "deliver %s webhook: %s", event, body)
	return status
}

func webhookStatus(event string) string {
	if event == "payment_session.completed" {
		return "COMPLETED"
	}
	return "EXPIRED"
}

// settle pays for a booking: creates the gateway session, then delivers the
// completed webhook. Returns the session id for assertions.
func (env *TestEnv) settle(t *testing.T, cust account, bookingID string) string {
	t.Helper()
	sessionID := env.payBooking(t, cust, bookingID)
	env.deliverWebhook(t, "payment_session.completed", bookingID, sessionID)
	return sessionID
}

// settleBooking is settle plus the wait for the Kafka round trip to settle the
// transaction and confirm the booking. This is the assertion chain the whole
// suite exists to verify.
func (env *TestEnv) settleBooking(t *testing.T, cust account, bookingID string) {
	t.Helper()
	sessionID := env.settle(t, cust, bookingID)

	env.waitPaymentStatus(t, cust, bookingID, "completed", 30*time.Second)
	env.waitBookingStatus(t, cust, bookingID, "confirmed", 60*time.Second)

	// A duplicate delivery must not double-settle.
	env.deliverWebhook(t, "payment_session.completed", bookingID, sessionID)
	require.Equal(t, "completed", env.paymentStatus(t, cust, bookingID),
		"duplicate webhook must not change the transaction")
}

// ── Cancel ──

// cancelEvent cancels an event as its owning organizer.
func (env *TestEnv) cancelEvent(t *testing.T, eo account, eventID string) {
	t.Helper()
	status, body, err := env.doJSON(http.MethodPost, "/api/events/"+eventID+"/cancel", nil, bearer(eo))
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, status, "cancel event: %s", body)
}

// ── Real gateway payloads ──

// fixtureWebhook loads a real gateway payload captured from a sandbox and
// substitutes the runtime booking and session ids.
func fixtureWebhook(t *testing.T, name, bookingID, sessionID string) string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("testdata", name))
	require.NoError(t, err, "read testdata/%s", name)
	body := strings.ReplaceAll(string(raw), "{{bookingID}}", bookingID)
	return strings.ReplaceAll(body, "{{sessionID}}", sessionID)
}

// postRawWebhook delivers a verbatim payload to the webhook route.
func (env *TestEnv) postRawWebhook(t *testing.T, payload string) int {
	t.Helper()
	req, err := http.NewRequest(http.MethodPost, env.url("/api/payments/webhook"), strings.NewReader(payload))
	require.NoError(t, err)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-callback-token", callbackToken)

	resp, err := env.client.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	require.Equal(t, http.StatusOK, resp.StatusCode, "raw webhook: %s", b)
	return resp.StatusCode
}
