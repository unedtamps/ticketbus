package fixtures

import (
	"time"

	"github.com/google/uuid"
	"github.com/nedo/TicketSaas/internal/auth/domain"
	sdomain "github.com/nedo/TicketSaas/internal/shared/domain"
)

// UserOption is a functional option for NewTestUser.
type UserOption func(*domain.User)

// WithUserID overrides the default user ID.
func WithUserID(id string) UserOption {
	return func(u *domain.User) { u.ID = id }
}

// WithUserEmail overrides the default email.
func WithUserEmail(email string) UserOption {
	return func(u *domain.User) { u.Email = email }
}

// WithUserName overrides the default name.
func WithUserName(name string) UserOption {
	return func(u *domain.User) { u.Name = name }
}

// WithUserRole overrides the default role (customer).
func WithUserRole(role sdomain.Role) UserOption {
	return func(u *domain.User) { u.Role = role }
}

// WithUserPasswordHash sets a known password hash.
func WithUserPasswordHash(hash string) UserOption {
	return func(u *domain.User) { u.PasswordHash = hash }
}

// NewTestUser creates a User with sensible defaults for tests.
// Override fields using With* option functions.
func NewTestUser(opts ...UserOption) *domain.User {
	now := time.Now().Truncate(time.Second)
	u := &domain.User{
		ID:           uuid.NewString(),
		Email:        "customer@example.com",
		PasswordHash: "$2a$10$test-hash",
		Name:         "Test Customer",
		Role:         sdomain.RoleCustomer,
		CreatedAt:    now,
		UpdatedAt:    now,
	}
	for _, o := range opts {
		o(u)
	}
	return u
}

// TokenPairOption is a functional option for NewTestTokenPair.
type TokenPairOption func(*domain.TokenPair)

// WithAccessToken overrides the default access token.
func WithAccessToken(token string) TokenPairOption {
	return func(p *domain.TokenPair) { p.AccessToken = token }
}

// WithRefreshTokenString overrides the default refresh token.
func WithRefreshTokenString(token string) TokenPairOption {
	return func(p *domain.TokenPair) { p.RefreshToken = token }
}

// NewTestTokenPair creates a TokenPair with sensible defaults.
func NewTestTokenPair(opts ...TokenPairOption) *domain.TokenPair {
	p := &domain.TokenPair{
		AccessToken:  "eyJ.access.token",
		RefreshToken: "base64RefreshTokenString",
		ExpiresIn:    900,
	}
	for _, o := range opts {
		o(p)
	}
	return p
}

// RefreshTokenOption is a functional option for NewTestRefreshToken.
type RefreshTokenOption func(*domain.RefreshToken)

// WithRefreshTokenUserID overrides the default user ID.
func WithRefreshTokenUserID(userID string) RefreshTokenOption {
	return func(r *domain.RefreshToken) { r.UserID = userID }
}

// WithRefreshTokenHash overrides the default token hash.
func WithRefreshTokenHash(hash string) RefreshTokenOption {
	return func(r *domain.RefreshToken) { r.TokenHash = hash }
}

// WithRefreshTokenExpiry sets the expiry.
func WithRefreshTokenExpiry(t time.Time) RefreshTokenOption {
	return func(r *domain.RefreshToken) { r.ExpiresAt = t }
}

// NewTestRefreshToken creates a RefreshToken with sensible defaults.
func NewTestRefreshToken(opts ...RefreshTokenOption) *domain.RefreshToken {
	now := time.Now().Truncate(time.Second)
	rt := &domain.RefreshToken{
		ID:        uuid.NewString(),
		UserID:    uuid.NewString(),
		TokenHash: "sha256-hash-" + uuid.NewString(),
		ExpiresAt: now.Add(7 * 24 * time.Hour),
		CreatedAt: now,
	}
	for _, o := range opts {
		o(rt)
	}
	return rt
}
