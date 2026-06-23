package application

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/nedo/TicketSaas/internal/inventory/domain"
	sdomain "github.com/nedo/TicketSaas/internal/shared/domain"
	"github.com/nedo/TicketSaas/internal/shared/outbox"
)

// InventoryService orchestrates reservation and booking operations.
type InventoryService struct {
	bookingRepo      domain.BookingRepository
	reservationCache domain.ReservationCache
	seatCounter      domain.SeatCounter
	consumer         domain.EventConsumer
	outbox           outbox.StoreInterface
	logger           *slog.Logger
	reservationTTL   int
}

// NewInventoryService creates a new InventoryService.
func NewInventoryService(
	bookingRepo domain.BookingRepository,
	reservationCache domain.ReservationCache,
	seatCounter domain.SeatCounter,
	consumer domain.EventConsumer,
	ob outbox.StoreInterface,
	logger *slog.Logger,
	reservationTTL int,
) *InventoryService {
	return &InventoryService{
		bookingRepo:      bookingRepo,
		reservationCache: reservationCache,
		seatCounter:       seatCounter,
		consumer:          consumer,
		outbox:            ob,
		logger:            logger,
		reservationTTL:    reservationTTL,
	}
}

// Reserve creates a temporary reservation.
func (s *InventoryService) Reserve(ctx context.Context, userID, eventID string, items []domain.BookingItem) (*domain.Reservation, error) {
	if len(items) == 0 {
		return nil, domain.ErrInvalidQuantity
	}

	for _, item := range items {
		if err := s.seatCounter.Reserve(ctx, eventID, item.TicketTypeID, item.Quantity); err != nil {
			// Rollback already reserved
			for _, rb := range items {
				if rb.TicketTypeID == item.TicketTypeID {
					break
				}
				_ = s.seatCounter.Release(ctx, eventID, rb.TicketTypeID, rb.Quantity)
			}
			return nil, fmt.Errorf("%w: %s", domain.ErrNoSeatsAvailable, err)
		}
	}

	res := &domain.Reservation{
		BookingID:  uuid.NewString(),
		UserID:     userID,
		EventID:    eventID,
		Items:      items,
		TotalCents: calculateTotal(items),
		Status:     "held",
		CreatedAt:  time.Now(),
	}

	if err := s.reservationCache.Save(ctx, res, s.reservationTTL); err != nil {
		// Rollback seat counters
		for _, item := range items {
			_ = s.seatCounter.Release(ctx, eventID, item.TicketTypeID, item.Quantity)
		}
		return nil, err
	}

	_ = s.outbox.Insert(ctx, "reservation.created", res.BookingID, reservationToPayload(res))
	return res, nil
}

// Confirm finalizes a reservation into a confirmed booking (triggered by payment completed).
func (s *InventoryService) Confirm(ctx context.Context, bookingID, paymentID string) error {
	res, err := s.reservationCache.Find(ctx, bookingID)
	if err != nil {
		// Reservation already expired or confirmed — not an error
		s.logger.Info("confirm skipped, reservation already removed", "booking_id", bookingID)
		return nil
	}

	booking := &domain.Booking{
		ID:         bookingID,
		UserID:     res.UserID,
		EventID:    res.EventID,
		Status:     "confirmed",
		TotalCents: res.TotalCents,
		PaymentID:  paymentID,
		Items:      res.Items,
	}

	for i := range booking.Items {
		booking.Items[i].ID = uuid.NewString()
		booking.Items[i].BookingID = bookingID
	}

	if err := s.bookingRepo.Create(ctx, booking); err != nil {
		return err
	}

	_ = s.reservationCache.Delete(ctx, bookingID)
	for _, item := range booking.Items {
		_ = s.outbox.Insert(ctx, "ticket.issued", booking.ID+"-"+item.ID, sdomain.TicketIssued{
			TicketID:     item.ID,
			BookingID:    booking.ID,
			UserID:       booking.UserID,
			EventID:      booking.EventID,
			TicketTypeID: item.TicketTypeID,
		})
	}
	return nil
}

// Release frees a reservation (triggered by payment failed or manual cancel).
func (s *InventoryService) Release(ctx context.Context, bookingID string) error {
	res, err := s.reservationCache.Find(ctx, bookingID)
	if err != nil {
		// Reservation already expired or confirmed — not an error
		s.logger.Info("release skipped, reservation already removed", "booking_id", bookingID)
		return nil
	}

	for _, item := range res.Items {
		_ = s.seatCounter.Release(ctx, res.EventID, item.TicketTypeID, item.Quantity)
	}
	_ = s.reservationCache.Delete(ctx, bookingID)
	return nil
}

