package application

import (
	"context"
	"log"
	"time"

	"github.com/google/uuid"
	"github.com/nedo/TicketSaas/internal/event/domain"
	sdomain "github.com/nedo/TicketSaas/internal/shared/domain"
	"github.com/nedo/TicketSaas/internal/shared/outbox"
)

// EventService orchestrates event management operations.
type EventService struct {
	repo              domain.EventRepository
	organizerConsumer domain.OrganizerConsumer
	seatReader        domain.SeatReader
	outbox            outbox.StoreInterface
}

// NewEventService creates a new EventService.
func NewEventService(
	repo domain.EventRepository,
	organizerConsumer domain.OrganizerConsumer,
	seatReader domain.SeatReader,
	ob outbox.StoreInterface,
) *EventService {
	return &EventService{repo: repo, organizerConsumer: organizerConsumer, seatReader: seatReader, outbox: ob}
}

// CreateOrganizer registers a new organizer linked to a user.
func (s *EventService) CreateOrganizer(
	ctx context.Context,
	userID, name, description, profileLink, contactEmail string,
) (*domain.Organizer, error) {
	existing, _ := s.repo.FindOrganizerByUserID(ctx, userID)
	if existing != nil {
		return nil, domain.ErrOrganizerExists
	}
	org := &domain.Organizer{
		ID:           uuid.NewString(),
		UserID:       userID,
		Name:         name,
		Description:  description,
		ProfileLink:  profileLink,
		ContactEmail: contactEmail,
	}
	if err := s.repo.CreateOrganizer(ctx, org); err != nil {
		return nil, err
	}
	return org, nil
}

// GetOrganizer retrieves an organizer by user ID.
func (s *EventService) GetOrganizer(ctx context.Context, userID string) (*domain.Organizer, error) {
	return s.repo.FindOrganizerByUserID(ctx, userID)
}

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

	_ = s.outbox.Insert(ctx, "event.created", event.ID, sdomain.EventCreated{
		EventID: event.ID,
		Status:  string(event.Status),
		At:      time.Now(),
	})
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
	_ = s.outbox.Insert(ctx, "event.updated", event.ID, sdomain.EventUpdated{
		EventID: event.ID,
		At:      time.Now(),
	})
	return event, nil
}

// ApproveEvent approves a pending event (admin only).
func (s *EventService) ApproveEvent(
	ctx context.Context,
	eventID, adminUserID string,
) (*domain.Event, error) {
	event, err := s.repo.FindEventByID(ctx, eventID)
	if err != nil {
		return nil, domain.ErrEventNotFound
	}
	if event.Status != sdomain.EventStatusPending {
		return nil, domain.ErrEventNotApprovable
	}
	event.Status = sdomain.EventStatusPublished
	event.ReviewedBy = &adminUserID
	now := time.Now()
	event.ReviewedAt = &now
	event.UpdatedAt = now
	if err := s.repo.UpdateEvent(ctx, event); err != nil {
		return nil, err
	}
	types, err := s.repo.ListTicketTypesByEvent(ctx, eventID)
	if err != nil {
		return nil, err
	}
	ttInfos := make([]sdomain.TicketTypeInfo, len(types))
	for i, tt := range types {
		ttInfos[i] = sdomain.TicketTypeInfo{
			TicketTypeID: tt.ID,
			Name:         tt.Name,
			Quantity:     tt.Quantity,
			PriceCents:   tt.PriceCents,
		}
	}
	_ = s.outbox.Insert(ctx, "event.approved", event.ID, sdomain.EventApproved{
		EventID:     event.ID,
		TicketTypes: ttInfos,
		At:          time.Now(),
	})
	return event, nil
}

// RejectEvent rejects a pending event (admin only).
func (s *EventService) RejectEvent(
	ctx context.Context,
	eventID, adminUserID, reason string,
) (*domain.Event, error) {
	event, err := s.repo.FindEventByID(ctx, eventID)
	if err != nil {
		return nil, domain.ErrEventNotFound
	}
	if event.Status != sdomain.EventStatusPending {
		return nil, domain.ErrEventNotApprovable
	}
	event.Status = sdomain.EventStatusRejected
	event.ReviewedBy = &adminUserID
	now := time.Now()
	event.ReviewedAt = &now
	event.UpdatedAt = now
	if err := s.repo.UpdateEvent(ctx, event); err != nil {
		return nil, err
	}
	_ = s.outbox.Insert(ctx, "event.rejected", event.ID, sdomain.EventRejected{
		EventID: event.ID,
		Reason:  reason,
		At:      time.Now(),
	})
	return event, nil
}

// CancelEvent cancels a published event.
func (s *EventService) CancelEvent(
	ctx context.Context,
	eventID, userID string,
) (*domain.Event, error) {
	event, err := s.repo.FindEventByID(ctx, eventID)
	if err != nil {
		return nil, domain.ErrEventNotFound
	}
	if event.Status != sdomain.EventStatusPublished {
		return nil, domain.ErrEventNotCancellable
	}
	if event.OrganizerID != userID {
		return nil, domain.ErrNotEventOwner
	}
	event.Status = sdomain.EventStatusCancelled
	event.UpdatedAt = time.Now()
	if err := s.repo.UpdateEvent(ctx, event); err != nil {
		return nil, err
	}
	_ = s.outbox.Insert(ctx, "event.cancelled", event.ID, sdomain.EventCancelled{
		EventID: event.ID,
		At:      time.Now(),
	})
	return event, nil
}

// GetEvent returns an event by ID.
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
func (s *EventService) ListPending(ctx context.Context, limit, offset int) ([]domain.Event, int, error) {
	return s.repo.ListPending(ctx, limit, offset)
}

// StartOrganizerConsumer listens for organizer.created events and creates organizers.
func (s *EventService) StartOrganizerConsumer(ctx context.Context) {
	s.organizerConsumer.OnOrganizerCreated(
		ctx,
		func(ctx context.Context, userID, name, description, profileLink, contactEmail string) error {
			_, err := s.CreateOrganizer(ctx, userID, name, description, profileLink, contactEmail)
			if err != nil {
				log.Printf("failed to create organizer for user %s: %v", userID, err)
			}
			return nil
		},
	)
	go func() {
		if err := s.organizerConsumer.Start(ctx); err != nil {
			log.Printf("organizer consumer exited: %v", err)
		}
	}()
}
