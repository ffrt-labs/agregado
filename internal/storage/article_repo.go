package storage

import (
	"context"

	"github.com/felipeafreitas/agregado/internal/domain"
	"github.com/jackc/pgx/v5"
)

type ArticleRepo struct {
	db *DB
}

func NewArticleRepo(db *DB) *ArticleRepo {
	return &ArticleRepo{
		db: db,
	}
}

func (r *ArticleRepo) GetById(ctx context.Context, id string) (*domain.Article, error) {
	row, err := r.db.pool.Query(ctx, "SELECT * FROM articles where id=$1", id)

	if err != nil {
		return nil, err
	}

	article, err := pgx.CollectOneRow(row, pgx.RowToStructByName[domain.Article])

	if err != nil {
		return nil, err
	}

	return &article, nil
}

func (r *ArticleRepo) List(ctx context.Context) ([]domain.Article, error) {
	rows, err := r.db.pool.Query(ctx, "SELECT * FROM articles")

	if err != nil {
		return nil, err
	}

	articles, err := pgx.CollectRows(rows, pgx.RowToStructByName[domain.Article])

	if err != nil {
		return nil, err
	}

	return articles, nil
}

func (r *ArticleRepo) Create(ctx context.Context, article domain.Article) error {
	_, err := r.db.pool.Exec(
		ctx,
		"INSERT INTO articles(source_id, external_url, title, author, summary, content, content_hash, published_at, word_count, estimated_read_minutes) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10) ON CONFLICT (external_url) DO NOTHING",
		article.SourceID,
		article.ExternalURL,
		article.Title,
		article.Author,
		article.Summary,
		article.Content,
		article.ContentHash,
		article.PublishedAt,
		article.WordCount,
		article.EstimatedReadMinutes,
	)

	return err
}

func (r *ArticleRepo) Delete(ctx context.Context, id string) error {
	_, err := r.db.pool.Exec(
		ctx,
		"DELETE FROM articles WHERE id=$1",
		id,
	)

	return err
}

func (r *ArticleRepo) MarkRead(ctx context.Context, id string) error {
	_, err := r.db.pool.Exec(
		ctx,
		"UPDATE articles SET is_read = $2, read_at = NOW() WHERE id=$1",
		id,
		true,
	)

	return err
}
