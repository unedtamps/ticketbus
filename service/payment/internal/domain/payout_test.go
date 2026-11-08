package domain

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestPayoutOutcome_SettlesEveryTerminalStatus locks the invariant that keeps
// an organizer payout from stranding: every gateway status that ends a payout
// must produce a terminal ledger outcome. A missing case leaves the attempt
// pending forever — the reconcile poll keeps re-reading the same terminal
// status, and ResetForRetry only accepts failed/processing.
func TestPayoutOutcome_SettlesEveryTerminalStatus(t *testing.T) {
	cases := []struct {
		gatewayStatus string
		wantStatus    string
	}{
		{"SUCCEEDED", PayoutStatusPaidOut},
		{"succeeded", PayoutStatusPaidOut},
		{"COMPLETED", PayoutStatusPaidOut},
		{"FAILED", PayoutStatusFailed},
		{"REVERSED", PayoutStatusFailed},
		{"REJECTED", PayoutStatusFailed},
		{"CANCELLED", PayoutStatusFailed},
		{"EXPIRED", PayoutStatusFailed},
	}
	for _, tc := range cases {
		status, _, terminal := PayoutOutcome(tc.gatewayStatus, "")
		assert.True(t, terminal, "%s must be terminal", tc.gatewayStatus)
		assert.Equal(t, tc.wantStatus, status, "%s maps to the wrong payout status", tc.gatewayStatus)
	}
}

// TestPayoutOutcome_ReversedIsNotPaidOut is the money-correctness case: a
// reversed payout bounced back to the platform's balance, so the organizer was
// never paid and the attempt must be retryable.
func TestPayoutOutcome_ReversedIsNotPaidOut(t *testing.T) {
	status, reason, terminal := PayoutOutcome("REVERSED", "")
	assert.True(t, terminal)
	assert.Equal(t, PayoutStatusFailed, status)
	assert.NotEmpty(t, reason)
	assert.Contains(t, reason, "ditarik kembali")
}

// TestPayoutOutcome_InFlightStaysOpen: a payout that is merely routing must not
// be closed as failed.
func TestPayoutOutcome_InFlightStaysOpen(t *testing.T) {
	for _, s := range []string{
		"PENDING", "ACCEPTED", "ROUTING", "REQUESTED", "READY", "LOCKED",
		"PENDING_COMPLIANCE_ASSESSMENT", "", "SOMETHING_NEW",
	} {
		status, reason, terminal := PayoutOutcome(s, "")
		assert.False(t, terminal, "%q must stay open", s)
		assert.Empty(t, status)
		assert.Empty(t, reason)
	}
}

// TestPayoutOutcome_FailureCodeIsTranslated: the organizer must never read a
// raw gateway enum.
func TestPayoutOutcome_FailureCodeIsTranslated(t *testing.T) {
	_, reason, terminal := PayoutOutcome("FAILED", "ACCOUNT_NAME_MISMATCH")
	assert.True(t, terminal)
	assert.Contains(t, reason, "Nama pemilik rekening")
	assert.NotContains(t, reason, "ACCOUNT_NAME_MISMATCH")
}
