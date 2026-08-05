package domain

import "time"

// Transaction represents a payment transaction.
type Transaction struct {
	ID           string    `json:"id"`
	UserID       string    `json:"user_id"`
	BookingID    string    `json:"booking_id"`
	EventID      string    `json:"event_id"`
	AmountCents  int       `json:"amount_cents"`
	Currency     string    `json:"currency"`
	Status       string    `json:"status"`
	Provider     string    `json:"provider"`
	ProviderRef  string    `json:"provider_ref"`
	RefundStatus string    `json:"refund_status"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

// PaymentStatus constants.
const (
	StatusPending = "pending"
	StatusSuccess = "success"
	StatusExpired = "expired"

	// Deprecated aliases kept while API consumers transition to the target vocabulary.
	StatusInitiated  = StatusPending
	StatusProcessing = StatusPending
	StatusCompleted  = StatusSuccess
	StatusFailed     = StatusExpired
)