// HandleExpiry processes a reservation that expired via Redis TTL.
func (s *InventoryService) HandleExpiry(ctx context.Context, res *domain.Reservation) {
	bookingID := res.BookingID
	for _, item := range res.Items {
		_ = s.seatCounter.Release(ctx, res.EventID, item.TicketTypeID, item.Quantity)
	}
	_ = s.reservationCache.Delete(ctx, bookingID)
	if err := s.outbox.Insert(ctx, "reservation.expired", bookingID, sdomain.ReservationExpired{
		BookingID: bookingID,
		EventID:   res.EventID,
		At:        time.Now(),
	}); err != nil {
		s.logger.Error("failed to insert expiry to outbox", "booking_id", bookingID, "error", err)
	}
	s.logger.Info("reservation expired", "booking_id", bookingID)
}

// InitSeats initializes seat counters when an event is approved.
func (s *InventoryService) InitSeats(ctx context.Context, eventID string, ticketTypes []domain.TicketTypeInfo) error {
	for _, tt := range ticketTypes {
		if err := s.seatCounter.Init(ctx, eventID, tt.TicketTypeID, tt.Quantity); err != nil {
			return err
		}
	}
	s.logger.Info("seat counters initialized", "event_id", eventID, "types", len(ticketTypes))
	return nil
}

// CancelBookings marks all bookings for an event as cancelled.
func (s *InventoryService) CancelBookings(ctx context.Context, eventID string) error {
	s.logger.Info("cancelling bookings for event", "event_id", eventID)
	return nil
}

// GetBooking returns a booking by ID.
func (s *InventoryService) GetBooking(ctx context.Context, bookingID string) (*domain.Booking, error) {
	return s.bookingRepo.FindByID(ctx, bookingID)
}

// ListMyBookings returns bookings for a user.
func (s *InventoryService) ListMyBookings(ctx context.Context, userID string) ([]domain.Booking, error) {
	return s.bookingRepo.ListByUser(ctx, userID)
}

// StartConsumers starts all Kafka consumers.
func (s *InventoryService) StartConsumers(ctx context.Context) error {
	s.consumer.OnPaymentCompleted(ctx, func(ctx context.Context, bookingID, transactionID string) error {
		s.logger.Info("payment completed received", "booking_id", bookingID, "transaction_id", transactionID)
		return s.Confirm(ctx, bookingID, transactionID)
	})

	s.consumer.OnPaymentFailed(ctx, func(ctx context.Context, bookingID string) error {
		s.logger.Info("payment failed received", "booking_id", bookingID)
		return s.Release(ctx, bookingID)
	})

	s.consumer.OnEventApproved(ctx, func(ctx context.Context, eventID string, ticketTypes []domain.TicketTypeInfo) error {
		s.logger.Info("event approved received", "event_id", eventID)
		return s.InitSeats(ctx, eventID, ticketTypes)
	})

	s.consumer.OnEventCancelled(ctx, func(ctx context.Context, eventID string) error {
		s.logger.Info("event cancelled received", "event_id", eventID)
		return s.CancelBookings(ctx, eventID)
	})

	go func() {
		if err := s.consumer.Start(ctx); err != nil {
			s.logger.Error("consumer error", "error", err)
		}
	}()

	return nil
}

// StartExpiryListener subscribes to Redis keyspace expiry events.
func (s *InventoryService) StartExpiryListener(ctx context.Context) {
	ch, err := s.reservationCache.SubscribeExpiry(ctx)
	if err != nil {
		s.logger.Error("failed to subscribe to keyspace events", "error", err)
		return
	}

		go func() {
			for {
				select {
				case <-ctx.Done():
					return
				case res := <-ch:
					s.HandleExpiry(ctx, res)
				}
			}
	}()
}

func calculateTotal(items []domain.BookingItem) int {
	total := 0
	for _, item := range items {
		total += item.UnitPriceCents * item.Quantity
	}
	return total
}

func reservationToPayload(res *domain.Reservation) sdomain.ReservationCreated {
	items := make([]sdomain.BookingItem, len(res.Items))
	for i, item := range res.Items {
		items[i] = sdomain.BookingItem{
			TicketTypeID:   item.TicketTypeID,
			Quantity:       item.Quantity,
			UnitPriceCents: item.UnitPriceCents,
		}
	}
	return sdomain.ReservationCreated{
		BookingID:  res.BookingID,
		UserID:     res.UserID,
		EventID:    res.EventID,
		Items:      items,
		TotalCents: res.TotalCents,
		At:         time.Now(),
	}
}
