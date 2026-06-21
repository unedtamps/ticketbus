package domain

import "time"

// Transaction represents a payment transaction.
type Transaction struct {
	ID          string    `json:"id"`
	UserID      string    `json:"user_id"`
	BookingID   string    `json:"booking_id"`
	AmountCents int       `json:"amount_cents"`
	Currency    string    `json:"currency"`
	Status      string    `json:"status"`
	Provider    string    `json:"provider"`
	ProviderRef string    `json:"provider_ref"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// PaymentStatus constants.
const (
	StatusInitiated  = "initiated"
	StatusProcessing = "processing"
	StatusCompleted  = "completed"
	StatusFailed     = "failed"
)
