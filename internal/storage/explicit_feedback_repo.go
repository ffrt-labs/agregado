package storage

import (
	"context"
	"time"
)

// ExplicitFeedbackRepo stores one current strong preference signal per Article
// Index record. A repeated direction changes neither the vote nor its observed
// time; an opposite direction replaces both. created_at, the first vote's
// time, never changes.
type ExplicitFeedbackRepo struct{ db *DB }

func NewExplicitFeedbackRepo(db *DB) *ExplicitFeedbackRepo { return &ExplicitFeedbackRepo{db: db} }

// SetCurrent stores vote and returns the record's created_at. The update
// always runs (a no-op for a repeated direction) so RETURNING yields a row.
func (r *ExplicitFeedbackRepo) SetCurrent(ctx context.Context, articleID, vote string) (time.Time, error) {
	var createdAt time.Time
	err := r.db.pool.QueryRow(ctx, `
		INSERT INTO article_index_feedback(article_index_id, vote)
		VALUES ($1, $2)
		ON CONFLICT (article_index_id) DO UPDATE
		SET vote = EXCLUDED.vote,
		    updated_at = CASE
		        WHEN article_index_feedback.vote IS DISTINCT FROM EXCLUDED.vote THEN NOW()
		        ELSE article_index_feedback.updated_at
		    END
		RETURNING created_at`, articleID, vote).Scan(&createdAt)
	return createdAt, err
}
