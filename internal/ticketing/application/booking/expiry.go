package booking

import (
	"context"
	"time"
)

// HandleExpiry processes a reservation that expired via the Redis TTL trigger.
func (s *BookingService) HandleExpiry(ctx context.Context, bookingID string) {
	booking, err := s.bookingRepo.FindByID(ctx, bookingID)
	if err != nil {
		s.logger.Warn("expiry skipped, booking not found", "booking_id", bookingID)
		return
	}
	s.expireBooking(ctx, booking)
}

// ExpireDueBookings expires all pending bookings past their expires_at
// (recovery for expiry triggers that were missed while the service was down).
func (s *BookingService) ExpireDueBookings(ctx context.Context) {
	const batchSize = 1000
	for {
		bookings, err := s.bookingRepo.ListExpiredPending(ctx, time.Now(), batchSize)
		if err != nil {
			s.logger.Error("failed to list expired pending bookings", "error", err)
			return
		}
		if len(bookings) == 0 {
			return
		}
		for i := range bookings {
			s.expireBooking(ctx, &bookings[i])
		}
		if len(bookings) < batchSize {
			return
		}
	}
}

// StartExpiryListener subscribes to Redis keyspace expiry events.
func (s *BookingService) StartExpiryListener(ctx context.Context) {
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
			case bookingID := <-ch:
				s.HandleExpiry(ctx, bookingID)
			}
		}
	}()
}

// StartExpiryRecovery runs the PostgreSQL recovery sweep once at startup and
// then periodically. It expires pending bookings whose expires_at has passed
// but whose Redis trigger was missed.
func (s *BookingService) StartExpiryRecovery(ctx context.Context, interval time.Duration) {
	s.ExpireDueBookings(ctx)
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				s.ExpireDueBookings(ctx)
			}
		}
	}()
}
