package storage

import "context"

type FeedbackRepo struct {
	db *DB
}

func NewFeedbackRepo(db *DB) *FeedbackRepo {
	return &FeedbackRepo{db: db}
}

func (r *FeedbackRepo) Create(ctx context.Context, articleID, vote string) error {
	_, err := r.db.pool.Exec(
		ctx,
		"INSERT INTO article_feedback(article_id, vote) VALUES ($1, $2)",
		articleID,
		vote,
	)
	return err
}
