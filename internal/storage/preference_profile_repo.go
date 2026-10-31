package storage

import (
	"context"
	"errors"

	"github.com/felipeafreitas/agregado/internal/preferences"
	"github.com/jackc/pgx/v5"
)

// PreferenceProfileRepo keeps every accepted version of PREFERENCES.md; the
// newest row is the profile in force.
type PreferenceProfileRepo struct{ db *DB }

func NewPreferenceProfileRepo(db *DB) *PreferenceProfileRepo { return &PreferenceProfileRepo{db: db} }

func (r *PreferenceProfileRepo) Current(ctx context.Context) (string, error) {
	var content string
	err := r.db.pool.QueryRow(ctx, `SELECT content FROM preference_profiles ORDER BY id DESC LIMIT 1`).Scan(&content)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", preferences.ErrNoProfile
	}
	return content, err
}

// Save inserts text unless it equals the newest version, so a nightly sync of
// an unedited Doc adds no row.
func (r *PreferenceProfileRepo) Save(ctx context.Context, text, source string) (bool, error) {
	tag, err := r.db.pool.Exec(ctx, `
		INSERT INTO preference_profiles (content, source)
		SELECT $1, $2
		WHERE $1 IS DISTINCT FROM (SELECT content FROM preference_profiles ORDER BY id DESC LIMIT 1)`, text, source)
	if err != nil {
		return false, err
	}
	return tag.RowsAffected() == 1, nil
}
