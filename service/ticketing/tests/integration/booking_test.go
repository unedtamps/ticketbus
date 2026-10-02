//go:build integration

package integration

// booking_test.go — TestBooking_*: reservation, seat counting, and release.

import (
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBooking_Reserve_Succeeds(t *testing.T) {
	env := getTestEnv()
	eventID, ttIDs, _ := setupApprovedEvent(t, env)
	cust := newCustomer()

	resp, body, rr := env.reserve(t, cust, eventID, ttIDs[0], 1, env.ticketPrice(t, ttIDs[0]))
	require.Equal(t, http.StatusCreated, resp.StatusCode, "reserve failed: %s", body)
	assert.NotEmpty(t, rr.Data.BookingID)
	assert.Equal(t, "pending", rr.Data.Status)

	// Reserve hands the booking to the payment service to create the transaction.
	assert.True(t, env.fakePay.calledFor(rr.Data.BookingID),
		"reserve should have initiated a payment transaction for %s", rr.Data.BookingID)
}

func TestBooking_Reserve_WrongPrice_400(t *testing.T) {
	env := getTestEnv()
	eventID, ttIDs, _ := setupApprovedEvent(t, env)
	cust := newCustomer()

	resp, body, _ := env.reserve(t, cust, eventID, ttIDs[0], 1, 1)
	assert.Equal(t, http.StatusBadRequest, resp.StatusCode, "expected 400 for wrong price: %s", body)
	assert.Contains(t, string(body), "unit_price_rupiah does not match",
		"error should mention price mismatch")
}

func TestBooking_Reserve_OverReserve_Conflict(t *testing.T) {
	env := getTestEnv()
	eventID, ttIDs, _ := setupApprovedEvent(t, env)
	cust := newCustomer()

	resp, body, _ := env.reserve(t, cust, eventID, ttIDs[0], 9999, env.ticketPrice(t, ttIDs[0]))
	assert.Equal(t, http.StatusConflict, resp.StatusCode, "body: %s", body)
}

func TestBooking_Reserve_CannotBeCancelled(t *testing.T) {
	env := getTestEnv()
	eventID, ttIDs, _ := setupApprovedEvent(t, env)
	cust := newCustomer()

	_, _, rr := env.reserve(t, cust, eventID, ttIDs[0], 1, env.ticketPrice(t, ttIDs[0]))
	require.NotEmpty(t, rr.Data.BookingID)

	// Cancelling a reservation is not allowed — seats release on expiry only.
	resp, body, err := doJSON(http.MethodDelete,
		env.apiURL+"/api/bookings/reserve/"+rr.Data.BookingID, nil, cust.headers)
	require.NoError(t, err)
	assert.Equal(t, http.StatusConflict, resp.StatusCode, "DELETE should be rejected: %s", body)
}

// TestBooking_Confirm_OnPaymentCompleted verifies a pending reservation is
// confirmed once payment.completed arrives, and that a duplicate delivery does
// not change the outcome.
func TestBooking_Confirm_OnPaymentCompleted(t *testing.T) {
	env := getTestEnv()
	eventID, ttIDs, _ := setupApprovedEvent(t, env)
	cust := newCustomer()

	resp, body, rr := env.reserve(t, cust, eventID, ttIDs[0], 2, env.ticketPrice(t, ttIDs[0]))
	require.Equal(t, http.StatusCreated, resp.StatusCode, "reserve failed: %s", body)
	bookingID := rr.Data.BookingID
	require.NotEmpty(t, bookingID, "reserve body: %s", body)

	txnID := uuid.NewString()
	env.emitPaymentCompleted(t, txnID, bookingID, eventID, cust.id)

	pollFor(t, 30*time.Second, 500*time.Millisecond, func() bool {
		return env.bookingStatus(t, bookingID) == "confirmed"
	}, "booking confirmed via payment.completed")

	// Duplicate delivery is idempotent.
	env.emitPaymentCompleted(t, txnID, bookingID, eventID, cust.id)
	assert.Equal(t, "confirmed", env.bookingStatus(t, bookingID))

	// The confirmed booking is visible to its owner.
	resp, listBody, err := doJSON(http.MethodGet, env.apiURL+"/api/bookings", nil, cust.headers)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, resp.StatusCode, "%s", listBody)
	assert.Contains(t, string(listBody), `"confirmed"`)
	assert.Contains(t, string(listBody), bookingID)
}

// TestBooking_Expiry_ReleasesSeats verifies a payment.expired event expires the
// reservation and returns its seats to the pool.
func TestBooking_Expiry_ReleasesSeats(t *testing.T) {
	env := getTestEnv()
	eventID, ttIDs, _ := setupApprovedEvent(t, env)
	cust := newCustomer()
	ttID := ttIDs[0]

	availableBefore := env.availableSeats(t, ttID)
	require.Greater(t, availableBefore, 4, "not enough seats for test")

	_, body, rr := env.reserve(t, cust, eventID, ttID, 5, env.ticketPrice(t, ttID))
	bookingID := rr.Data.BookingID
	require.NotEmpty(t, bookingID, "reserve body: %s", body)

	// Seats are held for the duration of the reservation.
	assert.Equal(t, availableBefore-5, env.availableSeats(t, ttID), "seats should be deducted after reserve")

	env.emitPaymentExpired(t, uuid.NewString(), bookingID, eventID, cust.id)

	pollFor(t, 30*time.Second, time.Second, func() bool {
		return env.bookingStatus(t, bookingID) == "expired"
	}, "booking expired via payment.expired")

	assert.Equal(t, availableBefore, env.availableSeats(t, ttID), "seats should be released after expiry")
}

// TestBooking_CancelCascade_Idempotent verifies that cancelling an event
// cancels its bookings, and that re-delivering event.cancelled changes nothing.
func TestBooking_CancelCascade_Idempotent(t *testing.T) {
	env := getTestEnv()
	eventID, ttIDs, eo := setupApprovedEvent(t, env)
	cust := newCustomer()

	_, body, rr := env.reserve(t, cust, eventID, ttIDs[0], 1, env.ticketPrice(t, ttIDs[0]))
	bookingID := rr.Data.BookingID
	require.NotEmpty(t, bookingID, "reserve body: %s", body)

	// Confirm it first, so cancellation has a settled booking to act on.
	env.emitPaymentCompleted(t, uuid.NewString(), bookingID, eventID, cust.id)
	pollFor(t, 30*time.Second, 500*time.Millisecond, func() bool {
		return env.bookingStatus(t, bookingID) == "confirmed"
	}, "booking confirmed before cancellation")

	// The organizer cancels the event, which publishes event.cancelled.
	resp, cancelBody, err := doJSON(http.MethodPost, env.apiURL+"/api/events/"+eventID+"/cancel", nil, eo.headers)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, resp.StatusCode, "cancel: %s", cancelBody)

	pollFor(t, 60*time.Second, 500*time.Millisecond, func() bool {
		return env.bookingStatus(t, bookingID) == "cancelled"
	}, "booking cancelled via event.cancelled")

	// Re-delivering the cancellation must not change anything.
	env.emitEventCancelled(t, eventID)
	assert.Equal(t, "cancelled", env.bookingStatus(t, bookingID))
}
