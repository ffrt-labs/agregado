# Phase 17: RSS Article Content Fetching + Enrichment Stage

## Context

**The problem.** The AI scores, categorizes and summarizes RSS articles from text
that usually isn't the article. `poller.go:104-112` maps `<description>` → `Summary`
and `<content:encoded>` → `Content`. Most feeds omit `content:encoded`, so `Content`
is nil, and `worker.go:49-54` silently falls back to `Summary` — a teaser. The AI
then confidently scores a 200-char blurb as if it had read the piece.

**Three instances of the same defect class.** This codebase repeatedly ships fields
that *look* populated but aren't, with no signal that they aren't:
1. `Content ?? Summary` — no marker distinguishing "real article" from "teaser".
2. `word_count` / `estimated_read_minutes` — columns since migration `000001`,
   never written by anything, yet the digest template renders a read-time from them.
3. `Summarize` (`cloudflare.go:195`) and `Digest` build prompts from **titles only**
   — they never receive a body, so `AI_MAX_CONTENT_CHARS` applies to just 3 of 5
   provider methods. Phase 16 raised a cap that two callers can't reach.

**Outcome.** Fetch and extract the real article body at ingest; distil it for AI;
record *where the content came from* so degradation is countable instead of
invisible; and give `Summarize` real substance.

**Supersedes** Phase 16's deferred round (16.1/16.2) and changes its conclusions:
algorithmic distillation instead of `ai.Compress`, plus a `content_source` column.

## Decisions (settled)

| Question | Decision |
|---|---|
| Fetch trigger | Always fetch on new article; feed content is the fallback |
| Placement | New `articles.enrich` stage after `Create` (newsletters ride it free) |
| Message payload | `{article_id}` only — Postgres is the source of truth |
| Storage | `content` (Markdown) + `distilled_content` + `content_source` |
| Libraries | `go-shiori/go-readability` → `JohannesKaufmann/html-to-markdown/v2` |
| Posture | Honest UA, per-host serialization, no robots.txt in v1 |
| Quality gate | Readability `.Length < 500` = failed; else keep the longer of fetched vs feed |
| Distillation | Algorithmic extractive (headings + lede + section leads), no AI call |
| Roundup pages | Out of scope — one level of fetch only |
| Throughput | Parametrize `Consumer.Consume` with prefetch + worker count |
| Backfill | `POST /api/admin/enrich` republishes thin articles |

