package application_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/nedo/TicketSaas/internal/auth/application"
	"github.com/nedo/TicketSaas/internal/auth/domain"
	"github.com/nedo/TicketSaas/internal/auth/domain/mocks"
	sdomain "github.com/nedo/TicketSaas/internal/shared/domain"
	"github.com/nedo/TicketSaas/internal/shared/outbox"
	"github.com/nedo/TicketSaas/tests/fixtures"
)

var tokensConfig = application.TokensConfig{
	AccessTokenTTL:  15 * time.Minute,
	RefreshTokenTTL: 7 * 24 * time.Hour,
}

func newAuthService(
	t testing.TB,
	userRepo domain.UserRepository,
	tokenRepo domain.RefreshTokenRepository,
	hasher domain.PasswordHasher,
	tokenSvc domain.TokenService,
) *application.AuthService {
	t.Helper()
	return application.NewAuthService(userRepo, tokenRepo, hasher, tokenSvc, tokensConfig, outbox.NoopStore{})
}

func TestRegister_Success(t *testing.T) {
	userRepo := mocks.NewMockUserRepository(t)
	tokenRepo := mocks.NewMockRefreshTokenRepository(t)
	hasher := mocks.NewMockPasswordHasher(t)
	tokenSvc := mocks.NewMockTokenService(t)

	ctx := context.Background()
	email, password, name := "new@example.com", "secret123", "New User"

	userRepo.EXPECT().FindByEmail(ctx, email).Return(nil, pgx.ErrNoRows)
	hasher.EXPECT().Hash(password).Return("$2a$hashed", nil)
	userRepo.EXPECT().Create(ctx, mock.AnythingOfType("*domain.User")).Return(nil)

	svc := newAuthService(t, userRepo, tokenRepo, hasher, tokenSvc)

	user, err := svc.Register(ctx, email, password, name, sdomain.RoleCustomer)
	require.NoError(t, err)
	assert.Equal(t, email, user.Email)
	assert.Equal(t, "New User", user.Name)
	assert.NotEmpty(t, user.ID)
}

func TestRegister_DuplicateEmail(t *testing.T) {
	userRepo := mocks.NewMockUserRepository(t)
	tokenRepo := mocks.NewMockRefreshTokenRepository(t)
	hasher := mocks.NewMockPasswordHasher(t)
	tokenSvc := mocks.NewMockTokenService(t)

	ctx := context.Background()
	existing := fixtures.NewTestUser(fixtures.WithUserEmail("dup@example.com"))

	userRepo.EXPECT().FindByEmail(ctx, "dup@example.com").Return(existing, nil)

	svc := newAuthService(t, userRepo, tokenRepo, hasher, tokenSvc)

	_, err := svc.Register(ctx, "dup@example.com", "pw", "Dup", sdomain.RoleCustomer)
	assert.ErrorIs(t, err, domain.ErrUserExists)
}

func TestRegister_HasherError(t *testing.T) {
	userRepo := mocks.NewMockUserRepository(t)
	tokenRepo := mocks.NewMockRefreshTokenRepository(t)
	hasher := mocks.NewMockPasswordHasher(t)
	tokenSvc := mocks.NewMockTokenService(t)

	ctx := context.Background()
	hashErr := errors.New("bcrypt failure")

	userRepo.EXPECT().FindByEmail(ctx, "e@x.com").Return(nil, pgx.ErrNoRows)
	hasher.EXPECT().Hash("pw").Return("", hashErr)

	svc := newAuthService(t, userRepo, tokenRepo, hasher, tokenSvc)

	_, err := svc.Register(ctx, "e@x.com", "pw", "Name", sdomain.RoleCustomer)
	assert.ErrorIs(t, err, hashErr)
}

func TestRegister_CreateError(t *testing.T) {
	userRepo := mocks.NewMockUserRepository(t)
	tokenRepo := mocks.NewMockRefreshTokenRepository(t)
	hasher := mocks.NewMockPasswordHasher(t)
	tokenSvc := mocks.NewMockTokenService(t)

	ctx := context.Background()
	createErr := errors.New("connection refused")

	userRepo.EXPECT().FindByEmail(ctx, "e@x.com").Return(nil, pgx.ErrNoRows)
	hasher.EXPECT().Hash("pw").Return("$2a$hash", nil)
	userRepo.EXPECT().Create(ctx, mock.AnythingOfType("*domain.User")).Return(createErr)

	svc := newAuthService(t, userRepo, tokenRepo, hasher, tokenSvc)

	_, err := svc.Register(ctx, "e@x.com", "pw", "N", sdomain.RoleCustomer)
	assert.ErrorIs(t, err, createErr)
}

