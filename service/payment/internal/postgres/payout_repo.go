package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/unedtamps/ticketbus/pkg/dto"
	"github.com/unedtamps/ticketbus/pkg/outbox"
	"github.com/unedtamps/ticketbus/service/payment/internal/domain"
)

// PayoutRepo implements domain.PayoutRepository.
type PayoutRepo struct {
	pool     *pgxpool.Pool
	provider string
}

// NewPayoutRepo creates a new PayoutRepo.
func NewPayoutRepo(pool *pgxpool.Pool, provider string) *PayoutRepo {
	return &PayoutRepo{pool: pool, provider: provider}
}

// Prepare atomically:
//   - creates the payout ledger row for one attempt (a repeated delivery of
//     the same attempt is a no-op),
//   - locks the event's completed, non-refunded, not-yet-paid transactions,
//   - marks them pending and records the total,
//   - writes either a gateway disbursement job or a failed payout.completed
//     event when there is nothing to pay.
//
// The transaction-level rules decide which transactions belong to a payout:
// transactions already paid out are never included again, while transactions
// whose earlier attempt failed are included again by the next attempt.
func (r *PayoutRepo) Prepare(ctx context.Context, req dto.PayoutRequested) (*domain.PreparedPayout, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	payoutID := uuid.NewString()
	var attempt int
	err = tx.QueryRow(ctx, `
		INSERT INTO payouts (id, event_id, attempt, organizer_id, amount_rupiah, currency, status, provider)
		VALUES ($1,$2,$3,$4,0,'IDR','pending',$5)
		ON CONFLICT (event_id, attempt) DO NOTHING
		RETURNING attempt`,
		payoutID, req.EventID, req.Attempt, req.OrganizerID, r.provider,
	).Scan(&attempt)
	if errors.Is(err, pgx.ErrNoRows) {
		// This attempt was already prepared: Kafka redelivery or a repeated
		// request. Nothing to do.
		return &domain.PreparedPayout{Duplicate: true}, nil
	}
	if err != nil {
		return nil, err
	}

	rows, err := tx.Query(ctx, `
		SELECT id, amount_rupiah
		FROM transactions
		WHERE event_id = $1
		  AND status = 'completed'
		  AND refund_status = 'none'
		  AND payout_status IN ('none', 'failed')
		FOR UPDATE`, req.EventID)
	if err != nil {
		return nil, err
	}
	var ids []string
	var total int64
	for rows.Next() {
		var id string
		var amount int64
		if err := rows.Scan(&id, &amount); err != nil {
			rows.Close()
			return nil, err
		}
		ids = append(ids, id)
		total += amount
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}

	now := time.Now()
	payout := &domain.Payout{
		ID:           payoutID,
		EventID:      req.EventID,
		Attempt:      attempt,
		OrganizerID:  req.OrganizerID,
		AmountRupiah: total,
		Currency:     "IDR",
		Status:       domain.PayoutStatusPending,
		Provider:     r.provider,
		CreatedAt:    now,
		UpdatedAt:    now,
	}

	if total == 0 {
		// Nothing to pay: the attempt is recorded as failed and no gateway
		// call is made. A previous paid out attempt is not affected — the
		// event keeps its paid out state and the money already sent.
		if _, err := tx.Exec(ctx, `
			UPDATE payouts SET status='failed', amount_rupiah=0,
				failure_reason='no_earnings', updated_at=NOW()
			WHERE id=$1`, payoutID); err != nil {
			return nil, err
		}
		if err := outbox.NewStore(tx).Insert(ctx,
			dto.TopicPayoutCompleted, req.EventID, dto.PayoutCompleted{
				EventID:      req.EventID,
				Attempt:      attempt,
				Status:       domain.PayoutStatusFailed,
				AmountRupiah: 0,
				Reason:       "no_earnings",
				At:           now,
			}); err != nil {
			return nil, err
		}
		if err := tx.Commit(ctx); err != nil {
			return nil, err
		}
		payout.Status = domain.PayoutStatusFailed
		payout.FailureReason = "no_earnings"
		return &domain.PreparedPayout{Payout: payout, NoEarnings: true}, nil
	}

	if _, err := tx.Exec(ctx, `
		UPDATE transactions SET payout_status='pending', updated_at=NOW()
		WHERE id = ANY($1)`, ids); err != nil {
		return nil, err
	}
	if _, err := tx.Exec(ctx, `
		UPDATE payouts SET amount_rupiah=$2, updated_at=NOW() WHERE id=$1`,
		payoutID, total); err != nil {
		return nil, err
	}

	description := fmt.Sprintf("Event payout %s", req.EventID)
	if err := outbox.NewStore(tx).Insert(ctx,
		dto.TopicPaymentDisbursement, req.EventID, dto.PaymentDisbursementRequested{
			PayoutID:              payoutID,
			EventID:               req.EventID,
			Attempt:               attempt,
			AmountRupiah:          total,
			Currency:              "IDR",
			BankCode:              req.BankCode,
			BankAccountNumber:     req.BankAccountNumber,
			BankAccountHolderName: req.BankAccountHolderName,
			Description:           description,
		}); err != nil {
		return nil, err
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return &domain.PreparedPayout{Payout: payout, AmountRupiah: total}, nil
}

// Complete applies the gateway outcome to one attempt of the payout ledger and
// its transactions, then emits payout.completed. It is idempotent: only a
// pending attempt is updated and only pending transactions are moved.
func (r *PayoutRepo) Complete(
	ctx context.Context,
	eventID string,
	attempt int,
	status string,
	amountRupiah int64,
	providerRef, reason string,
) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	tag, err := tx.Exec(ctx, `
		UPDATE payouts SET
			amount_rupiah = $3,
			status = $4,
			provider_ref = NULLIF($5, ''),
			failure_reason = NULLIF($6, ''),
			updated_at = NOW()
		WHERE event_id = $1 AND attempt = $2::int AND status = 'pending'`,
		eventID, attempt, amountRupiah, status, providerRef, reason)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return tx.Commit(ctx)
	}

	// The transactions of this attempt are exactly the ones still pending for
	// the event: an attempt marks them pending when it starts and only one
	// attempt may be in flight per event.
	if _, err := tx.Exec(ctx, `
		UPDATE transactions SET payout_status = $2, updated_at = NOW()
		WHERE event_id = $1 AND payout_status = 'pending'`,
		eventID, status); err != nil {
		return err
	}

	if err := outbox.NewStore(tx).Insert(ctx,
		dto.TopicPayoutCompleted, eventID, dto.PayoutCompleted{
			EventID:      eventID,
			Attempt:      attempt,
			Status:       status,
			AmountRupiah: amountRupiah,
			ProviderRef:  providerRef,
			Reason:       reason,
			At:           time.Now(),
		}); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// FindByEvent returns one attempt of the payout ledger.
func (r *PayoutRepo) FindByEvent(ctx context.Context, eventID string, attempt int) (*domain.Payout, error) {
	var p domain.Payout
	err := r.pool.QueryRow(ctx, `
		SELECT id, event_id, attempt, organizer_id, amount_rupiah, currency, status,
		       provider, COALESCE(provider_ref, ''), COALESCE(failure_reason, ''),
		       created_at, updated_at
		FROM payouts WHERE event_id=$1 AND attempt=$2::int`, eventID, attempt).
		Scan(&p.ID, &p.EventID, &p.Attempt, &p.OrganizerID, &p.AmountRupiah, &p.Currency,
			&p.Status, &p.Provider, &p.ProviderRef, &p.FailureReason, &p.CreatedAt, &p.UpdatedAt)
	if err != nil {
		return nil, err
	}
	return &p, nil
}
