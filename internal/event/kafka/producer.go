package kafka

import (
	"context"

	"github.com/nedo/TicketSaas/internal/event/domain"
	sharedkafka "github.com/nedo/TicketSaas/internal/shared/kafka"
	sdomain "github.com/nedo/TicketSaas/internal/shared/domain"
	"time"
)

// EventPublisher implements domain.EventPublisher using Kafka.
type EventPublisher struct {
	producer *sharedkafka.Producer
}

// NewEventPublisher creates a new Kafka event publisher.
func NewEventPublisher(brokers []string) *EventPublisher {
	return &EventPublisher{producer: sharedkafka.NewProducer(brokers)}
}

func (p *EventPublisher) PublishEventCreated(ctx context.Context, event *domain.Event) error {
	return p.producer.Produce(ctx, "event.created", event.ID, sdomain.EventCreated{
		EventID: event.ID,
		Status:  string(event.Status),
		At:      time.Now(),
	})
}

func (p *EventPublisher) PublishEventApproved(ctx context.Context, event *domain.Event, ticketTypes []domain.TicketType) error {
	ttInfos := make([]sdomain.TicketTypeInfo, len(ticketTypes))
	for i, tt := range ticketTypes {
		ttInfos[i] = sdomain.TicketTypeInfo{
			TicketTypeID: tt.ID,
			Name:         tt.Name,
			Quantity:     tt.Quantity,
			PriceCents:   tt.PriceCents,
		}
	}
	return p.producer.Produce(ctx, "event.approved", event.ID, sdomain.EventApproved{
		EventID:     event.ID,
		TicketTypes: ttInfos,
		At:          time.Now(),
	})
}

func (p *EventPublisher) PublishEventRejected(ctx context.Context, event *domain.Event, reason string) error {
	return p.producer.Produce(ctx, "event.rejected", event.ID, sdomain.EventRejected{
		EventID: event.ID,
		Reason:  reason,
		At:      time.Now(),
	})
}

func (p *EventPublisher) PublishEventUpdated(ctx context.Context, event *domain.Event) error {
	return p.producer.Produce(ctx, "event.updated", event.ID, sdomain.EventUpdated{
		EventID: event.ID,
		At:      time.Now(),
	})
}

func (p *EventPublisher) PublishEventCancelled(ctx context.Context, event *domain.Event) error {
	return p.producer.Produce(ctx, "event.cancelled", event.ID, sdomain.EventCancelled{
		EventID: event.ID,
		At:      time.Now(),
	})
}

// Close closes the underlying producer.
func (p *EventPublisher) Close() error {
	return p.producer.Close()
}
