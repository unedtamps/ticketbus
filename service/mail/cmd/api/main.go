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
	sdomain "github.com/unedtamps/ticketbus/pkg/dto"
	sharedhttp "github.com/unedtamps/ticketbus/pkg/http"
	sharedkafka "github.com/unedtamps/ticketbus/pkg/kafka"
	"github.com/unedtamps/ticketbus/pkg/log"
	"github.com/unedtamps/ticketbus/pkg/telemetry"
	"github.com/unedtamps/ticketbus/service/mail/internal/application"
	"github.com/unedtamps/ticketbus/service/mail/internal/config"
	"github.com/unedtamps/ticketbus/service/mail/internal/kafka"
	"github.com/unedtamps/ticketbus/service/mail/internal/mailer"
)

func main() {
	logger := log.New("mail-service", "info")

	cfg, err := config.Load()
	if err != nil {
		logger.Error("failed to load config", "error", err)
		os.Exit(1)
	}

	shutdownTelemetry, httpMetrics, err := telemetry.Init(context.Background(), telemetry.Config{
		ServiceName:    "mail-service",
		ServiceVersion: "dev",
		Environment:    cfg.AppEnv,
	})
	if err != nil {
		logger.Error("failed to init telemetry", "error", err)
		os.Exit(1)
	}

	kafkaBrokers := strings.Split(cfg.KafkaBrokers, ",")
	consumer := kafka.NewMailConsumer(kafkaBrokers, "mail-service", cfg.ConsumerConcurrency)
	defer consumer.Close()

	// ticket.issued is normally ensured by ticketing, but the mail service must
	// not die if it starts before ticketing ever ran.
	if err := sharedkafka.EnsureTopics(kafkaBrokers, []string{sdomain.TopicTicketIssued}, 4, 3); err != nil {
		logger.Error("failed to ensure kafka topics", "error", err)
		os.Exit(1)
	}

	sender := mailer.NewSMTP(cfg.SMTPHost, cfg.SMTPPort, cfg.SMTPFrom)
	svc, err := application.NewMailService(consumer, sender, logger)
	if err != nil {
		logger.Error("failed to build mail service", "error", err)
		os.Exit(1)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go svc.StartConsumer(ctx)

	r := chi.NewRouter()
	r.Use(middleware.RequestID)
	r.Use(sharedhttp.OTelMiddleware())
	r.Use(sharedhttp.RequestLogger(logger, httpMetrics))
	r.Use(middleware.Recoverer)

	r.Get("/api/mail/health", func(w http.ResponseWriter, r *http.Request) {
		sharedhttp.OK(w, map[string]string{"status": "ok", "service": "mail-service"})
	})

	srv := &http.Server{Addr: ":" + cfg.Port, Handler: r}

	go func() {
		logger.Info("mail-service starting",
			"env", cfg.AppEnv, "port", cfg.Port,
			"smtp", cfg.SMTPHost+":"+cfg.SMTPPort,
		)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Error("server failed", "error", err)
			os.Exit(1)
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	logger.Info("mail-service shutting down")
	cancel()
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer shutdownCancel()
	_ = srv.Shutdown(shutdownCtx)
	if err := shutdownTelemetry(shutdownCtx); err != nil {
		logger.Error("failed to flush telemetry", "error", err)
	}
}
