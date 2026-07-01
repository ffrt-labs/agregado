package storage

import (
	"context"
	"errors"

	"github.com/felipeafreitas/agregado/internal/domain"
	"github.com/jackc/pgx/v5"
)

type PromptRepo struct {
	db *DB
}

func NewPromptRepo(db *DB) *PromptRepo {
	return &PromptRepo{db: db}
}

// SystemPrompt returns the editable system prompt for an operation, or "" if no
// row exists (the caller falls back to its in-code default). Satisfies
// ai.PromptStore.
func (r *PromptRepo) SystemPrompt(ctx context.Context, operation string) (string, error) {
	var prompt string
	err := r.db.pool.QueryRow(ctx, "SELECT system_prompt FROM ai_prompts WHERE operation = $1", operation).Scan(&prompt)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	return prompt, nil
}

func (r *PromptRepo) List(ctx context.Context) ([]domain.AIPrompt, error) {
	rows, err := r.db.pool.Query(ctx, "SELECT * FROM ai_prompts ORDER BY operation")
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowToStructByName[domain.AIPrompt])
}

// Update upserts a system prompt so a missing row (e.g. on reset) is created.
func (r *PromptRepo) Update(ctx context.Context, operation, systemPrompt string) error {
	_, err := r.db.pool.Exec(
		ctx,
		`INSERT INTO ai_prompts (operation, system_prompt) VALUES ($1, $2)
		 ON CONFLICT (operation) DO UPDATE SET system_prompt = $2, updated_at = NOW()`,
		operation,
		systemPrompt,
	)
	return err
}
