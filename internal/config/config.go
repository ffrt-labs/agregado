package config

import (
	"time"

	"github.com/caarlos0/env/v10"
)

type Database struct {
	Host     string `env:"DATABASE_HOST" envDefault:"localhost"`
	Port     string `env:"DATABASE_PORT" envDefault:"5432"`
	User     string `env:"DATABASE_USER,required"`
	Password string `env:"DATABASE_PASSWORD,required"`
	Name     string `env:"DATABASE_DB,required"`
}

type Queue struct {
	User     string `env:"RABBITMQ_USER,required"`
	Password string `env:"RABBITMQ_PASS,required"`
	Host     string `env:"RABBITMQ_HOST" envDefault:"localhost"`
	Port     string `env:"RABBITMQ_PORT" envDefault:"5672"`
}

type Http struct {
	Port string `env:"HTTP_PORT" envDefault:"8080"`
}

type Pooler struct {
	Interval time.Duration `env:"RSS_POLL_INTERVAL" envDefault:"15m"`
}

type Webhook struct {
	Secret string `env:"WEBHOOK_SECRET" envDefault:"dev-secret-change-in-production"`
}

type Digest struct {
	RecipientEmail 	string	`env:"DIGEST_RECIPIENT_EMAIL" envDefault:""`
	Schedule 		string	`env:"DIGEST_SCHEDULE" envDefault:"0 8 * * *"`
	MaxArticles 	int		`env:"DIGEST_MAX_ARTICLES" envDefault:"20"`
	LookbackHours 	int		`env:"DIGEST_LOOKBACK_HOURS" envDefault:"24"`
}

type SMTP struct {
	Host		string	`env:"SMTP_HOST" envDefault:"smtp.gmail.com"`
	Port		int		`env:"SMTP_PORT" envDefault:"587"`
	Username	string	`env:"SMTP_USERNAME" envDefault:""`
	Password	string	`env:"SMTP_PASSWORD" envDefault:""`
	FromName	string	`env:"SMTP_FROM_NAME" envDefault:"Agregado Digest"`
	FromMail	string	`env:"SMTP_FROM_MAIL" envDefault:""`
}

type AI struct {
	Provider			string	`env:"AI_PROVIDER" envDefault:"cloudflare"`
	CloudflareAccountID	string	`env:"CLOUDFLARE_ACCOUNT_ID"`
	CloudflareAPIToken	string	`env:"CLOUDFLARE_API_TOKEN"`
	Model				string	`env:"AI_MODEL" envDefault:"@cf/google/gemma-4-26b-a4b-it"`
}

type Config struct {
	Database
	Queue
	Http
	Pooler
	Webhook
	Digest
	SMTP
	AI
}

func Load() (*Config, error) {
	cfg := &Config{}
	if err := env.Parse(cfg); err != nil {
		return nil, err
	}

	return cfg, nil
}
