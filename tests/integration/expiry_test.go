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

// Test_ExpiryRecoveryExpiresMissedBookings verifies that the PostgreSQL
// recovery sweeper expires pending bookings whose Redis TTL trigger was missed.
func Test_ExpiryRecoveryExpiresMissedBookings(t *testing.T) {
	env := getTestEnv()
	eventID, ttIDs := setupApprovedEvent(t, env)

	cust := env.registerAndLogin("customer")
	ch := env.authHeadersWith(cust.AccessToken)

	// Reserve → pending booking + Redis TTL marker
	_, body, err := doJSON(http.MethodPost, env.invURL+"/api/bookings/reserve", map[string]interface{}{
		"event_id": eventID,
		"items":    []map[string]interface{}{{"ticket_type_id": ttIDs[0], "quantity": 2, "unit_price_cents": 10000}},
	}, ch)
	require.NoError(t, err)
	var rr reserveResp
	require.NoError(t, jsonData(body, &rr))
	bookingID := rr.Data.BookingID
	require.NotEmpty(t, bookingID)

	// Simulate a missed Redis notification: remove the marker key and push
	// expires_at into the past, as if the expiry trigger never fired.
	ctx := context.Background()
	require.NoError(t, env.rdb.Del(ctx, "reservation:"+bookingID).Err())
	_, err = env.invPool.Exec(ctx, `UPDATE bookings SET expires_at = NOW() - INTERVAL '1 minute' WHERE id = $1`, bookingID)
	require.NoError(t, err)

	var availBefore int
	require.NoError(t, env.invPool.QueryRow(ctx, `SELECT available_seat FROM ticket_types WHERE id = $1`, ttIDs[0]).Scan(&availBefore))

	// Run the sweeper directly (deterministic, no ticker wait).
	env.invSvc.ExpireDueBookings(ctx)

	var status string
	require.NoError(t, env.invPool.QueryRow(ctx, `SELECT status FROM bookings WHERE id = $1`, bookingID).Scan(&status))
	assert.Equal(t, "expired", status)

	var availAfter int
	require.NoError(t, env.invPool.QueryRow(ctx, `SELECT available_seat FROM ticket_types WHERE id = $1`, ttIDs[0]).Scan(&availAfter))
	assert.Equal(t, availBefore+2, availAfter, "seats should be released by the sweeper")

	var outboxCount int
	require.NoError(t, env.invPool.QueryRow(ctx, `SELECT COUNT(*) FROM outbox WHERE topic = 'reservation.expired' AND key = $1`, bookingID).Scan(&outboxCount))
	assert.Equal(t, 1, outboxCount, "reservation.expired should be emitted once")

	// Wait for the outbox worker to flush so payment can observe the expiry.
	pollFor(t, 30*time.Second, 500*time.Millisecond, func() bool {
		var pending int
		if err := env.invPool.QueryRow(ctx, `SELECT COUNT(*) FROM outbox WHERE delivered = false`).Scan(&pending); err != nil {
			return false
		}
		return pending == 0
	}, "outbox flush after sweeper expiry")
}
