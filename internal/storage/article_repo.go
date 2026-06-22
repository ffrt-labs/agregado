package storage

import (
	"context"
	"errors"
	"time"

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

func (r *ArticleRepo) List(ctx context.Context, limit, offset int) ([]domain.Article, error) {
	rows, err := r.db.pool.Query(ctx, "SELECT * FROM articles LIMIT $1 OFFSET $2", limit, offset)

	if err != nil {
		return nil, err
	}

	articles, err := pgx.CollectRows(rows, pgx.RowToStructByName[domain.Article])

	if err != nil {
		return nil, err
	}

	return articles, nil
}

func (r *ArticleRepo) ListBySource(ctx context.Context, id string, limit, offset int) ([]domain.Article, error) {
	rows, err := r.db.pool.Query(ctx, "SELECT * FROM articles WHERE source_id = $1 LIMIT $2 OFFSET $3", id, limit, offset)

	if err != nil {
		return nil, err
	}

	articles, err := pgx.CollectRows(rows, pgx.RowToStructByName[domain.Article])

	if err != nil {
		return nil, err
	}

	return articles, nil
}

func (r *ArticleRepo) Create(ctx context.Context, article domain.Article) (string, error) {
	row := r.db.pool.QueryRow(
		ctx,
		"INSERT INTO articles(source_id, external_url, title, author, summary, content, content_hash, published_at, word_count, estimated_read_minutes) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10) ON CONFLICT (external_url) DO NOTHING RETURNING id",
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

	var id string
	err := row.Scan(&id)

	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return "", nil
		}
		return "", err
	}

	return id, nil
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

func (r *ArticleRepo) MarkUnread(ctx context.Context, id string) error {
	_, err := r.db.pool.Exec(
		ctx,
		"UPDATE articles SET is_read = $2, read_at = NULL WHERE id=$1",
		id,
		false,
	)

	return err
}

func (r *ArticleRepo) FindUnreadSince(ctx context.Context, since time.Time, minScore, limit int) ([]domain.Article, error) {
	rows, err := r.db.pool.Query(
		ctx,
		`
		SELECT * FROM articles
		WHERE ingested_at > $1
			AND is_read = false
			AND (relevance_score >= $2 OR relevance_score IS NULL)
		ORDER BY relevance_score DESC NULLS LAST, published_at DESC
		LIMIT $3
		`,
		since,
		minScore,
		limit,
	)

	if err != nil {
		return nil, err
	}

	articles, err := pgx.CollectRows(rows, pgx.RowToStructByName[domain.Article])

	if err != nil {
		return nil, err
	}

	ids := make([]string, len(articles))
	for i, a := range articles { ids[i] = a.ID}

	rows, err = r.db.pool.Query(ctx, `
		SELECT article_id, tags.id, tags.name, tags.slug, tags.color,
  		tags.created_at, tags.updated_at FROM article_tags JOIN tags ON
    	article_tags.tag_id = tags.ID  WHERE article_id = ANY($1)
    `, ids)

	if err != nil {
		return nil, err
	}

	articleTags, err := pgx.CollectRows(rows, pgx.RowToStructByName[domain.ArticleTag])

	if err != nil {
		return nil, err
	}

	articleMap := make(map[string][]domain.Tag)
	for _, a := range articleTags {
		articleMap[a.ArticleID] = append(articleMap[a.ArticleID], domain.Tag{
			ID: a.ID,
			Name: a.Name,
			Slug: a.Slug,
			Color: a.Color,
			CreatedAt: a.CreatedAt,
			UpdatedAt: a.UpdatedAt,
		})
	}

	for i := range articles {
		articles[i].Tags = articleMap[articles[i].ID]
	}

	return articles, nil
}

func (r *ArticleRepo) Search(ctx context.Context, query string, limit, offset int) ([]domain.Article, error) {
	rows, err := r.db.pool.Query(ctx,
		"SELECT * FROM articles WHERE to_tsvector('english', title || ' ' || coalesce(summary, '')) @@ plainto_tsquery('english', $1) LIMIT $2 OFFSET $3",
		query,
		limit,
		offset,
	)

	if err != nil {
		return nil, err
	}

	articles, err := pgx.CollectRows(rows, pgx.RowToStructByName[domain.Article])

	if err != nil {
		return nil, err
	}

	return articles, nil
}

func (r *ArticleRepo) UpdateRelevanceScore(ctx context.Context, id string, score int) error {
	_, err := r.db.pool.Exec(
		ctx,
		"UPDATE articles SET relevance_score = $2 WHERE id = $1",
		id,
		score,
	)

	return err
}
