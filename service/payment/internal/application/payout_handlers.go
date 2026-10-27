package application

import (
	"context"

	sdomain "github.com/unedtamps/ticketbus/pkg/dto"
)

// HandlePayoutRequested prepares an event payout: the repository atomically
// records/resets the payout ledger row, marks the event's eligible
// transactions pending, and writes either the gateway disbursement job or a
// failed payout.completed event when there is nothing to pay.
//
// It is idempotent under Kafka redelivery: an existing pending/paid_out payout
// makes the call a no-op.
func (s *PaymentService) HandlePayoutRequested(
	ctx context.Context,
	evt sdomain.PayoutRequested,
) error {
	prepared, err := s.payoutRepo.Prepare(ctx, evt)
	if err != nil {
		return err
	}
	switch {
	case prepared.Duplicate:
		s.logger.Info("duplicate payout request ignored", "event_id", evt.EventID)
	case prepared.NoEarnings:
		s.logger.Info("payout has no earnings", "event_id", evt.EventID)
	default:
		s.logger.Info(
			"payout prepared",
			"event_id", evt.EventID,
			"payout_id", prepared.Payout.ID,
			"amount_rupiah", prepared.AmountRupiah,
		)
	}
	return nil
}
