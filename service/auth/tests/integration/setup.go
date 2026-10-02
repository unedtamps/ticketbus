//go:build integration

// Package integration is the auth service's integration suite. It boots Postgres
// via pkg/testinfra, wires the auth handler against it in-process, and
// exercises the public HTTP API.
//
// The suite imports nothing from the other services. It needs no Kafka and no
// gateway: it owns identity, so it is the one service that issues real signed
// JWTs.
//
// Running the suite:
//
//	go test -tags=integration ./service/auth/tests/integration/
//
// Per-area filtering — test names follow Test<Area>_<Scenario>[_<Variant>]
// with area Auth:
//
//	go test -tags=integration -run 'TestAuth' ./service/auth/tests/integration/
package integration

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"net/http/httptest"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/nedo/TicketSaas/pkg/testinfra"
	"github.com/nedo/TicketSaas/service/auth/internal/application"
	"github.com/nedo/TicketSaas/service/auth/internal/bcrypt"
	authhandler "github.com/nedo/TicketSaas/service/auth/internal/handler"
	"github.com/nedo/TicketSaas/service/auth/internal/jwt"
	authpostgres "github.com/nedo/TicketSaas/service/auth/internal/postgres"
)

const (
	accessTokenTTL  = 15 * time.Minute
	refreshTokenTTL = 7 * 24 * time.Hour

	adminEmail    = "admin@test.com"
	adminPassword = "Admin123!"
)

var (
	testEnv *TestEnv
	once    sync.Once
)

// TestEnv holds the container handles, the wired auth service, and the base URL
// the tests drive.
type TestEnv struct {
	pool     *pgxpool.Pool
	svc      *application.AuthService
	tokenSvc *jwt.TokenService
	authURL  string
	srv      *httptest.Server
	infra    *testinfra.Infra
}

func TestMain(m *testing.M) {
	code := m.Run()
	if testEnv != nil {
		testEnv.infra.Close(context.Background())
		testEnv.srv.Close()
	}
	os.Exit(code)
}

func startContainers() *TestEnv {
	infra, err := testinfra.Start(context.Background(), "auth", testinfra.WithoutKafka())
	if err != nil {
		panic(err)
	}

	privatePEM, publicPEM := generateRSAKeys()

	userRepo := authpostgres.NewUserRepo(infra.Pool)
	tokenRepo := authpostgres.NewRefreshTokenRepo(infra.Pool)

	tokenSvc, err := jwt.NewTokenService(privatePEM, publicPEM, accessTokenTTL)
	if err != nil {
		panic(fmt.Sprintf("token service: %v", err))
	}

	svc := application.NewAuthService(
		userRepo, tokenRepo, bcrypt.NewHasher(), tokenSvc,
		application.TokensConfig{AccessTokenTTL: accessTokenTTL, RefreshTokenTTL: refreshTokenTTL},
	)

	srv := httptest.NewServer(authhandler.NewAuthHandler(svc).Routes())

	// Seed the admin the login test needs. Seeding is idempotent.
	if _, err := svc.SeedAdmins(context.Background(), []application.AdminSeed{
		{Email: adminEmail, Password: adminPassword, Name: "Admin"},
	}); err != nil {
		panic(fmt.Sprintf("seed admin: %v", err))
	}

	return &TestEnv{
		pool:     infra.Pool,
		svc:      svc,
		tokenSvc: tokenSvc,
		authURL:  srv.URL,
		srv:      srv,
		infra:    infra,
	}
}

func getTestEnv() *TestEnv {
	once.Do(func() { testEnv = startContainers() })
	return testEnv
}

// generateRSAKeys creates a throwaway signing pair for the suite.
func generateRSAKeys() (privatePEM, publicPEM string) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		panic(fmt.Sprintf("rsa: %v", err))
	}
	privBytes := x509.MarshalPKCS1PrivateKey(key)
	privatePEM = string(pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: privBytes}))
	pubBytes, err := x509.MarshalPKIXPublicKey(&key.PublicKey)
	if err != nil {
		panic(fmt.Sprintf("public key: %v", err))
	}
	publicPEM = string(pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: pubBytes}))
	return privatePEM, publicPEM
}
