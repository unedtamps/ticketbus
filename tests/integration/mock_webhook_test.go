//go:build integration

package integration

// mock_webhook_test.go — TestMock_*: dev-only mock webhook simulator route.

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// postMockSession simulates the gateway webhook via the dev-only mock route.
func postMockSession(t *testing.T, env *TestEnv, bookingID, status string) int {
	t.Helper()
	resp, _, err := doJSON(
		http.MethodPost,
		env.payURL+"/api/payments/booking/mock/"+bookingID+"/"+status,
		nil,
		nil,
	)
	require.NoError(t, err)
	return resp.StatusCode
}

// TestMock_CheckoutRoute_Success verifies that simulating a completed payment
// via the mock route settles the whole flow end-to-end.
func TestMock_CheckoutRoute_Success(t *testing.T) {
	env := getTestEnv()
	eventID, ttIDs := setupApprovedEvent(t, env)
	cust := env.registerAndLogin("customer")
	ch := env.authHeadersWith(cust.AccessToken)
	ctx := context.Background()

	bookingID, _ := reserveAndInitiate(t, env, eventID, ttIDs[0], 1, ch)
	require.Equal(t, 200, postMockSession(t, env, bookingID, "success"))

	// Transaction completed + outbox published exactly once
	pollFor(t, 30*time.Second, 500*time.Millisecond, func() bool {
		var status string
		if err := env.payPool.QueryRow(ctx, `SELECT status FROM transactions WHERE booking_id = $1`, bookingID).Scan(&status); err != nil {
			return false
		}
		return status == "completed"
	}, "txn completed via mock success route")

	var outboxCount int
	require.NoError(t, env.payPool.QueryRow(
		ctx, `SELECT COUNT(*) FROM outbox WHERE topic = 'payment.completed' AND key = (SELECT id::text FROM transactions WHERE booking_id = $1)`, bookingID).Scan(&outboxCount))
	assert.Equal(t, 1, outboxCount, "payment.completed published exactly once")

	// Booking confirmed via payment.completed → ticketing consumer
	pollFor(t, 30*time.Second, 500*time.Millisecond, func() bool {
		var status string
		if err := env.invPool.QueryRow(ctx, `SELECT status FROM bookings WHERE id = $1`, bookingID).Scan(&status); err != nil {
			return false
		}
		return status == "confirmed"
	}, "booking confirmed via mock success route")

	// Duplicate simulation is idempotent
	require.Equal(t, 200, postMockSession(t, env, bookingID, "success"))
	require.NoError(t, env.payPool.QueryRow(
		ctx, `SELECT COUNT(*) FROM outbox WHERE topic = 'payment.completed' AND key = (SELECT id::text FROM transactions WHERE booking_id = $1)`, bookingID).Scan(&outboxCount))
	assert.Equal(t, 1, outboxCount, "duplicate mock success must not republish")
}

// TestMock_CheckoutRoute_Expired verifies that simulating an expired payment
// via the mock route expires the transaction, releases seats, and publishes
// payment.expired exactly once.
func TestMock_CheckoutRoute_Expired(t *testing.T) {
	env := getTestEnv()
	eventID, ttIDs := setupApprovedEvent(t, env)
	cust := env.registerAndLogin("customer")
	ch := env.authHeadersWith(cust.AccessToken)
	ctx := context.Background()

	bookingID, _ := reserveAndInitiate(t, env, eventID, ttIDs[0], 2, ch)

	var availBefore int
	require.NoError(t, env.invPool.QueryRow(ctx, `SELECT available_seat FROM ticket_types WHERE id = $1`, ttIDs[0]).Scan(&availBefore))

	require.Equal(t, 200, postMockSession(t, env, bookingID, "expired"))

	pollFor(t, 30*time.Second, 500*time.Millisecond, func() bool {
		var status string
		if err := env.payPool.QueryRow(ctx, `SELECT status FROM transactions WHERE booking_id = $1`, bookingID).Scan(&status); err != nil {
			return false
		}
		return status == "expired"
	}, "txn expired via mock expired route")

	var outboxCount int
	require.NoError(t, env.payPool.QueryRow(
		ctx, `SELECT COUNT(*) FROM outbox WHERE topic = 'payment.expired' AND key = (SELECT id::text FROM transactions WHERE booking_id = $1)`, bookingID).Scan(&outboxCount))
	assert.Equal(t, 1, outboxCount, "payment.expired published exactly once")

	pollFor(t, 30*time.Second, 500*time.Millisecond, func() bool {
		var status string
		if err := env.invPool.QueryRow(ctx, `SELECT status FROM bookings WHERE id = $1`, bookingID).Scan(&status); err != nil {
			return false
		}
		return status == "expired"
	}, "booking expired via mock expired route")

	var availAfter int
	require.NoError(t, env.invPool.QueryRow(ctx, `SELECT available_seat FROM ticket_types WHERE id = $1`, ttIDs[0]).Scan(&availAfter))
	assert.Equal(t, availBefore+2, availAfter, "seats released after mock expiry")
}

// TestMock_CheckoutRoute_InvalidStatus verifies the mock route rejects
// unknown status values.
func TestMock_CheckoutRoute_InvalidStatus(t *testing.T) {
	env := getTestEnv()
	require.Equal(t, 400, postMockSession(t, env, "any-booking", "nonsense"))
}

// TestMock_CheckoutRoute_NotFound verifies the mock route 404s for an
// unknown booking.
func TestMock_CheckoutRoute_NotFound(t *testing.T) {
	env := getTestEnv()
	require.Equal(t, 404, postMockSession(t, env, "ghost-booking", "success"))
}
