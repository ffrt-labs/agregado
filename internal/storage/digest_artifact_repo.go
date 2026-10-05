package storage

import (
	"context"
	"encoding/json"
	"time"

	"github.com/felipeafreitas/agregado/internal/digestartifact"
	"github.com/jackc/pgx/v5"
)

// DigestArtifactRepo is the durable idempotency boundary for one calendar day.
type DigestArtifactRepo struct{ db *DB }

func NewDigestArtifactRepo(db *DB) *DigestArtifactRepo { return &DigestArtifactRepo{db: db} }

// Candidates returns the Articles whose Enrichment completed since the
// previous persisted Digest ended, bounded by lookback. Both bounds are
// computed by Postgres on the same clock that stamps enriched_at, so no Go
// time ever has to agree with a TIMESTAMP column's time zone.
func (r *DigestArtifactRepo) Candidates(ctx context.Context, day time.Time, lookback time.Duration) (digestartifact.Pool, error) {
	var pool digestartifact.Pool
	err := r.db.pool.QueryRow(ctx, `
		SELECT GREATEST(
		         (SELECT MAX(COALESCE(window_end, created_at)) FROM digest_artifacts WHERE digest_date < $1),
		         LOCALTIMESTAMP - make_interval(secs => $2)),
		       LOCALTIMESTAMP`, day, lookback.Seconds()).Scan(&pool.Since, &pool.Until)
	if err != nil {
		return pool, err
	}
	rows, err := r.db.pool.Query(ctx, `SELECT id, canonical_url, title, COALESCE(summary, ''), COALESCE(source_id, ''), score, tags FROM article_index WHERE processing_status = 'complete' AND enriched_at >= $1 AND enriched_at < $2 ORDER BY score DESC, enriched_at DESC`, pool.Since, pool.Until)
	if err != nil {
		return pool, err
	}
	defer rows.Close()
	for rows.Next() {
		var a digestartifact.Article
		var tags []byte
		if err := rows.Scan(&a.ID, &a.CanonicalURL, &a.Title, &a.Summary, &a.Source, &a.Score, &tags); err != nil {
			return pool, err
		}
		if err := json.Unmarshal(tags, &a.Topics); err != nil {
			return pool, err
		}
		pool.Articles = append(pool.Articles, a)
	}
	return pool, rows.Err()
}
func (r *DigestArtifactRepo) Find(ctx context.Context, day time.Time) (digestartifact.Artifact, bool, error) {
	var a digestartifact.Artifact
	var selection []byte
	var windowStart, windowEnd *time.Time
	err := r.db.pool.QueryRow(ctx, `SELECT id, digest_date, subject, html, plain_text, selection, candidate_count, floor_pass_count, selected_count, window_start, window_end FROM digest_artifacts WHERE digest_date = $1`, day).Scan(&a.ID, &a.Date, &a.Subject, &a.HTML, &a.Text, &selection, &a.CandidateCount, &a.FloorPassCount, &a.SelectedCount, &windowStart, &windowEnd)
	if err == pgx.ErrNoRows {
		return digestartifact.Artifact{}, false, nil
	}
	if err != nil {
		return digestartifact.Artifact{}, false, err
	}
	if windowStart != nil && windowEnd != nil {
		a.WindowStart, a.WindowEnd = *windowStart, *windowEnd
	}
	if err := json.Unmarshal(selection, &a.Items); err != nil {
		return digestartifact.Artifact{}, false, err
	}
	return a, true, nil
}
func (r *DigestArtifactRepo) Save(ctx context.Context, a digestartifact.Artifact) (digestartifact.Artifact, bool, error) {
	selection, err := json.Marshal(a.Items)
	if err != nil {
		return digestartifact.Artifact{}, false, err
	}
	err = r.db.pool.QueryRow(ctx, `INSERT INTO digest_artifacts(digest_date, subject, html, plain_text, selection, candidate_count, floor_pass_count, selected_count, window_start, window_end) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10) ON CONFLICT(digest_date) DO NOTHING RETURNING id`, a.Date, a.Subject, a.HTML, a.Text, selection, a.CandidateCount, a.FloorPassCount, a.SelectedCount, a.WindowStart, a.WindowEnd).Scan(&a.ID)
	if err == nil {
		return a, true, nil
	}
	if err != pgx.ErrNoRows {
		return digestartifact.Artifact{}, false, err
	}
	return r.Find(ctx, a.Date)
}
