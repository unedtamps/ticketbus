//go:build integration

package integration

// webhook_test.go — TestWebhook_*: gateway webhook delivery with real Xendit payloads.

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

// TestWebhook_Completed_RealPayload verifies the handler accepts the real
// Xendit payment_session.completed payload (session id in data.id), settles the
// transaction, and publishes payment.completed exactly once — a duplicate
// delivery must not republish.
func TestWebhook_Completed_RealPayload(t *testing.T) {
	env := getTestEnv()
	c := newCustomer()
	ctx := context.Background()

	bookingID, sessionID := env.seedTxnAndInitiate(t, c, defaultAmount)
	postRawWebhook(t, env.payURL, fixtureWebhook(t, "xendit_payment_session_completed.json", bookingID, sessionID))

	pollFor(t, 30*time.Second, 500*time.Millisecond, func() bool {
		var status string
		if err := env.payPool.QueryRow(ctx,
			`SELECT status FROM transactions WHERE booking_id = $1`, bookingID).Scan(&status); err != nil {
			return false
		}
		return status == "completed"
	}, "txn completed via real payload webhook")

	assert.Equal(t, 1, env.outboxCountForBooking(t, "payment.completed", bookingID),
		"payment.completed published exactly once")

	// Duplicate delivery is idempotent.
	postRawWebhook(t, env.payURL, fixtureWebhook(t, "xendit_payment_session_completed.json", bookingID, sessionID))
	assert.Equal(t, 1, env.outboxCountForBooking(t, "payment.completed", bookingID),
		"duplicate webhook must not republish")
}

// TestWebhook_Expired_RealPayload verifies the real payment_session.expired
// payload expires the transaction and publishes payment.expired exactly once.
func TestWebhook_Expired_RealPayload(t *testing.T) {
	env := getTestEnv()
	c := newCustomer()
	ctx := context.Background()

	bookingID, sessionID := env.seedTxnAndInitiate(t, c, defaultAmount)

	postRawWebhook(t, env.payURL, fixtureWebhook(t, "xendit_payment_session_expired.json", bookingID, sessionID))
	postRawWebhook(t, env.payURL, fixtureWebhook(t, "xendit_payment_session_expired.json", bookingID, sessionID))

	pollFor(t, 30*time.Second, 500*time.Millisecond, func() bool {
		var status string
		if err := env.payPool.QueryRow(ctx,
			`SELECT status FROM transactions WHERE booking_id = $1`, bookingID).Scan(&status); err != nil {
			return false
		}
		return status == "expired"
	}, "txn expired via real payload webhook")

	assert.Equal(t, 1, env.outboxCountForBooking(t, "payment.expired", bookingID),
		"payment.expired published exactly once")
}
