package postgres

import (
	"context"
	"strings"
	"time"

	shareddb "github.com/nedo/TicketSaas/pkg/db"
	"github.com/nedo/TicketSaas/service/payment/internal/domain"
)

// TransactionRepo implements domain.TransactionRepository.
type TransactionRepo struct {
	db shareddb.DBTx
}

// NewTransactionRepo creates a new TransactionRepo.
func NewTransactionRepo(db shareddb.DBTx) *TransactionRepo {
	return &TransactionRepo{db: db}
}

const txnColumns = `id, user_id, booking_id, event_id, amount_rupiah, currency, status, provider, provider_ref, payment_link_url, customer_email, refund_status, expires_at, created_at, updated_at`

func scanTxn(row interface{ Scan(...any) error }) (*domain.Transaction, error) {
	var t domain.Transaction
	err := row.Scan(
		&t.ID, &t.UserID, &t.BookingID, &t.EventID, &t.AmountRupiah, &t.Currency,
		&t.Status, &t.Provider, &t.ProviderRef, &t.PaymentLinkURL, &t.CustomerEmail,
		&t.RefundStatus, &t.ExpiresAt, &t.CreatedAt, &t.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}
	return &t, nil
}

// Create inserts a new transaction.
func (r *TransactionRepo) Create(ctx context.Context, txn *domain.Transaction) error {
	_, err := r.db.Exec(ctx, `
		INSERT INTO transactions (id, user_id, booking_id, event_id, amount_rupiah, currency, status, provider, provider_ref, payment_link_url, customer_email, refund_status, expires_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13)`,
		txn.ID, txn.UserID, txn.BookingID, txn.EventID, txn.AmountRupiah, txn.Currency,
		txn.Status, txn.Provider, txn.ProviderRef, txn.PaymentLinkURL, txn.CustomerEmail,
		txn.RefundStatus, txn.ExpiresAt)
	return err
}

// FindByID retrieves a transaction by ID.
func (r *TransactionRepo) FindByID(ctx context.Context, id string) (*domain.Transaction, error) {
	row := r.db.QueryRow(ctx, `SELECT `+txnColumns+` FROM transactions WHERE id=$1`, id)
	return scanTxn(row)
}

// FindByBookingID retrieves a transaction by booking ID.
func (r *TransactionRepo) FindByBookingID(ctx context.Context, bookingID string) (*domain.Transaction, error) {
	row := r.db.QueryRow(ctx, `SELECT `+txnColumns+` FROM transactions WHERE booking_id=$1 ORDER BY created_at DESC LIMIT 1`, bookingID)
	return scanTxn(row)
}

// UpdateStatus updates transaction status and provider reference.
func (r *TransactionRepo) UpdateStatus(ctx context.Context, id, status, providerRef string) error {
	_, err := r.db.Exec(ctx, `UPDATE transactions SET status=$1, provider_ref=$2, updated_at=NOW() WHERE id=$3`, status, providerRef, id)
	return err
}

// TransitionIfActive transitions the transaction only when it is still in an
// active (non-terminal) status, so concurrent paths (webhook vs poll) cannot
// race.
func (r *TransactionRepo) TransitionIfActive(ctx context.Context, id, status, providerRef string) (bool, error) {
	tag, err := r.db.Exec(ctx,
		`UPDATE transactions SET status=$1, provider_ref=$2, updated_at=NOW() WHERE id=$3 AND status IN (`+statusInClause(domain.ActiveStatuses())+`)`,
		status, providerRef, id)
	if err != nil {
		return false, err
	}
	return tag.RowsAffected() > 0, nil
}

// TransitionIfInitiated transitions the transaction only while it is still
// initiated. Used for the initiated → pending step so a concurrent
// ProcessPayment cannot clobber an existing session reference.
func (r *TransactionRepo) TransitionIfInitiated(ctx context.Context, id, status, providerRef string) (bool, error) {
	tag, err := r.db.Exec(ctx,
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
	_, err := r.db.Exec(ctx,
		`UPDATE transactions SET provider_ref=$1, payment_link_url=$2, updated_at=NOW() WHERE id=$3`,
		providerRef, paymentLinkURL, id)
	return err
}

// UpdateRefundStatus updates the refund status of a transaction.
func (r *TransactionRepo) UpdateRefundStatus(ctx context.Context, id, refundStatus string) error {
	_, err := r.db.Exec(ctx, `UPDATE transactions SET refund_status=$1, updated_at=NOW() WHERE id=$2`, refundStatus, id)
	return err
}

// ListByUser returns transactions for a user.
func (r *TransactionRepo) ListByUser(ctx context.Context, userID string) ([]domain.Transaction, error) {
	rows, err := r.db.Query(ctx, `SELECT `+txnColumns+` FROM transactions WHERE user_id=$1 ORDER BY created_at DESC`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return collectTxns(rows)
}

// ListByEventID returns transactions for an event.
func (r *TransactionRepo) ListByEventID(ctx context.Context, eventID string) ([]domain.Transaction, error) {
	rows, err := r.db.Query(ctx, `SELECT `+txnColumns+` FROM transactions WHERE event_id=$1 ORDER BY created_at DESC`, eventID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return collectTxns(rows)
}

// ListActiveExpired returns active transactions whose deadline has passed.
func (r *TransactionRepo) ListActiveExpired(ctx context.Context, now time.Time, limit int) ([]domain.Transaction, error) {
	rows, err := r.db.Query(ctx,
		`SELECT `+txnColumns+` FROM transactions WHERE status IN (`+statusInClause(domain.ActiveStatuses())+`) AND expires_at IS NOT NULL AND expires_at <= $1 ORDER BY expires_at ASC LIMIT $2`,
		now, limit)
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
