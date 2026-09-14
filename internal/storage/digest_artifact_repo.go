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
func (r *DigestArtifactRepo) Candidates(ctx context.Context, day time.Time) ([]digestartifact.Article, error) {
	rows, err := r.db.pool.Query(ctx, `SELECT id, canonical_url, title, COALESCE(summary, ''), COALESCE(source_id, ''), score, tags FROM article_index WHERE processing_status = 'complete' AND published_at >= $1 AND published_at < $1 + INTERVAL '1 day' ORDER BY score DESC, published_at DESC`, day)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []digestartifact.Article
	for rows.Next() {
		var a digestartifact.Article
		var tags []byte
		if err := rows.Scan(&a.ID, &a.CanonicalURL, &a.Title, &a.Summary, &a.Source, &a.Score, &tags); err != nil {
			return nil, err
		}
		if err := json.Unmarshal(tags, &a.Topics); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}
func (r *DigestArtifactRepo) Find(ctx context.Context, day time.Time) (digestartifact.Artifact, bool, error) {
	var a digestartifact.Artifact
	var selection []byte
	err := r.db.pool.QueryRow(ctx, `SELECT id, digest_date, subject, html, plain_text, selection, candidate_count, floor_pass_count, selected_count FROM digest_artifacts WHERE digest_date = $1`, day).Scan(&a.ID, &a.Date, &a.Subject, &a.HTML, &a.Text, &selection, &a.CandidateCount, &a.FloorPassCount, &a.SelectedCount)
	if err == pgx.ErrNoRows {
		return digestartifact.Artifact{}, false, nil
	}
	if err != nil {
		return digestartifact.Artifact{}, false, err
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
	err = r.db.pool.QueryRow(ctx, `INSERT INTO digest_artifacts(digest_date, subject, html, plain_text, selection, candidate_count, floor_pass_count, selected_count) VALUES($1,$2,$3,$4,$5,$6,$7,$8) ON CONFLICT(digest_date) DO NOTHING RETURNING id`, a.Date, a.Subject, a.HTML, a.Text, selection, a.CandidateCount, a.FloorPassCount, a.SelectedCount).Scan(&a.ID)
	if err == nil {
		return a, true, nil
	}
	if err != pgx.ErrNoRows {
		return digestartifact.Artifact{}, false, err
	}
	return r.Find(ctx, a.Date)
}
