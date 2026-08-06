package domain

import (
	"context"
	"time"
)

// TransactionRepository defines the contract for transaction persistence.
type TransactionRepository interface {
	Create(ctx context.Context, txn *Transaction) error
	FindByID(ctx context.Context, id string) (*Transaction, error)
	FindByBookingID(ctx context.Context, bookingID string) (*Transaction, error)
	UpdateStatus(ctx context.Context, id, status, providerRef string) error
	// UpdateStatusIfPending transitions the transaction only if it is still
	// pending, guarding concurrent paths (webhook vs expiry poll) atomically.
	UpdateStatusIfPending(ctx context.Context, id, status, providerRef string) (bool, error)
	// UpdateSession stores the gateway payment session details after the
	// customer initiates a payment for the booking.
	UpdateSession(ctx context.Context, id, providerRef, paymentLinkURL string) error
	UpdateRefundStatus(ctx context.Context, id, refundStatus string) error
	ListByUser(ctx context.Context, userID string) ([]Transaction, error)
	ListByEventID(ctx context.Context, eventID string) ([]Transaction, error)
	// ListPendingExpired returns pending transactions whose deadline has
	// passed, for the expiry fallback poller.
	ListPendingExpired(ctx context.Context, now time.Time, limit int) ([]Transaction, error)
}

// RefundRepository defines the contract for refund request persistence.
type RefundRepository interface {
	Create(ctx context.Context, refund *RefundRequest) error
}

// SessionResult is the normalized outcome of creating or fetching a gateway
// payment session.
type SessionResult struct {
	ProviderRef    string    `json:"provider_ref"`
	PaymentLinkURL string    `json:"payment_link_url"`
	ExpiresAt      time.Time `json:"expires_at"`
}

// PaymentProcessor defines the contract for external payment processing.
type PaymentProcessor interface {
	// CreateSession creates a payment session at the gateway. refID is the
	// caller's idempotency key (the booking ID); email is the customer email.
	CreateSession(ctx context.Context, refID string, amountCents int, currency string, expiresAt time.Time, allowedChannels []string, email string) (*SessionResult, error)
	// CancelSession closes/expires an existing payment session.
	CancelSession(ctx context.Context, providerRef string) error
	// GetSession fetches a payment session.
	GetSession(ctx context.Context, providerRef string) (*SessionResult, error)
}

// EventConsumer defines the contract for consuming reservation events.
type EventConsumer interface {
	OnEventCancelled(ctx context.Context, fn func(ctx context.Context, eventID string) error)
	Start(ctx context.Context) error
	Close() error
}
