package application

import (
	"log/slog"

	"github.com/nedo/TicketSaas/internal/payment/domain"
	"github.com/nedo/TicketSaas/internal/shared/outbox"
)

// PaymentService orchestrates payment operations.
type PaymentService struct {
	txnRepo                domain.TransactionRepository
	refundRepo             domain.RefundRepository
	processor              domain.PaymentProcessor
	consumer               domain.EventConsumer
	outbox                 outbox.StoreInterface
	logger                 *slog.Logger
	provider               string
	gatewayExpiryBufferMin int
	allowedChannels        []string
}

// NewPaymentService creates a new PaymentService.
func NewPaymentService(
	txnRepo domain.TransactionRepository,
	refundRepo domain.RefundRepository,
	processor domain.PaymentProcessor,
	consumer domain.EventConsumer,
	ob outbox.StoreInterface,
	logger *slog.Logger,
	provider string,
	gatewayExpiryBufferMin int,
	allowedChannels []string,
) *PaymentService {
	return &PaymentService{
		txnRepo:                txnRepo,
		refundRepo:             refundRepo,
		processor:              processor,
		consumer:               consumer,
		outbox:                 ob,
		logger:                 logger,
		provider:               provider,
		gatewayExpiryBufferMin: gatewayExpiryBufferMin,
		allowedChannels:        allowedChannels,
	}
}
