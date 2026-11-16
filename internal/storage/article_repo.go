package storage

import (
	"context"
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

	tagMap, err := r.loadTags(ctx, []string{article.ID})
	if err != nil {
		return nil, err
	}
	article.Tags = tagMap[article.ID]

	return &article, nil
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

func (r *ArticleRepo) FindUnreadSince(ctx context.Context, since time.Time, minScore, limit int) ([]domain.Article, error) {
	rows, err := r.db.pool.Query(
		ctx,
		`
		SELECT * FROM articles
		WHERE COALESCE(published_at, ingested_at) > $1
			AND is_read = false
			AND relevance_score >= $2
		ORDER BY relevance_score DESC, COALESCE(published_at, ingested_at) DESC
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
	for i, a := range articles {
		ids[i] = a.ID
	}

	tagMap, err := r.loadTags(ctx, ids)
	if err != nil {
		return nil, err
	}

	for i := range articles {
		articles[i].Tags = tagMap[articles[i].ID]
	}

	return articles, nil
}

// loadTags batch-loads tags for a set of article IDs, keyed by article ID.
// Shared by FindUnreadSince (batch) and GetById (single-ID slice) so there is
// one query building domain.Tag from the article_tags/tags join, not two.
func (r *ArticleRepo) loadTags(ctx context.Context, articleIDs []string) (map[string][]domain.Tag, error) {
	rows, err := r.db.pool.Query(ctx, `
		SELECT article_id, tags.id, tags.name, tags.slug, tags.color,
  		tags.created_at, tags.updated_at FROM article_tags JOIN tags ON
    	article_tags.tag_id = tags.ID  WHERE article_id = ANY($1)
    `, articleIDs)

	if err != nil {
		return nil, err
	}

	articleTags, err := pgx.CollectRows(rows, pgx.RowToStructByName[domain.ArticleTag])

	if err != nil {
		return nil, err
	}

	tagMap := make(map[string][]domain.Tag)
	for _, a := range articleTags {
		tagMap[a.ArticleID] = append(tagMap[a.ArticleID], domain.Tag{
			ID:        a.ID,
			Name:      a.Name,
			Slug:      a.Slug,
			Color:     a.Color,
			CreatedAt: a.CreatedAt,
			UpdatedAt: a.UpdatedAt,
		})
	}

	return tagMap, nil
}

// CountUnreadSince counts unread articles in the same window FindUnreadSince
// selects from, before the relevance-score bar is applied. It is the
// candidate pool the digest draws its "cleared the bar" list from.
func (r *ArticleRepo) CountUnreadSince(ctx context.Context, since time.Time) (int, error) {
	var count int
	err := r.db.pool.QueryRow(ctx,
		"SELECT COUNT(*) FROM articles WHERE COALESCE(published_at, ingested_at) > $1 AND is_read = false",
		since,
	).Scan(&count)
	return count, err
}
