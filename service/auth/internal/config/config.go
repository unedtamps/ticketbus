package config

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/caarlos0/env/v11"
	"github.com/nedo/TicketSaas/service/auth/internal/application"
)

// Config holds all configuration for the auth service.
type Config struct {
	AppEnv        string `env:"APP_ENV,required,notEmpty"`
	Port          string `env:"PORT,required,notEmpty"`
	DatabaseURL   string `env:"DATABASE_URL,required,notEmpty"`
	JWTPrivateKey string `env:"JWT_PRIVATE_KEY,required,notEmpty"`
	JWTPublicKey  string `env:"JWT_PUBLIC_KEY,required,notEmpty"`

	// AdminSeedsRaw is a JSON array of admin accounts to auto-seed on every
	// startup (idempotent). Leave empty to skip seeding.
	AdminSeedsRaw string `env:"ADMIN_SEEDS"`
}

// Load reads configuration from environment variables.
func Load() (*Config, error) {
	var cfg Config
	if err := env.Parse(&cfg); err != nil {
		return nil, fmt.Errorf("auth config: %w", err)
	}
	return &cfg, nil
}

// AdminSeeds parses the ADMIN_SEEDS JSON into admin seed entries.
func (c *Config) AdminSeeds() ([]application.AdminSeed, error) {
	if strings.TrimSpace(c.AdminSeedsRaw) == "" {
		return nil, nil
	}
	var seeds []application.AdminSeed
	if err := json.Unmarshal([]byte(c.AdminSeedsRaw), &seeds); err != nil {
		return nil, fmt.Errorf("ADMIN_SEEDS must be a valid JSON array: %w", err)
	}
	return seeds, nil
}

// Validate checks cross-field constraints.
func (c *Config) Validate() error {
	seeds, err := c.AdminSeeds()
	if err != nil {
		return err
	}
	for i, seed := range seeds {
		if seed.Email == "" || seed.Password == "" || seed.Name == "" {
			return fmt.Errorf("ADMIN_SEEDS[%d] requires email, password and name", i)
		}
	}
	return nil
}
