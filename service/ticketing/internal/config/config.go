package config

import (
	"fmt"

	"github.com/caarlos0/env/v11"
)

// Config holds the combined Ticketing service configuration.
type Config struct {
	AppEnv              string `env:"APP_ENV,required,notEmpty"`
	Port                string `env:"PORT,required,notEmpty"`
	DatabaseURL         string `env:"DATABASE_URL,required,notEmpty"`
	KafkaBrokers        string `env:"KAFKA_BROKERS,required,notEmpty"`
	ReservationTTL      int    `env:"RESERVATION_TTL,required,notEmpty"`
	ConsumerConcurrency int    `env:"CONSUMER_CONCURRENCY,required,notEmpty"`
	OutboxConcurrency   int    `env:"OUTBOX_CONCURRENCY,required,notEmpty"`
	OutboxPollMs        int    `env:"OUTBOX_POLL_MS,required,notEmpty"`

	// Payment service. PAYMENT_SERVICE_URL / INTERNAL_API_KEY are used by the
	// outbox REST dispatcher, which calls POST /api/payments/internal in the
	// background to create the payment transaction for each reservation.
	// PAYMENT_TIMEOUT_SEC bounds each REST attempt.
	PaymentServiceURL string `env:"PAYMENT_SERVICE_URL,required,notEmpty"`
	PaymentTimeoutSec int    `env:"PAYMENT_TIMEOUT_SEC,required,notEmpty"`
	InternalAPIKey    string `env:"INTERNAL_API_KEY,required,notEmpty"`

	// RestDispatchPollMs is how often the REST dispatcher wakes to look for
	// undelivered payment.initiate outbox rows. Defaults to 200ms.
	RestDispatchPollMs int `env:"REST_DISPATCH_POLL_MS" envDefault:"200"`
}

// Load reads configuration from environment variables.
func Load() (*Config, error) {
	var cfg Config
	if err := env.Parse(&cfg); err != nil {
		return nil, fmt.Errorf("ticketing config: %w", err)
	}
	return &cfg, nil
}

// Validate checks cross-field constraints.
func (c *Config) Validate() error {
	if c.ReservationTTL <= 0 {
		return fmt.Errorf("RESERVATION_TTL must be > 0, got %d", c.ReservationTTL)
	}
	if c.PaymentTimeoutSec <= 0 {
		return fmt.Errorf("PAYMENT_TIMEOUT_SEC must be > 0, got %d", c.PaymentTimeoutSec)
	}
	return nil
}
