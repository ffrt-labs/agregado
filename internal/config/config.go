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
	RecipientEmail 		string	`env:"DIGEST_RECIPIENT_EMAIL" envDefault:""`
	Schedule 			string	`env:"DIGEST_SCHEDULE" envDefault:"0 8 * * *"`
	MaxArticles 		int		`env:"DIGEST_MAX_ARTICLES" envDefault:"20"`
	LookbackHours 		int		`env:"DIGEST_LOOKBACK_HOURS" envDefault:"24"`
	MinRelevanceScore 	int 	`env:"DIGEST_MIN_SCORE" envDefault:"3"`
	// BaseURL is the public origin (e.g. https://agregado.example.com) used to
	// build absolute links in the digest email — relative links don't resolve
	// inside a mail client.
	BaseURL 			string	`env:"PUBLIC_BASE_URL" envDefault:"http://localhost:8080"`
}

type SMTP struct {
	Host		string	`env:"SMTP_HOST" envDefault:"smtp.gmail.com"`
	Port		int		`env:"SMTP_PORT" envDefault:"587"`
	Username	string	`env:"SMTP_USERNAME" envDefault:""`
	Password	string	`env:"SMTP_PASSWORD" envDefault:""`
	FromName	string	`env:"SMTP_FROM_NAME" envDefault:"Agregado Digest"`
	FromMail	string	`env:"SMTP_FROM_MAIL" envDefault:""`
}

type Backup struct {
	RecipientEmail	string	`env:"BACKUP_RECIPIENT_EMAIL" envDefault:""`
	// Schedule defaults to weekly (Sun 03:00) — sources change rarely, unlike
	// the daily digest content.
	Schedule		string	`env:"BACKUP_SCHEDULE" envDefault:"0 3 * * 0"`
}

type AI struct {
	Provider			string	`env:"AI_PROVIDER" envDefault:"cloudflare"`
	CloudflareAccountID	string	`env:"CLOUDFLARE_ACCOUNT_ID"`
	CloudflareAPIToken	string	`env:"CLOUDFLARE_API_TOKEN"`
	Model				string	`env:"AI_MODEL" envDefault:"@cf/google/gemma-4-26b-a4b-it"`
	// RequestTimeout bounds a single AI call. Digest compute makes several
	// calls back to back under one overall budget, so this caps how much of
	// that budget one slow call can consume.
	RequestTimeout		time.Duration	`env:"AI_REQUEST_TIMEOUT" envDefault:"30s"`
	// MaxContentChars caps how much article body Score/Categorize/Reason feed
	// the model. 8000 is a rough per-call budget, not a model-specific window.
	MaxContentChars		int	`env:"AI_MAX_CONTENT_CHARS" envDefault:"8000"`
}

type Fetch struct {
	// Timeout bounds a single article-page fetch.
	Timeout			time.Duration	`env:"FETCH_TIMEOUT" envDefault:"15s"`
	// MaxBytes caps how much of a response body is read, regardless of
	// Content-Length — a defensive limit against oversized/misbehaving pages.
	MaxBytes		int64	`env:"FETCH_MAX_BYTES" envDefault:"5242880"`
	// MinContentChars is the quality-gate floor: extracted plain text shorter
	// than this is treated as a failed fetch (consent wall, SPA shell,
	// paywall) and the article falls back to feed content.
	MinContentChars	int	`env:"FETCH_MIN_CONTENT_CHARS" envDefault:"500"`
	UserAgent		string	`env:"FETCH_USER_AGENT" envDefault:"Agregado/1.0 (+https://github.com/felipeafreitas/agregado)"`
	// DistillMaxChars caps the algorithmic extractive pass (internal/textutil.Distill)
	// that produces articles.distilled_content.
	DistillMaxChars	int	`env:"DISTILL_MAX_CHARS" envDefault:"2000"`
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
	Backup
	Fetch
}

func Load() (*Config, error) {
	cfg := &Config{}
	if err := env.Parse(cfg); err != nil {
		return nil, err
	}

	return cfg, nil
}
