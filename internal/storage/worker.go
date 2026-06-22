package storage

import (
	"context"
	"encoding/json"

	"github.com/felipeafreitas/agregado/internal/domain"
)

type ArticleCreator interface {
	Create(ctx context.Context, article domain.Article) (string, error)
}

type AIScorer interface {
    Score(ctx context.Context, title, content string, topicWeights map[string]float64) (int, error)
}

type ScoreUpdater interface {
    UpdateRelevanceScore(ctx context.Context, id string, score int) error
}

func NewWorker(repo ArticleCreator, scorer AIScorer, scoreUpdater ScoreUpdater) func([]byte) error {
	return func(body []byte) error {
		ctx := context.Background()
		var article domain.Article
		if err := json.Unmarshal(body, &article); err != nil {
			return err
		}
		id, err := repo.Create(ctx, article)

		if err != nil || id == "" {
			return err
		}

		var content string
		if article.Content != nil {
			content = *article.Content
		}

		score, err := scorer.Score(
			ctx,
			article.Title,
			content,
			map[string]float64{},
		)

		if err == nil {
			scoreUpdater.UpdateRelevanceScore(
				ctx,
				id,
				score,
			)
		}

		return nil
	}
}
