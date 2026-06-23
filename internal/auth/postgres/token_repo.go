package postgres

import (
	"context"
	"fmt"

	shareddb "github.com/nedo/TicketSaas/internal/shared/db"
	"github.com/nedo/TicketSaas/internal/auth/domain"
)

// RefreshTokenRepo implements domain.RefreshTokenRepository using PostgreSQL.
type RefreshTokenRepo struct {
	db shareddb.DBTx
}

// NewRefreshTokenRepo creates a new RefreshTokenRepo.
func NewRefreshTokenRepo(db shareddb.DBTx) *RefreshTokenRepo {
	return &RefreshTokenRepo{db: db}
}

// Create inserts a new refresh token.
func (r *RefreshTokenRepo) Create(ctx context.Context, token *domain.RefreshToken) error {
	query := `
		INSERT INTO refresh_tokens (id, user_id, token_hash, expires_at)
		VALUES ($1, $2, $3, $4)
	`
	_, err := r.db.Exec(ctx, query, token.ID, token.UserID, token.TokenHash, token.ExpiresAt)
	return err
}

// FindByHash retrieves a refresh token by its SHA-256 hash.
func (r *RefreshTokenRepo) FindByHash(ctx context.Context, hash string) (*domain.RefreshToken, error) {
	query := `SELECT id, user_id, token_hash, expires_at, created_at FROM refresh_tokens WHERE token_hash = $1`

	var token domain.RefreshToken
	err := r.db.QueryRow(ctx, query, hash).Scan(
		&token.ID, &token.UserID, &token.TokenHash, &token.ExpiresAt, &token.CreatedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("refresh token not found: %w", err)
	}
	return &token, nil
}

// DeleteByHash removes a specific refresh token by its hash.
func (r *RefreshTokenRepo) DeleteByHash(ctx context.Context, hash string) error {
	_, err := r.db.Exec(ctx, `DELETE FROM refresh_tokens WHERE token_hash = $1`, hash)
	return err
}

// DeleteByUserID removes all refresh tokens for a user.
func (r *RefreshTokenRepo) DeleteByUserID(ctx context.Context, userID string) error {
	_, err := r.db.Exec(ctx, `DELETE FROM refresh_tokens WHERE user_id = $1`, userID)
	return err
}
