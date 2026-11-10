package domain

import (
	"strings"
	"time"
)

// RefundRequest represents a refund to be executed against a successful
// payment transaction (e.g. after an event cancellation).
type RefundRequest struct {
	ID             string     `json:"id"`
	EventID        string     `json:"event_id"`
	BookingID      string     `json:"booking_id"`
	TransactionID  string     `json:"transaction_id"`
	CustomerEmail  string     `json:"customer_email"`
	AmountRupiah   int64      `json:"amount_rupiah"`
	Currency       string     `json:"currency"`
	Status         string     `json:"status"`
	Reason         string     `json:"reason"`
	ProviderRef    string     `json:"provider_ref"`
	IdempotencyKey string     `json:"idempotency_key"`
	CreatedAt      time.Time  `json:"created_at"`
	UpdatedAt      time.Time  `json:"updated_at"`
	ProcessedAt    *time.Time `json:"processed_at,omitempty"`

	// HasDestination reports whether the customer registered a refund account.
	// Payouts cannot succeed without one, so the admin UI disables retry until
	// it exists.
	HasDestination bool   `json:"has_destination"`
	FailureReason  string `json:"failure_reason,omitempty"`
}

// CustomerRefund is one refund as its owner sees it: the outcome plus the
// destination it was sent to. Destination fields are empty until the customer
// registers a refund account.
type CustomerRefund struct {
	ID            string     `json:"id"`
	BookingID     string     `json:"booking_id"`
	EventID       string     `json:"event_id"`
	AmountRupiah  int64      `json:"amount_rupiah"`
	Currency      string     `json:"currency"`
	Status        string     `json:"status"`
	Reason        string     `json:"reason"`
	FailureReason string     `json:"failure_reason,omitempty"`
	ChannelCode   string     `json:"channel_code,omitempty"`
	AccountNumber string     `json:"account_number,omitempty"`
	HasAccount    bool       `json:"has_account"`
	CreatedAt     time.Time  `json:"created_at"`
	ProcessedAt   *time.Time `json:"processed_at,omitempty"`
}

// RefundStatus constants.
const (
	RefundPending    = "pending"
	RefundProcessing = "processing"
	RefundSucceeded  = "succeeded"
	RefundFailed     = "failed"
)

// Gateway payout statuses that settle a refund. Everything else is still in
// flight and must keep waiting for the webhook or the reconciliation poll.
const (
	gatewayRefundSucceeded = "SUCCEEDED"
	gatewayRefundCompleted = "COMPLETED"
	gatewayRefundFailed    = "FAILED"
	gatewayRefundReversed  = "REVERSED"
	gatewayRefundRejected  = "REJECTED"
	gatewayRefundCancelled = "CANCELLED"
	gatewayRefundExpired   = "EXPIRED"
)

// RefundOutcome maps a gateway payout callback to the refund outcome to record.
// status is RefundSucceeded or RefundFailed when terminal is true, and empty
// otherwise. reason is a customer-facing Indonesian explanation, never a raw
// gateway code: the same gateway status can mean the customer gave bad details
// or that the platform ran out of balance, and only the first is actionable by
// the buyer.
//
// Every terminal gateway status is mapped. Missing one would strand the refund
// in processing: the sweep only claims pending rows, the reconciliation poll
// only settles terminal statuses, and the admin can only retry.
func RefundOutcome(gatewayStatus, failureCode string) (status, reason string, terminal bool) {
	switch strings.ToUpper(strings.TrimSpace(gatewayStatus)) {
	case gatewayRefundSucceeded, gatewayRefundCompleted:
		return RefundSucceeded, "", true
	case gatewayRefundFailed:
		return RefundFailed, RefundFailureMessage(failureCode), true
	case gatewayRefundReversed:
		// Funds bounced back after landing: the customer did not keep them.
		return RefundFailed, "Dana refund ditarik kembali oleh bank tujuan. Tim kami sedang menindaklanjuti.", true
	case gatewayRefundRejected:
		return RefundFailed, "Refund ditolak oleh penyedia pembayaran.", true
	case gatewayRefundCancelled:
		return RefundFailed, "Refund dibatalkan sebelum sempat diproses.", true
	case gatewayRefundExpired:
		return RefundFailed, "Refund kedaluwarsa sebelum sempat diproses.", true
	default:
		// ACCEPTED, ROUTING, REQUESTED, READY, LOCKED, PENDING,
		// PENDING_COMPLIANCE_ASSESSMENT, or an unroutable status: still moving.
		return "", "", false
	}
}

// RefundFailureMessage turns a gateway failure_code into a sentence the buyer
// can act on. Codes that are the platform's fault are worded so the customer
// does not go looking for a problem in their own account.
//
// The wording never promises an automatic retry: nothing re-queues a failed
// refund, so a "we will resend it" message would leave the customer waiting
// for something that never happens. Operations is what moves it forward.
func RefundFailureMessage(failureCode string) string {
	switch strings.ToUpper(strings.TrimSpace(failureCode)) {
	case "ACCOUNT_NAME_MISMATCH":
		return "Nama pemilik rekening tidak cocok dengan data bank. Perbarui nama sesuai rekening."
	case "INVALID_DESTINATION":
		return "Nomor rekening atau e-wallet tujuan tidak valid."
	case "SLS_RECIPIENT":
		return "Rekening tujuan tidak dapat menerima transfer."
	case "DESTINATION_MAXIMUM_LIMIT":
		return "Nominal refund melebihi batas maksimum rekening tujuan."
	case "REJECTED_BY_CHANNEL":
		return "Bank atau e-wallet tujuan menolak transfer ini."
	case "INSUFFICIENT_BALANCE":
		return "Refund tertunda karena kendala saldo di sisi kami. Tim kami sedang menindaklanjuti."
	case "SLS_SENDER":
		return "Refund tertunda karena kendala pada rekening pengirim. Tim kami sedang menindaklanjuti."
	case "TEMPORARY_TRANSFER_ERROR", "TRANSFER_ERROR":
		return "Refund tertunda karena gangguan sementara pada transfer. Tim kami sedang menindaklanjuti."
	case "":
		return "Refund gagal diproses oleh penyedia pembayaran."
	default:
		// Unknown code: keep it out of the customer's view, the raw value is
		// still logged for operations.
		return "Refund gagal diproses oleh penyedia pembayaran."
	}
}
