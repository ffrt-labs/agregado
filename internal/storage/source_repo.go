package storage

import (
	"context"

	"github.com/felipeafreitas/agregado/internal/domain"
	"github.com/jackc/pgx/v5"
)

type SourceRepo struct {
	db *DB
}

func NewSourceRepo(db *DB) *SourceRepo {
	return &SourceRepo{
		db: db,
	}
}

func (r *SourceRepo) List(ctx context.Context) ([]domain.Source, error) {
	rows, err := r.db.pool.Query(ctx, "SELECT * FROM sources")

	if err != nil {
		return nil, err
	}

	sources, err := pgx.CollectRows(rows, pgx.RowToStructByName[domain.Source])

	if err != nil {
		return nil, err
	}

	return sources, nil
}

func (r *SourceRepo) ListActive(ctx context.Context) ([]domain.Source, error) {
	rows, err := r.db.pool.Query(ctx, "SELECT * FROM sources WHERE is_active = true")

	if err != nil {
		return nil, err
	}

	sources, err := pgx.CollectRows(rows, pgx.RowToStructByName[domain.Source])

	if err != nil {
		return nil, err
	}

	return sources, nil
}

func (r *SourceRepo) Create(ctx context.Context, source domain.Source) (*domain.Source, error) {
	row, err := r.db.pool.Query(
		ctx,
		"INSERT INTO sources(name, type, url, email_sender, priority, is_active) VALUES ($1, $2, $3, $4, $5, $6) RETURNING *",
		source.Name,
		source.Type,
		source.URL,
		source.EmailSender,
		source.Priority,
		source.IsActive,
	)

	if err != nil {
		return nil, err
	}

	source, err = pgx.CollectOneRow(row, pgx.RowToStructByName[domain.Source])

	if err != nil {
		return nil, err
	}

	return &source, nil
}

func (r *SourceRepo) Delete(ctx context.Context, id string) error {
	_, err := r.db.pool.Exec(
		ctx,
		"DELETE FROM sources WHERE id=$1",
		id,
	)

	return err
}

func (r *SourceRepo) Update(ctx context.Context, source domain.Source) error {
	_, err := r.db.pool.Exec(
		ctx,
		"UPDATE sources SET name = $2, type = $3, url = $4, email_sender = $5, priority = $6, is_active = $7, last_fetched_at = $8, last_error = $9, error_count = $10, default_tag_id = $11, updated_at = NOW() WHERE id=$1",
		source.ID,
		source.Name,
		source.Type,
		source.URL,
		source.EmailSender,
		source.Priority,
		source.IsActive,
		source.LastFetchedAt,
		source.LastError,
		source.ErrorCount,
		source.DefaultTagID,
	)

	return err
}

func (r *SourceRepo) FindByEmailSender(ctx context.Context, email string) (*domain.Source, error) {
	rows, err := r.db.pool.Query(ctx, "SELECT * FROM sources WHERE email_sender=$1", email)
	if err != nil {
		return nil, err
	}

	source, err := pgx.CollectOneRow(rows, pgx.RowToStructByName[domain.Source])
	if err != nil {
		return nil, err
	}

	return &source, nil
}
