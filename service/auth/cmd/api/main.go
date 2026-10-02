package main

import (
	"context"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	shareddb "github.com/nedo/TicketSaas/pkg/db"
	sharedhttp "github.com/nedo/TicketSaas/pkg/http"
	"github.com/nedo/TicketSaas/pkg/log"
	"github.com/nedo/TicketSaas/service/auth/internal/application"
	"github.com/nedo/TicketSaas/service/auth/internal/bcrypt"
	"github.com/nedo/TicketSaas/service/auth/internal/config"
	"github.com/nedo/TicketSaas/service/auth/internal/handler"
	"github.com/nedo/TicketSaas/service/auth/internal/jwt"
	"github.com/nedo/TicketSaas/service/auth/internal/postgres"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

const (
	accessTokenTTL  = 15 * time.Minute
	refreshTokenTTL = 7 * 24 * time.Hour
)

func main() {
	logger := log.New("auth-service", "info")

	cfg, err := config.Load()
	if err != nil {
		logger.Error("failed to load config", "error", err)
		os.Exit(1)
	}
	if err := cfg.Validate(); err != nil {
		logger.Error("invalid config", "error", err)
		os.Exit(1)
	}

	tokenSvc, err := jwt.NewTokenService(cfg.JWTPrivateKey, cfg.JWTPublicKey, accessTokenTTL)
	if err != nil {
		logger.Error("failed to create token service", "error", err)
		os.Exit(1)
	}

	pubKey, _ := tokenSvc.PublicKeyPEM()
	logger.Info("JWT public key", "key", pubKey)

	// Database
	pool, err := shareddb.NewPool(cfg.DatabaseURL)
	if err != nil {
		logger.Error("failed to connect to database", "error", err)
		os.Exit(1)
	}
	defer pool.Close()

	// Adapters (secondary)
	userRepo := postgres.NewUserRepo(pool)
	tokenRepo := postgres.NewRefreshTokenRepo(pool)
	hasher := bcrypt.NewHasher()

	// Application
	authSvc := application.NewAuthService(
		userRepo,
		tokenRepo,
		hasher,
		tokenSvc,
		application.TokensConfig{
			AccessTokenTTL:  accessTokenTTL,
			RefreshTokenTTL: refreshTokenTTL,
		},
	)

	// Primary adapter (HTTP)
	authHandler := handler.NewAuthHandler(authSvc)

	// Seed admin users on every startup (idempotent — skips existing emails).
	admins, err := cfg.AdminSeeds()
	if err != nil {
		logger.Error("invalid admin seeds", "error", err)
		os.Exit(1)
	}
	if len(admins) > 0 {
		created, err := authSvc.SeedAdmins(context.Background(), admins)
		if err != nil {
			logger.Error("failed to seed admins", "error", err)
			os.Exit(1)
		}
		if created > 0 {
			logger.Info("admin users created", "count", created)
		}
	}

	// Router
	r := chi.NewRouter()
	r.Use(middleware.RequestID)
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)
	r.Use(sharedhttp.WithUserContext)
	r.Use(sharedhttp.NewMetricsMiddleware("auth-service"))

	r.Get("/api/auth/health", func(w http.ResponseWriter, r *http.Request) {
		sharedhttp.OK(w, map[string]string{"status": "ok", "service": "auth-service"})
	})

	r.Get("/metrics", promhttp.Handler().ServeHTTP)

	r.Mount("/", authHandler.Routes())

	srv := &http.Server{Addr: ":" + cfg.Port, Handler: r}

	go func() {
		logger.Info("auth-service starting", "env", cfg.AppEnv, "port", cfg.Port)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Error("server failed", "error", err)
			os.Exit(1)
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	logger.Info("auth-service shutting down")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_ = srv.Shutdown(ctx)
}
