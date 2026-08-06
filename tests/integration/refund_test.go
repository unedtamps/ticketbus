//go:build integration

package integration

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Test_CancelledEventTriggersRefunds verifies that event cancellation flows
// through the outbox to Kafka and is consumed by both ticketing (booking
// cancellation) and payment (refund request creation).
func Test_CancelledEventTriggersRefunds(t *testing.T) {
	env := getTestEnv()

	// Event owned by EO (kept token so we can cancel as the owner)
	eo := env.registerAndLogin("eo")
	event := createEventRaw(t, env, eo.AccessToken)
	eventID := event.ID

	admin := env.loginAdmin()
	resp, _, err := doJSON(http.MethodPost, env.eventURL+"/api/events/"+eventID+"/approve", nil, env.authHeadersWith(admin.AccessToken))
	require.NoError(t, err)
	require.Equal(t, 200, resp.StatusCode)

	cust := env.registerAndLogin("customer")
	ch := env.authHeadersWith(cust.AccessToken)

	// Ticket types are not returned by the create response, so load them.
	var detail eventDetailResp
	pollFor(t, 15*time.Second, 500*time.Millisecond, func() bool {
		_, b, _ := doJSON(http.MethodGet, env.eventURL+"/api/events/"+eventID, nil, ch)
		if jsonData(b, &detail) != nil {
			return false
		}
		return len(detail.TicketTypes) > 0 && detail.TicketTypes[0].Available > 0
	}, "seat init after approval")
	require.NotEmpty(t, detail.TicketTypes)
	ttID := detail.TicketTypes[0].ID

	// Full purchase: reserve → transaction → webhook success
	_, body, err := doJSON(http.MethodPost, env.invURL+"/api/bookings/reserve", map[string]interface{}{
		"event_id": eventID,
		"items": []map[string]interface{}{
			{"ticket_type_id": ttID, "quantity": 1, "unit_price_cents": 10000},
		},
	}, ch)
	require.NoError(t, err)

	var rr reserveResp
	require.NoError(t, json.Unmarshal(body, &rr))
	bookingID := rr.Data.BookingID
	require.NotEmpty(t, bookingID)

	// Initiate payment and simulate the completed webhook
	initiatePayment(t, env, bookingID, ch)
	completePaymentWebhook(t, env, bookingID)

	ctx := context.Background()
	pollFor(t, 30*time.Second, 500*time.Millisecond, func() bool {
		var status string
		if err := env.payPool.QueryRow(ctx, `SELECT status FROM transactions WHERE booking_id = $1`, bookingID).Scan(&status); err != nil {
			return false
		}
		return status == "completed"
	}, "payment completes after webhook")

	var txnID string
	require.NoError(t, env.payPool.QueryRow(ctx, `SELECT id FROM transactions WHERE booking_id = $1`, bookingID).Scan(&txnID))

	pollFor(t, 30*time.Second, 500*time.Millisecond, func() bool {
		var status string
		if err := env.invPool.QueryRow(ctx, `SELECT status FROM bookings WHERE id = $1`, bookingID).Scan(&status); err != nil {
			return false
		}
		return status == "confirmed"
	}, "booking confirmed after payment.completed")

	// Cancel the event as the owning EO.
	resp, _, err = doJSON(http.MethodPost, env.eventURL+"/api/events/"+eventID+"/cancel", nil, env.authHeadersWith(eo.AccessToken))
	require.NoError(t, err)
	assert.Equal(t, 200, resp.StatusCode)

	// 1. Ticketing consumer cancels the booking (async via event.cancelled)
	pollFor(t, 60*time.Second, 500*time.Millisecond, func() bool {
		var status string
		if err := env.invPool.QueryRow(ctx, `SELECT status FROM bookings WHERE id = $1`, bookingID).Scan(&status); err != nil {
			return false
		}
		return status == "cancelled"
	}, "booking cancelled via event.cancelled → ticketing consumer")

	// 2. Payment consumer creates a refund request and marks refund pending
	pollFor(t, 60*time.Second, 500*time.Millisecond, func() bool {
		var refundStatus string
		if err := env.payPool.QueryRow(ctx, `SELECT refund_status FROM transactions WHERE id = $1`, txnID).Scan(&refundStatus); err != nil {
			return false
		}
		var refundCount int
		if err := env.payPool.QueryRow(ctx, `
			SELECT COUNT(*) FROM refund_requests WHERE transaction_id = $1 AND status = 'pending' AND reason = 'event_cancelled'`, txnID).Scan(&refundCount); err != nil {
			return false
		}
		return refundStatus == "pending" && refundCount == 1
	}, "refund request created via event.cancelled → payment consumer")

	// 3. Idempotent: cancelling again must not create a second refund request
	_, _, err = doJSON(http.MethodPost, env.eventURL+"/api/events/"+eventID+"/cancel", nil, env.authHeadersWith(eo.AccessToken))
	require.NoError(t, err)

	time.Sleep(2 * time.Second)
	var refundCount int
	require.NoError(t, env.payPool.QueryRow(ctx, `SELECT COUNT(*) FROM refund_requests WHERE transaction_id = $1`, txnID).Scan(&refundCount))
	assert.Equal(t, 1, refundCount, "duplicate cancellation must not create duplicate refund requests")
}
