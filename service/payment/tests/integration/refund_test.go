//go:build integration

package integration

// refund_test.go — TestRefund_*: refund requests for cancelled events.

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestRefund_CancelledEvent_CreatesRequest verifies that an event.cancelled
// event produces exactly one pending refund request per completed transaction
// and flips the transaction's refund status, and that re-delivering the event
// is idempotent.
//
// The event is published directly to Kafka, standing in for the ticketing
// service's outbox.
func TestRefund_CancelledEvent_CreatesRequest(t *testing.T) {
	env := getTestEnv()
	c := newCustomer()

	bookingID, eventID := newBookingID(), newEventID()
	env.seedTxn(t, c, bookingID, eventID, defaultAmount, time.Hour)
	env.initiate(t, c, bookingID)

	// A pending transaction is deliberately left alone: the customer may
	// still complete it. Only completed transactions are refunded.
	env.postCompletedWebhook(t, bookingID)
	pollFor(t, 30*time.Second, 500*time.Millisecond, func() bool {
		return env.txnStatusByBooking(t, bookingID) == "completed"
	}, "txn completed before event cancellation")

	txnID := env.txnIDByBooking(t, bookingID)
	require.Equal(t, 0, env.pendingRefundCount(t, txnID, "event_cancelled"))

	env.emitEventCancelled(t, eventID)

	pollFor(t, 60*time.Second, 500*time.Millisecond, func() bool {
		return env.refundStatus(t, txnID) == "pending" &&
			env.pendingRefundCount(t, txnID, "event_cancelled") == 1
	}, "refund request created via event.cancelled")

	assert.Equal(t, "pending", env.refundStatus(t, txnID))

	// Re-delivering the cancellation must not create a second request.
	env.emitEventCancelled(t, eventID)
	pollFor(t, 5*time.Second, 500*time.Millisecond, func() bool {
		return env.pendingRefundCount(t, txnID, "event_cancelled") == 1
	}, "refund request stays unique after duplicate cancellation")

	assert.Equal(t, 1, env.pendingRefundCount(t, txnID, "event_cancelled"),
		"duplicate cancellation must not duplicate refund requests")
}
