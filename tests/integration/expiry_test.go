//go:build integration

package integration

// expiry_test.go — TestExpiry_*: gateway expiry webhook + internal expiry sweeper.

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestExpiry_WebhookExpiresBooking verifies that a payment_session.expired
// webhook from the gateway expires the transaction, publishes payment.expired,
// and the ticketing service releases the booking and its seats.
func TestExpiry_WebhookExpiresBooking(t *testing.T) {
	env := getTestEnv()
	eventID, ttIDs := setupApprovedEvent(t, env)

	cust := env.registerAndLogin("customer")
	ch := env.authHeadersWith(cust.AccessToken)
	ctx := context.Background()

	// Reserve + initiate (session exists)
	_, body, err := doJSON(http.MethodPost, env.invURL+"/api/bookings/reserve", map[string]interface{}{
		"event_id": eventID,
		"items":    []map[string]interface{}{{"ticket_type_id": ttIDs[0], "quantity": 2, "unit_price_rupiah": ticketPrice(t, env, ttIDs[0])}},
	}, ch)
	require.NoError(t, err)
	var rr reserveResp
	require.NoError(t, json.Unmarshal(body, &rr))
	bookingID := rr.Data.BookingID
	require.NotEmpty(t, bookingID, "reserve body: %s", string(body))
	sessionID, _ := initiatePayment(t, env, bookingID, ch)

	var availBefore int
	require.NoError(t, env.invPool.QueryRow(ctx, `SELECT available_seat FROM ticket_types WHERE id = $1`, ttIDs[0]).Scan(&availBefore))

	// Gateway sends the expiry webhook
	payload := map[string]interface{}{
		"event": "payment_session.expired",
		"data": map[string]string{
			"reference_id":       bookingID,
			"payment_session_id": sessionID,
			"status":             "EXPIRED",
		},
	}
	resp, b, err := doJSON(http.MethodPost, env.payURL+"/api/payments/webhook", payload, map[string]string{
		"x-callback-token": "test-webhook-token",
	})
	require.NoError(t, err)
	require.Equal(t, 200, resp.StatusCode, "webhook: %s", string(b))

	// Booking becomes expired via payment.expired → ticketing consumer
	pollFor(t, 30*time.Second, 500*time.Millisecond, func() bool {
		var status string
		if err := env.invPool.QueryRow(ctx, `SELECT status FROM bookings WHERE id = $1`, bookingID).Scan(&status); err != nil {
			return false
		}
		return status == "expired"
	}, "booking expiry via payment_session.expired webhook")

	// Transaction is expired
	var txnStatus string
	require.NoError(t, env.payPool.QueryRow(ctx, `SELECT status FROM transactions WHERE booking_id = $1`, bookingID).Scan(&txnStatus))
	assert.Equal(t, "expired", txnStatus)

	// Seats released
	var availAfter int
	require.NoError(t, env.invPool.QueryRow(ctx, `SELECT available_seat FROM ticket_types WHERE id = $1`, ttIDs[0]).Scan(&availAfter))
	assert.Equal(t, availBefore+2, availAfter, "seats should be released after expiry webhook")
}

