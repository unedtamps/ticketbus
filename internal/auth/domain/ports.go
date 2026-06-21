package domain

import (
	"context"
	"time"

	"github.com/nedo/TicketSaas/internal/shared/domain"
)

// UserRepository defines the contract for user persistence.
type UserRepository interface {
	Create(ctx context.Context, user *User) error
	FindByEmail(ctx context.Context, email string) (*User, error)
	FindByID(ctx context.Context, id string) (*User, error)
	DeleteByID(ctx context.Context, id string) error
}

// RefreshTokenRepository defines the contract for refresh token persistence.
type RefreshTokenRepository interface {
	Create(ctx context.Context, token *RefreshToken) error
	FindByHash(ctx context.Context, hash string) (*RefreshToken, error)
	DeleteByUserID(ctx context.Context, userID string) error
}

// PasswordHasher defines the contract for password hashing and verification.
type PasswordHasher interface {
	Hash(password string) (string, error)
	Verify(password, hash string) error
}

// TokenService defines the contract for JWT operations.
type TokenService interface {
	GenerateAccessToken(userID, email string, role domain.Role) (string, time.Duration, error)
	GenerateRefreshToken() (string, error)
	HashRefreshToken(token string) string
	VerifyAccessToken(tokenString string) (*Claims, error)
}

// Claims represents custom JWT claims.
type Claims struct {
	UserID string      `json:"sub"`
	Email  string      `json:"email"`
	Role   domain.Role `json:"role"`
}

// OrganizerPublisher publishes organizer-related events.
type OrganizerPublisher interface {
	PublishOrganizerCreated(ctx context.Context, userID, name, description, profileLink, contactEmail string) error
}
