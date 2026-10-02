package event

import (
	"context"

	"github.com/nedo/TicketSaas/service/ticketing/internal/domain"
)

// GetEvent returns an event by ID with live ticket availability.
func (s *EventService) GetEvent(
	ctx context.Context,
	eventID string,
) (*domain.Event, []domain.TicketType, error) {
	event, err := s.repo.FindEventByID(ctx, eventID)
	if err != nil {
		return nil, nil, domain.ErrEventNotFound
	}
	types, _ := s.repo.ListTicketTypesByEvent(ctx, eventID)
	for i := range types {
		types[i].Available = s.seatReader.Available(ctx, eventID, types[i].ID)
	}
	return event, types, nil
}

// ListPublished returns published events with pagination.
func (s *EventService) ListPublished(
	ctx context.Context,
	limit, offset int,
) ([]domain.Event, int, error) {
	return s.repo.ListPublished(ctx, limit, offset)
}

// ListByOrganizer returns events belonging to an organizer.
func (s *EventService) ListByOrganizer(
	ctx context.Context,
	organizerID string,
) ([]domain.Event, error) {
	return s.repo.ListByOrganizer(ctx, organizerID)
}

// ListPending returns pending events with pagination (admin).
func (s *EventService) ListPending(
	ctx context.Context,
	limit, offset int,
) ([]domain.Event, int, error) {
	return s.repo.ListPending(ctx, limit, offset)
}

// ListAll returns events with optional status filter (admin).
func (s *EventService) ListAll(
	ctx context.Context,
	status string,
	limit, offset int,
) ([]domain.Event, int, error) {
	return s.repo.FindAll(ctx, status, limit, offset)
}
