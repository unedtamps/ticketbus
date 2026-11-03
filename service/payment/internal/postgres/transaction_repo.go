package postgres

import (
	"context"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/unedtamps/ticketbus/pkg/dto"
	"github.com/unedtamps/ticketbus/pkg/outbox"
	"github.com/unedtamps/ticketbus/service/payment/internal/domain"
)

// TransactionRepo implements domain.TransactionRepository.
type TransactionRepo struct {
	pool *pgxpool.Pool
}

// NewTransactionRepo creates a new TransactionRepo.
func NewTransactionRepo(pool *pgxpool.Pool) *TransactionRepo {
	return &TransactionRepo{pool: pool}
}

const txnColumns = `id, user_id, booking_id, event_id, amount_rupiah, currency, status, provider, provider_ref, payment_link_url, customer_email, refund_status, payout_status, expires_at, created_at, updated_at`

func scanTxn(row interface{ Scan(...any) error }) (*domain.Transaction, error) {
	var t domain.Transaction
	err := row.Scan(
		&t.ID, &t.UserID, &t.BookingID, &t.EventID, &t.AmountRupiah, &t.Currency,
		&t.Status, &t.Provider, &t.ProviderRef, &t.PaymentLinkURL, &t.CustomerEmail,
		&t.RefundStatus, &t.PayoutStatus, &t.ExpiresAt, &t.CreatedAt, &t.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}
	return &t, nil
}

// Create inserts a new transaction.
func (r *TransactionRepo) Create(ctx context.Context, txn *domain.Transaction) error {
	_, err := r.pool.Exec(ctx, `
		INSERT INTO transactions (id, user_id, booking_id, event_id, amount_rupiah, currency, status, provider, provider_ref, payment_link_url, customer_email, refund_status, expires_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13)`,
		txn.ID, txn.UserID, txn.BookingID, txn.EventID, txn.AmountRupiah, txn.Currency,
		txn.Status, txn.Provider, txn.ProviderRef, txn.PaymentLinkURL, txn.CustomerEmail,
		txn.RefundStatus, txn.ExpiresAt)
	return err
}

// FindByID retrieves a transaction by ID.
func (r *TransactionRepo) FindByID(ctx context.Context, id string) (*domain.Transaction, error) {
	row := r.pool.QueryRow(ctx, `SELECT `+txnColumns+` FROM transactions WHERE id=$1`, id)
	return scanTxn(row)
}

// FindByBookingID retrieves a transaction by booking ID.
func (r *TransactionRepo) FindByBookingID(ctx context.Context, bookingID string) (*domain.Transaction, error) {
	row := r.pool.QueryRow(ctx, `SELECT `+txnColumns+` FROM transactions WHERE booking_id=$1 ORDER BY created_at DESC LIMIT 1`, bookingID)
	return scanTxn(row)
}

// UpdateStatus updates transaction status and provider reference.
func (r *TransactionRepo) UpdateStatus(ctx context.Context, id, status, providerRef string) error {
	_, err := r.pool.Exec(ctx, `UPDATE transactions SET status=$1, provider_ref=$2, updated_at=NOW() WHERE id=$3`, status, providerRef, id)
	return err
}

// TransitionIfActive transitions the transaction only when it is still in an
// active (non-terminal) status, so concurrent paths (webhook vs poll) cannot
// race.
func (r *TransactionRepo) TransitionIfActive(ctx context.Context, id, status, providerRef string) (bool, error) {
	tag, err := r.pool.Exec(ctx,
		`UPDATE transactions SET status=$1, provider_ref=$2, updated_at=NOW() WHERE id=$3 AND status IN (`+statusInClause(domain.ActiveStatuses())+`)`,
		status, providerRef, id)
	if err != nil {
		return false, err
	}
	return tag.RowsAffected() > 0, nil
}

// CompleteIfActive transitions an active transaction to completed and writes
// the payment.completed outbox row in one database transaction.
//
// Running the two as separate statements meant a failed insert (or a crash
// between them) left the money completed with ticketing never told: the
// transition never re-fires, a redelivered webhook is answered as a duplicate,
// and the payment.completed row that would carry the news simply never
// existed. The pair is the payment-side half of "customer paid → tickets
// issued", so it commits or rolls back together.
//
// It reports whether the transition was applied — false means the transaction
// was already settled (duplicate delivery) or expired, which the caller
// handles.
func (r *TransactionRepo) CompleteIfActive(ctx context.Context, txn *domain.Transaction) (bool, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return false, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	tag, err := tx.Exec(ctx,
		`UPDATE transactions SET status=$1, provider_ref=$2, updated_at=NOW()
		 WHERE id=$3 AND status IN (`+statusInClause(domain.ActiveStatuses())+`)`,
		domain.StatusCompleted, txn.ProviderRef, txn.ID)
	if err != nil {
		return false, err
	}
	if tag.RowsAffected() == 0 {
		return false, nil
	}

	if err := outbox.NewStore(tx).Insert(ctx, "payment.completed", txn.ID, dto.PaymentCompleted{
		TransactionID: txn.ID,
		BookingID:     txn.BookingID,
		EventID:       txn.EventID,
		UserID:        txn.UserID,
		At:            time.Now(),
	}); err != nil {
		return false, err
	}

	if err := tx.Commit(ctx); err != nil {
		return false, err
	}
	return true, nil
}

