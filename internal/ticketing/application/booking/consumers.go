package booking

import "context"

// StartConsumers starts all Kafka consumers.
func (s *BookingService) StartConsumers(ctx context.Context) error {
	s.consumer.OnPaymentCompleted(
		ctx,
		func(ctx context.Context, bookingID, transactionID string) error {
			s.logger.Info(
				"payment completed received",
				"booking_id",
				bookingID,
				"transaction_id",
				transactionID,
			)
			return s.Confirm(ctx, bookingID, transactionID)
		},
	)

	s.consumer.OnPaymentFailed(ctx, func(ctx context.Context, bookingID string) error {
		s.logger.Info("payment failed received", "booking_id", bookingID)
		return s.ExpireOnPaymentFailed(ctx, bookingID)
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
