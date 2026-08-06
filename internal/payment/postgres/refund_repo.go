package postgres

import (
	"context"

	"github.com/nedo/TicketSaas/internal/payment/domain"
	shareddb "github.com/nedo/TicketSaas/internal/shared/db"
)

// RefundRepo implements domain.RefundRepository.
type RefundRepo struct {
	db shareddb.DBTx
}

// NewRefundRepo creates a new RefundRepo.
func NewRefundRepo(db shareddb.DBTx) *RefundRepo {
	return &RefundRepo{db: db}
}

// Create inserts a refund request. Idempotent per idempotency_key: re-creating
// the same refund (e.g. duplicate event cancellation) is a no-op.
func (r *RefundRepo) Create(ctx context.Context, refund *domain.RefundRequest) error {
	_, err := r.db.Exec(ctx, `
		INSERT INTO refund_requests (id, event_id, booking_id, transaction_id, customer_email, amount_rupiah, currency, status, reason, idempotency_key)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)
		ON CONFLICT (idempotency_key) DO NOTHING`,
		refund.ID, refund.EventID, refund.BookingID, refund.TransactionID,
		refund.CustomerEmail, refund.AmountRupiah, refund.Currency, refund.Status, refund.Reason, refund.IdempotencyKey)
	return err
}
