package main

import (
	"context"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/nedo/TicketSaas/internal/auth/application"
	"github.com/nedo/TicketSaas/internal/auth/bcrypt"
	"github.com/nedo/TicketSaas/internal/auth/config"
	"github.com/nedo/TicketSaas/internal/auth/handler"
	"github.com/nedo/TicketSaas/internal/auth/jwt"
	"github.com/nedo/TicketSaas/internal/auth/postgres"
	shareddb "github.com/nedo/TicketSaas/internal/shared/db"
	sharedhttp "github.com/nedo/TicketSaas/internal/shared/http"
	sharedkafka "github.com/nedo/TicketSaas/internal/shared/kafka"
	"github.com/nedo/TicketSaas/internal/shared/log"
	"github.com/nedo/TicketSaas/internal/shared/outbox"
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
	outboxStore := outbox.NewStore(pool)
	kafkaProducer := sharedkafka.NewProducer(strings.Split(cfg.KafkaBrokers, ","))
	if err := sharedkafka.EnsureTopics(strings.Split(cfg.KafkaBrokers, ","), []string{
		"organizer.created",
		"event.created", "event.approved", "event.rejected", "event.updated", "event.cancelled",
		"reservation.created", "reservation.expired", "ticket.issued",
		"payment.initiated", "payment.completed", "payment.failed",
	}, 4, 3); err != nil {
		logger.Error("failed to ensure kafka topics", "error", err)
		os.Exit(1)
	}
	outboxWorker := outbox.NewWorker(pool, kafkaProducer, logger)

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
		outboxStore,
	)

	// Primary adapter (HTTP)
	authHandler := handler.NewAuthHandler(authSvc)

	// Seed admin user during development (idempotent).
	if cfg.AppEnv == "development" {
		if cfg.AdminEmail != "" && cfg.AdminPassword != "" {
			created, err := authSvc.SeedAdmin(context.Background(), cfg.AdminEmail, cfg.AdminPassword, "Admin")
			if err != nil {
				logger.Error("failed to seed admin", "error", err)
				os.Exit(1)
			}
			if created {
				logger.Info("admin user created", "email", cfg.AdminEmail)
			}
		} else {
			logger.Warn("ADMIN_EMAIL/ADMIN_PASSWORD not set, skipping admin seed")
		}
	}

	// Router
	r := chi.NewRouter()
	r.Use(middleware.RequestID)
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)
	r.Use(sharedhttp.WithUserContext)

	r.Get("/health", func(w http.ResponseWriter, r *http.Request) {
		sharedhttp.OK(w, map[string]string{"status": "ok", "service": "auth-service"})
	})

	r.Mount("/", authHandler.Routes())

	// Start outbox worker
	go outboxWorker.Run(context.Background())

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
