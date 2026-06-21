package kafka

import (
	"context"
	"time"

	"github.com/nedo/TicketSaas/internal/payment/domain"
	sharedkafka "github.com/nedo/TicketSaas/internal/shared/kafka"
	sdomain "github.com/nedo/TicketSaas/internal/shared/domain"
)

// PaymentProducer implements domain.EventPublisher.
type PaymentProducer struct {
	producer *sharedkafka.Producer
}

// NewPaymentProducer creates a new Kafka producer for payment events.
func NewPaymentProducer(brokers []string) *PaymentProducer {
	return &PaymentProducer{producer: sharedkafka.NewProducer(brokers)}
}

func (p *PaymentProducer) PublishPaymentInitiated(ctx context.Context, txn *domain.Transaction) error {
	return p.producer.Produce(ctx, "payment.initiated", txn.ID, sdomain.PaymentInitiated{
		TransactionID: txn.ID,
		BookingID:     txn.BookingID,
		UserID:        txn.UserID,
		AmountCents:   txn.AmountCents,
		At:            time.Now(),
	})
}

func (p *PaymentProducer) PublishPaymentCompleted(ctx context.Context, txn *domain.Transaction) error {
	return p.producer.Produce(ctx, "payment.completed", txn.ID, sdomain.PaymentCompleted{
		TransactionID: txn.ID,
		BookingID:     txn.BookingID,
		UserID:        txn.UserID,
		At:            time.Now(),
	})
}

func (p *PaymentProducer) PublishPaymentFailed(ctx context.Context, txn *domain.Transaction, reason string) error {
	return p.producer.Produce(ctx, "payment.failed", txn.ID, sdomain.PaymentFailed{
		TransactionID: txn.ID,
		BookingID:     txn.BookingID,
		UserID:        txn.UserID,
		Reason:        reason,
		At:            time.Now(),
	})
}

// Close closes the producer.
func (p *PaymentProducer) Close() error { return p.producer.Close() }
