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
	"github.com/prometheus/client_golang/prometheus/promhttp"

	bookingpkg "github.com/nedo/TicketSaas/service/ticketing/internal/application/booking"
	eventpkg "github.com/nedo/TicketSaas/service/ticketing/internal/application/event"
	eventhandler "github.com/nedo/TicketSaas/service/ticketing/internal/handler"
	eventkafka "github.com/nedo/TicketSaas/service/ticketing/internal/kafka"
	ticketingpayment "github.com/nedo/TicketSaas/service/ticketing/internal/payment"
	eventpostgres "github.com/nedo/TicketSaas/service/ticketing/internal/postgres"

	"github.com/nedo/TicketSaas/pkg/db"
	sharedhttp "github.com/nedo/TicketSaas/pkg/http"
	sharedkafka "github.com/nedo/TicketSaas/pkg/kafka"
	"github.com/nedo/TicketSaas/pkg/log"
	"github.com/nedo/TicketSaas/pkg/outbox"
	"github.com/nedo/TicketSaas/service/ticketing/internal/config"
)

func main() {
	logger := log.New("ticketing-service", "info")

	cfg, err := config.Load()
	if err != nil {
		logger.Error("failed to load config", "error", err)
		os.Exit(1)
	}
	if err := cfg.Validate(); err != nil {
		logger.Error("invalid config", "error", err)
		os.Exit(1)
	}

	pool, err := db.NewPool(cfg.DatabaseURL)
	if err != nil {
		logger.Error("failed to connect to database", "error", err)
		os.Exit(1)
	}
	defer pool.Close()

	eventRepo := eventpostgres.NewEventRepo(pool)
	bookingRepo := eventpostgres.NewBookingRepo(pool)
	eventStatusRepo := eventpostgres.NewEventStatusRepo(pool)
	seatCounter := eventpostgres.NewSeatCounter(pool)
	seatReader := eventpostgres.NewSeatReader(pool)
	paymentClient := ticketingpayment.NewClient(cfg.PaymentServiceURL, cfg.InternalAPIKey, cfg.PaymentTimeoutSec)

	kafkaBrokers := strings.Split(cfg.KafkaBrokers, ",")
	consumer := eventkafka.NewTicketingConsumer(
		kafkaBrokers,
		"ticketing-service",
		cfg.ConsumerConcurrency,
	)
	defer consumer.Close()

	outboxStore := outbox.NewStore(pool)
	kafkaProducer := sharedkafka.NewProducer(kafkaBrokers)
	if err := sharedkafka.EnsureTopics(kafkaBrokers, []string{
		"event.cancelled",
		"ticket.issued",
		"payment.completed", "payment.expired",
	}, 4, 3); err != nil {
		logger.Error("failed to ensure kafka topics", "error", err)
		os.Exit(1)
	}
	outboxWorker := outbox.NewWorker(
		pool,
		kafkaProducer,
		logger,
		cfg.OutboxConcurrency,
		cfg.OutboxPollMs,
	)

	bookingSvc := bookingpkg.NewBookingService(
		bookingRepo,
		paymentClient,
		seatCounter,
		consumer,
		eventStatusRepo,
		outboxStore,
		logger,
		cfg.ReservationTTL,
	)
	eventSvc := eventpkg.NewEventService(
		eventRepo,
		seatReader,
		seatCounter,
		outboxStore,
	)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go outboxWorker.Run(ctx)
	if err := bookingSvc.StartConsumers(ctx); err != nil {
		logger.Error("failed to start kafka consumers", "error", err)
		os.Exit(1)
	}

	eventHandler := eventhandler.NewEventHandler(eventSvc)
	bookingHandler := eventhandler.NewBookingHandler(bookingSvc)
	r := chi.NewRouter()
	r.Use(middleware.RequestID)
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)
	r.Use(sharedhttp.NewMetricsMiddleware("ticketing-service"))

	r.Get("/api/events/health", func(w http.ResponseWriter, r *http.Request) {
		sharedhttp.OK(w, map[string]string{"status": "ok", "service": "ticketing-service"})
	})
	r.Get("/api/bookings/health", func(w http.ResponseWriter, r *http.Request) {
		sharedhttp.OK(w, map[string]string{"status": "ok", "service": "ticketing-service"})
	})
	r.Get("/metrics", promhttp.Handler().ServeHTTP)
	eventHandler.Routes(r)
	bookingHandler.Routes(r)

	srv := &http.Server{Addr: ":" + cfg.Port, Handler: r}
	go func() {
		logger.Info("ticketing-service starting", "env", cfg.AppEnv, "port", cfg.Port)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Error("server failed", "error", err)
			os.Exit(1)
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	logger.Info("ticketing-service shutting down")
	cancel()
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer shutdownCancel()
	_ = srv.Shutdown(shutdownCtx)
}
