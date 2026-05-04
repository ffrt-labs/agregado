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
