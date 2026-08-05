package event

import (
	"context"
	"time"

	"github.com/google/uuid"
	sdomain "github.com/nedo/TicketSaas/internal/shared/domain"
	"github.com/nedo/TicketSaas/internal/ticketing/domain"
)

// CreateEvent creates a new event with initial status "draft" or "pending".
func (s *EventService) CreateEvent(
	ctx context.Context,
	organizerID, title, description, venueName, venueAddress string,
	venueCapacity int,
	startAt, endAt time.Time,
	ticketTypes []domain.TicketType,
) (*domain.Event, error) {
	if len(ticketTypes) == 0 {
		return nil, domain.ErrTicketTypesEmpty
	}
	totalQty := 0
	for _, tt := range ticketTypes {
		totalQty += tt.Quantity
	}
	if totalQty > venueCapacity {
		return nil, domain.ErrCapacityExceeded
	}

	eventID := uuid.NewString()
	event := &domain.Event{
		ID:            eventID,
		OrganizerID:   organizerID,
		Title:         title,
		Description:   description,
		VenueName:     venueName,
		VenueAddress:  venueAddress,
		VenueCapacity: venueCapacity,
		StartAt:       startAt,
		EndAt:         endAt,
		Status:        sdomain.EventStatusPending,
	}

	if err := s.repo.CreateEvent(ctx, event); err != nil {
		return nil, err
	}

	for i := range ticketTypes {
		ticketTypes[i].ID = uuid.NewString()
		ticketTypes[i].EventID = eventID
	}
	if err := s.repo.CreateTicketTypes(ctx, ticketTypes); err != nil {
		return nil, err
	}

	return event, nil
}

// UpdateEvent updates an existing event.
func (s *EventService) UpdateEvent(
	ctx context.Context,
	eventID, organizerID, title, description string,
	startAt, endAt time.Time,
) (*domain.Event, error) {
	event, err := s.repo.FindEventByID(ctx, eventID)
	if err != nil {
		return nil, domain.ErrEventNotFound
	}
	if event.OrganizerID != organizerID {
		return nil, domain.ErrNotEventOwner
	}
	if event.Status != sdomain.EventStatusDraft && event.Status != sdomain.EventStatusPending {
		return nil, domain.ErrEventNotEditable
	}
	event.Title = title
	event.Description = description
	event.StartAt = startAt
	event.EndAt = endAt
	event.UpdatedAt = time.Now()
	if err := s.repo.UpdateEvent(ctx, event); err != nil {
		return nil, err
	}
	return event, nil
}
