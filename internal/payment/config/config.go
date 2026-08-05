package config

import (
	"fmt"
	"strings"

	"github.com/caarlos0/env/v11"
)

// Config holds all configuration for the payment service.
type Config struct {
	AppEnv                 string `env:"APP_ENV" envDefault:"development"`
	Port                   string `env:"PORT" envDefault:"8084"`
	DatabaseURL            string `env:"DATABASE_URL" envDefault:"postgres://ticketsaas:ticketsaas@localhost:5435/payment_db?sslmode=disable"`
	KafkaBrokers           string `env:"KAFKA_BROKERS" envDefault:"localhost:9092"`
	WebhookBaseURL         string `env:"WEBHOOK_BASE_URL"       envDefault:"http://localhost:8000/api/payments/webhook"`
	ConsumerConcurrency    int    `env:"CONSUMER_CONCURRENCY"   envDefault:"4"`
	OutboxConcurrency      int    `env:"OUTBOX_CONCURRENCY"     envDefault:"4"`
	OutboxPollMs           int    `env:"OUTBOX_POLL_MS"         envDefault:"200"`

	// Provider: "mock" (default, for tests) or "xendit".
	Provider string `env:"PROVIDER" envDefault:"mock"`

	// Xendit configuration.
	XenditAPIKey      string `env:"XENDIT_API_KEY"      envDefault:""`
	XenditBaseURL     string `env:"XENDIT_BASE_URL"     envDefault:"https://api.xendit.co/v1"`
	XenditCallbackTok string `env:"XENDIT_CALLBACK_TOKEN" envDefault:""`

	// Internal service-to-service auth.
	InternalAPIKey string `env:"INTERNAL_API_KEY" envDefault:"dev-internal-key"`

	// Expiry: the gateway payment session expires this many minutes before
	// the booking deadline so its webhook lands before our own poll.
	GatewayExpiryBufferMin int `env:"GATEWAY_EXPIRY_BUFFER_MIN" envDefault:"5"`
	ExpiryPollSec          int `env:"EXPIRY_POLL_SEC"          envDefault:"30"`

	// Enabled payment channels shown on the Xendit hosted checkout page
	// (comma-separated Xendit channel codes).
	PaymentMethods string `env:"PAYMENT_METHODS" envDefault:"ID_QRIS"`
}

// EnabledMethods returns the parsed payment channel list.
func (c *Config) EnabledMethods() []string {
	parts := strings.Split(c.PaymentMethods, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

// Load reads configuration from environment variables.
func Load() (*Config, error) {
	var cfg Config
	if err := env.Parse(&cfg); err != nil {
		return nil, fmt.Errorf("payment config: %w", err)
	}
	return &cfg, nil
}
