package config

import (
	"fmt"
	"strings"

	"github.com/caarlos0/env/v11"
)

// Config holds all configuration for the payment service.
type Config struct {
	AppEnv                 string `env:"APP_ENV,required,notEmpty"`
	Port                   string `env:"PORT,required,notEmpty"`
	DatabaseURL            string `env:"DATABASE_URL,required,notEmpty"`
	KafkaBrokers           string `env:"KAFKA_BROKERS,required,notEmpty"`
	WebhookBaseURL         string `env:"WEBHOOK_BASE_URL,required,notEmpty"`
	ConsumerConcurrency    int    `env:"CONSUMER_CONCURRENCY,required,notEmpty"`
	OutboxConcurrency      int    `env:"OUTBOX_CONCURRENCY,required,notEmpty"`
	OutboxPollMs           int    `env:"OUTBOX_POLL_MS,required,notEmpty"`

	// Provider: "mock" (dev/tests) or "xendit" (sandbox/live).
	Provider string `env:"PROVIDER,required,notEmpty"`

	// Xendit configuration (required when PROVIDER=xendit).
	XenditAPIKey  string `env:"XENDIT_API_KEY"`
	XenditBaseURL string `env:"XENDIT_BASE_URL,required,notEmpty"`

	// Shared webhook verification token (both providers). Must match the
	// token the gateway includes as the x-callback-token header.
	WebhookCallbackTok string `env:"WEBHOOK_CALLBACK_TOKEN,required,notEmpty"`

	// Internal service-to-service auth (must match the ticketing service).
	InternalAPIKey string `env:"INTERNAL_API_KEY,required,notEmpty"`

	// Expiry: the gateway payment session expires this many minutes before
	// the booking deadline so its webhook lands before our own poll.
	GatewayExpiryBufferMin int `env:"GATEWAY_EXPIRY_BUFFER_MIN,required,notEmpty"`
	ExpiryPollSec          int `env:"EXPIRY_POLL_SEC,required,notEmpty"`

	// Optional payment channel list (comma-separated Xendit channel codes).
	// Not sent to Xendit anymore — the gateway enables all channels activated
	// for the account. Kept for the mock provider / future dashboard control.
	PaymentMethods string `env:"PAYMENT_METHODS"`
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

// Validate checks cross-field constraints.
func (c *Config) Validate() error {
	switch c.Provider {
	case "mock", "xendit":
	default:
		return fmt.Errorf("PROVIDER must be 'mock' or 'xendit', got %q", c.Provider)
	}
	if c.Provider == "xendit" {
		if c.XenditAPIKey == "" {
			return fmt.Errorf("XENDIT_API_KEY is required when PROVIDER=xendit")
		}
	}
	if c.GatewayExpiryBufferMin < 0 {
		return fmt.Errorf("GATEWAY_EXPIRY_BUFFER_MIN must be >= 0, got %d", c.GatewayExpiryBufferMin)
	}
	if c.ExpiryPollSec <= 0 {
		return fmt.Errorf("EXPIRY_POLL_SEC must be > 0, got %d", c.ExpiryPollSec)
	}
	return nil
}
