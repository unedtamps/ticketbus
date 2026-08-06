package event

import (
	"context"
	"time"

	sdomain "github.com/nedo/TicketSaas/internal/shared/domain"
	"github.com/nedo/TicketSaas/internal/ticketing/domain"
)

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
	for _, tt := range types {
		if s.seatInitializer != nil {
			if err := s.seatInitializer.Init(ctx, eventID, tt.ID, tt.Quantity); err != nil {
				return nil, err
			}
			if err := s.seatInitializer.SetPrice(ctx, eventID, tt.ID, tt.PriceRupiah); err != nil {
				return nil, err
			}
		}
	}
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

// ReprocessCancellation re-publishes event.cancelled for an already cancelled
// event. Consumers re-apply the cancellation idempotently, which ensures every
// booking is cancelled and any transaction that completed since the last pass
// is added to the refund requests. Callable by the event organizer.
func (s *EventService) ReprocessCancellation(
	ctx context.Context,
	eventID, userID string,
) (*domain.Event, error) {
	return s.reprocessCancellation(ctx, eventID, userID, true)
}

// ReprocessCancellationAsAdmin is the admin variant of ReprocessCancellation.
func (s *EventService) ReprocessCancellationAsAdmin(
	ctx context.Context,
	eventID string,
) (*domain.Event, error) {
	return s.reprocessCancellation(ctx, eventID, "", false)
}

func (s *EventService) reprocessCancellation(
	ctx context.Context,
	eventID, userID string,
	checkOwner bool,
) (*domain.Event, error) {
	event, err := s.repo.FindEventByID(ctx, eventID)
	if err != nil {
		return nil, domain.ErrEventNotFound
	}
	if event.Status != sdomain.EventStatusCancelled {
		return nil, domain.ErrEventNotCancelled
	}
	if checkOwner && event.OrganizerID != userID {
		return nil, domain.ErrNotEventOwner
	}
	_ = s.outbox.Insert(ctx, "event.cancelled", event.ID, sdomain.EventCancelled{
		EventID: event.ID,
		At:      time.Now(),
	})
	return event, nil
}
