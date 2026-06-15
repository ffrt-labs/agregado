# AI Relevance Scoring & Feedback Loop — Design Plan

Designed in session 2026-06-13 via structured Q&A.

---

## Problem

The digest currently sends all unread articles sorted by date. On a busy day this can be 50+ articles. The goal is to surface only the most valuable content and teach the system to improve based on user feedback.

---

## Decisions

| # | Question | Decision | Rationale |
|---|----------|----------|-----------|
| 1 | Cold start signal | Neutral weights (1.0 all topics) | Sources are already curated by the user — topic filtering is already done. AI focuses on quality/global impact. |
| 2 | External trend data | None — LLM uses its own knowledge | No external API needed. A well-prompted LLM already knows what's globally significant. |
| 3 | Score format | 1–5 integer (`SMALLINT`) | More consistent LLM output than floats; easy to threshold; human-readable. |
| 4 | Scoring timing | Ingest time (not digest time) | Stable property of article; distributes AI load; enables UI queries. |
| 5 | Feedback UX | 👍/👎 links in email, HMAC-signed, separate from article link | Email clients can't run JS. Opening ≠ liking. Token prevents spoofed feedback. |
| 6 | Learning mechanism | `topic_weights` table, updated on each feedback | Simple, explainable. Avoids prompt explosion from few-shot examples. |
| 7 | Newsletter external links | Extract + fetch → new Article records; keep newsletter text too | Consistent pipeline — an article is an article regardless of origin. |
| 8 | Digest cap | Top N articles, score ≥ threshold, score DESC sort | Prevents flood on busy days. Both N and threshold are configurable. |
| 9 | Topic weights cold start | Neutral (all 1.0) | AI focuses on impact; feedback gradually personalizes over time. |

---

## Architecture

### Ingest Pipeline (new steps)

```
RSS Poller / Email Webhook
    │
    ├── Save article to DB (existing)
    │
    ├── ai.Score(title, content, topicWeights) → relevance_score
    │       └── UPDATE articles SET relevance_score = N
    │
    └── [newsletters only] ai.Summarize() → summary field
         └── UPDATE articles SET summary = ...

Email Webhook (additional, newsletters only)
    │
    └── newsletter.ExtractLinks(html)
            ├── filter noise (unsubscribe, social, tracking)
            └── for each valid URL:
                    FetchArticle(url) → title, content
                    Create Article (parent_article_id = newsletter.id)
                    ai.Score() → relevance_score
```

### Digest Pipeline (updated)

```
Ranker.GetDigestArticles()
    ├── WHERE relevance_score >= MinRelevanceScore OR relevance_score IS NULL
    ├── ORDER BY relevance_score DESC NULLS LAST, published_at DESC
    └── LIMIT DigestCap

Generator.Generate()
    ├── For each article: generate HMAC tokens (up/down)
    ├── ai.Summarize() per tag group (existing)
    └── Render template with feedback links
```

### Feedback Loop

```
User clicks 👍/👎 in email
    │
    GET /api/feedback?article_id=X&vote=up&token=T
    │
    ├── Validate HMAC(secret, "X:up") == T
    ├── INSERT article_feedback (article_id, vote)
    ├── Fetch article tags
    └── UPSERT topic_weights: weight += ±0.1, clamp(0.1, 2.0)
```

---

## New DB Schema

```sql
-- Migration 000005
ALTER TABLE articles ADD COLUMN relevance_score SMALLINT;

-- Migration 000006
CREATE TABLE topic_weights (
  topic      VARCHAR(100) PRIMARY KEY,
  weight     FLOAT NOT NULL DEFAULT 1.0,
  updated_at TIMESTAMP NOT NULL DEFAULT NOW()
);

-- Migration 000007
CREATE TABLE article_feedback (
  id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  article_id UUID NOT NULL REFERENCES articles(id) ON DELETE CASCADE,
  vote       VARCHAR(4) NOT NULL CHECK (vote IN ('up', 'down')),
  created_at TIMESTAMP NOT NULL DEFAULT NOW()
);

-- Migration 000008 (completes deferred Phase 2.4)
ALTER TABLE sources  ADD COLUMN extract_links     BOOLEAN DEFAULT true;
ALTER TABLE articles ADD COLUMN parent_article_id UUID REFERENCES articles(id);
```

---

## New Files

| File | Purpose |
|------|---------|
| `internal/newsletter/extractor.go` | `ExtractLinks(html) []string` + `FetchArticle(ctx, url)` |
| `internal/storage/feedback_repo.go` | `Create(ctx, articleID, vote)` |
| `internal/storage/topic_weights_repo.go` | `Upsert(ctx, topic, delta)` |

## Modified Files

| File | Change |
|------|--------|
| `internal/ai/provider.go` | Add `Score(ctx, title, content, topicWeights) (int, error)` |
| `internal/ai/cloudflare.go` | Implement `Score` |
| `internal/storage/article_repo.go` | Add `UpdateRelevanceScore(ctx, id, score)` |
| `internal/config/config.go` | Add `DigestCap`, `MinRelevanceScore` to `Digest` struct |
| `internal/digest/ranker.go` | Filter/sort/cap by relevance score |
| `internal/digest/generator.go` | Generate HMAC tokens per article |
| `internal/digest/templates/digest.html` | Add score badge + 👍/👎 links |
| `internal/api/server.go` | Add `GET /api/feedback` endpoint |
| RSS poller + email webhook handler | Call `Score` after article save |

---

## Implementation Order

1. DB migrations (unblocks everything)
2. `Score` method + ingest wiring (core value)
3. Ranker update (makes scoring visible in digest)
4. Feedback endpoint (closes the loop)
5. Digest template update (surfaces feedback UX)
6. Newsletter link extraction (most complex, independent)

---

## Verification Checklist

- [ ] Article with `relevance_score = 1` excluded from `/api/digest/preview`
- [ ] Newsletter email → newsletter article saved + child articles from extracted links
- [ ] `/api/digest/preview` returns ≤ 20 articles, all score ≥ 3 (or unscored)
- [ ] 👍 URL → `article_feedback` row + `topic_weights` upserted
- [ ] Re-score after feedback reflects updated topic weights in prompt
