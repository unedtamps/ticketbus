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
	assert.Contains(t, reason, "payout")
	assert.NotContains(t, reason, "ACCOUNT_NAME_MISMATCH")
}

// TestPayoutFailureMessage_CoversEveryGatewayCode mirrors the refund coverage
// check: every documented Xendit code yields a sentence, never the raw enum.
// A failed payout is not a refund, so the copy must not say "Refund" either.
func TestPayoutFailureMessage_CoversEveryGatewayCode(t *testing.T) {
	codes := []string{
		"INSUFFICIENT_BALANCE",
		"INVALID_DESTINATION",
		"DESTINATION_MAXIMUM_LIMIT",
		"ACCOUNT_NAME_MISMATCH",
		"REJECTED_BY_CHANNEL",
		"TEMPORARY_TRANSFER_ERROR",
		"TRANSFER_ERROR",
		"SLS_SENDER",
		"SLS_RECIPIENT",
	}
	for _, code := range codes {
		msg := PayoutFailureMessage(code)
		assert.NotEmpty(t, msg, "%s needs a message", code)
		assert.NotEqual(t, code, msg, "%s must not leak the raw gateway code", code)
		assert.Contains(t, msg, " ", "%s should be a sentence", code)
		assert.NotContains(t, msg, "Refund", "%s: a failed payout is not a refund", code)
	}
}

// TestPayoutFailureMessage_PlatformFaultDoesNotBlameOrganizer: when the
// platform is out of balance or the sender account is broken, the organizer
// must not be told to fix their own account.
func TestPayoutFailureMessage_PlatformFaultDoesNotBlameOrganizer(t *testing.T) {
	for _, code := range []string{"INSUFFICIENT_BALANCE", "SLS_SENDER", "TEMPORARY_TRANSFER_ERROR", "TRANSFER_ERROR"} {
		msg := PayoutFailureMessage(code)
		assert.NotContains(t, msg, "Perbarui", "%s must not ask the organizer to change their details", code)
		assert.NotContains(t, msg, "tidak valid", "%s must not imply the destination is invalid", code)
	}
}

// TestPayoutFailureMessage_MismatchIsActionable: the one code the organizer
// can actually fix must say so.
func TestPayoutFailureMessage_MismatchIsActionable(t *testing.T) {
	msg := PayoutFailureMessage("ACCOUNT_NAME_MISMATCH")
	assert.Contains(t, msg, "Nama pemilik rekening")
	assert.Contains(t, msg, "Perbarui")
}
