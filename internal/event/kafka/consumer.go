package kafka

import (
	"context"
	"encoding/json"
	"time"

	sharedkafka "github.com/nedo/TicketSaas/internal/shared/kafka"
	sdomain "github.com/nedo/TicketSaas/internal/shared/domain"
)

// OrganizerConsumer implements domain.OrganizerConsumer using Kafka.
type OrganizerConsumer struct {
	brokers []string
	groupID string

	organizerCreatedFn func(context.Context, string, string, string, string, string) error
}

// NewOrganizerConsumer creates a new Kafka consumer for organizer events.
func NewOrganizerConsumer(brokers []string, groupID string) *OrganizerConsumer {
	return &OrganizerConsumer{
		brokers: brokers,
		groupID: groupID,
	}
}

func (c *OrganizerConsumer) OnOrganizerCreated(ctx context.Context, fn func(context.Context, string, string, string, string, string) error) {
	c.organizerCreatedFn = fn
}

// Start begins consuming from the organizer.created topic.
func (c *OrganizerConsumer) Start(ctx context.Context) error {
	time.Sleep(500 * time.Millisecond)
	consumer := sharedkafka.NewConsumer(c.brokers, "organizer.created", c.groupID)
	go func() {
		defer consumer.Close()
		_ = consumer.Consume(ctx, func(ctx context.Context, msg sharedkafka.Message) error {
			var event sdomain.OrganizerCreated
			if err := json.Unmarshal(msg.Value, &event); err != nil {
				return err
			}
			if c.organizerCreatedFn != nil {
				return c.organizerCreatedFn(ctx, event.UserID, event.Name, event.Description, event.ProfileLink, event.ContactEmail)
			}
			return nil
		})
	}()
	<-ctx.Done()
	return nil
}
