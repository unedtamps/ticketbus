package postgres

import (
	"context"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/unedtamps/ticketbus/service/payment/internal/domain"
)

// RefundDestinationRepo implements domain.RefundDestinationRepository.
type RefundDestinationRepo struct {
	pool *pgxpool.Pool
}

// NewRefundDestinationRepo creates a new RefundDestinationRepo.
func NewRefundDestinationRepo(pool *pgxpool.Pool) *RefundDestinationRepo {
	return &RefundDestinationRepo{pool: pool}
}

// Upsert saves the customer's refund destination, replacing any previous one.
func (r *RefundDestinationRepo) Upsert(ctx context.Context, dest *domain.RefundDestination) error {
	_, err := r.pool.Exec(ctx, `
		INSERT INTO customer_refund_destinations (user_id, channel_code, account_number, account_holder_name)
		VALUES ($1,$2,$3,$4)
		ON CONFLICT (user_id) DO UPDATE
		SET channel_code = $2, account_number = $3, account_holder_name = $4, updated_at = NOW()`,
		dest.UserID, dest.ChannelCode, dest.AccountNumber, dest.AccountHolderName)
	return err
}

// FindByUserID returns the customer's registered refund destination, or
// pgx.ErrNoRows when none was set.
func (r *RefundDestinationRepo) FindByUserID(ctx context.Context, userID string) (*domain.RefundDestination, error) {
	var d domain.RefundDestination
	err := r.pool.QueryRow(ctx, `
		SELECT user_id, channel_code, account_number, account_holder_name
		FROM customer_refund_destinations WHERE user_id = $1`, userID).
		Scan(&d.UserID, &d.ChannelCode, &d.AccountNumber, &d.AccountHolderName)
	if err != nil {
		return nil, err
	}
	return &d, nil
}
