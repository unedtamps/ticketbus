package application

import (
	"context"

	"github.com/google/uuid"

	"github.com/nedo/TicketSaas/internal/payment/domain"
)

// HandleEventCancelled creates refund requests for completed transactions of
// a cancelled event. Pending payments are left to run their course; late
// completions are picked up by a later cancel-reprocess. Idempotent per
// transaction.
func (s *PaymentService) HandleEventCancelled(ctx context.Context, eventID string) error {
	txns, err := s.txnRepo.ListByEventID(ctx, eventID)
	if err != nil {
		return err
	}

	for i := range txns {
		txn := &txns[i]
		switch txn.Status {
		case domain.StatusCompleted:
			refund := &domain.RefundRequest{
				ID:             uuid.NewString(),
				EventID:        txn.EventID,
				BookingID:      txn.BookingID,
				TransactionID:  txn.ID,
				CustomerEmail:  txn.CustomerEmail,
				AmountCents:    int64(txn.AmountCents),
				Currency:       txn.Currency,
				Status:         domain.RefundPending,
				Reason:         "event_cancelled",
				IdempotencyKey: "event-cancelled:" + txn.ID,
			}
			if err := s.refundRepo.Create(ctx, refund); err != nil {
				return err
			}
			if err := s.txnRepo.UpdateRefundStatus(ctx, txn.ID, "pending"); err != nil {
				return err
			}
			s.logger.Info(
				"refund requested for cancelled event",
				"event_id",
				eventID,
				"transaction_id",
				txn.ID,
			)

		case domain.StatusPending:
			// Pending payments are intentionally left to run their course:
			// the customer may still complete the payment, and the session
			// expires on its own. Any transaction that completes after this
			// pass is picked up by a later cancel-reprocess of event.cancelled
			// and refunded then.
			s.logger.Info(
				"pending payment left for cancelled event",
				"event_id",
				eventID,
				"transaction_id",
				txn.ID,
			)
		}
	}
	return nil
}

// StartConsumer starts the reservation and event lifecycle listeners.
func (s *PaymentService) StartConsumer(ctx context.Context) error {
	s.consumer.OnEventCancelled(ctx, func(ctx context.Context, eventID string) error {
		s.logger.Info("event cancelled received", "event_id", eventID)
		return s.HandleEventCancelled(ctx, eventID)
	})

	go func() {
		if err := s.consumer.Start(ctx); err != nil {
			s.logger.Error("consumer error", "error", err)
		}
	}()
	return nil
}
