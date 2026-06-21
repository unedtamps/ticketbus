package kafka

import (
	"context"
	"encoding/json"
	"time"

	sharedkafka "github.com/nedo/TicketSaas/internal/shared/kafka"
	sdomain "github.com/nedo/TicketSaas/internal/shared/domain"
)

// PaymentConsumer implements domain.EventConsumer for payment events.
type PaymentConsumer struct {
	brokers []string
	groupID string

	reservationCreatedFn func(context.Context, string, int, string) error
	reservationExpiredFn func(context.Context, string) error
}

// NewPaymentConsumer creates a new Kafka consumer for payment events.
func NewPaymentConsumer(brokers []string, groupID string) *PaymentConsumer {
	return &PaymentConsumer{brokers: brokers, groupID: groupID}
}

func (c *PaymentConsumer) OnReservationCreated(ctx context.Context, fn func(context.Context, string, int, string) error) {
	c.reservationCreatedFn = fn
}

func (c *PaymentConsumer) OnReservationExpired(ctx context.Context, fn func(context.Context, string) error) {
	c.reservationExpiredFn = fn
}

// Start begins consuming reservation.created and reservation.expired events.
func (c *PaymentConsumer) Start(ctx context.Context) error {
	time.Sleep(500 * time.Millisecond)

	startConsumer(ctx, c.brokers, c.groupID, "reservation.created", func(ctx context.Context, msg sharedkafka.Message) error {
		var event sdomain.ReservationCreated
		if err := json.Unmarshal(msg.Value, &event); err != nil {
			return err
		}
		if c.reservationCreatedFn != nil {
			return c.reservationCreatedFn(ctx, event.BookingID, event.TotalCents, event.UserID)
		}
		return nil
	})

	startConsumer(ctx, c.brokers, c.groupID, "reservation.expired", func(ctx context.Context, msg sharedkafka.Message) error {
		var event sdomain.ReservationExpired
		if err := json.Unmarshal(msg.Value, &event); err != nil {
			return err
		}
		if c.reservationExpiredFn != nil {
			return c.reservationExpiredFn(ctx, event.BookingID)
		}
		return nil
	})

	<-ctx.Done()
	return nil
}

func startConsumer(ctx context.Context, brokers []string, groupID, topic string, handler sharedkafka.Handler) {
	time.Sleep(500 * time.Millisecond)
	consumer := sharedkafka.NewConsumer(brokers, topic, groupID)
	go func() {
		defer consumer.Close()
		_ = consumer.Consume(ctx, handler)
	}()
}

// Close is a no-op.
func (c *PaymentConsumer) Close() error { return nil }
