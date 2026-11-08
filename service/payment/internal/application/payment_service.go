package application

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/unedtamps/ticketbus/service/payment/internal/domain"
)

// PaymentService orchestrates payment operations.
type PaymentService struct {
	txnRepo         domain.TransactionRepository
	refundRepo      domain.RefundRepository
	destRepo        domain.RefundDestinationRepository
	payoutRepo      domain.PayoutRepository
	processor       domain.PaymentProcessor
	consumer        domain.EventConsumer
	logger          *slog.Logger
	provider        string
	allowedChannels []string
}

// NewPaymentService creates a new PaymentService.
func NewPaymentService(
	txnRepo domain.TransactionRepository,
	refundRepo domain.RefundRepository,
	destRepo domain.RefundDestinationRepository,
	payoutRepo domain.PayoutRepository,
	processor domain.PaymentProcessor,
	consumer domain.EventConsumer,
	logger *slog.Logger,
	provider string,
	allowedChannels []string,
) *PaymentService {
	return &PaymentService{
		txnRepo:         txnRepo,
		refundRepo:      refundRepo,
		destRepo:        destRepo,
		payoutRepo:      payoutRepo,
		processor:       processor,
		consumer:        consumer,
		logger:          logger,
		provider:        provider,
		allowedChannels: allowedChannels,
	}
}

// RefundAccount returns the caller's registered refund destination, or nil
// when none was set yet.
func (s *PaymentService) RefundAccount(ctx context.Context, userID string) (*domain.RefundDestination, error) {
	dest, err := s.destRepo.FindByUserID(ctx, userID)
	if errors.Is(err, domain.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return dest, nil
}

// SaveRefundAccount registers or replaces the caller's refund destination.
// VA and retail-outlet payments cannot refund to their source, so payouts are
// the one refund rail — this account is where they land.
func (s *PaymentService) SaveRefundAccount(
	ctx context.Context,
	userID, channelCode, accountNumber, accountHolderName string,
) error {
	if userID == "" || channelCode == "" || accountNumber == "" || accountHolderName == "" {
		return fmt.Errorf("%w: channel, account number, and holder name are required", domain.ErrInvalidInput)
	}
	return s.destRepo.Upsert(ctx, &domain.RefundDestination{
		UserID:            userID,
		ChannelCode:       channelCode,
		AccountNumber:     accountNumber,
		AccountHolderName: accountHolderName,
	})
}

// ListRefunds returns refunds for the admin refund table, newest first.
func (s *PaymentService) ListRefunds(
	ctx context.Context,
	status string,
	limit, offset int,
) ([]*domain.RefundRequest, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	return s.refundRepo.ListAll(ctx, status, limit, offset)
}

// ListMyRefunds returns the caller's own refunds — the customer-facing view.
// Payment owns this state outright: there is no mirror in ticketing, so what
// the buyer sees is always the executor's live ledger.
func (s *PaymentService) ListMyRefunds(
	ctx context.Context,
	userID string,
	limit, offset int,
) ([]*domain.CustomerRefund, error) {
	if userID == "" {
		return nil, domain.ErrInvalidInput
	}
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	return s.refundRepo.ListByCustomer(ctx, userID, limit, offset)
}

// RetryRefund puts a failed (or stuck processing) refund back in the pending
// queue and bumps its attempt, so the executor's next sweep pays it out again
// under a fresh gateway idempotency key.
func (s *PaymentService) RetryRefund(ctx context.Context, refundID string) error {
	applied, err := s.refundRepo.ResetForRetry(ctx, refundID)
	if err != nil {
		return err
	}
	if !applied {
		return domain.ErrInvalidInput
	}
	return nil
}
