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
	"github.com/redis/go-redis/v9"

	"github.com/nedo/TicketSaas/internal/inventory/application"
	"github.com/nedo/TicketSaas/internal/inventory/config"
	"github.com/nedo/TicketSaas/internal/inventory/handler"
	invkafka "github.com/nedo/TicketSaas/internal/inventory/kafka"
	"github.com/nedo/TicketSaas/internal/inventory/postgres"
	invredis "github.com/nedo/TicketSaas/internal/inventory/redis"
	shareddb "github.com/nedo/TicketSaas/internal/shared/db"
	sharedhttp "github.com/nedo/TicketSaas/internal/shared/http"
	sharedkafka "github.com/nedo/TicketSaas/internal/shared/kafka"
	"github.com/nedo/TicketSaas/internal/shared/log"
	"github.com/nedo/TicketSaas/internal/shared/outbox"
)

func main() {
	logger := log.New("inventory-service", "info")

	cfg, err := config.Load()
	if err != nil {
		logger.Error("failed to load config", "error", err)
		os.Exit(1)
	}

	// Database
	pool, err := shareddb.NewPool(cfg.DatabaseURL)
	if err != nil {
		logger.Error("failed to connect to database", "error", err)
		os.Exit(1)
	}
	defer pool.Close()

	// Redis
	rdb := redis.NewClient(&redis.Options{Addr: cfg.RedisAddr})
	if err := rdb.Ping(context.Background()).Err(); err != nil {
		logger.Error("failed to connect to redis", "error", err)
		os.Exit(1)
	}
	defer rdb.Close()

	// Adapters
	bookingRepo := postgres.NewBookingRepo(pool)
	reservationCache := invredis.NewReservationCache(rdb)
	seatCounter := invredis.NewSeatCounter(rdb)
	eventStatusRepo := postgres.NewEventStatusRepo(pool)

	kafkaBrokers := strings.Split(cfg.KafkaBrokers, ",")
	consumer := invkafka.NewInventoryConsumer(kafkaBrokers, "inventory-service")
	defer consumer.Close()

	outboxStore := outbox.NewStore(pool)
	kafkaProducer := sharedkafka.NewProducer(kafkaBrokers)
	if err := sharedkafka.EnsureTopics(kafkaBrokers, []string{
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
	svc := application.NewInventoryService(bookingRepo, reservationCache, seatCounter, consumer, eventStatusRepo, outboxStore, logger, cfg.ReservationTTL)

	// Start Kafka consumers + Redis expiry listener + outbox worker
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go outboxWorker.Run(ctx)
	_ = svc.StartConsumers(ctx)
	svc.StartExpiryListener(ctx)

	// HTTP
	h := handler.NewInventoryHandler(svc)
	r := chi.NewRouter()
	r.Use(middleware.RequestID)
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)

	r.Get("/health", func(w http.ResponseWriter, r *http.Request) {
		sharedhttp.OK(w, map[string]string{"status": "ok", "service": "inventory-service"})
	})

	r.Mount("/", h.Routes())

	srv := &http.Server{Addr: ":" + cfg.Port, Handler: r}

	go func() {
		logger.Info("inventory-service starting", "env", cfg.AppEnv, "port", cfg.Port)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Error("server failed", "error", err)
			os.Exit(1)
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	logger.Info("inventory-service shutting down")
	cancel()
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer shutdownCancel()
	_ = srv.Shutdown(shutdownCtx)
}
