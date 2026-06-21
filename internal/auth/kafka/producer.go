package kafka

import (
	"context"
	"time"

	sharedkafka "github.com/nedo/TicketSaas/internal/shared/kafka"
	sdomain "github.com/nedo/TicketSaas/internal/shared/domain"
)

// OrganizerPublisher implements domain.OrganizerPublisher using Kafka.
type OrganizerPublisher struct {
	producer *sharedkafka.Producer
}

// NewOrganizerPublisher creates a new Kafka organizer publisher.
func NewOrganizerPublisher(brokers []string) *OrganizerPublisher {
	return &OrganizerPublisher{producer: sharedkafka.NewProducer(brokers)}
}

func (p *OrganizerPublisher) PublishOrganizerCreated(ctx context.Context, userID, name, description, profileLink, contactEmail string) error {
	return p.producer.Produce(ctx, "organizer.created", userID, sdomain.OrganizerCreated{
		UserID:       userID,
		Name:         name,
		Description:  description,
		ProfileLink:  profileLink,
		ContactEmail: contactEmail,
		At:           time.Now(),
	})
}

// Close closes the underlying producer.
func (p *OrganizerPublisher) Close() error {
	return p.producer.Close()
}
