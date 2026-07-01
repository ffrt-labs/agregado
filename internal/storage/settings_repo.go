package storage

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
)

const settingAILoggingEnabled = "ai_logging_enabled"

type SettingsRepo struct {
	db *DB
}

func NewSettingsRepo(db *DB) *SettingsRepo {
	return &SettingsRepo{db: db}
}

// Get returns a setting value, or "" if the key is absent.
func (r *SettingsRepo) Get(ctx context.Context, key string) (string, error) {
	var value string
	err := r.db.pool.QueryRow(ctx, "SELECT value FROM admin_settings WHERE key = $1", key).Scan(&value)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	return value, nil
}

// Set upserts a setting.
func (r *SettingsRepo) Set(ctx context.Context, key, value string) error {
	_, err := r.db.pool.Exec(
		ctx,
		`INSERT INTO admin_settings (key, value) VALUES ($1, $2)
		 ON CONFLICT (key) DO UPDATE SET value = $2, updated_at = NOW()`,
		key,
		value,
	)
	return err
}

func (r *SettingsRepo) AILoggingEnabled(ctx context.Context) (bool, error) {
	v, err := r.Get(ctx, settingAILoggingEnabled)
	if err != nil {
		return false, err
	}
	return v == "true", nil
}

func (r *SettingsRepo) SetAILoggingEnabled(ctx context.Context, enabled bool) error {
	value := "false"
	if enabled {
		value = "true"
	}
	return r.Set(ctx, settingAILoggingEnabled, value)
}
