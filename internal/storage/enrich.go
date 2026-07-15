package storage

import (
	"context"
	"encoding/json"
	"log"
	"strings"

	"github.com/felipeafreitas/agregado/internal/domain"
	"github.com/felipeafreitas/agregado/internal/ingestion/fetch"
	"github.com/felipeafreitas/agregado/internal/textutil"
)

// newsletterURLScheme marks an article whose ExternalURL is not a real page
// to fetch — the newsletter's own body is already the article (see the
// Phase 15 /r/{id} redirect, which special-cases this same scheme).
const newsletterURLScheme = "newsletter:"

// wordsPerMinute is the reading-speed constant used to derive
// estimated_read_minutes from a word count.
const wordsPerMinute = 200

type ArticleGetter interface {
	GetById(ctx context.Context, id string) (*domain.Article, error)
}

type ContentUpdater interface {
	UpdateContent(ctx context.Context, id, content, distilled, source string, wordCount, readMinutes int) error
}

// Fetcher is satisfied by *fetch.Fetcher; declared as an interface so tests
// can substitute a fake without a live network call.
type Fetcher interface {
	Fetch(ctx context.Context, url string) (fetch.Result, error)
}

// Categorizer, TagQuerier and TagSetter back per-article tag assignment.
// Categorize used to run lazily inside the digest ranker with no persist
// step — every compute re-categorized every article, forever, and the tag
// only ever existed in memory for that one compute (article_tags had no
// writer at all). It now runs once per article here, alongside Score/Reason,
// and is actually persisted.
type Categorizer interface {
	Categorize(ctx context.Context, title, content string) (string, error)
}

type TagQuerier interface {
	FindAll(ctx context.Context) ([]domain.Tag, error)
}

type TagSetter interface {
	SetTags(ctx context.Context, articleID string, tagIDs []string) error
}

// NewEnrichHandler consumes articles.enrich (a trigger carrying only an
// article ID — Postgres is the source of truth, see enrichTrigger). For each
// article it resolves the best available content (fetch the real page,
// falling back to whatever the feed shipped), distils it, persists both, and
// runs the same Score/Reason steps the storage worker used to run inline.
//
// Failure semantics: a fetch/quality-gate miss is normal operation, not an
// error — it degrades to feed content and keeps going. Only an error from
// articles/DB infrastructure (GetById, UpdateContent) is returned, which
// NACKs the message to the dead-letter queue. AI failures soft-fail (log +
// ACK), matching the storage worker's prior behavior.
func NewEnrichHandler(articles ArticleGetter, content ContentUpdater, fetcher Fetcher, categorizer Categorizer, tags TagQuerier, tagSetter TagSetter, scorer AIScorer, scoreUpdater ScoreUpdater, weights WeightsQuerier, minScore, distillMaxChars int) func([]byte) error {
	return func(body []byte) error {
		ctx := context.Background()

		var trigger enrichTrigger
		if err := json.Unmarshal(body, &trigger); err != nil {
			return err
		}

		article, err := articles.GetById(ctx, trigger.ArticleID)
		if err != nil {
			return err
		}

		finalContent, source := resolveContent(ctx, fetcher, *article)
		distilled := textutil.Distill(finalContent, distillMaxChars)
		wordCount := len(strings.Fields(finalContent))
		readMinutes := max(1, (wordCount+wordsPerMinute-1)/wordsPerMinute)

		if err := content.UpdateContent(ctx, article.ID, finalContent, distilled, source, wordCount, readMinutes); err != nil {
			return err
		}

		if categorizer != nil {
			categorizeArticle(ctx, categorizer, tags, tagSetter, *article, finalContent)
		}

		topicWeights, err := weights.FindAll(ctx)
		if err != nil {
			topicWeights = map[string]float64{}
		}

		score, err := scorer.Score(ctx, article.Title, finalContent, topicWeights)
		if err != nil {
			log.Printf("enrich: scoring failed id=%s title=%q: %v", article.ID, article.Title, err)
			return nil
		}

		log.Printf("enrich: scored id=%s score=%d source=%s title=%q", article.ID, score, source, article.Title)
		scoreUpdater.UpdateRelevanceScore(ctx, article.ID, score)

		if score >= minScore {
			reason, err := scorer.Reason(ctx, article.Title, finalContent)
			if err != nil {
				log.Printf("enrich: reason failed id=%s title=%q: %v", article.ID, article.Title, err)
				return nil
			}
			scoreUpdater.UpdateRelevanceReason(ctx, article.ID, reason)
		}

		return nil
	}
}

// resolveContent picks the best available body for an article and reports
// where it came from. Newsletters skip the fetch entirely (their body is
// already the article). Otherwise it fetches the external link and keeps
// whichever of {fetched, feed} is longer — a consent wall, SPA shell or
// paywall all return HTTP 200, so length is the only signal available after
// the fact that extraction actually got real content.
func resolveContent(ctx context.Context, fetcher Fetcher, article domain.Article) (text, source string) {
	feedPlain := textutil.Strip(article.BestText())

	if strings.HasPrefix(article.ExternalURL, newsletterURLScheme) {
		return feedPlain, "newsletter"
	}

	if fetcher != nil {
		result, err := fetcher.Fetch(ctx, article.ExternalURL)
		if err != nil {
			log.Printf("enrich: fetch fallback id=%s url=%s: %v", article.ID, article.ExternalURL, err)
		} else if result.Length > len([]rune(feedPlain)) {
			return result.Markdown, "fetched"
		}
	}

	if article.Content != nil && *article.Content != "" {
		return feedPlain, "feed_content"
	}
	return feedPlain, "feed_description"
}

// categorizeArticle assigns a persisted tag from the categorizer's slug
// output. Skips articles that already carry a tag — GetById now loads
// article_tags, so this only fires for genuinely untagged articles.
// Soft-fails throughout: an AI miss, an unrecognized slug, or a persist
// error all just leave the article uncategorized rather than blocking
// Score/Reason, matching how those two already degrade.
func categorizeArticle(ctx context.Context, categorizer Categorizer, tags TagQuerier, tagSetter TagSetter, article domain.Article, content string) {
	if len(article.Tags) > 0 {
		return
	}

	slug, err := categorizer.Categorize(ctx, article.Title, content)
	if err != nil {
		log.Printf("enrich: categorize failed id=%s title=%q: %v", article.ID, article.Title, err)
		return
	}

	allTags, err := tags.FindAll(ctx)
	if err != nil {
		log.Printf("enrich: tag lookup failed id=%s: %v", article.ID, err)
		return
	}

	normalized := strings.TrimSpace(strings.ToLower(slug))
	for _, t := range allTags {
		if t.Slug != normalized {
			continue
		}
		if err := tagSetter.SetTags(ctx, article.ID, []string{t.ID}); err != nil {
			log.Printf("enrich: set tags failed id=%s: %v", article.ID, err)
		}
		return
	}
	log.Printf("enrich: categorize returned unknown slug %q id=%s", slug, article.ID)
}
