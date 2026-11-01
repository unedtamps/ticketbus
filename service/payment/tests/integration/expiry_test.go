//go:build integration

package integration

// expiry_test.go — TestExpiry_*: gateway expiry webhook + expiry sweeper.
//
// Ownership split: a transaction with a live gateway session (pending)
// expires through the gateway's own webhook. The sweeper only covers what the
// gateway will never report — never-initiated transactions (abandoned carts)
// and sessions whose webhook never arrived.

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

// TestExpiry_Sweeper_ExpiresAbandonedCart covers the case nothing else can
// handle: the customer reserved seats but never opened a checkout session, so
// the gateway knows nothing about the transaction and will never send a
// webhook. Without the sweeper those seats would stay reserved forever.
func TestExpiry_Sweeper_ExpiresAbandonedCart(t *testing.T) {
	env := getTestEnv()
	c := newCustomer()

	// Seeded but never initiated — status stays initiated, no provider_ref.
	bookingID := newBookingID()
	env.seedTxn(t, c, bookingID, newEventID(), defaultAmount, time.Hour)

	_, err := env.payPool.Exec(context.Background(),
		`UPDATE transactions SET expires_at = now() - interval '1 minute' WHERE booking_id = $1`, bookingID)
	if err != nil {
		t.Fatalf("backdate txn deadline: %v", err)
	}

	pollFor(t, 30*time.Second, 500*time.Millisecond, func() bool {
		return env.txnStatusByBooking(t, bookingID) == "expired"
	}, "abandoned cart expired via sweeper")

	assert.Equal(t, 1, env.outboxCountForBooking(t, "payment.expired", bookingID),
		"payment.expired published exactly once by sweeper")
}

// TestExpiry_Sweeper_LeavesLiveSessionForTheGateway verifies the sweeper keeps
// its hands off a transaction that has a gateway session: the gateway's own
// webhook is the normal path, and racing it would cancel a session the customer
// may still be paying through.
func TestExpiry_Sweeper_LeavesLiveSessionForTheGateway(t *testing.T) {
	env := getTestEnv()
	c := newCustomer()

	bookingID, _ := env.seedTxnAndInitiate(t, c, defaultAmount)

	// Just past the deadline, well inside the grace window.
	_, err := env.payPool.Exec(context.Background(),
		`UPDATE transactions SET expires_at = now() - interval '1 minute' WHERE booking_id = $1`, bookingID)
	if err != nil {
		t.Fatalf("backdate txn deadline: %v", err)
	}

	// Several poller ticks pass without the sweeper touching it.
	time.Sleep(5 * time.Second)

	assert.Equal(t, "pending", env.txnStatusByBooking(t, bookingID),
		"a session-backed transaction must be left to the gateway webhook")
	assert.Equal(t, 0, env.outboxCountForBooking(t, "payment.expired", bookingID),
		"sweeper must not publish payment.expired while the gateway is still the owner")
}

// TestExpiry_Sweeper_ExpiresLostWebhookSession verifies the safety net: once a
// session-backed transaction is past the deadline by more than the grace
// window, its webhook is treated as lost and the session is swept.
func TestExpiry_Sweeper_ExpiresLostWebhookSession(t *testing.T) {
	env := getTestEnv()
	c := newCustomer()

	bookingID, _ := env.seedTxnAndInitiate(t, c, defaultAmount)

	_, err := env.payPool.Exec(context.Background(),
		`UPDATE transactions SET expires_at = now() - interval '11 minutes' WHERE booking_id = $1`, bookingID)
	if err != nil {
		t.Fatalf("backdate txn deadline: %v", err)
	}

	pollFor(t, 30*time.Second, 500*time.Millisecond, func() bool {
		return env.txnStatusByBooking(t, bookingID) == "expired"
	}, "session with a lost webhook expired via sweeper")

	assert.Equal(t, 1, env.outboxCountForBooking(t, "payment.expired", bookingID),
		"payment.expired published exactly once by sweeper")
}
