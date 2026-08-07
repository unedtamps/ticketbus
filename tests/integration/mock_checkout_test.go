//go:build integration

package integration

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

// Test_MockCheckoutRoute_Success verifies that simulating a completed payment
// via the mock route settles the whole flow end-to-end.
func Test_MockCheckoutRoute_Success(t *testing.T) {
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

// Test_MockCheckoutRoute_Expired verifies that simulating an expired payment
// via the mock route expires the transaction, releases seats, and publishes
// payment.expired exactly once.
func Test_MockCheckoutRoute_Expired(t *testing.T) {
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

// Test_MockCheckoutRoute_InvalidStatus verifies the mock route rejects
// unknown status values.
func Test_MockCheckoutRoute_InvalidStatus(t *testing.T) {
	env := getTestEnv()
	require.Equal(t, 400, postMockSession(t, env, "any-booking", "nonsense"))
}

// Test_MockCheckoutRoute_NotFound verifies the mock route 404s for an
// unknown booking.
func Test_MockCheckoutRoute_NotFound(t *testing.T) {
	env := getTestEnv()
	require.Equal(t, 404, postMockSession(t, env, "ghost-booking", "success"))
}

// Test_Sweeper_Expires_UnpaidSession verifies the expiry poller handles a
// transaction whose session deadline passed without any webhook (mock route
// never hit). The deadline is backdated so the poller (1s tick) picks it up.
func Test_Sweeper_Expires_UnpaidSession(t *testing.T) {
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
