package domain

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestRefundOutcome_SettlesEveryTerminalStatus locks the invariant that keeps
// refunds from stranding in processing: every gateway status that ends a payout
// must produce a terminal refund outcome. A missing case would leave the row
// unsettleable — the sweep only claims pending rows, the reconciliation poll
// only acts on terminal statuses, and admins can only retry.
func TestRefundOutcome_SettlesEveryTerminalStatus(t *testing.T) {
	cases := []struct {
		gatewayStatus string
		wantStatus    string
	}{
		{"SUCCEEDED", RefundSucceeded},
		{"succeeded", RefundSucceeded},
		{"COMPLETED", RefundSucceeded},
		{"FAILED", RefundFailed},
		{"REVERSED", RefundFailed},
		{"REJECTED", RefundFailed},
		{"CANCELLED", RefundFailed},
		{"EXPIRED", RefundFailed},
	}
	for _, tc := range cases {
		status, _, terminal := RefundOutcome(tc.gatewayStatus, "")
		assert.True(t, terminal, "%s must be terminal", tc.gatewayStatus)
		assert.Equal(t, tc.wantStatus, status, "%s maps to the wrong refund status", tc.gatewayStatus)
	}
}

// TestRefundOutcome_ReversedIsNotSuccess is the money-correctness case:
// REVERSED means the bank bounced the funds back to the platform, so the
// customer never kept the money and the refund cannot be reported as paid.
func TestRefundOutcome_ReversedIsNotSuccess(t *testing.T) {
	status, reason, terminal := RefundOutcome("REVERSED", "")
	assert.True(t, terminal)
	assert.Equal(t, RefundFailed, status)
	assert.NotEmpty(t, reason, "a reversed refund needs an explanation for the customer")
}

// TestRefundOutcome_InFlightStaysOpen covers statuses that are still moving.
// These must NOT be terminal, otherwise a payout that is merely routing would
// be closed as failed.
func TestRefundOutcome_InFlightStaysOpen(t *testing.T) {
	for _, s := range []string{
		"ACCEPTED", "PENDING", "ROUTING", "REQUESTED", "READY", "LOCKED",
		"PENDING_COMPLIANCE_ASSESSMENT", "", "SOMETHING_NEW",
	} {
		status, reason, terminal := RefundOutcome(s, "")
		assert.False(t, terminal, "%q must stay open", s)
		assert.Empty(t, status)
		assert.Empty(t, reason)
	}
}

// TestFailureMessage_CoversEveryGatewayCode asserts every documented
// Xendit failure_code produces a sentence, and that the result is Indonesian
// prose rather than the raw enum (a customer must never read
// ACCOUNT_NAME_MISMATCH).
func TestFailureMessage_CoversEveryGatewayCode(t *testing.T) {
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
		msg := FailureMessage(code)
		assert.NotEmpty(t, msg, "%s needs a message", code)
		assert.NotEqual(t, code, msg, "%s must not leak the raw gateway code", code)
		assert.Contains(t, msg, " ", "%s should be a sentence", code)
	}
}

// TestFailureMessage_PlatformFaultDoesNotBlameCustomer: when the platform
// is out of balance or the sender account is broken, the customer must not be
// told to fix their own account.
func TestFailureMessage_PlatformFaultDoesNotBlameCustomer(t *testing.T) {
	for _, code := range []string{"INSUFFICIENT_BALANCE", "SLS_SENDER", "TEMPORARY_TRANSFER_ERROR", "TRANSFER_ERROR"} {
		msg := FailureMessage(code)
		assert.NotContains(t, msg, "Perbarui", "%s must not ask the customer to change their details", code)
		assert.NotContains(t, msg, "tidak valid", "%s must not imply the destination is invalid", code)
	}
}

// TestFailureMessage_NeverPromisesAnAutomaticRetry: nothing re-queues a
// failed refund — the sweep only claims pending rows and reconciliation only
// settles processing rows. A message saying the money will be resent would
// leave the customer waiting for something that never happens.
func TestFailureMessage_NeverPromisesAnAutomaticRetry(t *testing.T) {
	codes := []string{
		"INSUFFICIENT_BALANCE", "SLS_SENDER", "TEMPORARY_TRANSFER_ERROR",
		"TRANSFER_ERROR", "ACCOUNT_NAME_MISMATCH", "INVALID_DESTINATION",
		"SLS_RECIPIENT", "DESTINATION_MAXIMUM_LIMIT", "REJECTED_BY_CHANNEL", "",
	}
	for _, code := range codes {
		msg := FailureMessage(code)
		assert.NotContains(t, msg, "akan mengirimnya ulang",
			"%q: no automatic retry exists, so it must not be promised", code)
		assert.NotContains(t, msg, "akan kami kirim ulang",
			"%q: no automatic retry exists, so it must not be promised", code)
	}
}

// TestRefundOutcome_ReversedReasonIsForwardLooking: a reversed refund is
// settled as failed, but the customer also needs to know someone is on it.
func TestRefundOutcome_ReversedReasonIsForwardLooking(t *testing.T) {
	_, reason, terminal := RefundOutcome("REVERSED", "")
	assert.True(t, terminal)
	assert.Contains(t, reason, "ditarik kembali")
	assert.Contains(t, reason, "menindaklanjuti")
}

// TestFailureMessage_MismatchIsActionable: the one code the customer can
// actually fix must say so.
func TestFailureMessage_MismatchIsActionable(t *testing.T) {
	msg := FailureMessage("ACCOUNT_NAME_MISMATCH")
	assert.Contains(t, msg, "Nama pemilik rekening")
	assert.Contains(t, msg, "Perbarui")
}