// TestExpiry_DuplicateWebhook_Idempotent verifies the second delivery of the
// same expiry webhook does not double-publish or corrupt state.
func TestExpiry_DuplicateWebhook_Idempotent(t *testing.T) {
	env := getTestEnv()
	eventID, ttIDs := setupApprovedEvent(t, env)

	cust := env.registerAndLogin("customer")
	ch := env.authHeadersWith(cust.AccessToken)
	ctx := context.Background()

	_, body, _ := doJSON(http.MethodPost, env.invURL+"/api/bookings/reserve", map[string]interface{}{
		"event_id": eventID,
		"items":    []map[string]interface{}{{"ticket_type_id": ttIDs[0], "quantity": 1, "unit_price_rupiah": ticketPrice(t, env, ttIDs[0])}},
	}, ch)
	var rr reserveResp
	require.NoError(t, json.Unmarshal(body, &rr))
	bookingID := rr.Data.BookingID
	require.NotEmpty(t, bookingID)
	sessionID, _ := initiatePayment(t, env, bookingID, ch)

	payload := map[string]interface{}{
		"event": "payment_session.expired",
		"data": map[string]string{
			"reference_id":       bookingID,
			"payment_session_id": sessionID,
			"status":             "EXPIRED",
		},
	}
	for i := 0; i < 2; i++ {
		resp, b, err := doJSON(http.MethodPost, env.payURL+"/api/payments/webhook", payload, map[string]string{
			"x-callback-token": "test-webhook-token",
		})
		require.NoError(t, err)
		require.Equal(t, 200, resp.StatusCode, "webhook %d: %s", i, string(b))
	}

	pollFor(t, 30*time.Second, 500*time.Millisecond, func() bool {
		var status string
		if err := env.invPool.QueryRow(ctx, `SELECT status FROM bookings WHERE id = $1`, bookingID).Scan(&status); err != nil {
			return false
		}
		return status == "expired"
	}, "booking expiry")

	// Only one payment.expired outbox event should have been published.
	var outboxCount int
	require.NoError(t, env.payPool.QueryRow(
		ctx, `SELECT COUNT(*) FROM outbox WHERE topic = 'payment.expired' AND key = (SELECT id::text FROM transactions WHERE booking_id = $1)`, bookingID).Scan(&outboxCount))
	assert.Equal(t, 1, outboxCount, "payment.expired should be published exactly once")
}

// TestExpiry_Sweeper_ExpiresUnpaidSession verifies the expiry poller handles a
// transaction whose session deadline passed without any webhook (mock route
// never hit). The deadline is backdated so the poller (1s tick) picks it up.
func TestExpiry_Sweeper_ExpiresUnpaidSession(t *testing.T) {
	env := getTestEnv()
	eventID, ttIDs := setupApprovedEvent(t, env)
	cust := env.registerAndLogin("customer")
	ch := env.authHeadersWith(cust.AccessToken)
	ctx := context.Background()

	bookingID, _ := reserveAndInitiate(t, env, eventID, ttIDs[0], 2, ch)

	var availBefore int
	require.NoError(t, env.invPool.QueryRow(ctx, `SELECT available_seat FROM ticket_types WHERE id = $1`, ttIDs[0]).Scan(&availBefore))

	// No webhook arrives — force the session past its deadline.
	_, err := env.payPool.Exec(ctx,
		`UPDATE transactions SET expires_at = now() - interval '1 minute' WHERE booking_id = $1`,
		bookingID)
	require.NoError(t, err)

	pollFor(t, 30*time.Second, 500*time.Millisecond, func() bool {
		var status string
		if err := env.payPool.QueryRow(ctx, `SELECT status FROM transactions WHERE booking_id = $1`, bookingID).Scan(&status); err != nil {
			return false
		}
		return status == "expired"
	}, "txn expired via sweeper")

	var outboxCount int
	require.NoError(t, env.payPool.QueryRow(
		ctx, `SELECT COUNT(*) FROM outbox WHERE topic = 'payment.expired' AND key = (SELECT id::text FROM transactions WHERE booking_id = $1)`, bookingID).Scan(&outboxCount))
	assert.Equal(t, 1, outboxCount, "payment.expired published exactly once by sweeper")

	pollFor(t, 30*time.Second, 500*time.Millisecond, func() bool {
		var status string
		if err := env.invPool.QueryRow(ctx, `SELECT status FROM bookings WHERE id = $1`, bookingID).Scan(&status); err != nil {
			return false
		}
		return status == "expired"
	}, "booking expired via sweeper")

	var availAfter int
	require.NoError(t, env.invPool.QueryRow(ctx, `SELECT available_seat FROM ticket_types WHERE id = $1`, ttIDs[0]).Scan(&availAfter))
	assert.Equal(t, availBefore+2, availAfter, "seats released by sweeper")
}
