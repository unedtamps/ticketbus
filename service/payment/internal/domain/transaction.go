package domain

import "time"

// Transaction represents a payment transaction.
type Transaction struct {
	ID             string     `json:"id"`
	UserID         string     `json:"user_id"`
	BookingID      string     `json:"booking_id"`
	EventID        string     `json:"event_id"`
	AmountRupiah    int        `json:"amount_rupiah"`
	Currency       string     `json:"currency"`
	Status         string     `json:"status"`
	Provider       string     `json:"provider"`
	ProviderRef    string     `json:"provider_ref"`
	PaymentLinkURL string     `json:"payment_link_url"`
	CustomerEmail  string     `json:"customer_email"`
	RefundStatus   string     `json:"refund_status"`
	ExpiresAt      *time.Time `json:"expires_at,omitempty"`
	CreatedAt      time.Time  `json:"created_at"`
	UpdatedAt      time.Time  `json:"updated_at"`
}

// PaymentStatus constants.
const (
	StatusInitiated = "initiated"
	StatusPending   = "pending"
	StatusCompleted = "completed"
	StatusExpired   = "expired"
)

// GatewayExpiredReason is used on payment.expired events published because the
// gateway expired the payment session.
const GatewayExpiredReason = "gateway_expired"

// ActiveStatuses returns the non-terminal transaction statuses (initiated and
// pending). Terminal statuses are completed and expired.
func ActiveStatuses() []string {
	return []string{StatusInitiated, StatusPending}
}
