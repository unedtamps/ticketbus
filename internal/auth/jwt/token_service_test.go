package jwt_test

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"testing"
	"time"

	jwtgo "github.com/golang-jwt/jwt/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/nedo/TicketSaas/internal/auth/jwt"
	sdomain "github.com/nedo/TicketSaas/internal/shared/domain"
)

func newTestTokenService(t testing.TB, ttl time.Duration) *jwt.TokenService {
	t.Helper()

	key, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)

	privPEM := pem.EncodeToMemory(&pem.Block{
		Type:  "RSA PRIVATE KEY",
		Bytes: x509.MarshalPKCS1PrivateKey(key),
	})

	pubBytes, err := x509.MarshalPKIXPublicKey(&key.PublicKey)
	require.NoError(t, err)
	pubPEM := pem.EncodeToMemory(&pem.Block{
		Type:  "PUBLIC KEY",
		Bytes: pubBytes,
	})

	svc, err := jwt.NewTokenService(string(privPEM), string(pubPEM), ttl)
	require.NoError(t, err)

	return svc
}

func parseAccessToken(t *testing.T, tokenString string) jwtgo.MapClaims {
	t.Helper()
	parser := jwtgo.NewParser()
	token, _, err := parser.ParseUnverified(tokenString, jwtgo.MapClaims{})
	require.NoError(t, err)
	claims, ok := token.Claims.(jwtgo.MapClaims)
	require.True(t, ok)
	return claims
}

// --- GenerateAccessToken ---

func TestGenerateAccessToken_ProducesRS256(t *testing.T) {
	svc := newTestTokenService(t, 15*time.Minute)

	token, _, err := svc.GenerateAccessToken("user-1", "e@x.com", sdomain.RoleCustomer)
	require.NoError(t, err)

	parser := jwtgo.NewParser()
	parsed, _, err := parser.ParseUnverified(token, jwtgo.MapClaims{})
	require.NoError(t, err)
	assert.Equal(t, "RS256", parsed.Header["alg"])
}

func TestGenerateAccessToken_EmbedsClaims(t *testing.T) {
	svc := newTestTokenService(t, 15*time.Minute)

	token, _, err := svc.GenerateAccessToken("usr-1", "e@x.com", sdomain.RoleCustomer)
	require.NoError(t, err)

	claims := parseAccessToken(t, token)
	assert.Equal(t, "usr-1", claims["sub"])
	assert.Equal(t, "e@x.com", claims["email"])
	assert.Equal(t, "customer", claims["role"])
}

func TestGenerateAccessToken_SetsExpiry(t *testing.T) {
	ttl := 15 * time.Minute
	before := time.Now()

	svc := newTestTokenService(t, ttl)

	token, _, err := svc.GenerateAccessToken("usr-1", "e@x.com", sdomain.RoleCustomer)
	require.NoError(t, err)

	claims := parseAccessToken(t, token)

	iatF, ok := claims["iat"].(float64)
	require.True(t, ok)
	iat := time.Unix(int64(iatF), 0)
	assert.WithinDuration(t, before, iat, 10*time.Second)

	expF, ok := claims["exp"].(float64)
	require.True(t, ok)
	exp := time.Unix(int64(expF), 0)
	expectedExp := iat.Add(ttl)
	assert.WithinDuration(t, expectedExp, exp, 5*time.Second)
}

// --- VerifyAccessToken ---

func TestVerifyAccessToken_ValidReturnsClaims(t *testing.T) {
	svc := newTestTokenService(t, 15*time.Minute)

	token, _, err := svc.GenerateAccessToken("user-1", "e@x.com", sdomain.RoleCustomer)
	require.NoError(t, err)

	claims, err := svc.VerifyAccessToken(token)
	require.NoError(t, err)
	assert.Equal(t, "user-1", claims.UserID)
	assert.Equal(t, "e@x.com", claims.Email)
	assert.Equal(t, sdomain.RoleCustomer, claims.Role)
}

func TestVerifyAccessToken_ExpiredRejected(t *testing.T) {
	// generate with TTL that's already expired
	svc := newTestTokenService(t, -1*time.Second)

	token, _, err := svc.GenerateAccessToken("user-1", "e@x.com", sdomain.RoleCustomer)
	require.NoError(t, err)

	_, err = svc.VerifyAccessToken(token)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "parse")
}

func TestVerifyAccessToken_DifferentKeyRejected(t *testing.T) {
	svcA := newTestTokenService(t, 15*time.Minute)
	svcB := newTestTokenService(t, 15*time.Minute)

	token, _, err := svcA.GenerateAccessToken("user-1", "e@x.com", sdomain.RoleCustomer)
	require.NoError(t, err)

	_, err = svcB.VerifyAccessToken(token)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "parse")
}

func TestVerifyAccessToken_TamperedTokenRejected(t *testing.T) {
	svc := newTestTokenService(t, 15*time.Minute)

	token, _, err := svc.GenerateAccessToken("user-1", "e@x.com", sdomain.RoleCustomer)
	require.NoError(t, err)

	tampered := token + "x"

	_, err = svc.VerifyAccessToken(tampered)
	require.Error(t, err)
}

// --- GenerateRefreshToken ---

func TestGenerateRefreshToken_Returns32Bytes(t *testing.T) {
	svc := newTestTokenService(t, 15*time.Minute)

	token, err := svc.GenerateRefreshToken()
	require.NoError(t, err)

	decoded, err := base64.URLEncoding.DecodeString(token)
	require.NoError(t, err)
	assert.Len(t, decoded, 32)
}

func TestGenerateRefreshToken_Unique(t *testing.T) {
	svc := newTestTokenService(t, 15*time.Minute)

	token1, err := svc.GenerateRefreshToken()
	require.NoError(t, err)
	token2, err := svc.GenerateRefreshToken()
	require.NoError(t, err)

	assert.NotEqual(t, token1, token2)
}

// --- HashRefreshToken ---

func TestHashRefreshToken_Deterministic(t *testing.T) {
	svc := newTestTokenService(t, 15*time.Minute)

	hash1 := svc.HashRefreshToken("abc")
	hash2 := svc.HashRefreshToken("abc")

	assert.Equal(t, hash1, hash2)
	assert.NotEmpty(t, hash1)
}
