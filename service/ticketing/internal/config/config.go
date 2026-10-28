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

	// Object storage (S3-compatible; Cloudflare R2 in production). Event
	// photos are uploaded server-side and served through the bucket's public
	// base URL (R2.dev subdomain or a custom domain).
	S3Endpoint  string `env:"S3_ENDPOINT,required,notEmpty"`
	S3Secure    bool   `env:"S3_SECURE" envDefault:"true"`
	S3Region    string `env:"S3_REGION" envDefault:"auto"`
	S3AccessKey string `env:"S3_ACCESS_KEY_ID,required,notEmpty"`
	S3SecretKey string `env:"S3_SECRET_ACCESS_KEY,required,notEmpty"`
	S3Bucket    string `env:"S3_BUCKET_NAME,required,notEmpty"`
	S3PublicURL string `env:"S3_PUBLIC_URL,required,notEmpty"`
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
