package domain

import "time"

// RefundRequest represents a refund to be executed against a successful
// payment transaction (e.g. after an event cancellation).
type RefundRequest struct {
	ID             string     `json:"id"`
	EventID        string     `json:"event_id"`
	BookingID      string     `json:"booking_id"`
	TransactionID  string     `json:"transaction_id"`
	CustomerEmail  string     `json:"customer_email"`
	AmountRupiah    int64      `json:"amount_rupiah"`
	Currency       string     `json:"currency"`
	Status         string     `json:"status"`
	Reason         string     `json:"reason"`
	ProviderRef    string     `json:"provider_ref"`
	IdempotencyKey string     `json:"idempotency_key"`
	CreatedAt      time.Time  `json:"created_at"`
	UpdatedAt      time.Time  `json:"updated_at"`
	ProcessedAt    *time.Time `json:"processed_at,omitempty"`
}

// RefundStatus constants.
const (
	RefundPending    = "pending"
	RefundProcessing = "processing"
	RefundSucceeded  = "succeeded"
	RefundFailed     = "failed"
)
