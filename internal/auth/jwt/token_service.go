package jwt

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"fmt"
	"time"

	jwtgo "github.com/golang-jwt/jwt/v5"
	"github.com/nedo/TicketSaas/internal/auth/domain"
	sdomain "github.com/nedo/TicketSaas/internal/shared/domain"
)

// TokenService implements domain.TokenService using RS256 JWT.
type TokenService struct {
	privateKey *rsa.PrivateKey
	publicKey  *rsa.PublicKey
	jwtTTL     time.Duration
}

// NewTokenService creates a new JWT token service.
func NewTokenService(privateKeyPEM, publicKeyPEM string, jwtTTL time.Duration) (*TokenService, error) {
	if privateKeyPEM == "" {
		return nil, fmt.Errorf("JWT_PRIVATE_KEY is required")
	}
	if publicKeyPEM == "" {
		return nil, fmt.Errorf("JWT_PUBLIC_KEY is required")
	}

	privateKey, err := jwtgo.ParseRSAPrivateKeyFromPEM([]byte(privateKeyPEM))
	if err != nil {
		return nil, fmt.Errorf("failed to parse private key: %w", err)
	}

	publicKey, err := jwtgo.ParseRSAPublicKeyFromPEM([]byte(publicKeyPEM))
	if err != nil {
		return nil, fmt.Errorf("failed to parse public key: %w", err)
	}

	return &TokenService{privateKey: privateKey, publicKey: publicKey, jwtTTL: jwtTTL}, nil
}

// GenerateAccessToken creates a new RS256 JWT.
func (s *TokenService) GenerateAccessToken(userID, email string, role sdomain.Role) (string, time.Duration, error) {
	claims := domain.Claims{
		UserID: userID,
		Email:  email,
		Role:   role,
	}

	token := jwtgo.NewWithClaims(jwtgo.SigningMethodRS256, jwtgo.MapClaims{
		"sub":   claims.UserID,
		"email": claims.Email,
		"role":  claims.Role,
		"exp":   time.Now().Add(s.jwtTTL).Unix(),
		"iat":   time.Now().Unix(),
	})

	signed, err := token.SignedString(s.privateKey)
	if err != nil {
		return "", 0, fmt.Errorf("failed to sign token: %w", err)
	}

	return signed, s.jwtTTL, nil
}

// GenerateRefreshToken creates a random opaque refresh token.
func (s *TokenService) GenerateRefreshToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("failed to generate refresh token: %w", err)
	}
	return base64.URLEncoding.EncodeToString(b), nil
}

// HashRefreshToken returns the SHA-256 hash of a refresh token.
func (s *TokenService) HashRefreshToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return base64.URLEncoding.EncodeToString(sum[:])
}

// VerifyAccessToken parses and validates an RS256 JWT.
func (s *TokenService) VerifyAccessToken(tokenString string) (*domain.Claims, error) {
	token, err := jwtgo.Parse(tokenString, func(token *jwtgo.Token) (interface{}, error) {
		if _, ok := token.Method.(*jwtgo.SigningMethodRSA); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", token.Header["alg"])
		}
		return s.publicKey, nil
	})
	if err != nil {
		return nil, fmt.Errorf("failed to parse token: %w", err)
	}

	claims, ok := token.Claims.(jwtgo.MapClaims)
	if !ok || !token.Valid {
		return nil, fmt.Errorf("invalid token claims")
	}

	userID, _ := claims["sub"].(string)
	email, _ := claims["email"].(string)
	role, _ := claims["role"].(string)

	return &domain.Claims{
		UserID: userID,
		Email:  email,
		Role:   sdomain.Role(role),
	}, nil
}

// PublicKeyPEM returns the public key in PEM format for Kong configuration.
func (s *TokenService) PublicKeyPEM() (string, error) {
	bytes, err := x509.MarshalPKIXPublicKey(s.publicKey)
	if err != nil {
		return "", fmt.Errorf("failed to marshal public key: %w", err)
	}
	block := &pem.Block{
		Type:  "PUBLIC KEY",
		Bytes: bytes,
	}
	return string(pem.EncodeToMemory(block)), nil
}
