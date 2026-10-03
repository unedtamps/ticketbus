//go:build e2e

package e2e

// cancel_test.go — event cancellation cascading to bookings and seats.

import (
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestE2E_EventCancelCascade verifies that cancelling an event after a purchase
// cancels the booking and returns its seats to the pool.
//
// The payment-side consequence — a pending refund request per completed
// transaction — is asserted in the payment service's own suite
// (TestRefund_CancelledEvent_CreatesRequest), because refund_requests has no
// public API and this suite is deliberately black-box.
func TestE2E_EventCancelCascade(t *testing.T) {
	env := getTestEnv()
	admin := env.loginAdmin(t)

	eo := env.registerEO(t)
	ev := env.createEvent(t, eo, 100)
	eventID := ev.Event.ID
	env.approveEvent(t, eventID, admin)

	var ticketTypes []ticketType
	pollFor(t, 30*time.Second, 500*time.Millisecond, func() bool {
		ticketTypes = env.fetchEvent(t, eventID).TicketTypes
		return len(ticketTypes) > 0 && ticketTypes[0].Available > 0
	}, "seat init after approval")
	require.NotEmpty(t, ticketTypes)
	tt := ticketTypes[0]
	availableBefore := tt.Available

	cust := env.registerCustomer(t)
	bookingID := env.reserve(t, cust, eventID, tt.ID, 2, tt.PriceRupiah)

	// Buy it, so cancellation has a settled booking to act on.
	env.settleBooking(t, cust, bookingID)
	require.Equal(t, availableBefore-2, env.availableSeats(t, eventID, tt.ID),
		"confirmed seats should be held")

	// The owning organizer cancels the event.
	env.cancelEvent(t, eo, eventID)

	env.waitBookingStatus(t, cust, bookingID, "cancelled", settleTimeout)
	assert.Equal(t, availableBefore, env.availableSeats(t, eventID, tt.ID),
		"seats should be released after event cancellation")
}

// TestE2E_EventCancel_CancelBeforeConfirm covers the race the payment service
// explicitly handles: the event is cancelled after the session is created but
// before the money arrives. The late completion must not resurrect the booking —
// it stays cancelled and the seats stay released.
func TestE2E_EventCancel_CancelBeforeConfirm(t *testing.T) {
	env := getTestEnv()
	admin := env.loginAdmin(t)

	eo := env.registerEO(t)
	ev := env.createEvent(t, eo, 100)
	eventID := ev.Event.ID
	env.approveEvent(t, eventID, admin)

	var ticketTypes []ticketType
	pollFor(t, 30*time.Second, 500*time.Millisecond, func() bool {
		ticketTypes = env.fetchEvent(t, eventID).TicketTypes
		return len(ticketTypes) > 0 && ticketTypes[0].Available > 0
	}, "seat init after approval")
	require.NotEmpty(t, ticketTypes)
	tt := ticketTypes[0]
	availableBefore := tt.Available

	cust := env.registerCustomer(t)
	bookingID := env.reserve(t, cust, eventID, tt.ID, 1, tt.PriceRupiah)

	// Session exists but the customer has not paid.
	sessionID := env.payBooking(t, cust, bookingID)

	// Cancel before the webhook lands.
	env.cancelEvent(t, eo, eventID)
	env.waitBookingStatus(t, cust, bookingID, "cancelled", settleTimeout)

	// Money arrives late. The booking must stay cancelled.
	env.deliverWebhook(t, "payment_session.completed", bookingID, sessionID)
	env.waitPaymentStatus(t, cust, bookingID, "completed", txnTimeout)

	assert.Equal(t, "cancelled", env.getBooking(t, cust, bookingID).Status,
		"a late payment must not confirm a booking whose event was cancelled")
	assert.Equal(t, availableBefore, env.availableSeats(t, eventID, tt.ID),
		"seats should stay released after cancel-before-confirm")
}

// TestE2E_EventCancel_Idempotent verifies a repeated cancellation is a no-op.
func TestE2E_EventCancel_Idempotent(t *testing.T) {
	env := getTestEnv()
	admin := env.loginAdmin(t)

	eo := env.registerEO(t)
	ev := env.createEvent(t, eo, 100)
	eventID := ev.Event.ID
	env.approveEvent(t, eventID, admin)

	var ticketTypes []ticketType
	pollFor(t, 30*time.Second, 500*time.Millisecond, func() bool {
		ticketTypes = env.fetchEvent(t, eventID).TicketTypes
		return len(ticketTypes) > 0 && ticketTypes[0].Available > 0
	}, "seat init after approval")
	require.NotEmpty(t, ticketTypes)
	tt := ticketTypes[0]
	availableBefore := tt.Available

	cust := env.registerCustomer(t)
	bookingID := env.reserve(t, cust, eventID, tt.ID, 1, tt.PriceRupiah)
	env.settleBooking(t, cust, bookingID)

	env.cancelEvent(t, eo, eventID)
	env.waitBookingStatus(t, cust, bookingID, "cancelled", settleTimeout)
	require.Equal(t, availableBefore, env.availableSeats(t, eventID, tt.ID),
		"seats released exactly once")

	// A second cancel is rejected — the event is no longer published — so it
	// cannot release the same seats again.
	//
	// The handler reports this as 400 Bad Request, not 409 Conflict, even though
	// the request itself is well-formed and the rejection is purely a state
	// conflict. CancelEvent funnels every non-not-found error through
	// sharedhttp.BadRequest, so ErrEventNotCancellable lands on 400. Pinned here
	// as observed behaviour; 409 would be the more conventional choice.
	status, body, err := env.doJSON(
		http.MethodPost, "/api/events/"+eventID+"/cancel", nil, bearer(eo))
	require.NoError(t, err)
	assert.Equal(t, http.StatusBadRequest, status, "repeat cancel: %s", body)
	assert.Contains(t, string(body), "only published events can be cancelled",
		"the rejection must name the state that blocked it")

	assert.Equal(t, availableBefore, env.availableSeats(t, eventID, tt.ID),
		"a rejected repeat cancel must not release seats a second time")
}
