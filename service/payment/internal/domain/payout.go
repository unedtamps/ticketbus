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

// Gateway payout statuses (Xendit Payouts API v2). Only the statuses below
// settle an attempt; everything else is still in flight.
const (
	GatewayPayoutPending   = "PENDING"
	GatewayPayoutSucceeded = "SUCCEEDED"
	GatewayPayoutFailed    = "FAILED"
	GatewayPayoutReversed  = "REVERSED"
	GatewayPayoutRejected  = "REJECTED"
	GatewayPayoutCancelled = "CANCELLED"
	GatewayPayoutExpired   = "EXPIRED"
	// COMPLETED is not a documented v2 payout status; it is accepted
	// defensively (and by the mock) in case a rail reports it.
	GatewayPayoutCompleted = "COMPLETED"
)

// PayoutOutcome maps a gateway payout status to the ledger outcome to record.
// status is PayoutStatusPaidOut or PayoutStatusFailed when terminal is true,
// and empty otherwise; reason is organizer-facing Indonesian text, never a raw
// gateway code.
//
// Every terminal gateway status must be mapped. Missing one strands the
// attempt: the payout stays pending, ListPendingDispatched keeps returning it,
// GetPayout keeps reporting the same terminal status, and the attempt can
// never be retried (ResetForRetry only accepts failed/processing).
func PayoutOutcome(gatewayStatus, failureCode string) (status, reason string, terminal bool) {
	switch strings.ToUpper(strings.TrimSpace(gatewayStatus)) {
	case GatewayPayoutSucceeded, GatewayPayoutCompleted:
		return PayoutStatusPaidOut, "", true
	case GatewayPayoutFailed:
		return PayoutStatusFailed, PayoutFailureMessage(failureCode), true
	case GatewayPayoutReversed:
		// Funds bounced back after landing: the organizer never kept them, so
		// the attempt cannot be recorded as paid out.
		return PayoutStatusFailed, "Dana payout ditarik kembali oleh bank tujuan. Tim kami sedang menindaklanjuti.", true
	case GatewayPayoutRejected:
		return PayoutStatusFailed, "Payout ditolak oleh penyedia pembayaran.", true
	case GatewayPayoutCancelled:
		return PayoutStatusFailed, "Payout dibatalkan sebelum sempat diproses.", true
	case GatewayPayoutExpired:
		return PayoutStatusFailed, "Payout kedaluwarsa sebelum sempat diproses.", true
	default:
		// PENDING, ACCEPTED, ROUTING, REQUESTED, READY, LOCKED,
		// PENDING_COMPLIANCE_ASSESSMENT, or an unroutable status: still moving.
		return "", "", false
	}
}

// PayoutFailureMessage turns a gateway failure_code into a sentence the
// organizer can act on. The gateway enum is an internal detail, so it never
// reaches the ledger or the dashboard.
//
// The wording is payout-specific: a failed payout is not a refund, and the
// organizer should not be told to look for one. Codes that are the platform's
// fault are worded so the organizer does not go looking for a problem in their
// own account. Like refunds, nothing re-queues a failed payout automatically —
// an admin retry creates a new attempt — so the message never promises one.
func PayoutFailureMessage(failureCode string) string {
	switch strings.ToUpper(strings.TrimSpace(failureCode)) {
	case "ACCOUNT_NAME_MISMATCH":
		return "Nama pemilik rekening payout tidak cocok dengan data bank. Perbarui nama sesuai rekening."
	case "INVALID_DESTINATION":
		return "Nomor rekening atau e-wallet tujuan payout tidak valid."
	case "SLS_RECIPIENT":
		return "Rekening tujuan payout tidak dapat menerima transfer."
	case "DESTINATION_MAXIMUM_LIMIT":
		return "Nominal payout melebihi batas maksimum rekening tujuan."
	case "REJECTED_BY_CHANNEL":
		return "Bank atau e-wallet tujuan menolak transfer payout ini."
	case "INSUFFICIENT_BALANCE":
		return "Payout tertunda karena kendala saldo di sisi kami. Tim kami sedang menindaklanjuti."
	case "SLS_SENDER":
		return "Payout tertunda karena kendala pada rekening pengirim. Tim kami sedang menindaklanjuti."
	case "TEMPORARY_TRANSFER_ERROR", "TRANSFER_ERROR":
		return "Payout tertunda karena gangguan sementara pada transfer. Tim kami sedang menindaklanjuti."
	case "":
		return "Payout gagal diproses oleh penyedia pembayaran."
	default:
		// Unknown code: keep it out of the organizer's view.
		return "Payout gagal diproses oleh penyedia pembayaran."
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
