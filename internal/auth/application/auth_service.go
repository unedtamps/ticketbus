package application

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/nedo/TicketSaas/internal/auth/domain"
	sdomain "github.com/nedo/TicketSaas/internal/shared/domain"
	"github.com/nedo/TicketSaas/internal/shared/outbox"
)

// TokensConfig holds token TTL values.
type TokensConfig struct {
	AccessTokenTTL  time.Duration
	RefreshTokenTTL time.Duration
}

// AuthService orchestrates authentication and authorization operations.
type AuthService struct {
	userRepo    domain.UserRepository
	tokenRepo   domain.RefreshTokenRepository
	hasher      domain.PasswordHasher
	tokenSvc    domain.TokenService
	tokenConfig TokensConfig
	outbox      outbox.StoreInterface
}

// NewAuthService creates a new AuthService.
func NewAuthService(
	userRepo domain.UserRepository,
	tokenRepo domain.RefreshTokenRepository,
	hasher domain.PasswordHasher,
	tokenSvc domain.TokenService,
	tokenConfig TokensConfig,
	ob outbox.StoreInterface,
) *AuthService {
	return &AuthService{
		userRepo:           userRepo,
		tokenRepo:          tokenRepo,
		hasher:             hasher,
		tokenSvc:           tokenSvc,
		tokenConfig:        tokenConfig,
		outbox:             ob,
	}
}

// Register creates a new user account.
func (s *AuthService) Register(ctx context.Context, email, password, name string, role sdomain.Role) (*domain.User, error) {
	existing, _ := s.userRepo.FindByEmail(ctx, email)
	if existing != nil {
		return nil, domain.ErrUserExists
	}

	passwordHash, err := s.hasher.Hash(password)
	if err != nil {
		return nil, err
	}

	user := &domain.User{
		ID:           uuid.NewString(),
		Email:        email,
		PasswordHash: passwordHash,
		Name:         name,
		Role:         role,
	}

	if err := s.userRepo.Create(ctx, user); err != nil {
		return nil, err
	}

	return user, nil
}

// RegisterOrganizer creates a user with role EO and publishes the organizer creation event.
func (s *AuthService) RegisterOrganizer(ctx context.Context, email, password, name, organizerName, description, profileLink, contactEmail string) (*domain.User, error) {
	user, err := s.Register(ctx, email, password, name, sdomain.RoleEO)
	if err != nil {
		return nil, err
	}

	_ = s.outbox.Insert(ctx, "organizer.created", user.ID, sdomain.OrganizerCreated{
		UserID:       user.ID,
		Name:         organizerName,
		Description:  description,
		ProfileLink:  profileLink,
		ContactEmail: contactEmail,
		At:           time.Now(),
	})

	return user, nil
}

// Login authenticates a user and returns a token pair.
func (s *AuthService) Login(ctx context.Context, email, password string) (*domain.TokenPair, error) {
	user, err := s.userRepo.FindByEmail(ctx, email)
	if err != nil {
		return nil, domain.ErrInvalidCredentials
	}

	if err := s.hasher.Verify(password, user.PasswordHash); err != nil {
		return nil, domain.ErrInvalidCredentials
	}

	accessToken, ttl, err := s.tokenSvc.GenerateAccessToken(user.ID, user.Email, user.Role)
	if err != nil {
		return nil, err
	}

	refreshToken, err := s.tokenSvc.GenerateRefreshToken()
	if err != nil {
		return nil, err
	}

	rt := &domain.RefreshToken{
		ID:        uuid.NewString(),
		UserID:    user.ID,
		TokenHash: s.tokenSvc.HashRefreshToken(refreshToken),
		ExpiresAt: time.Now().Add(s.tokenConfig.RefreshTokenTTL),
	}

	if err := s.tokenRepo.Create(ctx, rt); err != nil {
		return nil, err
	}

	return &domain.TokenPair{
		AccessToken:  accessToken,
		RefreshToken: refreshToken,
		ExpiresIn:    int64(ttl.Seconds()),
	}, nil
}

// RefreshToken exchanges a valid refresh token for a new token pair.
func (s *AuthService) RefreshToken(ctx context.Context, rawRefreshToken string) (*domain.TokenPair, error) {
	hash := s.tokenSvc.HashRefreshToken(rawRefreshToken)

	stored, err := s.tokenRepo.FindByHash(ctx, hash)
	if err != nil {
		return nil, domain.ErrTokenNotFound
	}

	if time.Now().After(stored.ExpiresAt) {
		return nil, domain.ErrTokenExpired
	}

	user, err := s.userRepo.FindByID(ctx, stored.UserID)
	if err != nil {
		return nil, domain.ErrUserNotFound
	}

	// Delete the consumed refresh token (rotation: one-time use)
	if err := s.tokenRepo.DeleteByHash(ctx, hash); err != nil {
		return nil, err
	}

	accessToken, ttl, err := s.tokenSvc.GenerateAccessToken(user.ID, user.Email, user.Role)
	if err != nil {
		return nil, err
	}

	newRefreshToken, err := s.tokenSvc.GenerateRefreshToken()
	if err != nil {
		return nil, err
	}

	rt := &domain.RefreshToken{
		ID:        uuid.NewString(),
		UserID:    user.ID,
		TokenHash: s.tokenSvc.HashRefreshToken(newRefreshToken),
		ExpiresAt: time.Now().Add(s.tokenConfig.RefreshTokenTTL),
	}

	if err := s.tokenRepo.Create(ctx, rt); err != nil {
		return nil, err
	}

	return &domain.TokenPair{
		AccessToken:  accessToken,
		RefreshToken: newRefreshToken,
		ExpiresIn:    int64(ttl.Seconds()),
	}, nil
}

// GetMe returns the user associated with the given user ID.
func (s *AuthService) GetMe(ctx context.Context, userID string) (*domain.User, error) {
	return s.userRepo.FindByID(ctx, userID)
}

// VerifyToken verifies an access token and returns the claims.
func (s *AuthService) VerifyToken(tokenString string) (*domain.Claims, error) {
	return s.tokenSvc.VerifyAccessToken(tokenString)
}

// DeleteUser removes a user by ID. Used to roll back registration on failure.
func (s *AuthService) DeleteUser(ctx context.Context, userID string) error {
	return s.userRepo.DeleteByID(ctx, userID)
}

// SeedAdmin creates the initial admin user if none exists with the given email.
// Returns (true, nil) if created, (false, nil) if already present.
func (s *AuthService) SeedAdmin(ctx context.Context, email, password, name string) (bool, error) {
	existing, _ := s.userRepo.FindByEmail(ctx, email)
	if existing != nil {
		return false, nil
	}
	hash, err := s.hasher.Hash(password)
	if err != nil {
		return false, err
	}
	user := &domain.User{
		ID:           uuid.NewString(),
		Email:        email,
		PasswordHash: hash,
		Name:         name,
		Role:         sdomain.RoleAdmin,
	}
	if err := s.userRepo.Create(ctx, user); err != nil {
		return false, err
	}
	return true, nil
}