func TestRegisterOrganizer_Success(t *testing.T) {
	userRepo := mocks.NewMockUserRepository(t)
	tokenRepo := mocks.NewMockRefreshTokenRepository(t)
	hasher := mocks.NewMockPasswordHasher(t)
	tokenSvc := mocks.NewMockTokenService(t)

	ctx := context.Background()
	email, password, name := "eo@example.com", "pw", "EO User"

	userRepo.EXPECT().FindByEmail(ctx, email).Return(nil, pgx.ErrNoRows)
	hasher.EXPECT().Hash(password).Return("$2a$hashed", nil)
	userRepo.EXPECT().Create(ctx, mock.AnythingOfType("*domain.User")).Return(nil)

	svc := newAuthService(t, userRepo, tokenRepo, hasher, tokenSvc)

	user, err := svc.RegisterOrganizer(ctx, email, password, name, "Acme Org", "desc", "https://a.com", "e@a.com")
	require.NoError(t, err)
	assert.Equal(t, sdomain.RoleEO, user.Role)
}

func TestRegisterOrganizer_DuplicateEmail(t *testing.T) {
	userRepo := mocks.NewMockUserRepository(t)
	tokenRepo := mocks.NewMockRefreshTokenRepository(t)
	hasher := mocks.NewMockPasswordHasher(t)
	tokenSvc := mocks.NewMockTokenService(t)

	ctx := context.Background()
	existing := fixtures.NewTestUser(fixtures.WithUserEmail("eo@example.com"))

	userRepo.EXPECT().FindByEmail(ctx, "eo@example.com").Return(existing, nil)

	svc := newAuthService(t, userRepo, tokenRepo, hasher, tokenSvc)

	_, err := svc.RegisterOrganizer(ctx, "eo@example.com", "pw", "EO", "Org", "desc", "url", "e@o.com")
	assert.ErrorIs(t, err, domain.ErrUserExists)
}

func TestLogin_Success(t *testing.T) {
	userRepo := mocks.NewMockUserRepository(t)
	tokenRepo := mocks.NewMockRefreshTokenRepository(t)
	hasher := mocks.NewMockPasswordHasher(t)
	tokenSvc := mocks.NewMockTokenService(t)

	ctx := context.Background()
	user := fixtures.NewTestUser(fixtures.WithUserEmail("user@x.com"))

	userRepo.EXPECT().FindByEmail(ctx, "user@x.com").Return(user, nil)
	hasher.EXPECT().Verify("correct", user.PasswordHash).Return(nil)
	tokenSvc.EXPECT().GenerateAccessToken(user.ID, user.Email, user.Role).
		Return("access-token", 900*time.Second, nil)
	tokenSvc.EXPECT().GenerateRefreshToken().Return("refresh-raw", nil)
	tokenSvc.EXPECT().HashRefreshToken("refresh-raw").Return("hashed-refresh")
	tokenRepo.EXPECT().Create(ctx, mock.AnythingOfType("*domain.RefreshToken")).Return(nil)

	svc := newAuthService(t, userRepo, tokenRepo, hasher, tokenSvc)

	pair, err := svc.Login(ctx, "user@x.com", "correct")
	require.NoError(t, err)
	assert.Equal(t, "access-token", pair.AccessToken)
	assert.Equal(t, "refresh-raw", pair.RefreshToken)
	assert.Equal(t, int64(900), pair.ExpiresIn)
}

func TestLogin_WrongEmail(t *testing.T) {
	userRepo := mocks.NewMockUserRepository(t)
	tokenRepo := mocks.NewMockRefreshTokenRepository(t)
	hasher := mocks.NewMockPasswordHasher(t)
	tokenSvc := mocks.NewMockTokenService(t)

	ctx := context.Background()
	userRepo.EXPECT().FindByEmail(ctx, "no@body.com").Return(nil, pgx.ErrNoRows)

	svc := newAuthService(t, userRepo, tokenRepo, hasher, tokenSvc)

	_, err := svc.Login(ctx, "no@body.com", "pw")
	assert.ErrorIs(t, err, domain.ErrInvalidCredentials)
}

