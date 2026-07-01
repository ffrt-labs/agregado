package storage

import (
	"context"

	"github.com/felipeafreitas/agregado/internal/domain"
	"github.com/jackc/pgx/v5"
)

type TagRepo struct {
	db *DB
}

func NewTagRepo(db *DB) *TagRepo {
	return &TagRepo{
		db: db,
	}
}

func (r *TagRepo) FindAll(ctx context.Context) ([]domain.Tag, error) {
	rows, err := r.db.pool.Query(ctx, "SELECT * FROM tags")

	if err != nil {
		return nil, err
	}

	tags, err := pgx.CollectRows(rows, pgx.RowToStructByName[domain.Tag])

	if err != nil {
		return nil, err
	}

	return tags, nil
}

// CategorySlugs returns the tag slugs, used to build the live categorize prompt.
// Satisfies ai.TagLister.
func (r *TagRepo) CategorySlugs(ctx context.Context) ([]string, error) {
	rows, err := r.db.pool.Query(ctx, "SELECT slug FROM tags ORDER BY slug")
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowTo[string])
}

func (r *TagRepo) FindByID(ctx context.Context, id string) (*domain.Tag, error) {
	rows, err := r.db.pool.Query(ctx, "SELECT * FROM tags WHERE id = $1", id)
	if err != nil {
		return nil, err
	}
	tag, err := pgx.CollectOneRow(rows, pgx.RowToStructByName[domain.Tag])
	if err != nil {
		return nil, err
	}
	return &tag, nil
}

func (r *TagRepo) Create(ctx context.Context, name, slug, color string) error {
	_, err := r.db.pool.Exec(
		ctx,
		"INSERT INTO tags (name, slug, color) VALUES ($1, $2, $3)",
		name, slug, color,
	)
	return err
}

func (r *TagRepo) Update(ctx context.Context, id, name, slug, color string) error {
	_, err := r.db.pool.Exec(
		ctx,
		"UPDATE tags SET name = $2, slug = $3, color = $4, updated_at = NOW() WHERE id = $1",
		id, name, slug, color,
	)
	return err
}

func (r *TagRepo) Delete(ctx context.Context, id string) error {
	_, err := r.db.pool.Exec(ctx, "DELETE FROM tags WHERE id = $1", id)
	return err
}
