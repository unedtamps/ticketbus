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
	"github.com/nedo/TicketSaas/internal/event/application"
	"github.com/nedo/TicketSaas/internal/event/config"
	"github.com/nedo/TicketSaas/internal/event/handler"
	"github.com/nedo/TicketSaas/internal/event/kafka"
	"github.com/nedo/TicketSaas/internal/event/postgres"
	eventredis "github.com/nedo/TicketSaas/internal/event/redis"
	shareddb "github.com/nedo/TicketSaas/internal/shared/db"
	sharedhttp "github.com/nedo/TicketSaas/internal/shared/http"
	"github.com/nedo/TicketSaas/internal/shared/log"
)

func main() {
	logger := log.New("event-service", "info")

	cfg, err := config.Load()
	if err != nil {
		logger.Error("failed to load config", "error", err)
		os.Exit(1)
	}

	pool, err := shareddb.NewPool(cfg.DatabaseURL)
	if err != nil {
		logger.Error("failed to connect to database", "error", err)
		os.Exit(1)
	}
	defer pool.Close()

	rdb := redis.NewClient(&redis.Options{Addr: cfg.RedisAddr})
	if err := rdb.Ping(context.Background()).Err(); err != nil {
		logger.Error("failed to connect to redis", "error", err)
		os.Exit(1)
	}
	defer rdb.Close()

	repo := postgres.NewEventRepo(pool)

	kafkaBrokers := strings.Split(cfg.KafkaBrokers, ",")
	publisher := kafka.NewEventPublisher(kafkaBrokers)
	defer publisher.Close()

	consumer := kafka.NewOrganizerConsumer(kafkaBrokers, "event-service")

	seatReader := eventredis.NewSeatReader(rdb)
	svc := application.NewEventService(repo, publisher, consumer, seatReader)
	h := handler.NewEventHandler(svc)

	// Start Kafka consumers
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go svc.StartOrganizerConsumer(ctx)

	r := chi.NewRouter()
	r.Use(middleware.RequestID)
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)

	r.Get("/health", func(w http.ResponseWriter, r *http.Request) {
		sharedhttp.OK(w, map[string]string{"status": "ok", "service": "event-service"})
	})

	r.Mount("/", h.Routes())

	srv := &http.Server{Addr: ":" + cfg.Port, Handler: r}

	go func() {
		logger.Info("event-service starting", "env", cfg.AppEnv, "port", cfg.Port)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Error("server failed", "error", err)
			os.Exit(1)
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	logger.Info("event-service shutting down")
	cancel()
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer shutdownCancel()
	_ = srv.Shutdown(shutdownCtx)
}
