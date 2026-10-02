//go:build integration

package integration

// expiry_test.go — TestExpiry_*: gateway expiry webhook + internal expiry sweeper.

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

// TestExpiry_Webhook_ExpiresTransaction verifies a payment_session.expired
// webhook expires the transaction and publishes payment.expired.
func TestExpiry_Webhook_ExpiresTransaction(t *testing.T) {
	env := getTestEnv()
	c := newCustomer()

	bookingID, sessionID := env.seedTxnAndInitiate(t, c, defaultAmount)
	postExpiredWebhook(t, env.payURL, bookingID, sessionID)

	pollFor(t, 30*time.Second, 500*time.Millisecond, func() bool {
		return env.txnStatusByBooking(t, bookingID) == "expired"
	}, "txn expired via payment_session.expired webhook")

	assert.Equal(t, 1, env.outboxCountForBooking(t, "payment.expired", bookingID),
		"payment.expired published exactly once")
}

// TestExpiry_DuplicateWebhook_Idempotent verifies a second delivery of the same
// expiry webhook does not double-publish.
func TestExpiry_DuplicateWebhook_Idempotent(t *testing.T) {
	env := getTestEnv()
	c := newCustomer()

	bookingID, sessionID := env.seedTxnAndInitiate(t, c, defaultAmount)
	for i := 0; i < 2; i++ {
		postExpiredWebhook(t, env.payURL, bookingID, sessionID)
	}

	pollFor(t, 30*time.Second, 500*time.Millisecond, func() bool {
		return env.txnStatusByBooking(t, bookingID) == "expired"
	}, "txn expired")

	assert.Equal(t, 1, env.outboxCountForBooking(t, "payment.expired", bookingID),
		"payment.expired should be published exactly once")
}

// TestExpiry_Sweeper_ExpiresUnpaidSession verifies the expiry poller handles a
// transaction whose deadline passed without any webhook. The deadline is
// backdated so the 1s-tick poller picks it up immediately.
func TestExpiry_Sweeper_ExpiresUnpaidSession(t *testing.T) {
	env := getTestEnv()
	c := newCustomer()

	bookingID, _ := env.seedTxnAndInitiate(t, c, defaultAmount)

	// No webhook arrives — force the session past its deadline.
	_, err := env.payPool.Exec(context.Background(),
		`UPDATE transactions SET expires_at = now() - interval '1 minute' WHERE booking_id = $1`, bookingID)
	if err != nil {
		t.Fatalf("backdate txn deadline: %v", err)
	}

	pollFor(t, 30*time.Second, 500*time.Millisecond, func() bool {
		return env.txnStatusByBooking(t, bookingID) == "expired"
	}, "txn expired via sweeper")

	assert.Equal(t, 1, env.outboxCountForBooking(t, "payment.expired", bookingID),
		"payment.expired published exactly once by sweeper")
}
