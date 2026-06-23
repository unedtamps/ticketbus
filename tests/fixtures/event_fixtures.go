package fixtures

import (
	"time"

	"github.com/google/uuid"
	"github.com/nedo/TicketSaas/internal/event/domain"
	sdomain "github.com/nedo/TicketSaas/internal/shared/domain"
)

// OrganizerOption is a functional option for NewTestOrganizer.
type OrganizerOption func(*domain.Organizer)

// WithOrganizerID overrides the default organizer ID.
func WithOrganizerID(id string) OrganizerOption {
	return func(o *domain.Organizer) { o.ID = id }
}

// WithOrganizerUserID overrides the default user ID.
func WithOrganizerUserID(userID string) OrganizerOption {
	return func(o *domain.Organizer) { o.UserID = userID }
}

// WithOrganizerName overrides the default name.
func WithOrganizerName(name string) OrganizerOption {
	return func(o *domain.Organizer) { o.Name = name }
}

// WithOrganizerContactEmail overrides the default contact email.
func WithOrganizerContactEmail(email string) OrganizerOption {
	return func(o *domain.Organizer) { o.ContactEmail = email }
}

// NewTestOrganizer creates an Organizer with sensible defaults.
func NewTestOrganizer(opts ...OrganizerOption) *domain.Organizer {
	now := time.Now().Truncate(time.Second)
	org := &domain.Organizer{
		ID:           uuid.NewString(),
		UserID:       uuid.NewString(),
		Name:         "Test Organizer",
		Description:  "A test event organizer for unit testing.",
		ProfileLink:  "https://test-org.example.com",
		ContactEmail: "org@example.com",
		CreatedAt:    now,
	}
	for _, o := range opts {
		o(org)
	}
	return org
}

// EventOption is a functional option for NewTestEvent.
type EventOption func(*domain.Event)

// WithEventID overrides the default event ID.
func WithEventID(id string) EventOption {
	return func(e *domain.Event) { e.ID = id }
}

// WithEventOrganizerID overrides the organizer ID.
func WithEventOrganizerID(id string) EventOption {
	return func(e *domain.Event) { e.OrganizerID = id }
}

// WithEventTitle overrides the title.
func WithEventTitle(title string) EventOption {
	return func(e *domain.Event) { e.Title = title }
}

// WithEventStatus overrides the status (default: draft).
func WithEventStatus(status sdomain.EventStatus) EventOption {
	return func(e *domain.Event) { e.Status = status }
}

// WithEventVenueCapacity overrides the venue capacity.
func WithEventVenueCapacity(capacity int) EventOption {
	return func(e *domain.Event) { e.VenueCapacity = capacity }
}

// WithEventStartAt overrides the start time.
func WithEventStartAt(t time.Time) EventOption {
	return func(e *domain.Event) { e.StartAt = t }
}

// WithEventEndAt overrides the end time.
func WithEventEndAt(t time.Time) EventOption {
	return func(e *domain.Event) { e.EndAt = t }
}

// NewTestEvent creates an Event with sensible defaults.
func NewTestEvent(opts ...EventOption) *domain.Event {
	now := time.Now().Truncate(time.Second)
	start := now.Add(24 * time.Hour).Truncate(time.Second)
	e := &domain.Event{
		ID:            uuid.NewString(),
		OrganizerID:   uuid.NewString(),
		Title:         "Test Event",
		Description:   "A test event for unit testing.",
		VenueName:     "Test Venue",
		VenueAddress:  "123 Test Street",
		VenueCapacity: 100,
		StartAt:       start,
		EndAt:         start.Add(3 * time.Hour),
		Status:        sdomain.EventStatusDraft,
		CreatedAt:     now,
		UpdatedAt:     now,
	}
	for _, o := range opts {
		o(e)
	}
	return e
}

// TicketTypeOption is a functional option for NewTestTicketType.
type TicketTypeOption func(*domain.TicketType)

// WithTicketTypeID overrides the default ticket type ID.
func WithTicketTypeID(id string) TicketTypeOption {
	return func(tt *domain.TicketType) { tt.ID = id }
}

// WithTicketTypeEventID overrides the event ID.
func WithTicketTypeEventID(eventID string) TicketTypeOption {
	return func(tt *domain.TicketType) { tt.EventID = eventID }
}

// WithTicketTypeName overrides the name.
func WithTicketTypeName(name string) TicketTypeOption {
	return func(tt *domain.TicketType) { tt.Name = name }
}

// WithTicketTypePrice overrides the price in cents.
func WithTicketTypePrice(cents int) TicketTypeOption {
	return func(tt *domain.TicketType) { tt.PriceCents = cents }
}

// WithTicketTypeQuantity overrides the quantity.
func WithTicketTypeQuantity(qty int) TicketTypeOption {
	return func(tt *domain.TicketType) { tt.Quantity = qty }
}

// WithTicketTypeMaxPerOrder overrides the max per order.
func WithTicketTypeMaxPerOrder(max int) TicketTypeOption {
	return func(tt *domain.TicketType) { tt.MaxPerOrder = max }
}

// NewTestTicketType creates a TicketType with sensible defaults.
func NewTestTicketType(opts ...TicketTypeOption) *domain.TicketType {
	tt := &domain.TicketType{
		ID:          uuid.NewString(),
		EventID:     uuid.NewString(),
		Name:        "General Admission",
		PriceCents:  5000,
		Quantity:    50,
		MaxPerOrder: 5,
		Available:   50,
	}
	for _, o := range opts {
		o(tt)
	}
	return tt
}
