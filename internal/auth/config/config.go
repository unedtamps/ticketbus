package config

import (
	"fmt"

	"github.com/caarlos0/env/v11"
)

// Config holds all configuration for the auth service.
type Config struct {
	AppEnv        string `env:"APP_ENV"         envDefault:"development"`
	Port          string `env:"PORT"             envDefault:"8081"`
	DatabaseURL   string `env:"DATABASE_URL"     envDefault:"postgres://ticketsaas:ticketsaas@localhost:5432/auth_db?sslmode=disable"`
	JWTPrivateKey string `env:"JWT_PRIVATE_KEY"`
	JWTPublicKey  string `env:"JWT_PUBLIC_KEY"`
	KafkaBrokers  string `env:"KAFKA_BROKERS"    envDefault:"localhost:9092"`
	AdminEmail    string `env:"ADMIN_EMAIL"`
	AdminPassword string `env:"ADMIN_PASSWORD"`
}

// Load reads configuration from environment variables.
func Load() (*Config, error) {
	var cfg Config
	if err := env.Parse(&cfg); err != nil {
		return nil, fmt.Errorf("auth config: %w", err)
	}
	return &cfg, nil
}
