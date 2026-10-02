package handler

import "time"

// TransactionResponse is the public transaction data.
type TransactionResponse struct {
	ID             string `json:"id"`
	BookingID      string `json:"booking_id"`
	EventID        string `json:"event_id"`
	AmountRupiah    int    `json:"amount_rupiah"`
	Currency       string `json:"currency"`
	Status         string `json:"status"`
	RefundStatus   string `json:"refund_status,omitempty"`
	PaymentLinkURL string `json:"payment_link_url,omitempty"`
	ExpiresAt      string `json:"expires_at,omitempty"`
	CreatedAt      string `json:"created_at"`
}

// InternalCreateRequest is the internal (service-to-service) payload for
// creating a transaction during reserve.
type InternalCreateRequest struct {
	BookingID   string    `json:"booking_id"`
	EventID     string    `json:"event_id"`
	UserID      string    `json:"user_id"`
	Email       string    `json:"email"`
	AmountRupiah int       `json:"amount_rupiah"`
	ExpiresAt   time.Time `json:"expires_at"`
}

// InternalCreateResponse confirms the transaction was created.
type InternalCreateResponse struct {
	TransactionID string `json:"transaction_id"`
	BookingID     string `json:"booking_id"`
}

// ProcessPaymentResponse returns the gateway session details to render/redirect.
type ProcessPaymentResponse struct {
	TransactionID    string `json:"transaction_id"`
	PaymentSessionID string `json:"payment_session_id"`
	PaymentLinkURL   string `json:"payment_link_url"`
	AmountRupiah      int    `json:"amount_rupiah"`
	Currency         string `json:"currency"`
	Status           string `json:"status"`
	ExpiresAt        string `json:"expires_at"`
}

// PaymentStatusResponse returns the payment status for a booking.
type PaymentStatusResponse struct {
	TransactionID  string `json:"transaction_id"`
	Status         string `json:"status"`
	PaymentLinkURL string `json:"payment_link_url,omitempty"`
	AmountRupiah    int    `json:"amount_rupiah"`
	Currency       string `json:"currency"`
}
