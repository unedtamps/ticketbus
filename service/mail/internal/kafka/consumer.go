package kafka

import (
	"context"
	"encoding/json"
	"time"

	sdomain "github.com/unedtamps/ticketbus/pkg/dto"
	sharedkafka "github.com/unedtamps/ticketbus/pkg/kafka"
)

// MailConsumer consumes the topics the mail service reacts to.
type MailConsumer struct {
	brokers           []string
	groupID           string
	concurrency       int
	ticketIssuedFn    func(context.Context, sdomain.TicketIssued) error
	eventCancelledFn  func(context.Context, sdomain.EventCancelled) error
	refundCompletedFn func(context.Context, sdomain.RefundCompleted) error
}

// NewMailConsumer creates a new Kafka consumer for mail events.
func NewMailConsumer(brokers []string, groupID string, concurrency int) *MailConsumer {
	return &MailConsumer{brokers: brokers, groupID: groupID, concurrency: concurrency}
}

// OnTicketIssued registers the handler for e-ticket issuance events.
func (c *MailConsumer) OnTicketIssued(
	ctx context.Context,
	fn func(context.Context, sdomain.TicketIssued) error,
) {
	c.ticketIssuedFn = fn
}

// OnEventCancelled registers the handler for event cancellation notifications.
func (c *MailConsumer) OnEventCancelled(
	ctx context.Context,
	fn func(context.Context, sdomain.EventCancelled) error,
) {
	c.eventCancelledFn = fn
}

// OnRefundCompleted registers the handler for settled refund notices.
func (c *MailConsumer) OnRefundCompleted(
	ctx context.Context,
	fn func(context.Context, sdomain.RefundCompleted) error,
) {
	c.refundCompletedFn = fn
}

// Start begins consuming and blocks until ctx is cancelled.
func (c *MailConsumer) Start(ctx context.Context) error {
	startConsumer(
		ctx,
		c.brokers,
		c.groupID,
		c.concurrency,
		sdomain.TopicTicketIssued,
		func(ctx context.Context, msg sharedkafka.Message) error {
			var event sdomain.TicketIssued
			if err := json.Unmarshal(msg.Value, &event); err != nil {
				return err
			}
			if c.ticketIssuedFn != nil {
				return c.ticketIssuedFn(ctx, event)
			}
			return nil
		},
	)

	startConsumer(
		ctx,
		c.brokers,
		c.groupID,
		c.concurrency,
		sdomain.TopicEventCancelled,
		func(ctx context.Context, msg sharedkafka.Message) error {
			var event sdomain.EventCancelled
			if err := json.Unmarshal(msg.Value, &event); err != nil {
				return err
			}
			if c.eventCancelledFn != nil {
				return c.eventCancelledFn(ctx, event)
			}
			return nil
		},
	)

	startConsumer(
		ctx,
		c.brokers,
		c.groupID,
		c.concurrency,
		sdomain.TopicRefundCompleted,
		func(ctx context.Context, msg sharedkafka.Message) error {
			var event sdomain.RefundCompleted
			if err := json.Unmarshal(msg.Value, &event); err != nil {
				return err
			}
			if c.refundCompletedFn != nil {
				return c.refundCompletedFn(ctx, event)
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
func (c *MailConsumer) Close() error { return nil }
