package application

import (
	"context"
	"time"

	"github.com/nedo/TicketSaas/internal/payment/domain"
	sdomain "github.com/nedo/TicketSaas/internal/shared/domain"
)

// ProcessExpired expires pending transactions whose deadline passed and whose
// gateway expiry webhook never arrived (or was never created because the
// customer did not initiate a payment session).
func (s *PaymentService) ProcessExpired(ctx context.Context, limit int) error {
	txns, err := s.txnRepo.ListPendingExpired(ctx, time.Now(), limit)
	if err != nil {
		return err
	}
	for i := range txns {
		txn := &txns[i]
		if txn.ProviderRef != "" {
			if err := s.processor.CancelSession(ctx, txn.ProviderRef); err != nil {
				s.logger.Warn("failed to cancel session at gateway", "txn_id", txn.ID, "error", err)
			}
		}
		applied, err := s.txnRepo.UpdateStatusIfPending(
			ctx,
			txn.ID,
			domain.StatusExpired,
			txn.ProviderRef,
		)
		if err != nil {
			s.logger.Error("failed to expire transaction", "txn_id", txn.ID, "error", err)
			continue
		}
		if !applied {
			continue
		}
		_ = s.outbox.Insert(ctx, "payment.expired", txn.ID, sdomain.PaymentExpired{
			TransactionID: txn.ID,
			BookingID:     txn.BookingID,
			EventID:       txn.EventID,
			UserID:        txn.UserID,
			Reason:        "internal_expiry_fallback",
			At:            time.Now(),
		})
		s.logger.Info("expired transaction (poller)", "txn_id", txn.ID)
	}
	return nil
}

// StartExpiryPoller expires overdue transactions in the background.
func (s *PaymentService) StartExpiryPoller(ctx context.Context, intervalSec int) {
	go func() {
		time.Sleep(500 * time.Millisecond)
		ticker := time.NewTicker(time.Duration(intervalSec) * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if err := s.ProcessExpired(ctx, 100); err != nil {
					s.logger.Error("expiry poll failed", "error", err)
				}
			}
		}
	}()
}
