package postgres

import (
	"context"

	"github.com/nedo/TicketSaas/internal/payment/domain"
	shareddb "github.com/nedo/TicketSaas/internal/shared/db"
)

// TransactionRepo implements domain.TransactionRepository.
type TransactionRepo struct {
	db shareddb.DBTx
}

// NewTransactionRepo creates a new TransactionRepo.
func NewTransactionRepo(db shareddb.DBTx) *TransactionRepo {
	return &TransactionRepo{db: db}
}

// Create inserts a new transaction.
func (r *TransactionRepo) Create(ctx context.Context, txn *domain.Transaction) error {
	_, err := r.db.Exec(ctx, `
		INSERT INTO transactions (id, user_id, booking_id, event_id, amount_cents, currency, status, provider, provider_ref, refund_status)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)`,
		txn.ID, txn.UserID, txn.BookingID, txn.EventID, txn.AmountCents, txn.Currency, txn.Status, txn.Provider, txn.ProviderRef, txn.RefundStatus)
	return err
}

// FindByID retrieves a transaction by ID.
func (r *TransactionRepo) FindByID(ctx context.Context, id string) (*domain.Transaction, error) {
	var t domain.Transaction
	err := r.db.QueryRow(ctx, `
		SELECT id, user_id, booking_id, event_id, amount_cents, currency, status, provider, provider_ref, refund_status, created_at, updated_at
		FROM transactions WHERE id=$1`, id).
		Scan(&t.ID, &t.UserID, &t.BookingID, &t.EventID, &t.AmountCents, &t.Currency, &t.Status, &t.Provider, &t.ProviderRef, &t.RefundStatus, &t.CreatedAt, &t.UpdatedAt)
	if err != nil {
		return nil, err
	}
	return &t, nil
}

// FindByBookingID retrieves a transaction by booking ID.
func (r *TransactionRepo) FindByBookingID(ctx context.Context, bookingID string) (*domain.Transaction, error) {
	var t domain.Transaction
	err := r.db.QueryRow(ctx, `
		SELECT id, user_id, booking_id, event_id, amount_cents, currency, status, provider, provider_ref, refund_status, created_at, updated_at
		FROM transactions WHERE booking_id=$1 ORDER BY created_at DESC LIMIT 1`, bookingID).
		Scan(&t.ID, &t.UserID, &t.BookingID, &t.EventID, &t.AmountCents, &t.Currency, &t.Status, &t.Provider, &t.ProviderRef, &t.RefundStatus, &t.CreatedAt, &t.UpdatedAt)
	if err != nil {
		return nil, err
	}
	return &t, nil
}

// UpdateStatus updates transaction status and provider reference.
func (r *TransactionRepo) UpdateStatus(ctx context.Context, id, status, providerRef string) error {
	_, err := r.db.Exec(ctx, `UPDATE transactions SET status=$1, provider_ref=$2, updated_at=NOW() WHERE id=$3`, status, providerRef, id)
	return err
}

// UpdateRefundStatus updates the refund status of a transaction.
func (r *TransactionRepo) UpdateRefundStatus(ctx context.Context, id, refundStatus string) error {
	_, err := r.db.Exec(ctx, `UPDATE transactions SET refund_status=$1, updated_at=NOW() WHERE id=$2`, refundStatus, id)
	return err
}

// ListByUser returns transactions for a user.
func (r *TransactionRepo) ListByUser(ctx context.Context, userID string) ([]domain.Transaction, error) {
	rows, err := r.db.Query(ctx, `
		SELECT id, user_id, booking_id, event_id, amount_cents, currency, status, provider, provider_ref, refund_status, created_at, updated_at
		FROM transactions WHERE user_id=$1 ORDER BY created_at DESC`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var txns []domain.Transaction
	for rows.Next() {
		var t domain.Transaction
		if err := rows.Scan(&t.ID, &t.UserID, &t.BookingID, &t.EventID, &t.AmountCents, &t.Currency, &t.Status, &t.Provider, &t.ProviderRef, &t.RefundStatus, &t.CreatedAt, &t.UpdatedAt); err != nil {
			return nil, err
		}
		txns = append(txns, t)
	}
	return txns, nil
}

// ListByEventID returns transactions for an event.
func (r *TransactionRepo) ListByEventID(ctx context.Context, eventID string) ([]domain.Transaction, error) {
	rows, err := r.db.Query(ctx, `
		SELECT id, user_id, booking_id, event_id, amount_cents, currency, status, provider, provider_ref, refund_status, created_at, updated_at
		FROM transactions WHERE event_id=$1 ORDER BY created_at DESC`, eventID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var txns []domain.Transaction
	for rows.Next() {
		var t domain.Transaction
		if err := rows.Scan(&t.ID, &t.UserID, &t.BookingID, &t.EventID, &t.AmountCents, &t.Currency, &t.Status, &t.Provider, &t.ProviderRef, &t.RefundStatus, &t.CreatedAt, &t.UpdatedAt); err != nil {
			return nil, err
		}
		txns = append(txns, t)
	}
	return txns, nil
}
