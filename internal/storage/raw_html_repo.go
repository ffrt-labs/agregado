package storage

import "context"

// RawHTMLRepo persists the original email HTML of a newsletter, keyed by
// article id, in the newsletter_raw_html table (migration 000015). Kept
// separate from ArticleRepo because raw HTML has a different lifetime and
// access pattern from article content — it is ingestion provenance, written
// once and read only when re-running the extraction heuristic. See issue #2.
type RawHTMLRepo struct {
	db *DB
}

func NewRawHTMLRepo(db *DB) *RawHTMLRepo {
	return &RawHTMLRepo{db: db}
}

// Store records the raw HTML for an article. ON CONFLICT DO NOTHING makes it
// idempotent: a redelivered queue message that re-runs the worker for the same
// article must not error on the primary-key clash.
func (r *RawHTMLRepo) Store(ctx context.Context, articleID, html string) error {
	_, err := r.db.pool.Exec(ctx,
		"INSERT INTO newsletter_raw_html(article_id, html) VALUES ($1, $2) ON CONFLICT (article_id) DO NOTHING",
		articleID, html,
	)
	return err
}
