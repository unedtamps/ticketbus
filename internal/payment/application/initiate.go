package application

import (
	"context"
	"time"

	"github.com/nedo/TicketSaas/internal/payment/domain"
)

// InitiatePayment creates the gateway payment session for a booking. Allowed
// only while at least one minute of the booking hold remains. Amount and
// currency come from the stored transaction, never from the request.
func (s *PaymentService) InitiatePayment(
	ctx context.Context,
	bookingID, userID string,
) (*domain.Transaction, *domain.SessionResult, error) {
	txn, err := s.txnRepo.FindByBookingID(ctx, bookingID)
	if err != nil {
		return nil, nil, domain.ErrTransactionNotFound
	}
	if txn.Status != domain.StatusPending {
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
	result, err := s.processor.CreateSession(
		ctx,
		bookingID,
		txn.AmountCents,
		txn.Currency,
		gatewayExpiresAt,
		s.allowedChannels,
		txn.CustomerEmail,
	)
	if err != nil {
		return nil, nil, err
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
