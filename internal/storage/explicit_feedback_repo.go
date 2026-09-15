package storage

import "context"

// ExplicitFeedbackRepo stores one current strong preference signal per Article
// Index record. A repeated direction changes neither the vote nor its observed
// time; an opposite direction replaces both.
type ExplicitFeedbackRepo struct{ db *DB }

func NewExplicitFeedbackRepo(db *DB) *ExplicitFeedbackRepo { return &ExplicitFeedbackRepo{db: db} }

func (r *ExplicitFeedbackRepo) SetCurrent(ctx context.Context, articleID, vote string) error {
	_, err := r.db.pool.Exec(ctx, `
		INSERT INTO article_index_feedback(article_index_id, vote)
		VALUES ($1, $2)
		ON CONFLICT (article_index_id) DO UPDATE
		SET vote = EXCLUDED.vote, updated_at = NOW()
		WHERE article_index_feedback.vote IS DISTINCT FROM EXCLUDED.vote`, articleID, vote)
	return err
}
