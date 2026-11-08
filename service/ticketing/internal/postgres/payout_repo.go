package postgres

import (
	"context"

	sdomain "github.com/unedtamps/ticketbus/pkg/dto"
	"github.com/unedtamps/ticketbus/pkg/outbox"
	"github.com/unedtamps/ticketbus/service/ticketing/internal/domain"
)

// UpsertPayoutAccount stores (or replaces) the organizer's payout destination.
func (r *EventRepo) UpsertPayoutAccount(ctx context.Context, acct *domain.OrganizerPayoutAccount) error {
	return r.pool.QueryRow(ctx, `
		INSERT INTO organizer_payout_accounts
			(organizer_id, bank_code, bank_account_number, bank_account_holder_name)
		VALUES ($1,$2,$3,$4)
		ON CONFLICT (organizer_id) DO UPDATE SET
			bank_code = EXCLUDED.bank_code,
			bank_account_number = EXCLUDED.bank_account_number,
			bank_account_holder_name = EXCLUDED.bank_account_holder_name,
			updated_at = NOW()
		RETURNING created_at, updated_at`,
		acct.OrganizerID, acct.BankCode, acct.BankAccountNumber, acct.BankAccountHolderName,
	).Scan(&acct.CreatedAt, &acct.UpdatedAt)
}

// FindPayoutAccount returns the organizer's saved payout account.
func (r *EventRepo) FindPayoutAccount(ctx context.Context, organizerID string) (*domain.OrganizerPayoutAccount, error) {
	var a domain.OrganizerPayoutAccount
	err := r.pool.QueryRow(ctx, `
		SELECT organizer_id, bank_code, bank_account_number, bank_account_holder_name,
		       created_at, updated_at
		FROM organizer_payout_accounts WHERE organizer_id=$1`, organizerID).
		Scan(&a.OrganizerID, &a.BankCode, &a.BankAccountNumber, &a.BankAccountHolderName,
			&a.CreatedAt, &a.UpdatedAt)
	if err != nil {
		return nil, err
	}
	return &a, nil
}

// RequestPayout inserts the next payout attempt for an event and the
// payout.requested outbox event in one transaction.
//
// The attempt number is allocated inside the transaction, so two concurrent
// requests cannot claim the same one (the unique constraint turns the loser
// into a conflict). An attempt that is still pending blocks new requests: the
// organizer waits for the gateway result before asking again.
func (r *EventRepo) RequestPayout(
	ctx context.Context,
	attempt *domain.EventPayoutAttempt,
	payload func(attempt int) any,
) (bool, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return false, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var next int
	if err := tx.QueryRow(ctx,
		`SELECT COALESCE(MAX(attempt), 0) + 1 FROM event_payout_attempts WHERE event_id = $1`,
		attempt.EventID,
	).Scan(&next); err != nil {
		return false, err
	}

	var pending bool
	if err := tx.QueryRow(ctx,
		`SELECT EXISTS (SELECT 1 FROM event_payout_attempts WHERE event_id = $1 AND status = 'pending')`,
		attempt.EventID,
	).Scan(&pending); err != nil {
		return false, err
	}
	if pending {
		return false, nil
	}

	attempt.Attempt = next
	_, err = tx.Exec(ctx, `
		INSERT INTO event_payout_attempts
			(id, event_id, attempt, organizer_id, bank_code, bank_account_number,
			 bank_account_holder_name, status, requested_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,'pending',NOW())`,
		attempt.ID, attempt.EventID, attempt.Attempt, attempt.OrganizerID,
		attempt.BankCode, attempt.BankAccountNumber, attempt.BankAccountHolderName)
	if err != nil {
		return false, err
	}

	if err := outbox.NewStore(tx).Insert(
		ctx, sdomain.TopicPayoutRequested, attempt.EventID, payload(attempt.Attempt),
	); err != nil {
		return false, err
	}
	if err := tx.Commit(ctx); err != nil {
		return false, err
	}
	return true, nil
}

