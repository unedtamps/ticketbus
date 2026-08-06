package application

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/nedo/TicketSaas/internal/payment/domain"
)

// CreateTxnForBooking creates a pending transaction for a booking. Called
// synchronously by the ticketing service during reserve, so the transaction
// row always exists before any gateway activity. Idempotent per booking.
func (s *PaymentService) CreateTxnForBooking(
	ctx context.Context,
	bookingID, eventID, userID, email string,
	amountCents int,
	expiresAt time.Time,
) (*domain.Transaction, error) {
	txn := &domain.Transaction{
		ID:            uuid.NewString(),
		UserID:        userID,
		BookingID:     bookingID,
		EventID:       eventID,
		AmountCents:   amountCents,
		Currency:      "IDR",
		Status:        domain.StatusPending,
		Provider:      s.provider,
		CustomerEmail: email,
		RefundStatus:  "none",
		ExpiresAt:     &expiresAt,
	}
	if err := s.txnRepo.Create(ctx, txn); err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			existing, ferr := s.txnRepo.FindByBookingID(ctx, bookingID)
			if ferr != nil {
				return nil, ferr
			}
			return existing, nil
		}
		return nil, err
	}
	return txn, nil
}

// GetPaymentStatus returns the transaction (including the stored payment
// link) for a booking, so the customer can re-open the checkout page.
func (s *PaymentService) GetPaymentStatus(
	ctx context.Context,
	bookingID string,
) (*domain.Transaction, error) {
	txn, err := s.txnRepo.FindByBookingID(ctx, bookingID)
	if err != nil {
		return nil, domain.ErrTransactionNotFound
	}
	return txn, nil
}

// GetTransaction returns a transaction by ID.
func (s *PaymentService) GetTransaction(
	ctx context.Context,
	txnID string,
) (*domain.Transaction, error) {
	return s.txnRepo.FindByID(ctx, txnID)
}

// ListMyTransactions returns transactions for a user.
func (s *PaymentService) ListMyTransactions(
	ctx context.Context,
	userID string,
) ([]domain.Transaction, error) {
	return s.txnRepo.ListByUser(ctx, userID)
}
