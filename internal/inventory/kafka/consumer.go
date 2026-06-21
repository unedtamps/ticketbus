package kafka

import (
	"context"
	"encoding/json"
	"time"

	sharedkafka "github.com/nedo/TicketSaas/internal/shared/kafka"
	sdomain "github.com/nedo/TicketSaas/internal/shared/domain"
	"github.com/nedo/TicketSaas/internal/inventory/domain"
)

// InventoryConsumer implements domain.EventConsumer using Kafka.
type InventoryConsumer struct {
	brokers []string
	groupID string

	paymentCompletedFn func(context.Context, string, string) error
	paymentFailedFn    func(context.Context, string) error
	eventApprovedFn    func(context.Context, string, []domain.TicketTypeInfo) error
	eventCancelledFn   func(context.Context, string) error
}

// NewInventoryConsumer creates a new Kafka consumer for inventory events.
func NewInventoryConsumer(brokers []string, groupID string) *InventoryConsumer {
	return &InventoryConsumer{
		brokers: brokers,
		groupID: groupID,
	}
}

func (c *InventoryConsumer) OnPaymentCompleted(ctx context.Context, fn func(context.Context, string, string) error) {
	c.paymentCompletedFn = fn
}

func (c *InventoryConsumer) OnPaymentFailed(ctx context.Context, fn func(context.Context, string) error) {
	c.paymentFailedFn = fn
}

func (c *InventoryConsumer) OnEventApproved(ctx context.Context, fn func(context.Context, string, []domain.TicketTypeInfo) error) {
	c.eventApprovedFn = fn
}

func (c *InventoryConsumer) OnEventCancelled(ctx context.Context, fn func(context.Context, string) error) {
	c.eventCancelledFn = fn
}

// Start begins consuming from all relevant topics.
func (c *InventoryConsumer) Start(ctx context.Context) error {
	startConsumer(ctx, c.brokers, c.groupID, "payment.completed", func(ctx context.Context, msg sharedkafka.Message) error {
		var event sdomain.PaymentCompleted
		if err := json.Unmarshal(msg.Value, &event); err != nil {
			return err
		}
		if c.paymentCompletedFn != nil {
			return c.paymentCompletedFn(ctx, event.BookingID, event.TransactionID)
		}
		return nil
	})

	startConsumer(ctx, c.brokers, c.groupID, "payment.failed", func(ctx context.Context, msg sharedkafka.Message) error {
		var event sdomain.PaymentFailed
		if err := json.Unmarshal(msg.Value, &event); err != nil {
			return err
		}
		if c.paymentFailedFn != nil {
			return c.paymentFailedFn(ctx, event.BookingID)
		}
		return nil
	})

	startConsumer(ctx, c.brokers, c.groupID, "event.approved", func(ctx context.Context, msg sharedkafka.Message) error {
		var event sdomain.EventApproved
		if err := json.Unmarshal(msg.Value, &event); err != nil {
			return err
		}
		ttInfos := make([]domain.TicketTypeInfo, len(event.TicketTypes))
		for i, tt := range event.TicketTypes {
			ttInfos[i] = domain.TicketTypeInfo{
				TicketTypeID: tt.TicketTypeID,
				Name:         tt.Name,
				Quantity:     tt.Quantity,
				PriceCents:   tt.PriceCents,
			}
		}
		if c.eventApprovedFn != nil {
			return c.eventApprovedFn(ctx, event.EventID, ttInfos)
		}
		return nil
	})

	startConsumer(ctx, c.brokers, c.groupID, "event.cancelled", func(ctx context.Context, msg sharedkafka.Message) error {
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
func (c *InventoryConsumer) Close() error { return nil }

func startConsumer(ctx context.Context, brokers []string, groupID, topic string, handler sharedkafka.Handler) {
	time.Sleep(500 * time.Millisecond)
	consumer := sharedkafka.NewConsumer(brokers, topic, groupID)
	go func() {
		defer consumer.Close()
		_ = consumer.Consume(ctx, handler)
	}()
}
