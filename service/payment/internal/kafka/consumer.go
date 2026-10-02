package kafka

import (
	"context"
	"encoding/json"
	"time"

	sdomain "github.com/nedo/TicketSaas/pkg/dto"
	sharedkafka "github.com/nedo/TicketSaas/pkg/kafka"
)

// PaymentConsumer implements domain.EventConsumer for payment events.
type PaymentConsumer struct {
	brokers     []string
	groupID     string
	concurrency int

	eventCancelledFn func(context.Context, string) error
}

// NewPaymentConsumer creates a new Kafka consumer for payment events.
func NewPaymentConsumer(brokers []string, groupID string, concurrency int) *PaymentConsumer {
	return &PaymentConsumer{brokers: brokers, groupID: groupID, concurrency: concurrency}
}

func (c *PaymentConsumer) OnEventCancelled(
	ctx context.Context,
	fn func(context.Context, string) error,
) {
	c.eventCancelledFn = fn
}

// Start begins consuming reservation and event lifecycle topics.
func (c *PaymentConsumer) Start(ctx context.Context) error {
	time.Sleep(500 * time.Millisecond)

	startConsumer(
		ctx,
		c.brokers,
		c.groupID,
		c.concurrency,
		"event.cancelled",
		func(ctx context.Context, msg sharedkafka.Message) error {
			var event sdomain.EventCancelled
			if err := json.Unmarshal(msg.Value, &event); err != nil {
				return err
			}
			if c.eventCancelledFn != nil {
				return c.eventCancelledFn(ctx, event.EventID)
			}
			return nil
		},
	)

	<-ctx.Done()
	return nil
}

func startConsumer(
	ctx context.Context,
	brokers []string,
	groupID string,
	concurrency int,
	topic string,
	handler sharedkafka.Handler,
) {
	time.Sleep(500 * time.Millisecond)
	consumer := sharedkafka.NewConsumer(
		brokers,
		topic,
		groupID,
		sharedkafka.WithConcurrency(concurrency),
	)
	go func() {
		defer consumer.Close()
		_ = consumer.Consume(ctx, handler)
	}()
}

// Close is a no-op.
func (c *PaymentConsumer) Close() error { return nil }
