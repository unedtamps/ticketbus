package kafka

import (
	"context"
	"time"

	"github.com/nedo/TicketSaas/internal/inventory/domain"
	sharedkafka "github.com/nedo/TicketSaas/internal/shared/kafka"
	sdomain "github.com/nedo/TicketSaas/internal/shared/domain"
)

// InventoryProducer implements domain.EventPublisher.
type InventoryProducer struct {
	producer *sharedkafka.Producer
}

// NewInventoryProducer creates a new Kafka producer for inventory events.
func NewInventoryProducer(brokers []string) *InventoryProducer {
	return &InventoryProducer{producer: sharedkafka.NewProducer(brokers)}
}

func (p *InventoryProducer) PublishReservationCreated(ctx context.Context, res *domain.Reservation) error {
	items := make([]sdomain.BookingItem, len(res.Items))
	for i, item := range res.Items {
		items[i] = sdomain.BookingItem{
			TicketTypeID:   item.TicketTypeID,
			Quantity:       item.Quantity,
			UnitPriceCents: item.UnitPriceCents,
		}
	}
	return p.producer.Produce(ctx, "reservation.created", res.BookingID, sdomain.ReservationCreated{
		BookingID:  res.BookingID,
		UserID:     res.UserID,
		EventID:    res.EventID,
		Items:      items,
		TotalCents: res.TotalCents,
		At:         time.Now(),
	})
}

func (p *InventoryProducer) PublishReservationExpired(ctx context.Context, bookingID, eventID string) error {
	return p.producer.Produce(ctx, "reservation.expired", bookingID, sdomain.ReservationExpired{
		BookingID: bookingID,
		EventID:   eventID,
		At:        time.Now(),
	})
}

func (p *InventoryProducer) PublishTicketIssued(ctx context.Context, booking *domain.Booking) error {
	for _, item := range booking.Items {
		_ = p.producer.Produce(ctx, "ticket.issued", booking.ID+"-"+item.ID, sdomain.TicketIssued{
			TicketID:     item.ID,
			BookingID:    booking.ID,
			UserID:       booking.UserID,
			EventID:      booking.EventID,
			TicketTypeID: item.TicketTypeID,
		})
	}
	return nil
}

// Close closes the producer.
func (p *InventoryProducer) Close() error {
	return p.producer.Close()
}
