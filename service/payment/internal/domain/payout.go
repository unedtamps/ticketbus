package domain

import (
	"errors"
	"strings"
	"time"
)

// Payout is the payment service's ledger row for one payout attempt. An event
// may have several attempts (a retry after a failure, or a top-up collecting
// transactions that completed after an earlier payout); UNIQUE (event_id,
// attempt) makes the Kafka delivery of one attempt idempotent.
type Payout struct {
	ID            string    `json:"id"`
	EventID       string    `json:"event_id"`
	Attempt       int       `json:"attempt"`
	OrganizerID   string    `json:"organizer_id"`
	AmountRupiah  int64     `json:"amount_rupiah"`
	Currency      string    `json:"currency"`
	Status        string    `json:"status"`
	Provider      string    `json:"provider"`
	ProviderRef   string    `json:"provider_ref,omitempty"`
	FailureReason string    `json:"failure_reason,omitempty"`
	CreatedAt     time.Time `json:"created_at"`
	UpdatedAt     time.Time `json:"updated_at"`
}

// Payout statuses (payment ledger and transactions.payout_status).
const (
	PayoutStatusPending = "pending"
	PayoutStatusPaidOut = "paid_out"
	PayoutStatusFailed  = "failed"
)

// PayoutRequest is the normalized gateway payout request.
type PayoutRequest struct {
	ReferenceID       string
	IdempotencyKey    string
	AmountRupiah      int64
	Currency          string
	BankCode          string
	AccountNumber     string
	AccountHolderName string
	Description       string
}

// PayoutResult is the normalized gateway payout outcome.
type PayoutResult struct {
	ProviderRef string
	Status      string
	FailureCode string
}

// Gateway payout statuses (Xendit Payouts API v2). Statuses outside this set
// are treated as non-terminal: an unrecognized status never finalizes a
// payout, it just keeps waiting.
// Gateway payout statuses (Xendit Payouts API v2). Statuses outside this set
// are treated as non-terminal: an unrecognized status never finalizes a
// payout, it just keeps waiting.
const (
	GatewayPayoutPending   = "PENDING"
	GatewayPayoutSucceeded = "SUCCEEDED"
	GatewayPayoutFailed    = "FAILED"
	GatewayPayoutReversed  = "REVERSED"
	// COMPLETED is not a documented v2 payout status; it is accepted
	// defensively (and by the mock) in case a rail reports it.
	GatewayPayoutCompleted = "COMPLETED"
)

// PayoutConfirmation maps a gateway payout status to the ledger outcome to
// record. A payout is final only on a terminal gateway status; PENDING and
// unknown statuses keep waiting for the gateway webhook or the dispatcher's
// reconciliation poll. REVERSED (funds bounced back after a success) records
// paid_out with a reason so the attempt terminates; the common case —
// REVERSED arriving after the ledger already went paid_out — is logged by the
// webhook handler instead.
func PayoutConfirmation(gatewayStatus, failureCode string) (status, reason string, terminal bool) {
	switch strings.ToUpper(strings.TrimSpace(gatewayStatus)) {
	case GatewayPayoutSucceeded, GatewayPayoutCompleted:
		return PayoutStatusPaidOut, "", true
	case GatewayPayoutReversed:
		return PayoutStatusPaidOut, "reversed_by_gateway", true
	case GatewayPayoutFailed:
		return PayoutStatusFailed, failureCode, true
	default:
		return "", "", false
	}
}

// PermanentProcessorError marks a gateway rejection that must not be retried
// (e.g. an invalid bank account). Dispatchers mark the payout failed instead of
// retrying forever.
type PermanentProcessorError struct {
	Err error
}

func (e *PermanentProcessorError) Error() string { return e.Err.Error() }
func (e *PermanentProcessorError) Unwrap() error { return e.Err }

// IsPermanentProcessorError reports whether err is a non-retryable gateway
// rejection (bad data).
func IsPermanentProcessorError(err error) bool {
	var target *PermanentProcessorError
	return errors.As(err, &target)
}

// AuthProcessorError marks an authentication or authorisation failure: the
// gateway rejected the credentials themselves (401/403), not the request
// payload. It is a configuration problem — a missing API key scope, for
// example — so the payout stays pending and is retried instead of failing the
// organizer's money.
type AuthProcessorError struct {
	Err error
}

func (e *AuthProcessorError) Error() string { return e.Err.Error() }
func (e *AuthProcessorError) Unwrap() error { return e.Err }

// IsAuthProcessorError reports whether err is a credentials problem.
func IsAuthProcessorError(err error) bool {
	var target *AuthProcessorError
	return errors.As(err, &target)
}

// SanitizeProcessorError shortens a gateway error for storage in a payout
// record that the organizer can read: the raw response body of a gateway
// error can be long and may echo internal detail.
func SanitizeProcessorError(err error) string {
	if err == nil {
		return ""
	}
	msg := err.Error()
	const max = 200
	if len(msg) > max {
		msg = msg[:max] + "…"
	}
	return msg
}
