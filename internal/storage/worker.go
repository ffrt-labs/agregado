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

// AIScorer, ScoreUpdater and WeightsQuerier are used by the enrich handler
// (enrich.go), not this worker — kept here since worker.go originally
// defined them and other files in this package still reference them.
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

// EnrichPublisher is satisfied by *broker.Publisher; declared here as an
// interface so this package doesn't need to import internal/broker.
type EnrichPublisher interface {
	Publish(exchange, routingKey string, body []byte) error
}

type enrichTrigger struct {
	ArticleID string `json:"article_id"`
}

// NewWorker consumes articles.store: it is durability-only (Create must
// succeed) and stays fast and Create-only. Fetch/distill/Score/Reason are
// best-effort enrichment with a different failure profile and live in their
// own articles.enrich stage (see enrich.go) — this handler's only job after
// a successful Create is to trigger that stage.
func NewWorker(repo ArticleCreator, publisher EnrichPublisher) func([]byte) error {
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

		msg, err := json.Marshal(enrichTrigger{ArticleID: id})
		if err != nil {
			log.Printf("worker: failed to marshal enrich trigger id=%s: %v", id, err)
			return nil
		}

		// Soft-fail: the article is already durably stored. If the trigger
		// never reaches articles.enrich, the row is left with
		// content_source = NULL, which the admin backfill (POST
		// /api/admin/enrich) can catch and re-drive. NACKing this message
		// instead would not help — a retried Create hits ON CONFLICT and
		// returns "" (looks like a duplicate), so the retry would silently
		// skip publishing too, leaving the article stuck un-enriched forever.
		if err := publisher.Publish("articles.enrich", "new", msg); err != nil {
			log.Printf("worker: failed to publish enrich trigger id=%s: %v", id, err)
		}

		return nil
	}
}
