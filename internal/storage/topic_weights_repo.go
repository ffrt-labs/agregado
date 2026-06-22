package storage

import "context"

type TopicWeightsRepo struct {
	db *DB
}

func NewTopicWeightsRepo(db *DB) *TopicWeightsRepo {
	return &TopicWeightsRepo{db: db}
}

func (r *TopicWeightsRepo) Upsert(ctx context.Context, topic string, delta float64) error {
	_, err := r.db.pool.Exec(
		ctx,
		`INSERT INTO topic_weights(topic, weight) VALUES ($1, $2)
		ON CONFLICT (topic) DO UPDATE
		SET weight = GREATEST(0.1, LEAST(2.0, topic_weights.weight + $3)),
		    updated_at = NOW()`,
		topic,
		1.0,
		delta,
	)
	return err
}
