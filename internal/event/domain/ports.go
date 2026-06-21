package domain

import "context"

// EventRepository defines the persistence contract for events, organizers, and ticket types.
type EventRepository interface {
	// Organizers
	CreateOrganizer(ctx context.Context, org *Organizer) error
	FindOrganizerByUserID(ctx context.Context, userID string) (*Organizer, error)

	// Events
	CreateEvent(ctx context.Context, event *Event) error
	UpdateEvent(ctx context.Context, event *Event) error
	FindEventByID(ctx context.Context, id string) (*Event, error)
	ListPublished(ctx context.Context, limit, offset int) ([]Event, int, error)
	ListByOrganizer(ctx context.Context, organizerID string) ([]Event, error)
	ListPending(ctx context.Context, limit, offset int) ([]Event, int, error)

	// Ticket types
	CreateTicketTypes(ctx context.Context, types []TicketType) error
	ListTicketTypesByEvent(ctx context.Context, eventID string) ([]TicketType, error)
}

// EventPublisher defines the contract for publishing event domain events.
type EventPublisher interface {
	PublishEventCreated(ctx context.Context, event *Event) error
	PublishEventApproved(ctx context.Context, event *Event, ticketTypes []TicketType) error
	PublishEventRejected(ctx context.Context, event *Event, reason string) error
	PublishEventUpdated(ctx context.Context, event *Event) error
	PublishEventCancelled(ctx context.Context, event *Event) error
}

// OrganizerConsumer consumes organizer creation events (from auth service).
type OrganizerConsumer interface {
	OnOrganizerCreated(
		ctx context.Context,
		fn func(context.Context, string, string, string, string, string) error,
	)
	Start(ctx context.Context) error
}

// SeatReader reads live seat availability from Redis.
type SeatReader interface {
	Available(ctx context.Context, eventID, ticketTypeID string) int
}
