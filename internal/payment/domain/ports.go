package domain

import "context"

// TransactionRepository defines the contract for transaction persistence.
type TransactionRepository interface {
	Create(ctx context.Context, txn *Transaction) error
	FindByID(ctx context.Context, id string) (*Transaction, error)
	FindByBookingID(ctx context.Context, bookingID string) (*Transaction, error)
	UpdateStatus(ctx context.Context, id, status, providerRef string) error
	ListByUser(ctx context.Context, userID string) ([]Transaction, error)
}

// PaymentProcessor defines the contract for external payment processing.
type PaymentProcessor interface {
	Charge(ctx context.Context, txnID string, amountCents int, currency string) (string, error)
}

// EventPublisher defines the contract for publishing payment events.
type EventPublisher interface {
	PublishPaymentInitiated(ctx context.Context, txn *Transaction) error
	PublishPaymentCompleted(ctx context.Context, txn *Transaction) error
	PublishPaymentFailed(ctx context.Context, txn *Transaction, reason string) error
}

// EventConsumer defines the contract for consuming reservation events.
type EventConsumer interface {
	OnReservationCreated(ctx context.Context, fn func(ctx context.Context, bookingID string, amountCents int, userID string) error)
	OnReservationExpired(ctx context.Context, fn func(ctx context.Context, bookingID string) error)
	Start(ctx context.Context) error
	Close() error
}
