package postgres

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/unedtamps/ticketbus/pkg/dto"
	"github.com/unedtamps/ticketbus/pkg/outbox"
	"github.com/unedtamps/ticketbus/service/payment/internal/domain"
)

// RefundRepo implements domain.RefundRepository.
type RefundRepo struct {
	pool *pgxpool.Pool
}

// NewRefundRepo creates a new RefundRepo.
func NewRefundRepo(pool *pgxpool.Pool) *RefundRepo {
	return &RefundRepo{pool: pool}
}

// Create inserts a refund request. Idempotent per idempotency_key: re-creating
// the same refund (e.g. duplicate event cancellation) is a no-op.
func (r *RefundRepo) Create(ctx context.Context, refund *domain.RefundRequest) error {
	_, err := r.pool.Exec(ctx, `
		INSERT INTO refund_requests (id, event_id, booking_id, transaction_id, customer_email, amount_rupiah, currency, status, reason, idempotency_key, provider_ref)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)
		ON CONFLICT (idempotency_key) DO NOTHING`,
		refund.ID, refund.EventID, refund.BookingID, refund.TransactionID,
		refund.CustomerEmail, refund.AmountRupiah, refund.Currency, refund.Status, refund.Reason, refund.IdempotencyKey, refund.ProviderRef)
	return err
}

