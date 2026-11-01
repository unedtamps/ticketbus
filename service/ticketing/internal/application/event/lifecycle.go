package event

import (
	"context"
	"time"

	sdomain "github.com/unedtamps/ticketbus/pkg/dto"
	"github.com/unedtamps/ticketbus/service/ticketing/internal/domain"
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

// CancelEvent cancels a published event as its organizer.
//
// Only allowed before the event's end_at. Once an event has run, the organizer
// has collected (or can collect) its earnings, so letting them cancel it would
// refund every buyer out of the platform's pocket while the organizer kept the
// payout — self-dealing that is indistinguishable from fraud. An admin can
// still cancel afterwards (CancelEventAsAdmin) for genuine cases such as a
// cancelled post-show or a chargeback investigation.
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
	if !time.Now().Before(event.EndAt) {
		return nil, domain.ErrCancelAfterEnd
	}
	return s.cancel(ctx, event)
}

// CancelEventAsAdmin cancels a published event regardless of whether it has
// already ended. The admin path carries no organizer earnings to protect, so
// it is the only way to cancel a finished event.
func (s *EventService) CancelEventAsAdmin(
	ctx context.Context,
	eventID string,
) (*domain.Event, error) {
	event, err := s.repo.FindEventByID(ctx, eventID)
	if err != nil {
		return nil, domain.ErrEventNotFound
	}
	if event.Status != sdomain.EventStatusPublished {
		return nil, domain.ErrEventNotCancellable
	}
	return s.cancel(ctx, event)
}

// cancel applies the cancellation and announces it, shared by both entry
// points so the two paths cannot drift.
func (s *EventService) cancel(ctx context.Context, event *domain.Event) (*domain.Event, error) {
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
