package config

import (
	"fmt"

	"github.com/caarlos0/env/v11"
)

// Config holds all configuration for the auth service.
type Config struct {
	AppEnv         string   `env:"APP_ENV,required,notEmpty"`
	Port           string   `env:"PORT,required,notEmpty"`
	DatabaseURL    string   `env:"DATABASE_URL,required,notEmpty"`
	JWTPrivateKey  string   `env:"JWT_PRIVATE_KEY,required,notEmpty"`
	JWTPublicKey   string   `env:"JWT_PUBLIC_KEY,required,notEmpty"`
	AdminEmails    []string `env:"ADMIN_EMAILS"`
	AdminPasswords []string `env:"ADMIN_PASSWORDS"`
	SeedAdmin      bool     `env:"SEED_ADMIN"                        envDefault:"false"`
}

// Load reads configuration from environment variables.
func Load() (*Config, error) {
	var cfg Config
	if err := env.Parse(&cfg); err != nil {
		return nil, fmt.Errorf("auth config: %w", err)
	}
	return &cfg, nil
}
