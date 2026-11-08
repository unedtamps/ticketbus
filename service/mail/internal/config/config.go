package config

import (
	"fmt"

	"github.com/caarlos0/env/v11"
)

// Config holds all configuration for the mail service.
type Config struct {
	AppEnv              string `env:"APP_ENV,required,notEmpty"`
	Port                string `env:"PORT,required,notEmpty"`
	KafkaBrokers        string `env:"KAFKA_BROKERS,required,notEmpty"`
	ConsumerConcurrency int    `env:"CONSUMER_CONCURRENCY,required,notEmpty"`

	// SMTP delivery. MailDev in dev accepts any credentials, so none are
	// configured; a real server would add SMTP_USER/SMTP_PASS later.
	SMTPHost string `env:"SMTP_HOST,required,notEmpty"`
	SMTPPort string `env:"SMTP_PORT,required,notEmpty"`
	SMTPFrom string `env:"SMTP_FROM,required,notEmpty"`

	// WebBaseURL is the frontend origin, used for links inside emails (e.g.
	// the cancellation email's "set your refund account" call to action).
	WebBaseURL string `env:"WEB_BASE_URL,required,notEmpty"`
}

// Load reads configuration from environment variables.
func Load() (*Config, error) {
	var cfg Config
	if err := env.Parse(&cfg); err != nil {
		return nil, fmt.Errorf("mail config: %w", err)
	}
	return &cfg, nil
}
