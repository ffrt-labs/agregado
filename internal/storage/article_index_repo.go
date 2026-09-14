package storage

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/felipeafreitas/agregado/internal/articleindex"
	"github.com/jackc/pgx/v5"
)

// ArticleIndexRepo persists only derived Article data. It deliberately has no
// content column or method that could write one.
type ArticleIndexRepo struct{ db *DB }

func NewArticleIndexRepo(db *DB) *ArticleIndexRepo { return &ArticleIndexRepo{db: db} }

func (r *ArticleIndexRepo) Create(ctx context.Context, record articleindex.Record) (articleindex.Record, bool, error) {
	row := r.db.pool.QueryRow(ctx, `
		INSERT INTO article_index(miniflux_entry_id, source_id, canonical_url, title, author, published_at, processing_status)
		VALUES ($1, NULLIF($2, ''), $3, $4, NULLIF($5, ''), $6, 'processing')
		ON CONFLICT (canonical_url) DO NOTHING
		RETURNING id, processing_status`, record.MinifluxEntryID, record.SourceID, record.CanonicalURL, record.Title, record.Author, record.PublishedAt)
	err := row.Scan(&record.ID, &record.Status)
	if err == nil {
		return record, true, nil
	}
	if err != pgx.ErrNoRows {
		return articleindex.Record{}, false, err
	}

	var tags []byte
	err = r.db.pool.QueryRow(ctx, `
		SELECT id, miniflux_entry_id, COALESCE(source_id, ''), canonical_url, title, COALESCE(author, ''), published_at,
		       COALESCE(summary, ''), tags, COALESCE(score, 0), processing_status, COALESCE(failure_reason, '')
		FROM article_index WHERE canonical_url = $1`, record.CanonicalURL).Scan(
		&record.ID, &record.MinifluxEntryID, &record.SourceID, &record.CanonicalURL, &record.Title, &record.Author,
		&record.PublishedAt, &record.Summary, &tags, &record.Score, &record.Status, &record.FailureReason)
	if err != nil {
		return articleindex.Record{}, false, err
	}
	if err := json.Unmarshal(tags, &record.Tags); err != nil {
		return articleindex.Record{}, false, fmt.Errorf("decode article index tags: %w", err)
	}
	return record, false, nil
}

func (r *ArticleIndexRepo) Complete(ctx context.Context, record articleindex.Record) error {
	tags, err := json.Marshal(record.Tags)
	if err != nil {
		return err
	}
	_, err = r.db.pool.Exec(ctx, `
		UPDATE article_index
		SET summary = $2, tags = $3, score = $4, processing_status = 'complete', failure_reason = NULL, updated_at = NOW()
		WHERE id = $1`, record.ID, record.Summary, tags, record.Score)
	return err
}

func (r *ArticleIndexRepo) Fail(ctx context.Context, id, reason string) error {
	_, err := r.db.pool.Exec(ctx, `UPDATE article_index SET processing_status = 'failed', failure_reason = $2, updated_at = NOW() WHERE id = $1`, id, reason)
	return err
}
