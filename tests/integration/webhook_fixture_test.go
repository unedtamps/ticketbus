//go:build integration

package integration

import (
	"context"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fixtureWebhook loads a real Xendit webhook payload (captured from the
// sandbox) and substitutes the runtime booking/session ids.
func fixtureWebhook(t *testing.T, name, bookingID, sessionID string) string {
	t.Helper()
	raw, err := os.ReadFile("testdata/" + name)
	require.NoError(t, err)
	body := strings.ReplaceAll(string(raw), "{{bookingID}}", bookingID)
	return strings.ReplaceAll(body, "{{sessionID}}", sessionID)
}

// postRawWebhook posts a raw JSON payload to the payment webhook endpoint.
func postRawWebhook(t *testing.T, env *TestEnv, body string) {
	t.Helper()
	req, err := http.NewRequest(
		http.MethodPost,
		env.payURL+"/api/payments/webhook/xendit",
		strings.NewReader(body),
	)
	require.NoError(t, err)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	require.Equal(t, 200, resp.StatusCode, "webhook: %s", string(b))
}

// reserveAndInitiate reserves tickets and creates the payment session,
// returning the booking id and the gateway session id.
func reserveAndInitiate(t *testing.T, env *TestEnv, eventID, ttID string, qty int, ch map[string]string) (bookingID, sessionID string) {
	t.Helper()
	_, body, err := doJSON(http.MethodPost, env.invURL+"/api/bookings/reserve", map[string]interface{}{
		"event_id": eventID,
		"items":    []map[string]interface{}{{"ticket_type_id": ttID, "quantity": qty, "unit_price_rupiah": 10000}},
	}, ch)
	require.NoError(t, err)
	var rr reserveResp
	require.NoError(t, jsonData(body, &rr))
	bookingID = rr.Data.BookingID
	require.NotEmpty(t, bookingID)
	sessionID, _ = initiatePayment(t, env, bookingID, ch)
	require.NotEmpty(t, sessionID)
	return bookingID, sessionID
}

// Test_FixtureWebhook_CompletedRealPayload verifies the handler accepts the
// real Xendit payment_session.completed payload (session id in data.id) and
// settles the whole flow end-to-end.
func Test_FixtureWebhook_CompletedRealPayload(t *testing.T) {
	env := getTestEnv()
	eventID, ttIDs := setupApprovedEvent(t, env)
	cust := env.registerAndLogin("customer")
	ch := env.authHeadersWith(cust.AccessToken)
	ctx := context.Background()

	bookingID, sessionID := reserveAndInitiate(t, env, eventID, ttIDs[0], 1, ch)
	postRawWebhook(t, env, fixtureWebhook(t, "xendit_payment_session_completed.json", bookingID, sessionID))

	// Transaction completed + outbox published exactly once
	pollFor(t, 30*time.Second, 500*time.Millisecond, func() bool {
		var status string
		if err := env.payPool.QueryRow(ctx, `SELECT status FROM transactions WHERE booking_id = $1`, bookingID).Scan(&status); err != nil {
			return false
		}
		return status == "completed"
	}, "txn completed via real payload webhook")

	var outboxCount int
	require.NoError(t, env.payPool.QueryRow(
		ctx, `SELECT COUNT(*) FROM outbox WHERE topic = 'payment.completed' AND key = (SELECT id FROM transactions WHERE booking_id = $1)`, bookingID).Scan(&outboxCount))
	assert.Equal(t, 1, outboxCount, "payment.completed published exactly once")

	// Booking confirmed via payment.completed → ticketing consumer
	pollFor(t, 30*time.Second, 500*time.Millisecond, func() bool {
		var status string
		if err := env.invPool.QueryRow(ctx, `SELECT status FROM bookings WHERE id = $1`, bookingID).Scan(&status); err != nil {
			return false
		}
		return status == "confirmed"
	}, "booking confirmed via real payload webhook")

	// Duplicate delivery is idempotent
	postRawWebhook(t, env, fixtureWebhook(t, "xendit_payment_session_completed.json", bookingID, sessionID))
	require.NoError(t, env.payPool.QueryRow(
		ctx, `SELECT COUNT(*) FROM outbox WHERE topic = 'payment.completed' AND key = (SELECT id FROM transactions WHERE booking_id = $1)`, bookingID).Scan(&outboxCount))
	assert.Equal(t, 1, outboxCount, "duplicate webhook must not republish")
}

// Test_FixtureWebhook_ExpiredRealPayload verifies the real
// payment_session.expired payload expires the transaction, releases seats,
// and idempotently publishes payment.expired.
func Test_FixtureWebhook_ExpiredRealPayload(t *testing.T) {
	env := getTestEnv()
	eventID, ttIDs := setupApprovedEvent(t, env)
	cust := env.registerAndLogin("customer")
	ch := env.authHeadersWith(cust.AccessToken)
	ctx := context.Background()

	bookingID, sessionID := reserveAndInitiate(t, env, eventID, ttIDs[0], 2, ch)

	var availBefore int
	require.NoError(t, env.invPool.QueryRow(ctx, `SELECT available_seat FROM ticket_types WHERE id = $1`, ttIDs[0]).Scan(&availBefore))

	postRawWebhook(t, env, fixtureWebhook(t, "xendit_payment_session_expired.json", bookingID, sessionID))
	postRawWebhook(t, env, fixtureWebhook(t, "xendit_payment_session_expired.json", bookingID, sessionID))

	pollFor(t, 30*time.Second, 500*time.Millisecond, func() bool {
		var status string
		if err := env.payPool.QueryRow(ctx, `SELECT status FROM transactions WHERE booking_id = $1`, bookingID).Scan(&status); err != nil {
			return false
		}
		return status == "expired"
	}, "txn expired via real payload webhook")

	var outboxCount int
	require.NoError(t, env.payPool.QueryRow(
		ctx, `SELECT COUNT(*) FROM outbox WHERE topic = 'payment.expired' AND key = (SELECT id FROM transactions WHERE booking_id = $1)`, bookingID).Scan(&outboxCount))
	assert.Equal(t, 1, outboxCount, "payment.expired published exactly once")

	pollFor(t, 30*time.Second, 500*time.Millisecond, func() bool {
		var status string
		if err := env.invPool.QueryRow(ctx, `SELECT status FROM bookings WHERE id = $1`, bookingID).Scan(&status); err != nil {
			return false
		}
		return status == "expired"
	}, "booking expired via real payload webhook")

	var availAfter int
	require.NoError(t, env.invPool.QueryRow(ctx, `SELECT available_seat FROM ticket_types WHERE id = $1`, ttIDs[0]).Scan(&availAfter))
	assert.Equal(t, availBefore+2, availAfter, "seats released after expiry")
}