// ListPayoutAttemptsForAdmin returns the latest payout attempt of every event
// that has one, newest first, with the event title so the admin queue is
// readable. One row per event: the newest attempt carries the current state.
//
// CollectableRupiah is what a retry would actually send: confirmed revenue that
// was never paid out. A failed attempt with 0 collectable is the `no_earnings`
// case — retrying it would fail identically, so the UI disables the button and
// the service rejects the request.
func (r *EventRepo) ListPayoutAttemptsForAdmin(
	ctx context.Context,
	limit int,
) ([]domain.AdminPayoutRow, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT a.id, a.event_id, COALESCE(e.title, ''), a.attempt, a.organizer_id,
		       a.bank_code, a.bank_account_number, a.bank_account_holder_name,
		       a.status, COALESCE(a.amount_rupiah, 0), a.provider,
		       COALESCE(a.provider_ref, ''), COALESCE(a.failure_reason, ''),
		       a.requested_at, a.completed_at,
		       -- What a retry would pay: confirmed revenue not yet paid out.
		       GREATEST(0,
		           COALESCE((SELECT SUM(bi.total_price_rupiah)
		                     FROM bookings b
		                     JOIN booking_items bi ON bi.booking_id = b.id
		                     WHERE b.event_id = a.event_id AND b.status = 'confirmed'), 0)
		         - COALESCE((SELECT SUM(paid.amount_rupiah)
		                     FROM event_payout_attempts paid
		                     WHERE paid.event_id = a.event_id AND paid.status = 'paid_out'), 0)
		       ) AS collectable_rupiah
		FROM event_payout_attempts a
		LEFT JOIN events e ON e.id = a.event_id
		WHERE a.attempt = (
			SELECT MAX(inner_a.attempt) FROM event_payout_attempts inner_a
			WHERE inner_a.event_id = a.event_id
		)
		ORDER BY a.updated_at DESC
		LIMIT $1`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []domain.AdminPayoutRow
	for rows.Next() {
		var row domain.AdminPayoutRow
		var status string
		if err := rows.Scan(
			&row.ID, &row.EventID, &row.EventTitle, &row.Attempt, &row.OrganizerID,
			&row.BankCode, &row.BankAccountNumber, &row.BankAccountHolderName,
			&status, &row.AmountRupiah, &row.Provider, &row.ProviderRef,
			&row.FailureReason, &row.RequestedAt, &row.CompletedAt,
			&row.CollectableRupiah,
		); err != nil {
			return nil, err
		}
		row.Status = sdomain.PayoutStatus(status)
		out = append(out, row)
	}
	return out, rows.Err()
}

// ListPayoutAttempts returns every payout attempt of an event, oldest first.
func (r *EventRepo) ListPayoutAttempts(ctx context.Context, eventID string) ([]domain.EventPayoutAttempt, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT id, event_id, attempt, organizer_id, bank_code, bank_account_number,
		       bank_account_holder_name, status, COALESCE(amount_rupiah, 0),
		       provider, COALESCE(provider_ref, ''), COALESCE(failure_reason, ''),
		       requested_at, completed_at, created_at, updated_at
		FROM event_payout_attempts WHERE event_id = $1 ORDER BY attempt ASC`, eventID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var attempts []domain.EventPayoutAttempt
	for rows.Next() {
		var a domain.EventPayoutAttempt
		var status string
		if err := rows.Scan(
			&a.ID, &a.EventID, &a.Attempt, &a.OrganizerID, &a.BankCode,
			&a.BankAccountNumber, &a.BankAccountHolderName, &status, &a.AmountRupiah,
			&a.Provider, &a.ProviderRef, &a.FailureReason, &a.RequestedAt,
			&a.CompletedAt, &a.CreatedAt, &a.UpdatedAt,
		); err != nil {
			return nil, err
		}
		a.Status = sdomain.PayoutStatus(status)
		attempts = append(attempts, a)
	}
	return attempts, rows.Err()
}

// SetPayoutResult applies the gateway result to one attempt while it is still
// pending. Returns false for duplicate deliveries.
func (r *EventRepo) SetPayoutResult(
	ctx context.Context,
	eventID string,
	attempt int,
	status sdomain.PayoutStatus,
	amountRupiah int64,
	providerRef, reason string,
) (bool, error) {
	tag, err := r.pool.Exec(ctx, `
		UPDATE event_payout_attempts SET
			status = $3,
			amount_rupiah = $4,
			provider_ref = NULLIF($5, ''),
			failure_reason = NULLIF($6, ''),
			completed_at = NOW(),
			updated_at = NOW()
		WHERE event_id = $1 AND attempt = $2::int AND status = 'pending'`,
		eventID, attempt, status, amountRupiah, providerRef, reason)
	if err != nil {
		return false, err
	}
	return tag.RowsAffected() > 0, nil
}

// PayoutSummary aggregates the attempts of an event into the event-level
// payout state.
//
// The status rule keeps a successful payout visible: if any attempt paid out,
// the event counts as paid_out even when a later attempt failed (a retry or a
// top-up with nothing left to collect). The amount is the sum of the paid out
// attempts — what the organizer actually received.
func (r *EventRepo) PayoutSummary(ctx context.Context, eventID string) (*domain.EventPayoutSummary, error) {
	attempts, err := r.ListPayoutAttempts(ctx, eventID)
	if err != nil {
		return nil, err
	}
	summary := &domain.EventPayoutSummary{
		EventID:      eventID,
		Status:       sdomain.PayoutStatusNone,
		Attempts:     len(attempts),
		AttemptsList: attempts,
	}
	if len(attempts) == 0 {
		return summary, nil
	}

	latest := attempts[len(attempts)-1]
	summary.RequestedAt = &latest.RequestedAt
	summary.FailureReason = latest.FailureReason
	summary.ProviderRef = latest.ProviderRef
	summary.CompletedAt = latest.CompletedAt

	for _, a := range attempts {
		if a.Status == sdomain.PayoutStatusPaidOut {
			summary.AmountRupiah += a.AmountRupiah
			summary.Status = sdomain.PayoutStatusPaidOut
			if a.CompletedAt != nil {
				summary.CompletedAt = a.CompletedAt
			}
		}
	}
	if summary.Status != sdomain.PayoutStatusPaidOut {
		summary.Status = latest.Status
		if latest.AmountRupiah > 0 {
			summary.AmountRupiah = latest.AmountRupiah
		}
	}
	return summary, nil
}

// CollectableRupiah returns what a new payout attempt would actually send:
// confirmed booking revenue minus every amount already paid out. Zero means a
// retry has nothing to pay (the no_earnings case).
func (r *EventRepo) CollectableRupiah(ctx context.Context, eventID string) int64 {
	var collectable int64
	_ = r.pool.QueryRow(ctx, `
		SELECT GREATEST(0,
		    COALESCE((SELECT SUM(bi.total_price_rupiah)
		              FROM bookings b
		              JOIN booking_items bi ON bi.booking_id = b.id
		              WHERE b.event_id = $1 AND b.status = 'confirmed'), 0)
		  - COALESCE((SELECT SUM(paid.amount_rupiah)
		              FROM event_payout_attempts paid
		              WHERE paid.event_id = $1 AND paid.status = 'paid_out'), 0)
		)`, eventID).Scan(&collectable)
	return collectable
}
