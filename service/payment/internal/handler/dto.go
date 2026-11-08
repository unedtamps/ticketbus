package handler

import "time"

// TransactionResponse is the public transaction data.
type TransactionResponse struct {
	ID             string `json:"id"`
	BookingID      string `json:"booking_id"`
	EventID        string `json:"event_id"`
	AmountRupiah   int    `json:"amount_rupiah"`
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
	BookingID    string    `json:"booking_id"`
	EventID      string    `json:"event_id"`
	UserID       string    `json:"user_id"`
	Email        string    `json:"email"`
	AmountRupiah int       `json:"amount_rupiah"`
	ExpiresAt    time.Time `json:"expires_at"`
}

// InternalCreateResponse confirms the transaction exists. created=true when
// this call inserted the row; created=false when a transaction for the same
// booking already existed (idempotent success — safe to retry).
type InternalCreateResponse struct {
	TransactionID string `json:"transaction_id"`
	BookingID     string `json:"booking_id"`
	Created       bool   `json:"created"`
}

// ProcessPaymentResponse returns the gateway session details to render/redirect.
type ProcessPaymentResponse struct {
	TransactionID    string `json:"transaction_id"`
	PaymentSessionID string `json:"payment_session_id"`
	PaymentLinkURL   string `json:"payment_link_url"`
	AmountRupiah     int    `json:"amount_rupiah"`
	Currency         string `json:"currency"`
	Status           string `json:"status"`
	ExpiresAt        string `json:"expires_at"`
}

// PaymentStatusResponse returns the payment status for a booking.
type PaymentStatusResponse struct {
	TransactionID  string `json:"transaction_id"`
	Status         string `json:"status"`
	PaymentLinkURL string `json:"payment_link_url,omitempty"`
	AmountRupiah   int    `json:"amount_rupiah"`
	Currency       string `json:"currency"`
}

// RefundAccountRequest is the payload for PUT /api/refund-account.
type RefundAccountRequest struct {
	ChannelCode       string `json:"channel_code"`
	AccountNumber     string `json:"account_number"`
	AccountHolderName string `json:"account_holder_name"`
}

// RefundAccountResponse is the registered refund destination.
type RefundAccountResponse struct {
	ChannelCode       string `json:"channel_code"`
	AccountNumber     string `json:"account_number"`
	AccountHolderName string `json:"account_holder_name"`
}

// RefundAdminResponse is one row of the admin refund table.
type RefundAdminResponse struct {
	ID            string `json:"id"`
	BookingID     string `json:"booking_id"`
	EventID       string `json:"event_id"`
	CustomerEmail string `json:"customer_email"`
	AmountRupiah  int64  `json:"amount_rupiah"`
	Currency      string `json:"currency"`
	Status        string `json:"status"`
	Reason        string `json:"reason"`
	ProviderRef    string `json:"provider_ref,omitempty"`
	FailureReason  string `json:"failure_reason,omitempty"`
	HasDestination bool   `json:"has_destination"`
	CreatedAt      string `json:"created_at"`
	ProcessedAt    string `json:"processed_at,omitempty"`
}

// CustomerRefundResponse is one row of the customer's own refund list. It
// carries no gateway internals: no provider_ref, no raw failure code, and no
// internal reason enum. Status is one of pending/processing/succeeded/failed
// and FailureReason is already a customer-facing sentence.
type CustomerRefundResponse struct {
	ID            string `json:"id"`
	BookingID     string `json:"booking_id"`
	EventID       string `json:"event_id"`
	AmountRupiah  int64  `json:"amount_rupiah"`
	Currency      string `json:"currency"`
	Status        string `json:"status"`
	FailureReason string `json:"failure_reason,omitempty"`
	ChannelCode   string `json:"channel_code,omitempty"`
	AccountNumber string `json:"account_number,omitempty"`
	HasAccount    bool   `json:"has_account"`
	CreatedAt     string `json:"created_at"`
	ProcessedAt   string `json:"processed_at,omitempty"`
}
