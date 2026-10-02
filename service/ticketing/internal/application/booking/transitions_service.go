package booking

import (
	"context"

	sdomain "github.com/nedo/TicketSaas/pkg/dto"
	"github.com/nedo/TicketSaas/service/ticketing/internal/domain"
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

	applied, err := s.bookingRepo.UpdateStatusIfPending(ctx, bookingID, "confirmed")
	if err != nil {
		return err
	}
	if !applied {
		s.logger.Info("confirm skipped, booking already transitioned", "booking_id", bookingID)
		return nil
	}

	for _, item := range booking.Items {
		_ = s.outbox.Insert(ctx, "ticket.issued", bookingID+"-"+item.ID, sdomain.TicketIssued{
			TicketID:     item.ID,
			BookingID:    bookingID,
			UserID:       booking.UserID,
			EventID:      booking.EventID,
			TicketTypeID: item.TicketTypeID,
		})
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
