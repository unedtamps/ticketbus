//go:build e2e

package e2e

// payout_test.go — happy-path organizer payout through the real stack with the
// mock payment gateway. Failure/retry paths live in the payment integration
// suite; this test proves the end-to-end async choreography and the public API.
//
// The flow needs an event that has ended, so the test books while the event is
// still live and waits for it to finish before asking for the money. The e2e
// stack shortens the reservation window (MIN_RESERVATION_WINDOW_SEC) so the
// wait stays short.

import (
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestE2E_Payout_FullFlow(t *testing.T) {
	env := getTestEnv()
	admin := env.loginAdmin(t)
	eo := env.registerEO(t)

	// A live event: it started an hour ago and ends in 90 seconds. The
	// booking hold is clamped to the end of the event, so the customer pays
	// while the event is still running.
	endAt := time.Now().Add(90 * time.Second)
	ev := env.createEventAt(t, eo, 100, time.Now().Add(-time.Hour), endAt)
	env.approveEvent(t, ev.Event.ID, admin)

	var detail eventDetail
	pollFor(t, 30*time.Second, 500*time.Millisecond, func() bool {
		detail = env.fetchEvent(t, ev.Event.ID)
		return len(detail.TicketTypes) > 0 && detail.TicketTypes[0].Available > 0
	}, "seat init after approval")
	require.NotEmpty(t, detail.TicketTypes)
	tt := detail.TicketTypes[0]

	// Two paid tickets: the payout amount is the sum of completed,
	// non-refunded transactions.
	cust := env.registerCustomer(t)
	bookingID := env.reserve(t, cust, ev.Event.ID, tt.ID, 2, tt.PriceRupiah)
	env.settleBooking(t, cust, bookingID)

	env.setPayoutAccount(t, eo, "BCA", "1234567890", "E2E Organizer")

	// The event must be over before its earnings can be paid out.
	pollFor(t, 2*time.Minute, time.Second, func() bool {
		return time.Now().After(endAt)
	}, "event to end")

	status, body := env.requestPayout(t, eo, ev.Event.ID)
	require.Equal(t, http.StatusAccepted, status, "request payout: %s", body)

	payout := env.waitPayoutStatus(t, eo, ev.Event.ID, "paid_out", settleTimeout)
	assert.EqualValues(t, 2*tt.PriceRupiah, payout.AmountRupiah,
		"payout should equal the settled booking total")
	assert.Contains(t, payout.ProviderRef, "dis-mock-",
		"mock gateway reference expected, got %q", payout.ProviderRef)
	assert.NotEmpty(t, payout.CompletedAt)
	assert.Equal(t, 1, payout.Attempts)

	// The event can be paid out again, but only to collect what was not paid
	// out before — here there is nothing left, so the attempt finds no
	// earnings.
	status, body = env.requestPayout(t, eo, ev.Event.ID)
	require.Equal(t, http.StatusAccepted, status, "second payout request: %s", body)

	payout = env.waitPayoutStatus(t, eo, ev.Event.ID, "paid_out", settleTimeout)
	assert.EqualValues(t, 2*tt.PriceRupiah, payout.AmountRupiah,
		"a further payout must not pay the same transactions twice")
	assert.Equal(t, 2, payout.Attempts, "the second attempt is recorded")
}
