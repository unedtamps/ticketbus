package handler

import "time"

// TransactionResponse is the public transaction data.
type TransactionResponse struct {
	ID           string `json:"id"`
	BookingID    string `json:"booking_id"`
	EventID      string `json:"event_id"`
	AmountCents  int    `json:"amount_cents"`
	Currency     string `json:"currency"`
	Status       string `json:"status"`
	RefundStatus string `json:"refund_status,omitempty"`
	CreatedAt    string `json:"created_at"`
}

// InternalCreateRequest is the internal (service-to-service) payload for
// creating a transaction during reserve.
type InternalCreateRequest struct {
	BookingID   string    `json:"booking_id"`
	EventID     string    `json:"event_id"`
	UserID      string    `json:"user_id"`
	Email       string    `json:"email"`
	AmountCents int       `json:"amount_cents"`
	ExpiresAt   time.Time `json:"expires_at"`
}

// InternalCreateResponse confirms the transaction was created.
type InternalCreateResponse struct {
	TransactionID string `json:"transaction_id"`
	BookingID     string `json:"booking_id"`
}

// InitiateResponse returns the gateway session details to render/redirect.
type InitiateResponse struct {
	TransactionID   string `json:"transaction_id"`
	PaymentSessionID string `json:"payment_session_id"`
	PaymentLinkURL  string `json:"payment_link_url"`
	AmountCents     int    `json:"amount_cents"`
	Currency        string `json:"currency"`
	Status          string `json:"status"`
	ExpiresAt       string `json:"expires_at"`
}

// PaymentStatusResponse returns the payment status for a booking.
type PaymentStatusResponse struct {
	TransactionID  string `json:"transaction_id"`
	Status         string `json:"status"`
	PaymentLinkURL string `json:"payment_link_url,omitempty"`
	AmountCents    int    `json:"amount_cents"`
	Currency       string `json:"currency"`
}
