package booking

import (
	"context"
	"log/slog"

	"github.com/nedo/TicketSaas/internal/shared/outbox"
	"github.com/nedo/TicketSaas/internal/ticketing/domain"
)

// BookingService orchestrates reservation and booking operations.
type BookingService struct {
	bookingRepo      domain.BookingRepository
	reservationCache domain.ReservationCache
	seatCounter      domain.SeatCounter
	consumer         domain.EventConsumer
	eventStatus      domain.EventStatusRepository
	outbox           outbox.StoreInterface
	logger           *slog.Logger
	reservationTTL   int
}

// NewBookingService creates a new BookingService.
func NewBookingService(
	bookingRepo domain.BookingRepository,
	reservationCache domain.ReservationCache,
	seatCounter domain.SeatCounter,
	consumer domain.EventConsumer,
	eventStatus domain.EventStatusRepository,
	ob outbox.StoreInterface,
	logger *slog.Logger,
	reservationTTL int,
) *BookingService {
	return &BookingService{
		bookingRepo:      bookingRepo,
		reservationCache: reservationCache,
		seatCounter:      seatCounter,
		consumer:         consumer,
		eventStatus:      eventStatus,
		outbox:           ob,
		logger:           logger,
		reservationTTL:   reservationTTL,
	}
}

// GetBooking returns a booking by ID.
func (s *BookingService) GetBooking(
	ctx context.Context,
	bookingID string,
) (*domain.Booking, error) {
	return s.bookingRepo.FindByID(ctx, bookingID)
}

// ListMyBookings returns bookings for a user.
func (s *BookingService) ListMyBookings(
	ctx context.Context,
	userID string,
) ([]domain.Booking, error) {
	return s.bookingRepo.ListByUser(ctx, userID)
}