// ListPendingWithDestination returns pending refunds whose customer registered
// a refund destination, joined with the destination details — the executor's
// claim queue. Refunds without a destination wait (the customer is asked to
// register one); refunds past the 25-day guard stay pending for admin
// handling, since channels have refund windows (QRIS: 30 days).
func (r *RefundRepo) ListPendingWithDestination(ctx context.Context, limit int) ([]domain.RefundSweep, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT r.id, r.amount_rupiah, r.currency, r.attempt,
		       d.channel_code, d.account_number, d.account_holder_name
		FROM refund_requests r
		JOIN transactions t ON t.id = r.transaction_id
		JOIN customer_refund_destinations d ON d.user_id = t.user_id
		WHERE r.status = 'pending' AND r.created_at > NOW() - INTERVAL '25 days'
		ORDER BY r.created_at
		LIMIT $1`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []domain.RefundSweep
	for rows.Next() {
		var j domain.RefundSweep
		if err := rows.Scan(&j.RefundID, &j.AmountRupiah, &j.Currency, &j.Attempt,
			&j.ChannelCode, &j.AccountNumber, &j.AccountHolderName); err != nil {
			return nil, err
		}
		out = append(out, j)
	}
	return out, rows.Err()
}

// ClaimForDisbursement atomically moves one refund from pending to processing
// and writes the refund.disbursement outbox row in the same transaction — the
// claim and the payout job commit together, so a claimed refund is never left
// without a payout job after a crash. Reports whether the claim was applied.
func (r *RefundRepo) ClaimForDisbursement(
	ctx context.Context,
	refundID string,
	job dto.RefundDisbursementRequested,
) (bool, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return false, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	tag, err := tx.Exec(ctx,
		`UPDATE refund_requests SET status='processing', updated_at=NOW()
		 WHERE id=$1 AND status='pending'`, refundID)
	if err != nil {
		return false, err
	}
	if tag.RowsAffected() == 0 {
		return false, nil
	}
	if _, err := tx.Exec(ctx, `
		UPDATE transactions SET refund_status='processing', updated_at=NOW()
		WHERE id = (SELECT transaction_id FROM refund_requests WHERE id=$1)`,
		refundID); err != nil {
		return false, err
	}
	if err := outbox.NewStore(tx).Insert(ctx, dto.TopicRefundDisbursement, refundID, job); err != nil {
		return false, err
	}
	if err := tx.Commit(ctx); err != nil {
		return false, err
	}
	return true, nil
}

// AttachProviderRef stores the gateway payout id on a processing refund after
// the gateway accepted the disbursement.
func (r *RefundRepo) AttachProviderRef(ctx context.Context, refundID, providerRef string) error {
	_, err := r.pool.Exec(ctx, `
		UPDATE refund_requests SET provider_ref=$2, updated_at=NOW() WHERE id=$1`,
		refundID, providerRef)
	return err
}

// Complete settles a processing refund and writes the refund.completed outbox
// row in the same transaction, so a refund can never settle without the rest of
// the system hearing about it. The transaction ledger is updated in the same
// transaction so the customer-facing payment view stays consistent. The
// processing-only guard makes it idempotent against concurrent webhook
// deliveries; an already-settled refund is a no-op rather than an error.
func (r *RefundRepo) Complete(ctx context.Context, refundID, status, providerRef, failureReason string) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var bookingID, transactionID, eventID, email, currency string
	var amount int64
	err = tx.QueryRow(ctx, `
		UPDATE refund_requests
		SET status=$2, provider_ref=$3, failure_reason=NULLIF($4,''),
		    processed_at=NOW(), updated_at=NOW()
		WHERE id=$1 AND status='processing'
		RETURNING booking_id, transaction_id, event_id,
		          COALESCE(customer_email,''), amount_rupiah, currency`,
		refundID, status, providerRef, failureReason).
		Scan(&bookingID, &transactionID, &eventID, &email, &amount, &currency)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			// Already settled by a concurrent webhook or by reconciliation.
			return nil
		}
		return err
	}
	if _, err := tx.Exec(ctx, `
		UPDATE transactions SET refund_status=$2, updated_at=NOW() WHERE id=$1`,
		transactionID, status); err != nil {
		return err
	}
	payload := dto.RefundCompleted{
		BookingID:     bookingID,
		RefundID:      refundID,
		EventID:       eventID,
		Email:         email,
		AmountRupiah:  amount,
		Currency:      currency,
		Status:        status,
		FailureReason: failureReason,
		At:            time.Now(),
	}
	if err := outbox.NewStore(tx).Insert(ctx, dto.TopicRefundCompleted, bookingID, payload); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// FindByProviderRef locates the refund that owns a gateway payout id — payout
// webhooks address refunds by their gateway payout id.
func (r *RefundRepo) FindByProviderRef(ctx context.Context, providerRef string) (*domain.RefundRequest, error) {
	var refund domain.RefundRequest
	err := r.pool.QueryRow(ctx, `
		SELECT id, event_id, booking_id, transaction_id, customer_email, amount_rupiah,
		       currency, status, reason, provider_ref, idempotency_key, created_at, updated_at, processed_at
		FROM refund_requests WHERE provider_ref = $1`, providerRef).
		Scan(&refund.ID, &refund.EventID, &refund.BookingID, &refund.TransactionID,
			&refund.CustomerEmail, &refund.AmountRupiah, &refund.Currency, &refund.Status,
			&refund.Reason, &refund.ProviderRef, &refund.IdempotencyKey,
			&refund.CreatedAt, &refund.UpdatedAt, &refund.ProcessedAt)
	if err != nil {
		return nil, err
	}
	return &refund, nil
}

// ListDispatchedStale returns processing refunds already sent to the gateway
// (provider_ref set) and untouched since olderThan — the reconciliation poll's
// work queue.
func (r *RefundRepo) ListDispatchedStale(ctx context.Context, olderThan time.Time, limit int) ([]*domain.RefundRequest, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT id, event_id, booking_id, transaction_id, customer_email, amount_rupiah,
		       currency, status, reason, provider_ref, idempotency_key, created_at, updated_at, processed_at
		FROM refund_requests
		WHERE status = 'processing' AND provider_ref <> '' AND updated_at < $1
		ORDER BY updated_at LIMIT $2`, olderThan, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []*domain.RefundRequest
	for rows.Next() {
		refund := &domain.RefundRequest{}
		if err := rows.Scan(&refund.ID, &refund.EventID, &refund.BookingID, &refund.TransactionID,
			&refund.CustomerEmail, &refund.AmountRupiah, &refund.Currency, &refund.Status,
			&refund.Reason, &refund.ProviderRef, &refund.IdempotencyKey,
			&refund.CreatedAt, &refund.UpdatedAt, &refund.ProcessedAt); err != nil {
			return nil, err
		}
		out = append(out, refund)
	}
	return out, rows.Err()
}

