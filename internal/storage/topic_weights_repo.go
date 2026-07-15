package storage

import "context"

type TopicWeightsRepo struct {
	db *DB
}

func NewTopicWeightsRepo(db *DB) *TopicWeightsRepo {
	return &TopicWeightsRepo{db: db}
}

// Upsert nudges a topic's weight by delta, clamped to [0.1, 2.0]. Both the
// insert and conflict branches apply the same delta-from-neutral formula —
// previously the insert branch hardcoded weight = 1.0 (exactly neutral),
// silently discarding the first vote on any topic and only taking effect
// from the second vote onward.
func (r *TopicWeightsRepo) Upsert(ctx context.Context, topic string, delta float64) error {
	_, err := r.db.pool.Exec(
		ctx,
		`INSERT INTO topic_weights(topic, weight) VALUES ($1, GREATEST(0.1, LEAST(2.0, 1.0 + $2)))
		ON CONFLICT (topic) DO UPDATE
		SET weight = GREATEST(0.1, LEAST(2.0, topic_weights.weight + $2)),
		    updated_at = NOW()`,
		topic,
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