// ExpireIfActive transitions an active transaction to expired and writes the
// payment.expired outbox row in one database transaction — same reasoning as
// CompleteIfActive: a lost row after a standalone transition leaves the
// booking pending forever and its seats held, and neither the gateway (the
// session is already expired) nor the sweeper (the transaction is terminal)
// ever revisits it. reason distinguishes the gateway's own expiry from the
// sweeper's fallback.
//
// It reports whether the transition was applied; false means the transaction
// was not in an active state (duplicate delivery or already settled).
func (r *TransactionRepo) ExpireIfActive(ctx context.Context, txn *domain.Transaction, reason string) (bool, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return false, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	tag, err := tx.Exec(ctx,
		`UPDATE transactions SET status=$1, provider_ref=$2, updated_at=NOW()
		 WHERE id=$3 AND status IN (`+statusInClause(domain.ActiveStatuses())+`)`,
		domain.StatusExpired, txn.ProviderRef, txn.ID)
	if err != nil {
		return false, err
	}
	if tag.RowsAffected() == 0 {
		return false, nil
	}

	if err := outbox.NewStore(tx).Insert(ctx, "payment.expired", txn.ID, dto.PaymentExpired{
		TransactionID: txn.ID,
		BookingID:     txn.BookingID,
		EventID:       txn.EventID,
		UserID:        txn.UserID,
		Reason:        reason,
		At:            time.Now(),
	}); err != nil {
		return false, err
	}

	if err := tx.Commit(ctx); err != nil {
		return false, err
	}
	return true, nil
}

// TransitionIfInitiated transitions the transaction only while it is still
// initiated. Used for the initiated → pending step so a concurrent
// ProcessPayment cannot clobber an existing session reference.
func (r *TransactionRepo) TransitionIfInitiated(ctx context.Context, id, status, providerRef string) (bool, error) {
	tag, err := r.pool.Exec(ctx,
		`UPDATE transactions SET status=$1, provider_ref=$2, updated_at=NOW() WHERE id=$3 AND status='initiated'`,
		status, providerRef, id)
	if err != nil {
		return false, err
	}
	return tag.RowsAffected() > 0, nil
}

func statusInClause(statuses []string) string {
	quoted := make([]string, len(statuses))
	for i, s := range statuses {
		quoted[i] = "'" + s + "'"
	}
	return strings.Join(quoted, ",")
}

// UpdateSession stores the gateway payment session details.
func (r *TransactionRepo) UpdateSession(ctx context.Context, id, providerRef, paymentLinkURL string) error {
	_, err := r.pool.Exec(ctx,
		`UPDATE transactions SET provider_ref=$1, payment_link_url=$2, updated_at=NOW() WHERE id=$3`,
		providerRef, paymentLinkURL, id)
	return err
}

// UpdateRefundStatus updates the refund status of a transaction.
func (r *TransactionRepo) UpdateRefundStatus(ctx context.Context, id, refundStatus string) error {
	_, err := r.pool.Exec(ctx, `UPDATE transactions SET refund_status=$1, updated_at=NOW() WHERE id=$2`, refundStatus, id)
	return err
}

// ListByUser returns transactions for a user.
func (r *TransactionRepo) ListByUser(ctx context.Context, userID string) ([]domain.Transaction, error) {
	rows, err := r.pool.Query(ctx, `SELECT `+txnColumns+` FROM transactions WHERE user_id=$1 ORDER BY created_at DESC`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return collectTxns(rows)
}

// ListByEventID returns transactions for an event.
func (r *TransactionRepo) ListByEventID(ctx context.Context, eventID string) ([]domain.Transaction, error) {
	rows, err := r.pool.Query(ctx, `SELECT `+txnColumns+` FROM transactions WHERE event_id=$1 ORDER BY created_at DESC`, eventID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return collectTxns(rows)
}

// ListSweepableExpired returns transactions the expiry sweeper owns.
//
// Two disjoint sets, so the sweeper never races the gateway webhook:
//   - initiated: the customer never opened a checkout session, so the gateway
//     knows nothing about this transaction and will never send a webhook. These
//     are swept as soon as the deadline passes.
//   - pending: a session exists and the gateway should report its expiry. The
//     grace window only catches deliveries that never arrived; by then any
//     in-flight webhook has long been handled.
func (r *TransactionRepo) ListSweepableExpired(ctx context.Context, now time.Time, pendingGrace time.Duration, limit int) ([]domain.Transaction, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT `+txnColumns+` FROM transactions
		WHERE expires_at IS NOT NULL AND (
			(status = 'initiated' AND expires_at <= $1)
			OR (status = 'pending' AND expires_at <= $2)
		)
		ORDER BY expires_at ASC
		LIMIT $3`,
		now, now.Add(-pendingGrace), limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return collectTxns(rows)
}

func collectTxns(rows interface {
	Next() bool
	Scan(...any) error
}) ([]domain.Transaction, error) {
	var txns []domain.Transaction
	for rows.Next() {
		t, err := scanTxn(rows)
		if err != nil {
			return nil, err
		}
		txns = append(txns, *t)
	}
	return txns, nil
}
