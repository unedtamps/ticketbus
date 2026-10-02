//go:build integration

package integration

// mock_webhook_test.go — TestMock_*: dev-only mock webhook simulator route.

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestMock_CheckoutRoute_Success verifies that simulating a completed payment
// via the mock route settles the transaction and publishes payment.completed
// exactly once.
func TestMock_CheckoutRoute_Success(t *testing.T) {
	env := getTestEnv()
	c := newCustomer()
	ctx := context.Background()

	bookingID, _ := env.seedTxnAndInitiate(t, c, defaultAmount)
	require.Equal(t, 200, postMockSession(t, env.payURL, bookingID, "success"))

	pollFor(t, 30*time.Second, 500*time.Millisecond, func() bool {
		var status string
		if err := env.payPool.QueryRow(ctx,
			`SELECT status FROM transactions WHERE booking_id = $1`, bookingID).Scan(&status); err != nil {
			return false
		}
		return status == "completed"
	}, "txn completed via mock success route")

	assert.Equal(t, 1, env.outboxCountForBooking(t, "payment.completed", bookingID),
		"payment.completed published exactly once")

	// Duplicate simulation is idempotent.
	require.Equal(t, 200, postMockSession(t, env.payURL, bookingID, "success"))
	assert.Equal(t, 1, env.outboxCountForBooking(t, "payment.completed", bookingID),
		"duplicate mock success must not republish")
}

// TestMock_CheckoutRoute_Expired verifies that simulating an expired payment
// expires the transaction and publishes payment.expired exactly once.
func TestMock_CheckoutRoute_Expired(t *testing.T) {
	env := getTestEnv()
	c := newCustomer()
	ctx := context.Background()

	bookingID, _ := env.seedTxnAndInitiate(t, c, defaultAmount)
	require.Equal(t, 200, postMockSession(t, env.payURL, bookingID, "expired"))

	pollFor(t, 30*time.Second, 500*time.Millisecond, func() bool {
		var status string
		if err := env.payPool.QueryRow(ctx,
			`SELECT status FROM transactions WHERE booking_id = $1`, bookingID).Scan(&status); err != nil {
			return false
		}
		return status == "expired"
	}, "txn expired via mock expired route")

	assert.Equal(t, 1, env.outboxCountForBooking(t, "payment.expired", bookingID),
		"payment.expired published exactly once")
}

// TestMock_CheckoutRoute_InvalidStatus verifies the mock route rejects unknown
// status values.
func TestMock_CheckoutRoute_InvalidStatus(t *testing.T) {
	env := getTestEnv()
	require.Equal(t, 400, postMockSession(t, env.payURL, "any-booking", "nonsense"))
}

// TestMock_CheckoutRoute_NotFound verifies the mock route 404s for an unknown
// booking.
func TestMock_CheckoutRoute_NotFound(t *testing.T) {
	env := getTestEnv()
	require.Equal(t, 404, postMockSession(t, env.payURL, newBookingID(), "success"))
}
