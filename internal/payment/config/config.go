package config

import (
	"fmt"

	"github.com/caarlos0/env/v11"
)

// Config holds all configuration for the payment service.
type Config struct {
	AppEnv         string `env:"APP_ENV" envDefault:"development"`
	Port           string `env:"PORT" envDefault:"8084"`
	DatabaseURL    string `env:"DATABASE_URL" envDefault:"postgres://ticketsaas:ticketsaas@localhost:5435/payment_db?sslmode=disable"`
	KafkaBrokers   string `env:"KAFKA_BROKERS" envDefault:"localhost:9092"`
	WebhookBaseURL string `env:"WEBHOOK_BASE_URL" envDefault:"http://localhost:8000/api/payments/webhook"`
}

// Load reads configuration from environment variables.
func Load() (*Config, error) {
	var cfg Config
	if err := env.Parse(&cfg); err != nil {
		return nil, fmt.Errorf("payment config: %w", err)
	}
	return &cfg, nil
}
