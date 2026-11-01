package application

import (
	"context"
	"time"

	sdomain "github.com/unedtamps/ticketbus/pkg/dto"
	"github.com/unedtamps/ticketbus/service/payment/internal/domain"
)

// PendingExpiryGrace is how long a transaction with a live gateway session is
// left alone after its deadline, giving the gateway's own expiry webhook time
// to arrive. Only deliveries that never came are picked up after this.
const PendingExpiryGrace = 10 * time.Minute

// ProcessExpired expires transactions the gateway will never report on.
//
// A transaction with a gateway session (pending) expires through the gateway's
// webhook — that is the normal path, and the sweeper stays out of it unless the
// webhook is already long overdue. A transaction that never got a session
// (initiated) is invisible to the gateway, so nothing else can expire it; the
// sweeper is the only thing that stops its seats from leaking.
func (s *PaymentService) ProcessExpired(ctx context.Context, limit int) error {
	txns, err := s.txnRepo.ListSweepableExpired(ctx, time.Now(), PendingExpiryGrace, limit)
	if err != nil {
		return err
	}
	for i := range txns {
		txn := &txns[i]
		// Only a transaction with a session has anything to cancel. A swept
		// pending row may still hold one whose webhook went missing.
		if txn.ProviderRef != "" {
			if err := s.processor.CancelSession(ctx, txn.ProviderRef); err != nil {
				s.logger.Warn("failed to cancel session at gateway", "txn_id", txn.ID, "error", err)
			}
		}
		applied, err := s.txnRepo.TransitionIfActive(
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
		s.logger.Info("expired transaction (sweeper)", "txn_id", txn.ID, "prior_status", txn.Status)
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
