package storage

import (
	"context"
	"encoding/json"
	"log"

	"github.com/felipeafreitas/agregado/internal/domain"
)

type ArticleCreator interface {
	Create(ctx context.Context, article domain.Article) (string, error)
}

type AIScorer interface {
	Score(ctx context.Context, title, content string, topicWeights map[string]float64) (int, error)
	Reason(ctx context.Context, title, content string) (string, error)
}

type ScoreUpdater interface {
	UpdateRelevanceScore(ctx context.Context, id string, score int) error
	UpdateRelevanceReason(ctx context.Context, id string, reason string) error
}

type WeightsQuerier interface {
	FindAll(ctx context.Context) (map[string]float64, error)
}

func NewWorker(repo ArticleCreator, scorer AIScorer, scoreUpdater ScoreUpdater, weights WeightsQuerier, minScore int) func([]byte) error {
	return func(body []byte) error {
		ctx := context.Background()
		var article domain.Article
		if err := json.Unmarshal(body, &article); err != nil {
			return err
		}
		id, err := repo.Create(ctx, article)

		if err != nil {
			return err
		}
		if id == "" {
			log.Printf("worker: skipped duplicate article external_url=%q title=%q", article.ExternalURL, article.Title)
			return nil
		}

		// RSS feeds populate the body in either <content:encoded> (Content)
		// or <description> (Summary); most send only one. Fall back to Summary
		// so the scorer always sees real text instead of just the title.
		var content string
		if article.Content != nil && *article.Content != "" {
			content = *article.Content
		} else if article.Summary != nil {
			content = *article.Summary
		}

		topicWeights, err := weights.FindAll(ctx)
		if err != nil {
			topicWeights = map[string]float64{}
		}

		score, err := scorer.Score(ctx, article.Title, content, topicWeights)
		if err != nil {
			log.Printf("worker: scoring failed id=%s title=%q: %v", id, article.Title, err)
			return nil
		}

		log.Printf("worker: scored id=%s score=%d title=%q", id, score, article.Title)
		scoreUpdater.UpdateRelevanceScore(ctx, id, score)

		if score >= minScore {
			reason, err := scorer.Reason(ctx, article.Title, content)
			if err != nil {
				log.Printf("worker: reason failed id=%s title=%q: %v", id, article.Title, err)
				return nil
			}
			scoreUpdater.UpdateRelevanceReason(ctx, id, reason)
		}

		return nil
	}
}
