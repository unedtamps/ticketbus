//go:build e2e

package e2e

// payout_test.go — organizer payout through the real stack with the mock
// payment gateway. The gateway accepts disbursements asynchronously (PENDING,
// like Xendit's rails), so the tests drive the confirmation — success and
// failure — the way the gateway's webhook would, and assert the full
// choreography: ticketing → payment → gateway → confirmation → ticketing.
// Per-service failure/retry edge cases live in the payment integration suite.
//
// The flow needs an event that has ended, so the test books while the event is
// still live and waits for it to finish before asking for the money. The e2e
// stack shortens the reservation window (MIN_RESERVATION_WINDOW_SEC) so the
// wait stays short.

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestE2E_Payout_FullFlow(t *testing.T) {
	env := getTestEnv()
	admin := env.loginAdmin(t)
	eo := env.registerEO(t)

	// Two paid tickets: the payout amount is the sum of completed,
	// non-refunded transactions.
	eventID, tt := env.endedEventWithSales(t, admin, eo, 2)
	env.setPayoutAccount(t, eo, "BCA", "1234567890", "E2E Organizer")

	status, body := env.requestPayout(t, eo, eventID)
	require.Equal(t, http.StatusAccepted, status, "request payout: %s", body)

	// The gateway accepted the disbursement but has not settled it: the
	// confirmation webhook (retried here, like Xendit retries) finalizes it.
	payout := env.confirmPayout(t, eo, eventID, "success", "paid_out", settleTimeout)
	assert.EqualValues(t, 2*tt.PriceRupiah, payout.AmountRupiah,
		"payout should equal the settled booking total")
	assert.Contains(t, payout.ProviderRef, "dis-mock-",
		"mock gateway reference expected, got %q", payout.ProviderRef)
	assert.NotEmpty(t, payout.CompletedAt)
	assert.Equal(t, 1, payout.Attempts)

	// The event can be paid out again, but only to collect what was not paid
	// out before — here there is nothing left, so the attempt finds no
	// earnings.
	status, body = env.requestPayout(t, eo, eventID)
	require.Equal(t, http.StatusAccepted, status, "second payout request: %s", body)

	payout = env.waitPayoutStatus(t, eo, eventID, "paid_out", settleTimeout)
	assert.EqualValues(t, 2*tt.PriceRupiah, payout.AmountRupiah,
		"a further payout must not pay the same transactions twice")
	assert.Equal(t, 2, payout.Attempts, "the second attempt is recorded")
}

// A payout the gateway accepted but then rejected: the failed confirmation
// records an organizer-facing reason on the attempt. The raw gateway code is
// translated, never stored or shown to the organizer.
func TestE2E_Payout_WebhookFailed(t *testing.T) {
	env := getTestEnv()
	admin := env.loginAdmin(t)
	eo := env.registerEO(t)

	eventID, _ := env.endedEventWithSales(t, admin, eo, 1)
	env.setPayoutAccount(t, eo, "BCA", "1234567890", "E2E Organizer")

	status, body := env.requestPayout(t, eo, eventID)
	require.Equal(t, http.StatusAccepted, status, "request payout: %s", body)

	payout := env.confirmPayout(t, eo, eventID, "failed", "failed", settleTimeout)
	assert.Equal(t, "failed", payout.Status)
	assert.Contains(t, payout.FailureReason, "Payout tertunda karena kendala saldo",
		"failure reason should explain the payout failure: %q", payout.FailureReason)
	assert.NotContains(t, payout.FailureReason, "Refund",
		"a failed payout is not a refund")
	assert.NotContains(t, payout.FailureReason, "INSUFFICIENT_BALANCE",
		"the raw gateway enum must not leak to the organizer")
	assert.Contains(t, payout.ProviderRef, "dis-mock-",
		"even a failed attempt records which gateway payout it belongs to")
}
