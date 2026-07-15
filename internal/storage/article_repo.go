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

	tagMap, err := r.loadTags(ctx, []string{article.ID})
	if err != nil {
		return nil, err
	}
	article.Tags = tagMap[article.ID]

	return &article, nil
}

func (r *ArticleRepo) Count(ctx context.Context) (int, error) {
	var count int
	err := r.db.pool.QueryRow(ctx, "SELECT COUNT(*) FROM articles").Scan(&count)
	return count, err
}

// sortClause maps a sort keyword to a fixed ORDER BY clause. Only ever
// returns one of these two literal strings — sort is never interpolated
// into the query, so an unrecognized value just falls back to "recent"
// rather than reaching the database as text.
func sortClause(sort string) string {
	if sort == "relevant" {
		return "ORDER BY relevance_score DESC NULLS LAST, COALESCE(published_at, ingested_at) DESC"
	}
	return "ORDER BY COALESCE(published_at, ingested_at) DESC"
}

func (r *ArticleRepo) List(ctx context.Context, limit, offset int, sort string) ([]domain.Article, error) {
	rows, err := r.db.pool.Query(ctx,
		"SELECT * FROM articles "+sortClause(sort)+" LIMIT $1 OFFSET $2",
		limit, offset,
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

func (r *ArticleRepo) ListBySource(ctx context.Context, id string, limit, offset int, sort string) ([]domain.Article, error) {
	rows, err := r.db.pool.Query(ctx,
		"SELECT * FROM articles WHERE source_id = $1 "+sortClause(sort)+" LIMIT $2 OFFSET $3",
		id, limit, offset,
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
	for i, a := range articles { ids[i] = a.ID}

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
			ID: a.ID,
			Name: a.Name,
			Slug: a.Slug,
			Color: a.Color,
			CreatedAt: a.CreatedAt,
			UpdatedAt: a.UpdatedAt,
		})
	}

	return tagMap, nil
}

// SetTags replaces an article's tag assignments with exactly tagIDs
// (delete-then-insert inside a transaction, so a reader never observes a
// half-written state). Called from the enrich stage after Categorize —
// article_tags previously had no writer at all, so a categorized article's
// tag only ever existed in memory for the duration of one digest compute.
func (r *ArticleRepo) SetTags(ctx context.Context, articleID string, tagIDs []string) error {
	tx, err := r.db.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	if _, err := tx.Exec(ctx, "DELETE FROM article_tags WHERE article_id = $1", articleID); err != nil {
		return err
	}

	for _, tagID := range tagIDs {
		if _, err := tx.Exec(ctx,
			"INSERT INTO article_tags(article_id, tag_id) VALUES ($1, $2) ON CONFLICT DO NOTHING",
			articleID, tagID,
		); err != nil {
			return err
		}
	}

	return tx.Commit(ctx)
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

func (r *ArticleRepo) UpdateRelevanceReason(ctx context.Context, id string, reason string) error {
	_, err := r.db.pool.Exec(
		ctx,
		"UPDATE articles SET relevance_reason = $2 WHERE id = $1",
		id,
		reason,
	)

	return err
}

// UpdateContent persists an article's enriched body: content is the display
// form (fetched Markdown or the best available feed text), distilled is the
// short algorithmic extract used to budget AI prompts, source records where
// content came from (see the content_source CHECK constraint), and wordCount/
// readMinutes are derived from content so the digest's read-time estimate
// stops being computed from unset columns.
func (r *ArticleRepo) UpdateContent(ctx context.Context, id, content, distilled, source string, wordCount, readMinutes int) error {
	_, err := r.db.pool.Exec(
		ctx,
		"UPDATE articles SET content = $2, distilled_content = $3, content_source = $4, word_count = $5, estimated_read_minutes = $6 WHERE id = $1",
		id,
		content,
		distilled,
		source,
		wordCount,
		readMinutes,
	)

	return err
}

func (r *ArticleRepo) UpdateSummary(ctx context.Context, id string, summary string) error {
	_, err := r.db.pool.Exec(
		ctx,
		"UPDATE articles SET summary = $2 WHERE id = $1",
		id,
		summary,
	)

	return err
}

func (r *ArticleRepo) FindSaved(ctx context.Context) ([]domain.Article, error) {
	rows, err := r.db.pool.Query(ctx,
		"SELECT * FROM articles WHERE is_saved = true ORDER BY saved_at DESC NULLS LAST",
	)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowToStructByName[domain.Article])
}

func (r *ArticleRepo) ToggleBookmark(ctx context.Context, id string) error {
	_, err := r.db.pool.Exec(ctx, `
		UPDATE articles
		SET is_saved = NOT is_saved,
		    saved_at = CASE WHEN is_saved = false THEN NOW() ELSE NULL END
		WHERE id = $1`,
		id,
	)
	return err
}

func (r *ArticleRepo) Unsave(ctx context.Context, id string) error {
	_, err := r.db.pool.Exec(ctx,
		"UPDATE articles SET is_saved = false, saved_at = NULL WHERE id = $1",
		id,
	)
	return err
}

func (r *ArticleRepo) SaveExternalURL(ctx context.Context, url string) error {
	_, err := r.db.pool.Exec(ctx, `
		INSERT INTO articles(external_url, title, is_saved, saved_at)
		VALUES ($1, $1, true, NOW())
		ON CONFLICT (external_url) DO UPDATE SET is_saved = true, saved_at = NOW()`,
		url,
	)
	return err
}

func (r *ArticleRepo) CountAboveScore(ctx context.Context, minScore int) (int, error) {
	var count int
	err := r.db.pool.QueryRow(ctx,
		"SELECT COUNT(*) FROM articles WHERE relevance_score >= $1 AND created_at >= NOW()::date",
		minScore,
	).Scan(&count)
	return count, err
}

// FindUnenriched returns the IDs of articles that have never been through the
// enrichment stage (content_source is still NULL) — new rows waiting on the
// enrich queue, or older rows created before Phase 17 existed. Backs the
// admin backfill trigger (POST /api/admin/enrich).
func (r *ArticleRepo) FindUnenriched(ctx context.Context) ([]string, error) {
	rows, err := r.db.pool.Query(ctx, "SELECT id FROM articles WHERE content_source IS NULL")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

func (r *ArticleRepo) CountSaved(ctx context.Context) (int, error) {
	var count int
	err := r.db.pool.QueryRow(ctx, "SELECT COUNT(*) FROM articles WHERE is_saved = true").Scan(&count)
	return count, err
}