func TestLogin_WrongPassword(t *testing.T) {
	userRepo := mocks.NewMockUserRepository(t)
	tokenRepo := mocks.NewMockRefreshTokenRepository(t)
	hasher := mocks.NewMockPasswordHasher(t)
	tokenSvc := mocks.NewMockTokenService(t)

	ctx := context.Background()
	user := fixtures.NewTestUser(fixtures.WithUserEmail("user@x.com"))

	userRepo.EXPECT().FindByEmail(ctx, "user@x.com").Return(user, nil)
	hasher.EXPECT().Verify("wrong", user.PasswordHash).Return(errors.New("mismatch"))

	svc := newAuthService(t, userRepo, tokenRepo, hasher, tokenSvc)

	_, err := svc.Login(ctx, "user@x.com", "wrong")
	assert.ErrorIs(t, err, domain.ErrInvalidCredentials)
}

func TestLogin_AccessTokenGenFails(t *testing.T) {
	userRepo := mocks.NewMockUserRepository(t)
	tokenRepo := mocks.NewMockRefreshTokenRepository(t)
	hasher := mocks.NewMockPasswordHasher(t)
	tokenSvc := mocks.NewMockTokenService(t)

	ctx := context.Background()
	user := fixtures.NewTestUser(fixtures.WithUserEmail("user@x.com"))

	userRepo.EXPECT().FindByEmail(ctx, "user@x.com").Return(user, nil)
	hasher.EXPECT().Verify("pw", user.PasswordHash).Return(nil)
	tokenSvc.EXPECT().GenerateAccessToken(user.ID, user.Email, user.Role).
		Return("", 0*time.Second, errors.New("signing failed"))

	svc := newAuthService(t, userRepo, tokenRepo, hasher, tokenSvc)

	_, err := svc.Login(ctx, "user@x.com", "pw")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "signing failed")
}

func TestLogin_TokenSaveFails(t *testing.T) {
	userRepo := mocks.NewMockUserRepository(t)
	tokenRepo := mocks.NewMockRefreshTokenRepository(t)
	hasher := mocks.NewMockPasswordHasher(t)
	tokenSvc := mocks.NewMockTokenService(t)

	ctx := context.Background()
	user := fixtures.NewTestUser(fixtures.WithUserEmail("user@x.com"))

	userRepo.EXPECT().FindByEmail(ctx, "user@x.com").Return(user, nil)
	hasher.EXPECT().Verify("pw", user.PasswordHash).Return(nil)
	tokenSvc.EXPECT().GenerateAccessToken(user.ID, user.Email, user.Role).
		Return("access", 900*time.Second, nil)
	tokenSvc.EXPECT().GenerateRefreshToken().Return("refresh-raw", nil)
	tokenSvc.EXPECT().HashRefreshToken("refresh-raw").Return("hashed")
	tokenRepo.EXPECT().Create(ctx, mock.AnythingOfType("*domain.RefreshToken")).
		Return(errors.New("db down"))

	svc := newAuthService(t, userRepo, tokenRepo, hasher, tokenSvc)

	_, err := svc.Login(ctx, "user@x.com", "pw")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "db down")
}

func TestRefreshToken_Success(t *testing.T) {
	userRepo := mocks.NewMockUserRepository(t)
	tokenRepo := mocks.NewMockRefreshTokenRepository(t)
	hasher := mocks.NewMockPasswordHasher(t)
	tokenSvc := mocks.NewMockTokenService(t)

	ctx := context.Background()
	oldToken := "old-refresh-raw"
	oldHash := "old-hash"

	stored := fixtures.NewTestRefreshToken(
		fixtures.WithRefreshTokenHash(oldHash),
		fixtures.WithRefreshTokenExpiry(time.Now().Add(time.Hour)),
	)
	user := fixtures.NewTestUser(fixtures.WithUserID(stored.UserID))

	tokenSvc.EXPECT().HashRefreshToken(oldToken).Return(oldHash)
	tokenRepo.EXPECT().FindByHash(ctx, oldHash).Return(stored, nil)
	userRepo.EXPECT().FindByID(ctx, stored.UserID).Return(user, nil)
	tokenRepo.EXPECT().DeleteByHash(ctx, oldHash).Return(nil)
	tokenSvc.EXPECT().GenerateAccessToken(user.ID, user.Email, user.Role).
		Return("new-access", 900*time.Second, nil)
	tokenSvc.EXPECT().GenerateRefreshToken().Return("new-refresh-raw", nil)
	tokenSvc.EXPECT().HashRefreshToken("new-refresh-raw").Return("new-hash")
	tokenRepo.EXPECT().Create(ctx, mock.AnythingOfType("*domain.RefreshToken")).Return(nil)

	svc := newAuthService(t, userRepo, tokenRepo, hasher, tokenSvc)

	pair, err := svc.RefreshToken(ctx, oldToken)
	require.NoError(t, err)
	assert.Equal(t, "new-access", pair.AccessToken)
	assert.Equal(t, "new-refresh-raw", pair.RefreshToken)
}

