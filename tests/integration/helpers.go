//go:build integration

package integration

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"

	authapp "github.com/nedo/TicketSaas/internal/auth/application"
	"github.com/nedo/TicketSaas/internal/auth/jwt"
	eventapp "github.com/nedo/TicketSaas/internal/event/application"
	invapp "github.com/nedo/TicketSaas/internal/inventory/application"
	invredis "github.com/nedo/TicketSaas/internal/inventory/redis"
	payapp "github.com/nedo/TicketSaas/internal/payment/application"
	sharedkafka "github.com/nedo/TicketSaas/internal/shared/kafka"
	"github.com/testcontainers/testcontainers-go"
)

// TestEnv holds all infrastructure, service, and client refs for integration tests.
type TestEnv struct {
	authPool  *pgxpool.Pool
	eventPool *pgxpool.Pool
	invPool   *pgxpool.Pool
	payPool   *pgxpool.Pool
	adminPool *pgxpool.Pool
	rdb       *redis.Client

	containers containers

	kafkaProducer    *sharedkafka.Producer
	authSvc          *authapp.AuthService
	eventSvc         *eventapp.EventService
	invSvc           *invapp.InventoryService
	paySvc           *payapp.PaymentService
	tokenSvc         *jwt.TokenService
	jwtPublicPEM     string
	reservationCache *invredis.ReservationCache
	authURL          string
	eventURL         string
	invURL           string
	payURL           string
	cancelBg         context.CancelFunc
	authSrv          *httptest.Server
	eventSrv         *httptest.Server
	invSrv           *httptest.Server
	paySrv           *httptest.Server
}

type containers struct {
	pg    testcontainers.Container
	redis testcontainers.Container
	kafka testcontainers.Container
}

// ── HTTP helpers ──

func doJSON(
	method, url string,
	body interface{},
	headers map[string]string,
) (*http.Response, []byte, error) {
	var r io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		r = strings.NewReader(string(b))
	}
	req, err := http.NewRequest(method, url, r)
	if err != nil {
		return nil, nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, nil, err
	}
	defer resp.Body.Close()
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return resp, nil, err
	}
	return resp, respBody, nil
}

func mustJSON(data []byte, target interface{}) {
	if err := json.Unmarshal(data, &target); err != nil {
		panic(fmt.Sprintf("json: %s\nraw: %s", err, string(data)))
	}
}

// ── Auth helpers ──

type credentials struct {
	AccessToken  string
	RefreshToken string
	UserID       string
	Email        string
	Role         string
}

// registerAndLogin creates a customer or eo and returns tokens.
// Uses the auth service directly (no HTTP) for simplicity.
func (env *TestEnv) registerAndLogin(role string) credentials {
	email := fmt.Sprintf("%s_%s@test.com", role, uuid.NewString()[:8])
	name := role + " Test"
	pass := "Test123!"

	var userID string

	switch role {
	case "customer":
		user, err := env.authSvc.Register(context.Background(), email, pass, name, "customer")
		if err != nil {
			panic(fmt.Sprintf("register customer: %v", err))
		}
		userID = user.ID
	case "eo":
		user, err := env.authSvc.RegisterOrganizer(context.Background(),
			email, pass, name, "EO Organizer", "desc", "https://eo.test", email)
		if err != nil {
			panic(fmt.Sprintf("register eo: %v", err))
		}
		userID = user.ID
	default:
		panic("unknown role: " + role)
	}

	pair, err := env.authSvc.Login(context.Background(), email, pass)
	if err != nil {
		panic(fmt.Sprintf("login %s: %v", role, err))
	}

	return credentials{
		AccessToken:  pair.AccessToken,
		RefreshToken: pair.RefreshToken,
		UserID:       userID,
		Email:        email,
		Role:         role,
	}
}

func (env *TestEnv) loginAdmin() credentials {
	pair, err := env.authSvc.Login(context.Background(), "admin@test.com", "Admin123!")
	if err != nil {
		panic(fmt.Sprintf("admin login: %v", err))
	}
	claims, err := env.tokenSvc.VerifyAccessToken(pair.AccessToken)
	if err != nil {
		panic(fmt.Sprintf("verify admin token: %v", err))
	}
	return credentials{
		AccessToken:  pair.AccessToken,
		RefreshToken: pair.RefreshToken,
		UserID:       claims.UserID,
		Email:        claims.Email,
		Role:         "admin",
	}
}

// authHeaders decodes a JWT and returns the X-Authenticated-* headers expected
// by WithUserContext middleware on event/inventory/payment services.
func (env *TestEnv) authHeaders(accessToken string) map[string]string {
	claims, err := env.tokenSvc.VerifyAccessToken(accessToken)
	if err != nil {
		panic(fmt.Sprintf("verify token: %v", err))
	}
	return map[string]string{
		"X-Authenticated-User-ID":    claims.UserID,
		"X-Authenticated-User-Role":  string(claims.Role),
		"X-Authenticated-User-Email": claims.Email,
	}
}

// pollFor calls fn every interval until it returns true or timeout expires.
func pollFor(t *testing.T, timeout, interval time.Duration, fn func() bool, msg string) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if fn() {
			return
		}
		time.Sleep(interval)
	}
	t.Fatalf("%s timed out after %v", msg, timeout)
}
// authHeadersWith contains both Bearer auth and X-auth headers.
func (env *TestEnv) authHeadersWith(accessToken string) map[string]string {
	h := env.authHeaders(accessToken)
	h["Authorization"] = "Bearer " + accessToken
	return h
}
