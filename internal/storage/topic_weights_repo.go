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

func (r *TopicWeightsRepo) FindAll(ctx context.Context) (map[string]float64, error) {
	rows, err := r.db.pool.Query(ctx, "SELECT topic, weight FROM topic_weights")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	weights := make(map[string]float64)
	for rows.Next() {
		var topic string
		var weight float64
		if err := rows.Scan(&topic, &weight); err != nil {
			return nil, err
		}
		weights[topic] = weight
	}
	return weights, rows.Err()
}
