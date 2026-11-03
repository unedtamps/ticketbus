package application

import (
	"log/slog"

	"github.com/unedtamps/ticketbus/service/payment/internal/domain"
)

// PaymentService orchestrates payment operations.
type PaymentService struct {
	txnRepo         domain.TransactionRepository
	refundRepo      domain.RefundRepository
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
		payoutRepo:      payoutRepo,
		processor:       processor,
		consumer:        consumer,
		logger:          logger,
		provider:        provider,
		allowedChannels: allowedChannels,
	}
}
