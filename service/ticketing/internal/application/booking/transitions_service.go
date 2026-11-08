package booking

import (
	"context"

	sdomain "github.com/unedtamps/ticketbus/pkg/dto"
	"github.com/unedtamps/ticketbus/service/ticketing/internal/domain"
)

// Confirm finalizes a reservation into a confirmed booking (triggered by payment completed).
func (s *BookingService) Confirm(ctx context.Context, bookingID, paymentID string) error {
	booking, err := s.loadPendingBooking(ctx, bookingID)
	if err != nil {
		return err
	}
	if booking == nil {
		return nil
	}

	published, err := s.eventStatus.IsPublished(ctx, booking.EventID)
	if err != nil {
		return err
	}
	if !published {
		// Event no longer live: cancel the booking and release its seats
		// atomically (e.g. payment completed after event cancellation). No
		// ticket is issued; the refund is handled by the payment service via
		// the cancel-reprocess flow.
		applied, err := s.bookingRepo.TransitionAndReleaseSeats(ctx, booking, "cancelled")
		if err != nil {
			return err
		}
		if !applied {
			s.logger.Info("confirm skipped, booking already transitioned", "booking_id", bookingID)
			return nil
		}
		return nil
	}

	events := make([]domain.OutboxEvent, 0, len(booking.Items))
	for _, item := range booking.Items {
		// Everything the mail service needs is snapshotted here: the booking
		// arrived enriched with the event context and ticket names, so the
		// consumer can render and deliver the e-ticket without a lookup.
		events = append(events, domain.OutboxEvent{
			Topic: sdomain.TopicTicketIssued,
			Key:   bookingID + "-" + item.ID,
			Payload: sdomain.TicketIssued{
				TicketID:       item.ID,
				BookingID:      bookingID,
				UserID:         booking.UserID,
				Email:          booking.CustomerEmail,
				EventID:        booking.EventID,
				EventTitle:     booking.EventTitle,
				EventVenue:     booking.EventVenue,
				EventStartAt:   booking.EventStartAt,
				TicketTypeID:   item.TicketTypeID,
				TicketTypeName: item.TicketTypeName,
				Quantity:       item.Quantity,
			},
		})
	}

	// Confirm and its tickets commit together: writing the rows separately
	// left room for a confirmed booking with no (or partial) tickets, and
	// nothing re-fires once the status has moved on.
	applied, err := s.bookingRepo.ConfirmWithTickets(ctx, bookingID, events)
	if err != nil {
		return err
	}
	if !applied {
		s.logger.Info("confirm skipped, booking already transitioned", "booking_id", bookingID)
	}
	return nil
}

// ExpireOnPaymentExpired ends a reservation because its payment expired
// (triggered by payment.expired). The booking becomes expired, not cancelled.
func (s *BookingService) ExpireOnPaymentExpired(ctx context.Context, bookingID string) error {
	return s.releaseReservation(ctx, bookingID, "expired")
}

// releaseReservation transitions a pending booking to the given terminal
// status and releases its seats atomically.
func (s *BookingService) releaseReservation(ctx context.Context, bookingID, status string) error {
	booking, err := s.loadPendingBooking(ctx, bookingID)
	if err != nil {
		return err
	}
	if booking == nil {
		return nil
	}

	applied, err := s.bookingRepo.TransitionAndReleaseSeats(ctx, booking, status)
	if err != nil {
		return err
	}
	if !applied {
		return nil
	}
	return nil
}

// CancelBookings cancels bookings and releases their seats when an event is
// cancelled. Each booking transitions atomically in its own transaction.
// Idempotent: bookings already cancelled (or otherwise terminal) are skipped.
func (s *BookingService) CancelBookings(ctx context.Context, eventID string) error {
	s.logger.Info("cancelling bookings for event", "event_id", eventID)

	bookings, err := s.bookingRepo.ListByEventID(ctx, eventID)
	if err != nil {
		return err
	}

	for i := range bookings {
		b := &bookings[i]
		if b.Status != "confirmed" && b.Status != "pending" {
			continue
		}
		applied, err := s.bookingRepo.TransitionAndReleaseSeats(ctx, b, "cancelled")
		if err != nil {
			return err
		}
		if !applied {
			continue
		}
		s.logger.Info("cancelled booking for event", "event_id", eventID, "booking_id", b.ID)
	}
	return nil
}

// loadPendingBooking loads a booking and returns nil if it is not pending,
// so transitions can skip already-settled bookings.
func (s *BookingService) loadPendingBooking(
	ctx context.Context,
	bookingID string,
) (*domain.Booking, error) {
	booking, err := s.bookingRepo.FindByID(ctx, bookingID)
	if err != nil {
		s.logger.Info("skipped, booking not found", "booking_id", bookingID)
		return nil, nil
	}
	if booking.Status != "pending" {
		s.logger.Info(
			"skipped, booking no longer pending",
			"booking_id",
			bookingID,
			"status",
			booking.Status,
		)
		return nil, nil
	}
	return booking, nil
}
