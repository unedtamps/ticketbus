//go:build integration

package integration

// helpers.go — HTTP helpers, identity synthesis, transaction seeding, and the
// Kafka producer used to stand in for events the ticketing service would send.

import (
	"context"
	"encoding/json"

	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
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

// customer is a synthetic customer. The payment handler runs behind
// sharedhttp.WithUserContext, which trusts the X-Authenticated-* headers the
// API gateway sets, so no auth service call or signed JWT is needed.
//
// The id is a bare UUID because transactions.user_id is a UUID column.
type customer struct {
	id      string
	email   string
	headers map[string]string
}

func newCustomer() customer {
	id := uuid.NewString()
	return customer{
		id:    id,
		email: id + "@test.local",
		headers: map[string]string{
			"X-Authenticated-User-ID":    id,
			"X-Authenticated-User-Role":  string(dto.RoleCustomer),
			"X-Authenticated-User-Email": id + "@test.local",
		},
	}
}

// ── Transaction seeding ──

// seedTxn creates the transaction row the way the ticketing service does during
// reserve, by calling the internal endpoint with X-Internal-Key. The payment
// service never checks that the booking exists, so a synthetic id is enough.
func (env *TestEnv) seedTxn(t *testing.T, c customer, bookingID, eventID string, amountRupiah int, ttl time.Duration) {
	t.Helper()
	resp, body, err := doJSON(http.MethodPost, env.payURL+"/api/payments/internal", map[string]interface{}{
		"booking_id":    bookingID,
		"event_id":      eventID,
		"user_id":       c.id,
		"email":         c.email,
		"amount_rupiah": amountRupiah,
		"expires_at":    time.Now().Add(ttl).UTC().Format(time.RFC3339),
	}, map[string]string{"X-Internal-Key": internalKey})
	require.NoError(t, err)
	// 201 when the transaction is created, 200 when it already existed.
	require.Contains(t, []int{http.StatusOK, http.StatusCreated}, resp.StatusCode, "seed txn: %s", body)
}

// initiate creates the gateway payment session for a seeded transaction,
// returning the session id and the checkout link.
func (env *TestEnv) initiate(t *testing.T, c customer, bookingID string) (sessionID, link string) {
	t.Helper()
	resp, body, err := doJSON(http.MethodPost, env.payURL+"/api/payments/booking/"+bookingID, nil, c.headers)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, resp.StatusCode, "initiate: %s", body)

	var ir struct {
		Data struct {
			PaymentSessionID string `json:"payment_session_id"`
			PaymentLinkURL   string `json:"payment_link_url"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(body, &ir))
	require.NotEmpty(t, ir.Data.PaymentSessionID, "initiate body: %s", body)
	return ir.Data.PaymentSessionID, ir.Data.PaymentLinkURL
}

// seedTxnAndInitiate seeds a transaction and then creates the gateway session,
// returning the booking id and the gateway session id.
func (env *TestEnv) seedTxnAndInitiate(t *testing.T, c customer, amountRupiah int) (bookingID, sessionID string) {
	t.Helper()
	bookingID = newBookingID()
	eventID := newEventID()
	env.seedTxn(t, c, bookingID, eventID, amountRupiah, time.Hour)
	sessionID, _ = env.initiate(t, c, bookingID)
	return bookingID, sessionID
}

// Booking, event, and user ids are bare UUIDs: transactions.booking_id and
// transactions.event_id are UUID columns.
func newBookingID() string { return uuid.NewString() }

func newEventID() string { return uuid.NewString() }

// ── Webhooks ──

// postCompletedWebhook simulates the gateway reporting a completed session. The
// provider ref is read from the stored transaction so it always matches the
// mock processor's session.
func (env *TestEnv) postCompletedWebhook(t *testing.T, bookingID string) {
	t.Helper()
	var providerRef string
	require.NoError(t, env.payPool.QueryRow(
		context.Background(), `SELECT provider_ref FROM transactions WHERE booking_id = $1`, bookingID).Scan(&providerRef))
	require.NotEmpty(t, providerRef, "transaction has no provider_ref (initiate first)")

	payload := map[string]interface{}{
		"event": "payment_session.completed",
		"data": map[string]string{
			"reference_id":       bookingID,
			"payment_session_id": providerRef,
			"status":             "COMPLETED",
		},
	}
	resp, body, err := doJSON(http.MethodPost, env.payURL+"/api/payments/webhook", payload, map[string]string{
		"x-callback-token": webhookToken,
	})
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, resp.StatusCode, "webhook: %s", body)
}

// postExpiredWebhook simulates the gateway reporting an expired session.
func postExpiredWebhook(t *testing.T, payURL, bookingID, sessionID string) {
	t.Helper()
	payload := map[string]interface{}{
		"event": "payment_session.expired",
		"data": map[string]string{
			"reference_id":       bookingID,
			"payment_session_id": sessionID,
			"status":             "EXPIRED",
		},
	}
	resp, body, err := doJSON(http.MethodPost, payURL+"/api/payments/webhook", payload, map[string]string{
		"x-callback-token": webhookToken,
	})
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, resp.StatusCode, "webhook: %s", body)
}

// postRawWebhook posts a raw JSON body to the webhook endpoint and requires 200.
func postRawWebhook(t *testing.T, payURL, body string) {
	t.Helper()
	req, err := http.NewRequest(http.MethodPost, payURL+"/api/payments/webhook", strings.NewReader(body))
	require.NoError(t, err)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-callback-token", webhookToken)
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	require.Equal(t, http.StatusOK, resp.StatusCode, "webhook: %s", b)
}

// fixtureWebhook loads a real Xendit webhook payload captured from the sandbox
// and substitutes the runtime booking and session ids.
func fixtureWebhook(t *testing.T, name, bookingID, sessionID string) string {
	t.Helper()
	raw, err := readTestdata(name)
	require.NoError(t, err)
	body := strings.ReplaceAll(string(raw), "{{bookingID}}", bookingID)
	return strings.ReplaceAll(body, "{{sessionID}}", sessionID)
}

// postMockSession calls the dev-only mock webhook simulator route.
func postMockSession(t *testing.T, payURL, bookingID, status string) int {
	t.Helper()
	resp, _, err := doJSON(
		http.MethodPost,
		payURL+"/api/payments/booking/mock/"+bookingID+"/"+status,
		nil,
		nil,
	)
	require.NoError(t, err)
	return resp.StatusCode
}

// ── Upstream events ──

// emitEventCancelled publishes event.cancelled the way the ticketing service
// does through its outbox, so the payment consumer reacts without ticketing
// running.
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

func (env *TestEnv) txnStatusByBooking(t *testing.T, bookingID string) string {
	t.Helper()
	var status string
	require.NoError(t, env.payPool.QueryRow(
		context.Background(), `SELECT status FROM transactions WHERE booking_id = $1`, bookingID).Scan(&status))
	return status
}

func (env *TestEnv) txnIDByBooking(t *testing.T, bookingID string) string {
	t.Helper()
	var id string
	require.NoError(t, env.payPool.QueryRow(
		context.Background(), `SELECT id FROM transactions WHERE booking_id = $1`, bookingID).Scan(&id))
	return id
}

func (env *TestEnv) refundStatus(t *testing.T, txnID string) string {
	t.Helper()
	var s string
	require.NoError(t, env.payPool.QueryRow(
		context.Background(), `SELECT refund_status FROM transactions WHERE id = $1`, txnID).Scan(&s))
	return s
}

func (env *TestEnv) pendingRefundCount(t *testing.T, txnID, reason string) int {
	t.Helper()
	var n int
	require.NoError(t, env.payPool.QueryRow(
		context.Background(),
		`SELECT COUNT(*) FROM refund_requests WHERE transaction_id = $1 AND status = 'pending' AND reason = $2`,
		txnID, reason).Scan(&n))
	return n
}

// outboxCountForBooking counts published events for a booking's transaction.
func (env *TestEnv) outboxCountForBooking(t *testing.T, topic, bookingID string) int {
	t.Helper()
	var n int
	require.NoError(t, env.payPool.QueryRow(
		context.Background(),
		`SELECT COUNT(*) FROM outbox WHERE topic = $1 AND key = (SELECT id::text FROM transactions WHERE booking_id = $2)`,
		topic, bookingID).Scan(&n))
	return n
}

func readTestdata(name string) ([]byte, error) {
	return os.ReadFile(filepath.Join("testdata", name))
}
