package postgres

import (
	"context"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/nedo/TicketSaas/internal/payment/domain"
)

// TransactionRepo implements domain.TransactionRepository.
type TransactionRepo struct {
	pool *pgxpool.Pool
}

// NewTransactionRepo creates a new TransactionRepo.
func NewTransactionRepo(pool *pgxpool.Pool) *TransactionRepo {
	return &TransactionRepo{pool: pool}
}

// Create inserts a new transaction.
func (r *TransactionRepo) Create(ctx context.Context, txn *domain.Transaction) error {
	_, err := r.pool.Exec(ctx, `
		INSERT INTO transactions (id, user_id, booking_id, amount_cents, currency, status, provider, provider_ref)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8)`,
		txn.ID, txn.UserID, txn.BookingID, txn.AmountCents, txn.Currency, txn.Status, txn.Provider, txn.ProviderRef)
	return err
}

// FindByID retrieves a transaction by ID.
func (r *TransactionRepo) FindByID(ctx context.Context, id string) (*domain.Transaction, error) {
	var t domain.Transaction
	err := r.pool.QueryRow(ctx, `
		SELECT id, user_id, booking_id, amount_cents, currency, status, provider, provider_ref, created_at, updated_at
		FROM transactions WHERE id=$1`, id).
		Scan(&t.ID, &t.UserID, &t.BookingID, &t.AmountCents, &t.Currency, &t.Status, &t.Provider, &t.ProviderRef, &t.CreatedAt, &t.UpdatedAt)
	if err != nil {
		return nil, err
	}
	return &t, nil
}

// FindByBookingID retrieves a transaction by booking ID.
func (r *TransactionRepo) FindByBookingID(ctx context.Context, bookingID string) (*domain.Transaction, error) {
	var t domain.Transaction
	err := r.pool.QueryRow(ctx, `
		SELECT id, user_id, booking_id, amount_cents, currency, status, provider, provider_ref, created_at, updated_at
		FROM transactions WHERE booking_id=$1`, bookingID).
		Scan(&t.ID, &t.UserID, &t.BookingID, &t.AmountCents, &t.Currency, &t.Status, &t.Provider, &t.ProviderRef, &t.CreatedAt, &t.UpdatedAt)
	if err != nil {
		return nil, err
	}
	return &t, nil
}

// UpdateStatus updates transaction status and provider reference.
func (r *TransactionRepo) UpdateStatus(ctx context.Context, id, status, providerRef string) error {
	_, err := r.pool.Exec(ctx, `UPDATE transactions SET status=$1, provider_ref=$2, updated_at=NOW() WHERE id=$3`, status, providerRef, id)
	return err
}

// ListByUser returns transactions for a user.
func (r *TransactionRepo) ListByUser(ctx context.Context, userID string) ([]domain.Transaction, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT id, user_id, booking_id, amount_cents, currency, status, provider, provider_ref, created_at, updated_at
		FROM transactions WHERE user_id=$1 ORDER BY created_at DESC`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var txns []domain.Transaction
	for rows.Next() {
		var t domain.Transaction
		if err := rows.Scan(&t.ID, &t.UserID, &t.BookingID, &t.AmountCents, &t.Currency, &t.Status, &t.Provider, &t.ProviderRef, &t.CreatedAt, &t.UpdatedAt); err != nil {
			return nil, err
		}
		txns = append(txns, t)
	}
	return txns, nil
}
