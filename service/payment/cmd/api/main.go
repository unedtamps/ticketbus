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
	shareddb "github.com/nedo/TicketSaas/pkg/db"
	sharedhttp "github.com/nedo/TicketSaas/pkg/http"
	sharedkafka "github.com/nedo/TicketSaas/pkg/kafka"
	"github.com/nedo/TicketSaas/pkg/log"
	"github.com/nedo/TicketSaas/pkg/outbox"
	"github.com/nedo/TicketSaas/pkg/telemetry"
	"github.com/nedo/TicketSaas/service/payment/internal/application"
	"github.com/nedo/TicketSaas/service/payment/internal/config"
	"github.com/nedo/TicketSaas/service/payment/internal/domain"
	"github.com/nedo/TicketSaas/service/payment/internal/handler"
	paykafka "github.com/nedo/TicketSaas/service/payment/internal/kafka"
	"github.com/nedo/TicketSaas/service/payment/internal/postgres"
	"github.com/nedo/TicketSaas/service/payment/internal/processor"
)

func main() {
	logger := log.New("payment-service", "info")

	cfg, err := config.Load()
	if err != nil {
		logger.Error("failed to load config", "error", err)
		os.Exit(1)
	}
	if err := cfg.Validate(); err != nil {
		logger.Error("invalid config", "error", err)
		os.Exit(1)
	}

	// Before db.NewPool: the pgx tracer captures the global providers by
	// reference and would stay a no-op if built first.
	shutdownTelemetry, httpMetrics, err := telemetry.Init(context.Background(), telemetry.Config{
		ServiceName:    "payment-service",
		ServiceVersion: "dev",
		Environment:    cfg.AppEnv,
	})
	if err != nil {
		logger.Error("failed to init telemetry", "error", err)
		os.Exit(1)
	}

	pool, err := shareddb.NewPool(cfg.DatabaseURL)
	if err != nil {
		logger.Error("failed to connect to database", "error", err)
		os.Exit(1)
	}
	defer pool.Close()

	txnRepo := postgres.NewTransactionRepo(pool)
	refundRepo := postgres.NewRefundRepo(pool)

	var payProcessor domain.PaymentProcessor
	switch cfg.Provider {
	case "xendit":
		payProcessor = processor.NewXenditProcessor(cfg.XenditBaseURL, cfg.XenditAPIKey)
	case "mock":
		fallthrough
	default:
		payProcessor = processor.NewMockProcessor()
	}

	kafkaBrokers := strings.Split(cfg.KafkaBrokers, ",")
	consumer := paykafka.NewPaymentConsumer(kafkaBrokers, "payment-service", cfg.ConsumerConcurrency)
	defer consumer.Close()

	outboxStore := outbox.NewStore(pool)
	kafkaProducer := sharedkafka.NewProducer(kafkaBrokers)
	if err := sharedkafka.EnsureTopics(kafkaBrokers, []string{
		"event.cancelled",
		"payment.completed", "payment.expired",
	}, 4, 3); err != nil {
		logger.Error("failed to ensure kafka topics", "error", err)
		os.Exit(1)
	}
	outboxWorker := outbox.NewWorker(pool, kafkaProducer, logger, cfg.OutboxConcurrency, cfg.OutboxPollMs)

	svc := application.NewPaymentService(
		txnRepo,
		refundRepo,
		payProcessor,
		consumer,
		outboxStore,
		logger,
		cfg.Provider,
		cfg.GatewayExpiryBufferMin,
		cfg.EnabledMethods(),
	)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go outboxWorker.Run(ctx)
	_ = svc.StartConsumer(ctx)
	svc.StartExpiryPoller(ctx, cfg.ExpiryPollSec)

	h := handler.NewPaymentHandler(svc, cfg.InternalAPIKey, cfg.WebhookCallbackTok)
	r := chi.NewRouter()
	r.Use(middleware.RequestID)
	r.Use(sharedhttp.OTelMiddleware())
	r.Use(sharedhttp.RequestLogger(logger, httpMetrics))
	r.Use(middleware.Recoverer)

	r.Get("/api/payments/health", func(w http.ResponseWriter, r *http.Request) {
		sharedhttp.OK(w, map[string]string{"status": "ok", "service": "payment-service"})
	})

	r.Mount("/", h.Routes())

	srv := &http.Server{Addr: ":" + cfg.Port, Handler: r}

	go func() {
		logger.Info("payment-service starting", "env", cfg.AppEnv, "port", cfg.Port, "provider", cfg.Provider)
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
	if err := shutdownTelemetry(shutdownCtx); err != nil {
		logger.Error("failed to flush telemetry", "error", err)
	}
}
