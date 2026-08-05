package fixtures

import (
	"time"

	"github.com/google/uuid"
	"github.com/nedo/TicketSaas/internal/payment/domain"
)

// TransactionOption is a functional option for NewTestTransaction.
type TransactionOption func(*domain.Transaction)

// WithTransactionID overrides the transaction ID.
func WithTransactionID(id string) TransactionOption {
	return func(tx *domain.Transaction) { tx.ID = id }
}

// WithTransactionUserID overrides the user ID.
func WithTransactionUserID(userID string) TransactionOption {
	return func(tx *domain.Transaction) { tx.UserID = userID }
}

// WithTransactionBookingID overrides the booking ID.
func WithTransactionBookingID(bookingID string) TransactionOption {
	return func(tx *domain.Transaction) { tx.BookingID = bookingID }
}

// WithTransactionAmount overrides the amount in cents.
func WithTransactionAmount(cents int) TransactionOption {
	return func(tx *domain.Transaction) { tx.AmountCents = cents }
}

// WithTransactionCurrency overrides the currency.
func WithTransactionCurrency(currency string) TransactionOption {
	return func(tx *domain.Transaction) { tx.Currency = currency }
}

// WithTransactionStatus overrides the status (default: initiated).
func WithTransactionStatus(status string) TransactionOption {
	return func(tx *domain.Transaction) { tx.Status = status }
}

// WithTransactionProvider overrides the provider.
func WithTransactionProvider(provider string) TransactionOption {
	return func(tx *domain.Transaction) { tx.Provider = provider }
}

// WithTransactionProviderRef overrides the provider ref.
func WithTransactionProviderRef(ref string) TransactionOption {
	return func(tx *domain.Transaction) { tx.ProviderRef = ref }
}

// WithTransactionExpiresAt overrides the transaction expiry.
func WithTransactionExpiresAt(t time.Time) TransactionOption {
	return func(tx *domain.Transaction) { tx.ExpiresAt = &t }
}

// WithTransactionPaymentLinkURL overrides the stored payment link.
func WithTransactionPaymentLinkURL(url string) TransactionOption {
	return func(tx *domain.Transaction) { tx.PaymentLinkURL = url }
}

// WithTransactionCustomerEmail overrides the customer email.
func WithTransactionCustomerEmail(email string) TransactionOption {
	return func(tx *domain.Transaction) { tx.CustomerEmail = email }
}

// NewTestTransaction creates a Transaction with sensible defaults.
func NewTestTransaction(opts ...TransactionOption) *domain.Transaction {
	now := time.Now().Truncate(time.Second)
	tx := &domain.Transaction{
		ID:          uuid.NewString(),
		UserID:      uuid.NewString(),
		BookingID:   uuid.NewString(),
		AmountCents: 10000,
		Currency:    "USD",
		Status:      domain.StatusPending,
		Provider:    "mock",
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	for _, o := range opts {
		o(tx)
	}
	return tx
}
