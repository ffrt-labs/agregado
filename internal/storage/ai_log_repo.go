package storage

import (
	"context"

	"github.com/felipeafreitas/agregado/internal/ai"
	"github.com/felipeafreitas/agregado/internal/domain"
	"github.com/jackc/pgx/v5"
)

type AILogRepo struct {
	db *DB
}

func NewAILogRepo(db *DB) *AILogRepo {
	return &AILogRepo{db: db}
}

func (r *AILogRepo) Insert(ctx context.Context, e ai.LogEntry) error {
	_, err := r.db.pool.Exec(
		ctx,
		`INSERT INTO ai_logs (operation, model, system_prompt, user_prompt, response, success, error, duration_ms)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`,
		e.Operation,
		nullIfEmpty(e.Model),
		nullIfEmpty(e.SystemPrompt),
		nullIfEmpty(e.UserPrompt),
		nullIfEmpty(e.Response),
		e.Success,
		nullIfEmpty(e.Err),
		e.DurationMs,
	)
	return err
}

// List returns logs newest-first, optionally filtered by operation ("" = all).
func (r *AILogRepo) List(ctx context.Context, limit, offset int, operation string) ([]domain.AILog, error) {
	var rows pgx.Rows
	var err error
	if operation == "" {
		rows, err = r.db.pool.Query(
			ctx,
			"SELECT * FROM ai_logs ORDER BY created_at DESC LIMIT $1 OFFSET $2",
			limit, offset,
		)
	} else {
		rows, err = r.db.pool.Query(
			ctx,
			"SELECT * FROM ai_logs WHERE operation = $1 ORDER BY created_at DESC LIMIT $2 OFFSET $3",
			operation, limit, offset,
		)
	}
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowToStructByName[domain.AILog])
}

func (r *AILogRepo) Clear(ctx context.Context) error {
	_, err := r.db.pool.Exec(ctx, "DELETE FROM ai_logs")
	return err
}

// nullIfEmpty stores empty strings as SQL NULL to keep the log table clean.
func nullIfEmpty(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}