func TestRefreshToken_TokenNotFound(t *testing.T) {
	userRepo := mocks.NewMockUserRepository(t)
	tokenRepo := mocks.NewMockRefreshTokenRepository(t)
	hasher := mocks.NewMockPasswordHasher(t)
	tokenSvc := mocks.NewMockTokenService(t)

	ctx := context.Background()

	tokenSvc.EXPECT().HashRefreshToken("stale").Return("stale-hash")
	tokenRepo.EXPECT().FindByHash(ctx, "stale-hash").Return(nil, pgx.ErrNoRows)

	svc := newAuthService(t, userRepo, tokenRepo, hasher, tokenSvc)

	_, err := svc.RefreshToken(ctx, "stale")
	assert.ErrorIs(t, err, domain.ErrTokenNotFound)
}

func TestRefreshToken_Expired(t *testing.T) {
	userRepo := mocks.NewMockUserRepository(t)
	tokenRepo := mocks.NewMockRefreshTokenRepository(t)
	hasher := mocks.NewMockPasswordHasher(t)
	tokenSvc := mocks.NewMockTokenService(t)

	ctx := context.Background()
	hash := "expired-hash"

	stored := fixtures.NewTestRefreshToken(
		fixtures.WithRefreshTokenHash(hash),
		fixtures.WithRefreshTokenExpiry(time.Now().Add(-time.Hour)),
	)

	tokenSvc.EXPECT().HashRefreshToken("old-raw").Return(hash)
	tokenRepo.EXPECT().FindByHash(ctx, hash).Return(stored, nil)

	svc := newAuthService(t, userRepo, tokenRepo, hasher, tokenSvc)

	_, err := svc.RefreshToken(ctx, "old-raw")
	assert.ErrorIs(t, err, domain.ErrTokenExpired)
}

func TestRefreshToken_DeleteFails_AbortsRotation(t *testing.T) {
	userRepo := mocks.NewMockUserRepository(t)
	tokenRepo := mocks.NewMockRefreshTokenRepository(t)
	hasher := mocks.NewMockPasswordHasher(t)
	tokenSvc := mocks.NewMockTokenService(t)

	ctx := context.Background()
	hash := "valid-hash"
	deleteErr := errors.New("connection lost")

	stored := fixtures.NewTestRefreshToken(
		fixtures.WithRefreshTokenHash(hash),
		fixtures.WithRefreshTokenExpiry(time.Now().Add(time.Hour)),
	)
	user := fixtures.NewTestUser(fixtures.WithUserID(stored.UserID))

	tokenSvc.EXPECT().HashRefreshToken("raw").Return(hash)
	tokenRepo.EXPECT().FindByHash(ctx, hash).Return(stored, nil)
	userRepo.EXPECT().FindByID(ctx, stored.UserID).Return(user, nil)
	tokenRepo.EXPECT().DeleteByHash(ctx, hash).Return(deleteErr)

	svc := newAuthService(t, userRepo, tokenRepo, hasher, tokenSvc)

	_, err := svc.RefreshToken(ctx, "raw")
	assert.ErrorIs(t, err, deleteErr)
}

func TestSeedAdmins_CreatesAdmin(t *testing.T) {
	userRepo := mocks.NewMockUserRepository(t)
	tokenRepo := mocks.NewMockRefreshTokenRepository(t)
	hasher := mocks.NewMockPasswordHasher(t)
	tokenSvc := mocks.NewMockTokenService(t)

	ctx := context.Background()

	userRepo.EXPECT().FindByEmail(ctx, "admin@ticketsaas.com").Return(nil, pgx.ErrNoRows)
	hasher.EXPECT().Hash("admin123").Return("$2a$admin", nil)
	userRepo.EXPECT().Create(ctx, mock.MatchedBy(func(u *domain.User) bool {
		return u.Email == "admin@ticketsaas.com" && u.Role == sdomain.RoleAdmin
	})).Return(nil)

	svc := newAuthService(t, userRepo, tokenRepo, hasher, tokenSvc)

	n, err := svc.SeedAdmins(ctx, []application.AdminSeed{
		{Email: "admin@ticketsaas.com", Password: "admin123", Name: "Admin"},
	})
	require.NoError(t, err)
	assert.Equal(t, 1, n)
}

