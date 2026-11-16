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
