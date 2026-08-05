package kafka

import (
	"context"
	"encoding/json"
	"time"

	sdomain "github.com/nedo/TicketSaas/internal/shared/domain"
	sharedkafka "github.com/nedo/TicketSaas/internal/shared/kafka"
)

// TicketingConsumer implements domain.EventConsumer using Kafka.
type TicketingConsumer struct {
	brokers     []string
	groupID     string
	concurrency int

	paymentCompletedFn func(context.Context, string, string) error
	paymentFailedFn    func(context.Context, string) error
	eventCancelledFn   func(context.Context, string) error
}

// NewTicketingConsumer creates a new Kafka consumer for payment events.
func NewTicketingConsumer(brokers []string, groupID string, concurrency int) *TicketingConsumer {
	return &TicketingConsumer{
		brokers:     brokers,
		groupID:     groupID,
		concurrency: concurrency,
	}
}

func (c *TicketingConsumer) OnPaymentCompleted(ctx context.Context, fn func(context.Context, string, string) error) {
	c.paymentCompletedFn = fn
}

func (c *TicketingConsumer) OnPaymentFailed(ctx context.Context, fn func(context.Context, string) error) {
	c.paymentFailedFn = fn
}

func (c *TicketingConsumer) OnEventCancelled(ctx context.Context, fn func(context.Context, string) error) {
	c.eventCancelledFn = fn
}

// Start begins consuming from all relevant topics.
func (c *TicketingConsumer) Start(ctx context.Context) error {
	startConsumer(ctx, c.brokers, c.groupID, c.concurrency, "payment.completed", func(ctx context.Context, msg sharedkafka.Message) error {
		var event sdomain.PaymentCompleted
		if err := json.Unmarshal(msg.Value, &event); err != nil {
			return err
		}
		if c.paymentCompletedFn != nil {
			return c.paymentCompletedFn(ctx, event.BookingID, event.TransactionID)
		}
		return nil
	})

	startConsumer(ctx, c.brokers, c.groupID, c.concurrency, "payment.failed", func(ctx context.Context, msg sharedkafka.Message) error {
		var event sdomain.PaymentFailed
		if err := json.Unmarshal(msg.Value, &event); err != nil {
			return err
		}
		if c.paymentFailedFn != nil {
			return c.paymentFailedFn(ctx, event.BookingID)
		}
		return nil
	})

	startConsumer(ctx, c.brokers, c.groupID, c.concurrency, "event.cancelled", func(ctx context.Context, msg sharedkafka.Message) error {
		var event sdomain.EventCancelled
		if err := json.Unmarshal(msg.Value, &event); err != nil {
			return err
		}
		if c.eventCancelledFn != nil {
			return c.eventCancelledFn(ctx, event.EventID)
		}
		return nil
	})

	<-ctx.Done()
	return nil
}

// Close is a no-op; consumers shut down when context is cancelled.
func (c *TicketingConsumer) Close() error { return nil }

func startConsumer(ctx context.Context, brokers []string, groupID string, concurrency int, topic string, handler sharedkafka.Handler) {
	time.Sleep(500 * time.Millisecond)
	consumer := sharedkafka.NewConsumer(brokers, topic, groupID, sharedkafka.WithConcurrency(concurrency))
	go func() {
		defer consumer.Close()
		_ = consumer.Consume(ctx, handler)
	}()
}