Explicitly **not** in this phase: clustering (that's digest-time, Phase 3.1),
`ai.Compress`, link extraction / `parent_article_id` children.

## Implementation

### 17.1 Schema + domain — migration `000014_article_content_source`
- `ALTER TABLE articles ADD COLUMN distilled_content TEXT`, `ADD COLUMN content_source
  VARCHAR(32) CHECK (content_source IN ('fetched','feed_content','feed_description','newsletter'))`.
  CHECK, not bare VARCHAR — mirrors `sources.type` in `000001`, and a column whose job
  is making degradation observable shouldn't be able to drift silently.
- `internal/domain/article.go`: add `DistilledContent *string`, `ContentSource *string`
  with `db:` tags. `FindUnreadSince` uses `SELECT *` (`article_repo.go:141`), so these
  scan through automatically — no query edits.
- Add a `BestText() string` method on `domain.Article` implementing the precedence
  `DistilledContent → Content → Summary`. **Replace the hand-written fallbacks in
  `worker.go:49-54` and `ranker.go:76-82` with it** — that logic is already duplicated
  and this phase would make it a third copy.

### 17.2 Fetcher — new `internal/ingestion/fetch/`
- `Fetcher.Fetch(ctx, url) (Result, error)` where `Result{Markdown, Title, Byline, Length}`.
- Own `http.Client`: explicit `Timeout` (~15s), `io.LimitReader` cap (~5MB),
  `Content-Type` must be `text/html`, redirect cap ~5.
- UA: `Agregado/1.0 (+https://github.com/felipeafreitas/agregado)`.
- Per-host serialization: `map[string]*sync.Mutex` (or a semaphore map) keyed by
  `url.Host`, with a small inter-request delay. 403/429 → return a typed error, never retry.
- Pipeline: fetch → `readability.FromReader` → `article.Content` (cleaned HTML) →
  html-to-markdown → Markdown.
- **Title/Byline: fill gaps only, feed wins.** Use `readability.Byline` only when the
  feed left `Author` nil (common — `poller.go:98-102`). Never override `Title`: feed
  titles are publisher-curated, while readability scrapes `<h1>`/`og:title` and drags
  in site suffixes like `" | The Verge"`. Titles are the most visible thing in the
  digest, so the downside is asymmetric.
- Mirror the `CloudflareProvider` convention: config-injected timeout with a
  `<= 0 → default` guard (`cloudflare.go:19-31`).

### 17.3 Distiller — `internal/textutil/distill.go`
- `Distill(markdown string, max int) string`: keep headings, the lede, and the first
  sentence of each section; drop boilerplate; cap ~2000 chars.
- Dependency-free and deterministic, matching the existing `textutil` style — this is
  what makes it unit-testable, which is how the Phase 16 fix was actually proven
  (`internal/textutil/textutil_test.go`).

### 17.4 Enrich stage
- `broker`: declare `articles.enrich` exchange + queue in `DeclareTopology()`,
  bound to the existing dead-letter exchange like `articles.store`.
- `internal/broker/consumer.go`: change `Consume(queueName, handler)` to accept
  prefetch + worker count (options struct). **This fixes a latent bug independent of
  this feature**: `Qos(1, 0, false)` with a single consumer tag means the broker never
  delivers message N+1 until N is acked, so today's 5 goroutines (line 52-54) are
  starved and ingest is effectively serial. Storage keeps prefetch 1; enrich gets 5/5.
- Slim `internal/storage/worker.go` to Create-only; on non-duplicate `Create`, publish
  `{article_id}` to `articles.enrich`. **Move `Score`/`Reason` out** into the enrich
  handler — they belong with the content they consume.
- New enrich handler: `GetById` → if `external_url` has the `newsletter:` scheme
  (see Phase 15) skip fetch, else `Fetch` → quality gate → `Distill` → compute
  `word_count`/`estimated_read_minutes` → `UpdateContent(...)` → `Score` → `Reason`
  (gated on `>= minScore`, as today).
- New `ArticleRepo.UpdateContent(ctx, id, content, distilled, source string, wordCount, readMinutes int) error`.
  Leave `UpdateSummary` (`article_repo.go:256`, zero callers) alone — wiring it means a
  third AI call per article in the bottleneck stage, which is exactly the cost this
  phase avoids. Revisit when `ai.Compress` is.
- **Failure semantics**: a fetch failure is *not* a pipeline failure — fall back to feed
  content, set `content_source` accordingly, still score, ACK. Only genuine
  infrastructure errors (`GetById`, `UpdateContent`) return an error → NACK → DLQ.
  AI failures stay soft-fail + ACK, as `worker.go:62-64` does today.

### 17.5 Summarize sees substance
- `internal/ai/cloudflare.go` `Summarize` (line 195): per article emit title +
  `RelevanceReason` + first ~400 chars of `DistilledContent`, budgeted per article
  against `p.maxContentChars`.
- No new data plumbing: `FindUnreadSince` filters `relevance_score >= $2`
  (`article_repo.go:144`), and `NULL >= 3` is false — so every digest article has a
  score at/above the bar and therefore already has a `relevance_reason` from ingest.
- Update the `summarize` default prompt in `internal/ai/prompts.go` to match the new
  input shape (it's admin-editable per Phase 7, so note the DB row may need a reset).

### 17.6 Config + wiring
- New `Fetch` struct in `internal/config/config.go` (flat-embedded like the others):
  `FETCH_TIMEOUT` (15s), `FETCH_MAX_BYTES` (5MB), `FETCH_MIN_CONTENT_CHARS` (500),
  `FETCH_USER_AGENT`, `DISTILL_MAX_CHARS` (2000). Document all in `.env.example`.
- `cmd/agregado/main.go`: build the fetcher, wire the enrich handler, add a second
  `consumer.Consume("articles.enrich", ...)` alongside line 108.
- **Dependency risk**: `go.mod` pins `golang.org/x/net v0.4.0` (2022). Both new libs
  depend on `x/net/html` and will bump it several majors. `goquery v1.8.0` is currently
  indirect-only. Run `go build ./...` early to surface the transitive churn.

### 17.7 Backfill
- `POST /api/admin/enrich` — republish articles with
  `content_source IS NULL` to `articles.enrich`. Mirror the existing
  `/api/digest/refresh` + `/api/backup/send` handler shape.

### 17.8 Docs
- New PRD section **F15** (F14 is currently the last real one, `docs/PRD.md:684`).
- Mark PRD F14 and TODO Phase 16.1/16.2 **superseded by F15/Phase 17**, so a future
  session reading top-to-bottom doesn't act on the stale `ai.Compress` plan.
- Add Phase 17 to `docs/TODO.md`; update the Current State block in `CLAUDE.md`.

## Verification

Phases 15 and 16 both closed with "not verified live — no data to observe." The
backfill endpoint exists specifically so this one doesn't join them.

1. `go build ./...` && `go vet ./...` && `go test ./...`.
2. Unit tests (deterministic, no network): `Distill` on a real fetched Markdown
   fixture; the quality gate on a short/nav-soup fixture; `BestText()` precedence;
   `Fetcher` against an `httptest.Server` serving a real article, a consent wall
   (<500 chars → fallback), a 403, a `Content-Type: application/pdf`, and an
   oversized body.
3. Live, against the running local stack: `POST /api/admin/enrich`, then in
   Postgres — `SELECT content_source, count(*) FROM articles GROUP BY 1`. Expect a
   real `fetched` majority. Confirm `length(content) >> length(summary)` and that
   `word_count` is populated.
4. `/admin/logs`: a `score` row should now show dense prose, no CSS, not truncated at
   500 chars. This is the observation Phase 16 couldn't make.
5. `POST /api/digest/refresh` → the `summarize` log row shows titles + reasons +
   excerpts, not a bare title list. Compare the resulting digest against the
   pre-change one.
6. Throughput sanity: confirm the enrich queue drains concurrently (5 in flight
   across distinct hosts) rather than one at a time.

## Known gaps, deliberately left open

- **No robots.txt.** Proportionate for a single-user, subscriber-driven, once-per-URL
  fetch. Revisit if this ever serves more than one person.
- **No enrichment retry.** A transient fetch failure leaves an article permanently on
  feed content. `content_source` makes this countable, and the backfill endpoint can
  re-drive it manually. Delayed-retry needs a TTL+DLX trick RabbitMQ won't do natively.
- **Roundup pages ingest as roundups.** Link extraction / `parent_article_id` children
  stay deferred (TODO Phase 2.5 + 5.6.3) — but this phase builds the `Fetch` primitive
  they were always blocked on.
- **`ai.Compress` not built.** If algorithmic `Distill` proves too lossy, the
  `distilled_content` column is already the right seam to swap it in behind.