// ListAll returns refunds for the admin refund table, newest first, optionally
// filtered by status.
func (r *RefundRepo) ListAll(ctx context.Context, status string, limit, offset int) ([]*domain.RefundRequest, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT r.id, r.event_id, r.booking_id, r.transaction_id, r.customer_email, r.amount_rupiah,
		       r.currency, r.status, r.reason, r.provider_ref, r.idempotency_key,
		       r.created_at, r.updated_at, r.processed_at, COALESCE(r.failure_reason, ''),
		       (d.user_id IS NOT NULL) AS has_destination
		FROM refund_requests r
		JOIN transactions t ON t.id = r.transaction_id
		LEFT JOIN customer_refund_destinations d ON d.user_id = t.user_id
		WHERE ($1 = '' OR r.status = $1)
		ORDER BY r.created_at DESC LIMIT $2 OFFSET $3`, status, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []*domain.RefundRequest
	for rows.Next() {
		refund := &domain.RefundRequest{}
		if err := rows.Scan(&refund.ID, &refund.EventID, &refund.BookingID, &refund.TransactionID,
			&refund.CustomerEmail, &refund.AmountRupiah, &refund.Currency, &refund.Status,
			&refund.Reason, &refund.ProviderRef, &refund.IdempotencyKey,
			&refund.CreatedAt, &refund.UpdatedAt, &refund.ProcessedAt,
			&refund.FailureReason, &refund.HasDestination); err != nil {
			return nil, err
		}
		out = append(out, refund)
	}
	return out, rows.Err()
}

// ListByCustomer returns the caller's own refunds, newest first, joined with
// the destination the payout was sent to. Scoped by transactions.user_id so a
// customer can only ever see their own rows.
func (r *RefundRepo) ListByCustomer(ctx context.Context, userID string, limit, offset int) ([]*domain.CustomerRefund, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT r.id, r.booking_id, r.event_id, r.amount_rupiah, r.currency,
		       r.status, r.reason, COALESCE(r.failure_reason, ''),
		       COALESCE(d.channel_code, ''), COALESCE(d.account_number, ''),
		       (d.user_id IS NOT NULL) AS has_account,
		       r.created_at, r.processed_at
		FROM refund_requests r
		JOIN transactions t ON t.id = r.transaction_id
		LEFT JOIN customer_refund_destinations d ON d.user_id = t.user_id
		WHERE t.user_id = $1
		ORDER BY r.created_at DESC LIMIT $2 OFFSET $3`, userID, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []*domain.CustomerRefund
	for rows.Next() {
		refund := &domain.CustomerRefund{}
		if err := rows.Scan(&refund.ID, &refund.BookingID, &refund.EventID,
			&refund.AmountRupiah, &refund.Currency, &refund.Status, &refund.Reason,
			&refund.FailureReason, &refund.ChannelCode, &refund.AccountNumber,
			&refund.HasAccount, &refund.CreatedAt, &refund.ProcessedAt); err != nil {
			return nil, err
		}
		out = append(out, refund)
	}
	return out, rows.Err()
}

// ResetForRetry returns a failed or stuck processing refund to the pending
// queue and bumps its attempt counter — the Payouts idempotency key includes
// the attempt, so the retry gets a fresh gateway payout instead of replaying
// the failed one. Reports whether the reset was applied.
func (r *RefundRepo) ResetForRetry(ctx context.Context, refundID string) (bool, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return false, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var transactionID string
	err = tx.QueryRow(ctx, `
		UPDATE refund_requests r
		SET status='pending', attempt = r.attempt + 1, provider_ref=NULL,
		    failure_reason=NULL, processed_at=NULL, updated_at=NOW()
		WHERE r.id=$1 AND r.status IN ('failed', 'processing')
		  AND EXISTS (
			SELECT 1 FROM transactions t
			JOIN customer_refund_destinations d ON d.user_id = t.user_id
			WHERE t.id = r.transaction_id
		  )
		RETURNING r.transaction_id`, refundID).Scan(&transactionID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return false, nil
		}
		return false, err
	}
	if _, err := tx.Exec(ctx, `
		UPDATE transactions SET refund_status='pending', updated_at=NOW() WHERE id=$1`,
		transactionID); err != nil {
		return false, err
	}
	if err := tx.Commit(ctx); err != nil {
		return false, err
	}
	return true, nil
}
