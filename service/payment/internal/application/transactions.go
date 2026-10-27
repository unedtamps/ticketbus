package application

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/unedtamps/ticketbus/service/payment/internal/domain"
)

// InitiateTxnForBooking creates a pending transaction for a booking. Called
// by the ticketing service's REST dispatcher during reserve (asynchronously,
// after the reservation commits), so the transaction row always exists before
// any gateway activity. Idempotent per booking: a duplicate request reports
// created=false and returns the existing transaction untouched.
func (s *PaymentService) InitiateTxnForBooking(
	ctx context.Context,
	bookingID, eventID, userID, email string,
	amountRupiah int,
	expiresAt time.Time,
) (*domain.Transaction, bool, error) {
	txn := &domain.Transaction{
		ID:            uuid.NewString(),
		UserID:        userID,
		BookingID:     bookingID,
		EventID:       eventID,
		AmountRupiah:  amountRupiah,
		Currency:      "IDR",
		Status:        domain.StatusInitiated,
		Provider:      s.provider,
		CustomerEmail: email,
		RefundStatus:  "none",
		ExpiresAt:     &expiresAt,
	}
	if err := s.txnRepo.Create(ctx, txn); err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			// booking_id is UNIQUE: another dispatch for the same booking won
			// the race. Return the existing transaction as-is — its status,
			// session reference and expiry are never overwritten.
			existing, ferr := s.txnRepo.FindByBookingID(ctx, bookingID)
			if ferr != nil {
				return nil, false, ferr
			}
			return existing, false, nil
		}
		return nil, false, err
	}
	return txn, true, nil
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

// ProcessPayment creates the gateway payment session for a booking and moves
// the transaction from initiated to pending. Allowed only while at least one
// minute of the booking hold remains and the transaction is still initiated.
// Amount and currency come from the stored transaction, never from the
// request.
func (s *PaymentService) ProcessPayment(
	ctx context.Context,
	bookingID, userID string,
) (*domain.Transaction, *domain.SessionResult, error) {
	txn, err := s.txnRepo.FindByBookingID(ctx, bookingID)
	if err != nil {
		return nil, nil, domain.ErrTransactionNotFound
	}
	if txn.Status != domain.StatusInitiated {
		return nil, nil, domain.ErrAlreadyProcessed
	}
	if txn.ExpiresAt == nil || !txn.ExpiresAt.After(time.Now()) ||
		time.Until(*txn.ExpiresAt) < time.Minute {
		return nil, nil, domain.ErrTransactionExpired
	}
	if txn.UserID != userID {
		return nil, nil, domain.ErrTransactionNotFound
	}

	gatewayExpiresAt := txn.ExpiresAt.Add(-time.Duration(s.gatewayExpiryBufferMin) * time.Minute)
	if minGatewayExpiry := time.Now().Add(10 * time.Minute); gatewayExpiresAt.Before(minGatewayExpiry) {
		// Xendit requires the session expiry to be at least 10 minutes in the
		// future; a late retry of the same booking would otherwise be rejected.
		gatewayExpiresAt = minGatewayExpiry
	}
	result, err := s.processor.CreateSession(
		ctx,
		bookingID,
		txn.AmountRupiah,
		txn.Currency,
		gatewayExpiresAt,
		s.allowedChannels,
		txn.CustomerEmail,
	)
	if err != nil {
		return nil, nil, fmt.Errorf("%w: %v", domain.ErrGatewayUnavailable, err)
	}

	// Transition initiated → pending. Guarded on initiated only, so a
	// concurrent request cannot overwrite an existing session reference. If
	// another path settled the transaction first, void the session we just
	// created and report the conflict.
	applied, err := s.txnRepo.TransitionIfInitiated(
		ctx,
		txn.ID,
		domain.StatusPending,
		result.ProviderRef,
	)
	if err != nil {
		return nil, nil, err
	}
	if !applied {
		_ = s.processor.CancelSession(ctx, result.ProviderRef)
		return nil, nil, domain.ErrAlreadyProcessed
	}

	if err := s.txnRepo.UpdateSession(
		ctx,
		txn.ID,
		result.ProviderRef,
		result.PaymentLinkURL,
	); err != nil {
		return nil, nil, err
	}

	s.logger.Info(
		"payment session initiated",
		"booking_id", bookingID,
		"session_id", result.ProviderRef,
	)
	return txn, result, nil
}
