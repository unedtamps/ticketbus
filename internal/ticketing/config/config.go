package config

import (
	"fmt"

	"github.com/caarlos0/env/v11"
)

// Config holds the combined Ticketing service configuration.
type Config struct {
	AppEnv              string `env:"APP_ENV" envDefault:"development"`
	Port                string `env:"PORT" envDefault:"8082"`
	DatabaseURL         string `env:"DATABASE_URL" envDefault:"postgres://ticketsaas:ticketsaas@localhost:5433/ticketing_db?sslmode=disable"`
	RedisAddr           string `env:"REDIS_ADDR" envDefault:"localhost:6379"`
	KafkaBrokers        string `env:"KAFKA_BROKERS" envDefault:"localhost:9092"`
	ReservationTTL      int    `env:"RESERVATION_TTL" envDefault:"900"`
	ReservationSweepSec int    `env:"RESERVATION_SWEEP_INTERVAL_SEC" envDefault:"60"`
	ConsumerConcurrency int    `env:"CONSUMER_CONCURRENCY" envDefault:"4"`
	OutboxConcurrency   int    `env:"OUTBOX_CONCURRENCY" envDefault:"4"`
	OutboxPollMs        int    `env:"OUTBOX_POLL_MS" envDefault:"200"`
}

// Load reads configuration from environment variables.
func Load() (*Config, error) {
	var cfg Config
	if err := env.Parse(&cfg); err != nil {
		return nil, fmt.Errorf("ticketing config: %w", err)
	}
	return &cfg, nil
}
