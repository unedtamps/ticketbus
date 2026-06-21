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
	"github.com/nedo/TicketSaas/internal/payment/application"
	"github.com/nedo/TicketSaas/internal/payment/config"
	"github.com/nedo/TicketSaas/internal/payment/handler"
	paykafka "github.com/nedo/TicketSaas/internal/payment/kafka"
	"github.com/nedo/TicketSaas/internal/payment/postgres"
	"github.com/nedo/TicketSaas/internal/payment/processor"
	shareddb "github.com/nedo/TicketSaas/internal/shared/db"
	sharedhttp "github.com/nedo/TicketSaas/internal/shared/http"
	"github.com/nedo/TicketSaas/internal/shared/log"
)

func main() {
	logger := log.New("payment-service", "info")

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

	txnRepo := postgres.NewTransactionRepo(pool)
	mockProcessor := processor.NewMockProcessor()

	kafkaBrokers := strings.Split(cfg.KafkaBrokers, ",")
	producer := paykafka.NewPaymentProducer(kafkaBrokers)
	defer producer.Close()

	consumer := paykafka.NewPaymentConsumer(kafkaBrokers, "payment-service")
	defer consumer.Close()

	svc := application.NewPaymentService(txnRepo, mockProcessor, producer, consumer, logger, cfg.WebhookBaseURL)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	_ = svc.StartConsumer(ctx)

	h := handler.NewPaymentHandler(svc)
	r := chi.NewRouter()
	r.Use(middleware.RequestID)
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)

	r.Get("/health", func(w http.ResponseWriter, r *http.Request) {
		sharedhttp.OK(w, map[string]string{"status": "ok", "service": "payment-service"})
	})

	r.Mount("/", h.Routes())

	srv := &http.Server{Addr: ":" + cfg.Port, Handler: r}

	go func() {
		logger.Info("payment-service starting", "env", cfg.AppEnv, "port", cfg.Port)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Error("server failed", "error", err)
			os.Exit(1)
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	logger.Info("payment-service shutting down")
	cancel()
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer shutdownCancel()
	_ = srv.Shutdown(shutdownCtx)
}