func TestSeedAdmins_AlreadyExists(t *testing.T) {
	userRepo := mocks.NewMockUserRepository(t)
	tokenRepo := mocks.NewMockRefreshTokenRepository(t)
	hasher := mocks.NewMockPasswordHasher(t)
	tokenSvc := mocks.NewMockTokenService(t)

	ctx := context.Background()
	existing := fixtures.NewTestUser(
		fixtures.WithUserEmail("admin@ticketsaas.com"),
		fixtures.WithUserRole(sdomain.RoleAdmin),
	)

	userRepo.EXPECT().FindByEmail(ctx, "admin@ticketsaas.com").Return(existing, nil)

	svc := newAuthService(t, userRepo, tokenRepo, hasher, tokenSvc)

	n, err := svc.SeedAdmins(ctx, []application.AdminSeed{
		{Email: "admin@ticketsaas.com", Password: "admin123", Name: "Admin"},
	})
	require.NoError(t, err)
	assert.Equal(t, 0, n)
}

func TestGetMe_Found(t *testing.T) {
	userRepo := mocks.NewMockUserRepository(t)
	tokenRepo := mocks.NewMockRefreshTokenRepository(t)
	hasher := mocks.NewMockPasswordHasher(t)
	tokenSvc := mocks.NewMockTokenService(t)

	ctx := context.Background()
	expected := fixtures.NewTestUser(fixtures.WithUserID("user-1"))

	userRepo.EXPECT().FindByID(ctx, "user-1").Return(expected, nil)

	svc := newAuthService(t, userRepo, tokenRepo, hasher, tokenSvc)

	user, err := svc.GetMe(ctx, "user-1")
	require.NoError(t, err)
	assert.Equal(t, expected.ID, user.ID)
}

func TestGetMe_NotFound(t *testing.T) {
	userRepo := mocks.NewMockUserRepository(t)
	tokenRepo := mocks.NewMockRefreshTokenRepository(t)
	hasher := mocks.NewMockPasswordHasher(t)
	tokenSvc := mocks.NewMockTokenService(t)

	ctx := context.Background()

	userRepo.EXPECT().FindByID(ctx, "nobody").Return(nil, pgx.ErrNoRows)

	svc := newAuthService(t, userRepo, tokenRepo, hasher, tokenSvc)

	_, err := svc.GetMe(ctx, "nobody")
	require.Error(t, err)
}

func TestDeleteUser_Success(t *testing.T) {
	userRepo := mocks.NewMockUserRepository(t)
	tokenRepo := mocks.NewMockRefreshTokenRepository(t)
	hasher := mocks.NewMockPasswordHasher(t)
	tokenSvc := mocks.NewMockTokenService(t)

	ctx := context.Background()

	userRepo.EXPECT().DeleteByID(ctx, "user-1").Return(nil)

	svc := newAuthService(t, userRepo, tokenRepo, hasher, tokenSvc)

	err := svc.DeleteUser(ctx, "user-1")
	require.NoError(t, err)
}

func TestVerifyToken_Success(t *testing.T) {
	userRepo := mocks.NewMockUserRepository(t)
	tokenRepo := mocks.NewMockRefreshTokenRepository(t)
	hasher := mocks.NewMockPasswordHasher(t)
	tokenSvc := mocks.NewMockTokenService(t)

	expected := &domain.Claims{
		UserID: "usr-123",
		Email:  "usr@x.com",
		Role:   sdomain.RoleCustomer,
	}

	tokenSvc.EXPECT().VerifyAccessToken("valid-jwt").Return(expected, nil)

	svc := newAuthService(t, userRepo, tokenRepo, hasher, tokenSvc)

	claims, err := svc.VerifyToken("valid-jwt")
	require.NoError(t, err)
	assert.Equal(t, expected, claims)
}

func TestVerifyToken_Invalid(t *testing.T) {
	userRepo := mocks.NewMockUserRepository(t)
	tokenRepo := mocks.NewMockRefreshTokenRepository(t)
	hasher := mocks.NewMockPasswordHasher(t)
	tokenSvc := mocks.NewMockTokenService(t)

	tokenSvc.EXPECT().VerifyAccessToken("bad").Return(nil, errors.New("expired"))

	svc := newAuthService(t, userRepo, tokenRepo, hasher, tokenSvc)

	_, err := svc.VerifyToken("bad")
	require.Error(t, err)
}
