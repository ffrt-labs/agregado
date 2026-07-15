# Agregado - Implementation TODO

Progress tracker for building Agregado. Check items as you complete them.

---

## Phase 1: Foundation + RSS

### 1.1 Project Setup
- [x] Install Go 1.22+
- [x] Initialize Go module (`go mod init`)
- [x] Create directory structure
- [x] Set up Docker Compose (PostgreSQL + RabbitMQ)
- [x] Create `.env.example` with configuration template
- [x] Create `Makefile` with dev targets
- [x] Verify services start with `docker-compose up`

### 1.2 Database Layer
- [x] Create migration files (`000001_initial_schema.up.sql`, `000001_initial_schema.down.sql`)
- [x] Set up golang-migrate
- [x] Run migrations successfully
- [x] Verify tables exist in PostgreSQL

### 1.3 Configuration
- [x] Create `internal/config/config.go`
- [x] Load config from environment variables using `caarlos0/env/v10`
- [x] Add validation for required fields with `required` tag
- [x] Organize config into nested structs (Database, Queue, Http)

### 1.4 Domain Entities
- [x] Create `internal/domain/article.go`
- [x] Create `internal/domain/source.go`
- [x] Define structs matching database schema
- [x] Use pointers for nullable fields
- [x] Use custom type with constants for source types

### 1.4b Article Tagging
- [x] Create migration `000002_add_tags.up.sql` and `000002_add_tags.down.sql`
- [x] Add `tags` table with predefined entries (Tech, Business, Personal, etc.)
- [x] Add `article_tags` junction table for many-to-many relationship
- [x] Add `default_tag_id` column to sources table
- [x] Create `internal/domain/tag.go` entity
- [x] Update `internal/domain/source.go` with `DefaultTagID` field
- [x] Update `internal/domain/article.go` with `Tags` field
- [x] Run migration and verify tables exist

### 1.4c Nullable Source ID
- [x] Create migration `000003_nullable_source_id.up.sql` and `000003_nullable_source_id.down.sql`
- [x] Remove `manual` from source type CHECK constraint (only `rss`, `newsletter`)
- [x] Make `source_id` nullable on articles (NULL = manually added)
- [x] Update `internal/domain/source.go` - remove `Manual` constant
- [x] Update `internal/domain/article.go` - change `SourceID` to `*string`
- [x] Run migration and verify constraint updated

### 1.5 RabbitMQ Integration
- [x] Create `internal/broker/rabbitmq.go` - connection management
- [x] Implement reconnection logic with backoff
- [x] Create exchanges and queues on startup
- [x] Create `internal/broker/publisher.go` - publish helper
- [x] Create `internal/broker/consumer.go` - consume helper
- [x] Set up dead-letter exchange and queue
- [x] Test publish/consume round-trip

### 1.6 PostgreSQL Storage
- [x] Create `internal/storage/postgres.go` - connection pool (pgxpool)
- [x] Create `internal/storage/source_repo.go` - CRUD for sources
- [x] Create `internal/storage/article_repo.go` - CRUD for articles
- [x] Implement URL-based deduplication (ON CONFLICT DO NOTHING)
- [x] Repository interfaces - decided consumer-side (Option B); defined with consumers in 1.7/1.8

### 1.7 RSS Poller
- [x] Create `internal/ingestion/rss/parser.go` - feed parsing with gofeed
- [x] Create `internal/ingestion/rss/poller.go` - polling service
- [x] Implement periodic fetching (configurable interval)
- [x] Publish articles to `articles.ingest` exchange
- [x] Handle errors with error tracking (LastError, ErrorCount updates)
- [x] Update source `last_fetched_at` and `error_count`

### 1.8 Storage Worker
- [x] Create `internal/storage/worker.go`
- [x] Consume from `articles.store` queue
- [x] Store articles via repository
- [x] Implement ACK/NACK based on success/failure
- [x] Handle graceful shutdown (via consumer's context)

### 1.9 Health Endpoints
- [x] Create `internal/api/server.go` - HTTP server setup
- [x] Add `GET /health` - basic health check
- [x] Add `GET /health/rabbit` - RabbitMQ status
- [x] Add `GET /health/db` - PostgreSQL status

### 1.10 Main Entry Point
- [x] Create `cmd/agregado/main.go`
- [x] Wire up all components
- [x] Implement graceful shutdown (SIGINT, SIGTERM)
- [x] Start HTTP server, poller, and workers

### Phase 1 Verification
- [x] `docker-compose up` starts all services
- [x] Health endpoint returns 200
- [x] RabbitMQ management UI shows exchanges/queues
- [x] Add RSS feed via database → articles appear after poll

---

## Phase 2: Email Integration

### 2.1 Webhook Handler
- [x] Create `internal/ingestion/email/webhook.go`
- [x] Add `POST /webhook/email` endpoint
- [x] Validate webhook secret header
- [x] Parse Cloudflare Email Routing payload structure

### 2.2 Email Parsing
- [x] Create `internal/ingestion/email/parser.go`
- [x] Extract subject → title
- [x] Extract from → source identifier
- [x] Convert HTML body to text/markdown
- [x] Handle multipart emails
- [x] **Always** create main article from newsletter body
- [ ] If `source.summarize = true`, call `provider.Summarize` on newsletter body → store in `articles.summary`
- [ ] If `source.extract_links = true`, trigger link extraction pipeline (Phase 2.4)

### 2.3 Newsletter Source Management
- [x] Auto-create source from new sender email
- [x] Link newsletters to sources by `email_sender` field
- [x] Publish parsed articles to RabbitMQ

### 2.4 Newsletter Source Toggles
- [x] Create migration `000008_source_summarize.up.sql` — add `summarize BOOLEAN NOT NULL DEFAULT true` to `sources`
- [x] Update `internal/domain/source.go` — change `ExtractLinks` tag from `json:"-"` to `json:"extract_links"`; add `Summarize bool \`db:"summarize" json:"summarize"\``
- [x] Update `source_repo.go` Create (INSERT) — add `extract_links, summarize` as `$7, $8`
- [x] Update `source_repo.go` Update (UPDATE SET) — add `extract_links=$12, summarize=$13`
- [x] Add `GetByID(ctx, id) (*domain.Source, error)` to `SourceRepo` and `SourceRepository` interface
- [x] Add `PATCH /api/sources/{id}` handler (`SourcePatch` struct with `*bool` fields, fetch+merge+update pattern)
- [x] Wire `PATCH` route in `internal/api/server.go`
- [x] Wire `provider ai.Provider` into `email.NewHandler(...)` in webhook handler + `main.go`
- [x] Implement `source.Summarize` check in webhook handler → set `article.Summary` before publish (summary stored by worker on INSERT)
- [x] Add `UpdateSummary(ctx, id, summary string) error` to `ArticleRepo`
- [x] Update `templates/sources.html` — add `extract_links` and `summarize` checkbox toggles for newsletter sources (HTMX PATCH on change)

### 2.5 Link Extraction Pipeline
- [ ] Create `internal/newsletter/extractor.go` — `ExtractLinks(html string) []string` using goquery
  - Filter: `http/https` only; skip URLs containing `unsubscribe`, `pixel`, `mailto:`, social share links
- [ ] Add `FetchArticle(ctx, url) (title, content string, err error)` using go-readability
- [ ] Wire extraction into email webhook: after saving newsletter article, check `source.ExtractLinks` → extract → fetch → create child Articles with `parent_article_id` → publish to RabbitMQ

### 2.5 Cloudflare Worker (Email Bridge)
> The Cloudflare Worker is the glue between Email Routing and the Go webhook. Email Routing can't POST to webhooks directly — the Worker parses the raw email and forwards it.

- [ ] Install Wrangler CLI: `npm install -g wrangler`
- [ ] Authenticate: `wrangler login`
- [ ] Initialize Worker project: `wrangler init email-worker` (JavaScript, Hello World)
- [ ] Install postal-mime: `npm install postal-mime` (inside `email-worker/`)
- [x] Write Worker script (`src/index.js`) — email event handler → parse → POST to webhook
- [x] Set Worker secrets via wrangler: `WEBHOOK_URL`, `WEBHOOK_SECRET`
- [x] Deploy Worker: `wrangler deploy`
- [x] Create Email Routing rule in Cloudflare dashboard pointing to the Worker
- [x] Make app publicly accessible (ngrok for local dev, or deploy)
- [x] End-to-end test: send email to routing address → verify article created in DB

### 2.6 Auto-Confirm Newsletter Subscriptions
- [x] **Temporary stopgap:** `email-worker` forwards a copy of every incoming email to `FORWARD_EMAIL` (worker secret) so confirmation links can be clicked manually. Forward is best-effort (logged, never bounces ingestion). Proper auto-confirm below still pending.
- [ ] Add `IsConfirmationEmail(subject, body string) bool` helper — heuristic check (subject/body keywords: "confirm", "verify", "activate")
- [ ] Add `ExtractConfirmationLink(htmlBody string) (string, bool)` — use goquery to find the most prominent link near confirmation language
- [ ] In `internal/ingestion/email/webhook.go`, after parsing the email: if `IsConfirmationEmail`, call `ExtractConfirmationLink` → HTTP GET with 10s timeout → log result
- [ ] Continue normal article processing regardless of confirmation outcome
- [ ] Write unit tests for detection heuristics with sample confirmation emails from common senders

### Phase 2 Verification
- [x] POST to webhook endpoint returns 200
- [x] Forwarded email creates article in database
- [x] Source auto-created for new sender
- [x] Real email sent via Cloudflare → article appears in UI

**Note:** Phase 2.4 (Link Extraction Pipeline) has been deferred for later implementation.

---

## Phase 3: Digest Generation

### 3.1 Digest Query + Ranking
- [x] Create `internal/digest/ranker.go` (existed with bugs — fixed: duration units, nil guards, type mismatch, uncategorized sort)
- [x] **Group articles by tag first** (implemented in ranker — tagged + uncategorized groups, sorted by recency)
- [x] Add `FindUnreadSince` to `ArticleRepo` (two-query approach: articles + batch tag load)
- [x] Add `TagRepo` with `FindAll` method (satisfies `digest.TagQuerier`)
- [ ] **Within tag, cluster by AI-detected topic** — still absent (no "cluster" anywhere in
      the repo). Distinct from per-article `Categorize`, which is done (Phase 18) —
      this would group articles *within* a tag by sub-topic
- [x] Apply relevance scoring to filter low-value articles — `FindUnreadSince` filters
      `relevance_score >= $2` (Phase 5.6.4, done)
- [x] Limit to configurable max articles — enforced as SQL `LIMIT` via
      `DIGEST_MAX_ARTICLES` (Phase 5.6.4, done — see naming note there)

### 3.2 Email Generation
- [x] Create `internal/digest/generator.go`
- [x] Create HTML email template with **Tag → Articles** structure (AI-detected
      sub-topic clustering within a tag still deferred, see 3.1)
- [x] Include AI-generated topic summaries in template — `generator.go` calls
      `Summarize` per group (Phase 17 gave it real substance instead of titles-only);
      rendered in both `templates/digest.html` and the email template
- [x] Add digest-level overview: after group summaries are computed, call `provider.Digest(ctx, summaries)` → add `Overview string` to `templateData` → render at top of template (omit if AI fails)
- [x] Create plain text fallback
- [x] Format article summaries and links

### 3.3 SMTP Integration
- [x] Create `internal/digest/mailer.go`
- [x] Send emails via go-mail library
- [ ] Log digest history to `digest_logs` table (deferred)

### 3.4 Scheduling
- [x] Add cron scheduler (robfig/cron)
- [x] Schedule digest at configured time
- [x] Add `POST /api/digest/send` for manual trigger
- [x] Add `POST /api/digest/preview` for preview

### Phase 3 Verification
- [x] Manual digest trigger sends email
- [x] Email contains correct articles
- [ ] Scheduled digest fires at configured time

---

## Phase 4: Web UI

### 4.1 API Layer
- [x] Set up Chi router with middleware
- [x] Add JSON error response format
- [x] Implement pagination helpers
- [x] Create source handlers (CRUD)
- [x] Create article handlers (list, read/unread)
- [x] Create search handler
- [x] `POST /api/articles/{id}/bookmark` — toggle `is_saved` on article (star button in list)
- [x] `POST /api/bookmarks` — save arbitrary URL as article (`SaveExternalURL`; title = URL,
      no title/summary fetch — a smaller scope than originally specced, not fixed here)
- [x] `DELETE /api/bookmarks/{id}` — remove bookmark
- [ ] `GET /api/bookmarks` — list saved articles as JSON. **Genuinely absent** — only
      the HTML page `GET /bookmarks` exists (`bookmarkHandler.ListPage`)

### 4.2 Templates Setup
- [x] Create base layout template
- [x] Set up template rendering helper
- [x] Add HTMX library
- [x] Add minimal CSS
- [x] Register `add`, `dots`, `scoreLabel` template functions in rendering helper (used in `articles.html`)
- [x] Build and populate `Nav` struct (ArticleCount, SourceCount, BookmarkCount, ClearedCount) for sidebar on every page render
- [x] Add `.toggle` CSS class to `layout.html` (used by newsletter toggle checkboxes in `sources.html`)

### 4.3 Article Views
- [x] Article list page with HTMX pagination
- [x] Article detail page — `GET /articles/{id}` → `templates/reader.html` (Phase 15)
- [x] Read/unread toggle (HTMX partial)
- [x] Filter by source dropdown
- [ ] Date range filter
- [x] Sort by `?sort=relevant|recent` (Phase 18) — **was a real bug, not just unimplemented:**
      `ListPage` read `?sort=` into the template purely to drive a CSS class; `List`/
      `ListBySource` took no sort argument at all, so "Recent" silently returned
      identical rows. Fixed: both repo methods take a whitelisted `sort` param
      (`internal/storage/article_repo.go`'s `sortClause`, never interpolated into SQL)

### 4.4 Source Management
- [x] Source list page
- [x] Add source form (validates feed URL)
- [x] Delete source with confirmation
- [x] Show source status (last fetch, errors)
- [x] Manual refresh button
- [x] `extract_links` toggle checkbox (newsletter sources only, HTMX PATCH)
- [x] `summarize` toggle checkbox (newsletter sources only, HTMX PATCH)
- [x] Migration `000010_source_last_email_received` — add nullable `last_email_received_at` to `sources`
- [x] Add `LastEmailReceivedAt *time.Time` to `domain.Source`
- [x] Add `TouchEmailReceived(ctx, id)` to `SourceRepo`; webhook stamps it (best-effort) after publish
- [x] Type-aware "Last activity" column in `sources.html` (RSS → last fetch, newsletter → last email received)

### 4.5 Search
- [x] Search input with HTMX
- [x] PostgreSQL full-text search query
- [x] Display search results

### 4.6 Blocklist Management
- [ ] Add `GET /api/preferences/blocklist` endpoint
- [ ] Add `PUT /api/preferences/blocklist` endpoint
- [ ] Add blocklist management page in UI
- [ ] Allow adding/removing blocked terms

### 4.7 Polish
- [ ] Mobile-responsive layout
- [ ] Loading states
- [ ] Error messages
- [ ] Empty states

### 4.8 Bookmarks — DONE
- [x] Add `is_saved BOOLEAN DEFAULT false` column to `articles` (migration `000009_is_saved`)
- [x] Add `IsSaved bool` to `domain.Article`
- [x] `GET /bookmarks` page — renders `bookmarks.html` with saved articles
- [x] Bookmark list query in `ArticleRepo` (`FindSaved`)
- [x] Bookmark item fields — rendered directly from `domain.Article` in the template
      rather than a separate `BookmarkView` struct; simpler than specced, same result

### 4.9 Daily Digest Home Page — DONE (via Phase 8, different shape than specced)
- [x] `GET /` route — `digestHandler.HomePage` using `scheduler.TodayOrTrigger` (Phase 9's
      non-blocking cache-or-compute, not a fresh per-request query)
- [x] Renders today's digest as a web page — `templates/digest.html`, not a separate
      `home.html`; shares the `DigestView` view-model with the email template (Phase 8)
- [x] Groups by tag, reuses the ranker — same `digest.Ranker` the email path uses

### 4.10 Source Backup & Restore
- [x] Add `GET /api/sources/export` — returns all sources as OPML (RSS standard; importable into any feed reader)
- [x] Add `POST /api/sources/import` — accepts an OPML file, bulk-creates missing sources (skip duplicates by URL)
- [x] Add Export button to Sources page → triggers file download
- [x] Add Import file input to Sources page → POSTs OPML, refreshes list
- [x] Add scheduled job to email OPML backup to self (`BACKUP_SCHEDULE`, `BACKUP_RECIPIENT_EMAIL`, weekly default)
- [x] Add manual trigger endpoint `POST /api/backup/send`
- [ ] **Known gap (found 2026-07-15, OPML import debugging session):** clicking
      "Export OPML" over the local dev server's plain HTTP triggers Chrome's
      "insecure download blocked" warning (any file download over unencrypted
      HTTP, not specific to OPML/MIME type). Not fixed — tracked under
      Post-MVP Features ("Local HTTPS for dev server")

### Phase 4 Verification
- [ ] Can browse articles in browser
- [ ] Can add/remove sources
- [ ] Search returns relevant results
- [ ] Works on mobile viewport
- [ ] Bookmarks save and render correctly
- [ ] Sidebar counts reflect real data

---

## Phase 5: Hardening

### 5.1 Monitoring
- [ ] Add structured logging with slog
- [ ] Add Prometheus metrics endpoint
- [ ] Track key metrics (articles ingested, stored, etc.)
- [ ] Monitor queue depths

### 5.2 Error Handling
- [ ] Dead-letter queue consumer (log failed messages)
- [ ] Retry logic for transient failures
- [ ] Circuit breaker for external services (optional)

### 5.3 Testing
- [ ] Unit tests for domain logic
- [ ] Unit tests for parsers
- [ ] Integration tests for repositories
- [ ] Integration tests for RabbitMQ flow

### Phase 5 Verification
- [ ] Metrics endpoint returns Prometheus format
- [ ] Failed messages appear in dead-letter queue
- [ ] Logs are structured JSON
- [ ] Tests pass

---

## Phase 5.5: AI Infrastructure

**Note:** AI processing runs at digest time (batch), not on article ingestion.
**Provider:** Cloudflare Workers AI (swappable via `ai.Provider` interface — Ollama planned as alternative).

### 5.5.1 AI Client Layer
- [x] Create `internal/ai/provider.go` — swappable `Provider` interface (`Summarize`, `Categorize`)
- [x] Create `internal/ai/cloudflare.go` — Cloudflare Workers AI HTTP client
- [x] Add AI config to `internal/config/config.go` (provider, account ID, token, model)
- [x] Add `Digest(ctx context.Context, topicSummaries []string) (string, error)` to `Provider` interface + implement in Cloudflare provider (prompt: 2-sentence intro from the topic summaries)

### 5.5.2 AI Features
- [x] Per-tag group summarization in digest generator (soft failure — AI error never blocks digest)
- [x] Integrate summaries into digest HTML template
- [x] `Categorize` integration — auto-assign tags to articles. **Moved from digest-time
      to ingest-time in Phase 18**: it originally ran lazily in the ranker with no
      persist step (`article_tags` had no writer at all), so every digest compute
      re-categorized every article from scratch, forever (72 `categorize` calls
      logged for 45 articles). Now runs once per article in the `articles.enrich`
      stage and persists via `ArticleRepo.SetTags`
- [ ] Create `internal/ai/relevance.go` — score article relevance using blocklist.
      Functionally, relevance scoring exists (`Score`/`Reason` on the provider,
      Phase 5.6.2) — this item is specifically about a *blocklist* factoring into
      relevance, which is absent (see 4.6, also absent)
- [ ] Integrate blocklist from preferences table (`key='blocklist'`) — the
      `preferences` table exists (migration `000001`) but nothing reads/writes it

### 5.5.3 Ollama Alternative (Future)
- [ ] Add Ollama service to `docker-compose.yml`
- [ ] Create `internal/ai/ollama.go` — implement `Provider` interface for local Ollama
- [ ] Switch provider via `AI_PROVIDER=ollama` env var

### 5.5.4 Digest Integration
- [ ] Modify digest generator to call AI categorizer before grouping
- [ ] Modify digest generator to call AI summarizer for topic clusters
- [ ] Apply relevance scoring to filter articles

### Phase 5.5 Verification
- [ ] Ollama container starts with docker-compose
- [ ] AI client can communicate with Ollama
- [ ] Batch categorization works with sample articles
- [ ] Topic summaries appear in digest preview

---

## Phase 5.6: AI Relevance Scoring & Feedback Loop

**Goal:** Surface only the most valuable articles in each digest using per-article AI scoring, cap the digest size, and close the loop with thumbs up/down feedback that improves future scoring over time.

**Design decisions:** See `docs/ai-relevance-feedback-plan.md` for full rationale.

### 5.6.1 DB Schema
- [x] Create migration `000004_relevance_score.up.sql` — add `relevance_score SMALLINT` (nullable) to `articles`
- [x] Create migration `000005_topic_weights.up.sql` — add `topic_weights` table (topic slug, weight float, updated_at)
- [x] Create migration `000006_article_feedback.up.sql` — add `article_feedback` table (id, article_id FK, vote `up|down`, created_at)
- [x] Create migration `000007_extract_links.up.sql` — add `extract_links BOOLEAN DEFAULT true` to `sources` and `parent_article_id UUID` to `articles`
- [x] Run all migrations and verify tables exist

### 5.6.2 AI Scoring at Ingest — DONE (shape evolved through Phases 17/18)
- [x] Add `Score(ctx, title, content string, topicWeights map[string]float64) (int, error)` to `internal/ai/provider.go`
- [x] Implement `Score` in `internal/ai/cloudflare.go`
- [x] Add `UpdateRelevanceScore(ctx, id, score)` to `internal/storage/article_repo.go`
- [x] Wire `Score` — not into the RSS poller/webhook directly (as originally specced),
      but into the `articles.enrich` stage (Phase 17) that both the poller and the
      newsletter webhook publish into via a shared storage worker, so this covers
      both ingest paths through one handler
- [x] Newsletter `Summarize` — `source.Summarize` toggle calls `Summarize` in the
      webhook handler before publish (predates Phase 17/18, unchanged)

### 5.6.3 Newsletter Link Extraction
- [ ] Create `internal/newsletter/extractor.go` — `ExtractLinks(html string) []string` using goquery
  - Filter: `http/https` only; skip URLs containing `unsubscribe`, `pixel`, `mailto:`, social share links
- [ ] Create fetch helper in same package — `FetchArticle(ctx, url) (title, content string, err error)` using go-readability
- [ ] Wire extraction into email webhook handler: after saving newsletter article, extract links → fetch → create child Articles with `parent_article_id` set → score each

### 5.6.4 Ranker Update — DONE (naming differs from spec)
- [x] Cap + min score config exist as `DIGEST_MAX_ARTICLES` (default `20`) and
      `DIGEST_MIN_SCORE` (default `3`) — not literally named `DIGEST_CAP`, same purpose
- [x] `GetDigestArticles` / `FindUnreadSince`:
  - Filter: `relevance_score >= MinRelevanceScore` (SQL `WHERE`)
  - Sort: score DESC, then `COALESCE(published_at, ingested_at)` DESC (`ranker.go`'s
    `lessByScoreThenDate`)
  - Limit: SQL `LIMIT` from `DIGEST_MAX_ARTICLES`

### 5.6.5 Feedback Endpoint — DONE, but was completely non-functional until Phase 18
- [x] `internal/storage/feedback_repo.go` — `Create(ctx, articleID, vote)`
- [x] `internal/storage/topic_weights_repo.go` — `Upsert(ctx, topic, delta)`, clamp 0.1–2.0
- [x] Feedback endpoint — not `GET /api/feedback` + HMAC as originally specced;
      Phase 18 **deleted** that design (its token generator had been removed in an
      earlier refactor, making the route permanently unreachable) and replaced it with
      `POST /api/articles/{id}/feedback` (JSON body `{"vote":"up"|"down"}`, no
      signature — same-origin, matches the web template's existing call). Fetches
      article tags → upserts topic_weights (±0.1)
- [x] **Found and fixed in Phase 18, not originally scoped:** `TopicWeightsRepo.Upsert`'s
      insert branch hardcoded `weight = 1.0` (neutral), so the *first* vote on any
      topic silently discarded itself — only the second vote onward had any effect

### 5.6.6 Digest Template — Feedback Links — DONE (web only, HMAC dropped)
- [x] Web template (`templates/digest.html`) renders 👍/👎 buttons calling the POST
      endpoint above; no token needed (see 5.6.5)
- [ ] Email template deliberately has **no** feedback links — mail clients prefetch
      links, so a state-mutating GET would auto-vote. The email template explicitly
      defers: "Your feedback on the web app retunes tomorrow's digest." Reviving this
      needs its own prefetch-safe design, not a straightforward port of the web POST

### Phase 5.6 Verification
- [x] `/api/digest/preview` / `FindUnreadSince` respects `MinRelevanceScore` and
      `DigestCap` (`DIGEST_MAX_ARTICLES`)
- [ ] Newsletter link extraction (child articles from extracted links) — still absent,
      see Phase 2.5 / 5.6.3
- [ ] Click 👍/👎 → `article_feedback` row created, `topic_weights` upserted (code
      fixed in Phase 18; not yet live-verified against the real stack — both tables
      were provably at 0 rows before this fix despite the UI existing since this
      phase originally shipped)
- [ ] Topic weight used in subsequent `Score` prompt — `enrich.go` passes
      `weights.FindAll()` into `Score`; not yet confirmed live that the prompt's
      weights block has content instead of a dangling header

---

## Phase 6: Social Media Integration (Post-MVP)

**Prerequisites:** Phases 1-5 complete, AI summarization infrastructure

### 6.1 Social Sources Schema
- [ ] Create migration `000005_add_social_sources.up.sql`
- [ ] Create migration `000005_add_social_sources.down.sql`
- [ ] Add `social_sources` table (platform, handle, display_name, etc.)
- [ ] Add `social_posts` table (temporary buffer for posts)
- [ ] Add `social_digests` table (AI-generated summaries)
- [ ] Add indexes for efficient queries
- [ ] Run migration and verify tables exist

### 6.2 Domain Entities
- [ ] Create `internal/domain/social_source.go` with Platform type
- [ ] Create `internal/domain/social_post.go`
- [ ] Create `internal/domain/social_digest.go` with HighlightPost struct

### 6.3 Storage Repositories
- [ ] Create `internal/storage/social_source_repo.go` - CRUD for social sources
- [ ] Create `internal/storage/social_post_repo.go` - CRUD + batch operations
- [ ] Create `internal/storage/social_digest_repo.go` - CRUD for digests

### 6.4 Bluesky Integration
- [ ] Research AT Protocol and Bluesky API
- [ ] Create `internal/ingestion/social/bluesky.go` - API client
- [ ] Implement authentication (app password)
- [ ] Implement `GetAuthorFeed` to fetch posts
- [ ] Handle pagination and rate limits
- [ ] Test fetching posts from followed accounts

### 6.5 Social Poller Orchestrator
- [ ] Create `internal/ingestion/social/poller.go` - orchestrator
- [ ] Implement periodic polling (configurable interval)
- [ ] Store posts in `social_posts` buffer
- [ ] Update source `last_fetched_at` and `error_count`
- [ ] Handle errors with exponential backoff

### 6.6 AI Client Abstraction
- [ ] Create `internal/ai/client.go` - provider interface
- [ ] Create `internal/ai/openai.go` - OpenAI adapter (optional)
- [ ] Create `internal/ai/anthropic.go` - Anthropic adapter (optional)
- [ ] Create `internal/ai/ollama.go` - Local LLM adapter (optional)
- [ ] Add configuration for AI provider selection

### 6.7 Social Post Summarizer
- [ ] Create `internal/ai/summarizer.go` - summarization logic
- [ ] Design prompt for topic grouping and summarization
- [ ] Fetch unprocessed posts from last 24h
- [ ] Send to AI provider with structured prompt
- [ ] Parse AI response (JSON with topics, summaries, highlights)
- [ ] Store results in `social_digests` table
- [ ] Mark posts as `processed = true`
- [ ] Implement cleanup of old processed posts

### 6.8 Digest Integration
- [ ] Modify `internal/digest/generator.go` to query social_digests
- [ ] Update digest email template with "Social Highlights" section
- [ ] Group social digests by topic in email
- [ ] Add social digest to preview endpoint
- [ ] Test combined digest generation

### 6.9 Web UI for Social Sources
- [ ] Add social sources list page
- [ ] Add form to follow new social account
- [ ] Show platform-specific icons
- [ ] Show source status (last fetch, errors)
- [ ] Add delete with confirmation

### 6.10 Additional Platforms (Optional)
- [ ] Create `internal/ingestion/social/reddit.go` - Reddit API client
- [ ] Create `internal/ingestion/social/mastodon.go` - Mastodon API client
- [ ] Handle multi-instance Mastodon authentication
- [ ] Add platform selection in UI

### Phase 6 Verification
- [ ] Bluesky poller fetches posts from configured accounts
- [ ] Posts stored in buffer table
- [ ] AI summarizer groups and summarizes posts correctly
- [ ] Social digests appear in daily email
- [ ] Old posts cleaned up after processing
- [ ] Web UI allows managing social sources

---

## Phase 7: Admin Console (AI Logs, Editable Prompts, Tag Settings)

**Goal:** An `/admin` area to observe AI calls, edit prompts without redeploying, and manage tags. See `docs/PRD.md` F9 and the plan file.

**Decisions:** system-prompt-only editable; log-all with live toggle + clear; `/admin` unauthenticated (v1 — prod-public risk noted); categorize injects live tag slugs. Provider is the single seam (`cmd/agregado/main.go`); `internal/ai` stays free of `internal/storage` via interfaces.

### 7.1 Data Model — migration `000011`
- [x] Create `migrations/000011_admin.up.sql` / `.down.sql`
- [x] `ai_prompts` (operation PK, system_prompt, updated_at) + seed 4 prompts (categorize seed WITHOUT the inline slug list)
- [x] `ai_logs` (id, operation, model, system_prompt, user_prompt, response, success, error, duration_ms, created_at) + index on `created_at DESC`
- [x] `admin_settings` (key PK, value, updated_at) + seed `ai_logging_enabled = 'true'`
- [ ] Run migration; verify 3 tables + seeds

### 7.2 Domain + Repos
- [x] `internal/domain/ai_prompt.go` (`AIPrompt`), `ai_log.go` (`AILog`) with `db:` tags
- [x] `internal/storage/prompt_repo.go` — `SystemPrompt(op)`, `List`, `Update(op, text)`
- [x] `internal/storage/ai_log_repo.go` — `Insert(entry)`, `List(limit, offset, opFilter)`, `Clear`
- [x] `internal/storage/settings_repo.go` — `Get(key)`, `Set(key, value)`, `AILoggingEnabled()`
- [x] `AILogger` composing settings+log (`Record` gates on the flag) → satisfies `ai.AILogSink`
- [x] Tag adapter satisfying `ai.TagLister` (`TagRepo.CategorySlugs`)

### 7.3 AI Layer Refactor (the single seam)
- [x] Add interfaces to `internal/ai`: `PromptStore`, `TagLister`, `AILogSink`, `LogEntry`
- [x] Move the 4 hardcoded prompts into `ai.DefaultPrompts` (runtime fallback)
- [x] `CloudflareProvider`: add fields (`prompts`, `tags`, `logs`) + constructor params
- [x] Each method loads its system prompt via the store (fallback to default); `Categorize` appends live tag slugs
- [x] `complete(operation, …)` — thread the operation, time the call, `Record` on success **and** error
- [x] Wire `cmd/agregado/main.go` — build repos + `AILogger`, pass into `NewCloudflareProvider`
- [ ] ✅ Verify: trigger a digest → `ai_logs` rows appear, prompts sourced from DB (**answers the tagging debug**)

### 7.4 Admin Shell + Logs Page
- [x] `internal/api/admin.go` (`AdminHandler`, repos from `db`)
- [x] Routes in `server.go`: `/admin` pages + `/api/admin/logs/*`
- [x] `templates/admin_logs.html` — paginated table, operation filter, newest first
- [x] Logging toggle (`POST /api/admin/logs/toggle`) + Clear (`DELETE /api/admin/logs`)
- [x] "Admin" nav group in `templates/layout.html`

### 7.5 Prompts Page
- [x] `templates/admin_prompts.html` — list 4 ops, edit system prompt (textarea), Reset to default
- [x] Handlers: `PromptsPage`, `UpdatePrompt(op)`, `ResetPrompt(op)` (writes `ai.DefaultPrompts[op]`)

### 7.6 Tag Settings Page
- [x] Extend `TagRepo` with `Create` / `Update` / `Delete` / `FindByID`
- [x] `templates/admin_tags.html` — CRUD (name, slug, color), reusing `sources.html` form/table style
- [ ] ✅ Verify: add a tag → it appears in the next `categorize` log's prompt list

### 7.7 Follow-ups / Risks
- [ ] (Deferred) Basic Auth middleware on `/admin` — prod is public via Cloudflare tunnel

### Phase 7 Verification
- [x] `go build ./...` && `go test ./...`
- [ ] `make migrate-up` → 3 tables + seeds present
- [ ] Regenerate on `/` → `ai_logs` rows for score/categorize/summarize/digest
- [ ] `/admin/logs`: a `categorize` row shows the live tag slugs in its prompt + the raw returned slug
- [ ] Toggle logging OFF → no new rows; ON → resume; Clear empties the table
- [ ] Edit `categorize` prompt → next Regenerate's log reflects it; Reset restores default
- [ ] Add a tag → next `categorize` log lists it

---

## Phase 8: Unify Digest Templates (web ↔ email)

**Goal:** Make the sent email adopt the web UI's Daily Digest look (email-safe), and refactor so formatting logic + the view-model are shared, not duplicated. See `docs/PRD.md` F4.1 and the plan file `now-we-have-2-sequential-newt.md`.

**Decisions:** email-safe mirrored CSS (web keeps its own); share `funcMap` (leaf pkg) + view-model builder (in `internal/digest`); email keeps ▲/▼ feedback links + footer browse link (no HTMX).

### 8.1 Shared template funcs — leaf package
- [x] Create `internal/tmplfunc/funcmap.go` — move `funcMap` (`add`, `excerpt`, `dots`, `scoreLabel`) + `excerptChars` const out of `internal/api/render.go`; export as `tmplfunc.Map` (depends only on `internal/textutil`)
- [x] Update `internal/api/render.go` — use `tmplfunc.Map` at both `.Funcs(...)` call sites; drop the moved var/const

### 8.2 Shared view-model — into `internal/digest`
- [x] Create `internal/digest/view.go` — move `DigestGroupView`/`DigestItemView` from `api/digest.go`; add `DigestView` (Greeting, DeliveryTime, Date string, Intro, Groups)
- [x] Add `BuildView(computed ComputedDigest, sourceNames map[string]string) DigestView` (Position/SourceName/Topic mapping + greeting/date formatting; plain map, no repo interface → no cycle)
- [x] Slim `internal/api/digest.go` `HomePage` — build sourceMap → `digest.BuildView(...)` → render `{ DigestView; Nav }` (embeds `digest.DigestView`). Note: `BuildView` now also populates `IsSaved` (old web loop left it zero — a fix, not a regression)

### 8.3 Config + wiring
- [x] Add `BaseURL string \`env:"PUBLIC_BASE_URL"\`` to `config.Digest` (default `http://localhost:8080`)
- [x] Inject source-name map + base URL into the email path — `digest.SourceLister` interface on the scheduler builds the map; generator holds `baseURL`
- [x] Update `cmd/agregado/main.go` (`NewDefaultGenerator(provider, secret, baseURL)`, `NewScheduler(..., sourceRepo, ...)`) and `.env.example`

### 8.4 Email generator renders shared view
- [x] Register `tmplfunc.Map` when parsing the embedded email template
- [x] `Render(c, sourceNames)` builds `DigestView` via `BuildView`, decorates items with absolute `UpURL`/`DownURL` (`feedbackURL` = `tokenFor` + `baseURL`) + footer browse URL (`emailItem` embeds `DigestItemView`); removed dead `DigestArticle`/`digestGroup`/`templateData`/`Generate`
- [x] Keep/lightly enrich the plain-text fallback

### 8.5 Email template rewrite (email-safe, mirrors web)
- [x] Rewrite `internal/digest/templates/digest.html` — full document, table-based ~650px column
- [x] Email-safe CSS mirroring `.digest-*`: hardcoded hex (no `var()`), table layout (no flexbox), inline styles + minimal `<head>` reset, `Georgia,serif` + `'Courier New',monospace` fallbacks
- [x] Structure parallels web: deliver line, kicker, H1 greeting, subtitle, intro, per-group topic-rule + summary, per-article meta (num/src/date/read-time/`dots`), title link, `excerpt`, ▲/▼ feedback links, footer browse link
- [x] `.digest-count` reworded (web uses Nav counts email lacks) → "The stories that cleared the bar {DeliveryTime}."
- [x] Polish: `<meta name="color-scheme" content="light only">`

### Phase 8 Verification
- [x] `go build ./...` && `go vet ./...` pass
- [x] Render smoke test (throwaway) — template executes; Position/SourceName/dots/excerpt/absolute feedback+browse URLs all present; excerpt strips markup
- [x] Web `/` renders identically to before the view-model move (manual — verified via Mailpit smoke test)
- [x] `POST /api/digest/preview` → inspect HTML in browser AND a mail client (Gmail draft / send to self) — verified via Mailpit
- [ ] Click a feedback link from the email → absolute URL hits `/api/feedback`, records a vote

## Phase 9: Fix digest AI-call context exhaustion (production incident)

**Symptom:** In production, `summarize`/`digest` AI operations stopped appearing in the admin AI-log table while `score`/`categorize` kept working. Prod logs showed `context deadline exceeded` on every `summarize` call, `digest: computed overview=false`, and `GET /` taking the full 3-minute timeout.

**Root cause:** `Categorize` → `Summarize` → `Digest` ran sequentially inside one shared 3-minute deadline (`Scheduler.computeLocked`). With the production model, the deadline was exhausted during categorization, so every later call failed instantly — and because the AI logger reused that same expired context, the failure also failed to write its own log row, making the calls look like they'd silently stopped rather than like they were failing.

### 9.1 Per-call AI timeout + log visibility
- [x] `CloudflareProvider` gets its own bounded context per call (`AI_REQUEST_TIMEOUT`, default 30s) instead of inheriting the caller's remaining deadline
- [x] `http.Client` given an explicit `Timeout` (was `http.DefaultClient`, unbounded)
- [x] Log writes detached from the call's context (`context.WithoutCancel` + short timeout) so a timed-out call still records a failed row instead of vanishing from the AI log table
- [x] `AI_REQUEST_TIMEOUT` added to config + `.env.example`

### 9.2 Non-blocking digest homepage
- [x] `Scheduler` split into `Today` (blocking — used by digest send/preview) and `TodayOrTrigger` (non-blocking — used by the web homepage; kicks off a background compute on a cold cache)
- [x] Computes are single-flighted (`inFlight` channel guarded by the scheduler's mutex) so a background trigger and a concurrent blocking caller join the same run instead of duplicating AI calls
- [x] Homepage renders a "generating" banner (`DigestPageData.Generating`) while a background compute is in progress
- [x] Compute-wide ceiling widened from 3 min to 10 min (safe now that it's not blocking a request, and each AI call inside it is individually bounded)

### Phase 9 Verification
- [x] `go build ./...` && `go vet ./...` pass
- [x] Local smoke test: cold `GET /` returns in ~10ms (previously blocked for the duration of the compute); background compute completes and logs `digest: computing for N groups` / `computed overview=...`
- [x] Second `GET /` while warm serves from cache without triggering a duplicate compute (single `digest: computing` log line)
- [x] `POST /api/digest/refresh` and `GET /api/digest/preview` still block synchronously and return real content
- [ ] Verify in production: `summarize`/`digest` rows reappear in `/admin/logs`, and any real failures show up as failed rows instead of disappearing

---

## Phase 10: Fix newsletter ingestion — stable source identity (production incident)

**Symptom:** Newsletter subscription confirmed, emails visibly arrive in the
forwarding inbox, but no coherent source or article appears in the app — no error
visible anywhere in the app.

**Diagnosis (DONE — boundary-evidence method).** `wrangler tail` on `email-worker`
showed the Cloudflare Worker successfully POSTing every incoming newsletter and
getting `Ok`/200 back (three live TLDR emails observed). So the infra hypothesis
(WEBHOOK_SECRET / `cloudflared` / tunnel drift) is **ruled out** — the plumbing is
fine. The real defect is **semantic**: the webhook keys the source on
`payload.From` (`internal/ingestion/email/parser.go:33`), which is the SMTP
**envelope** sender — an ESP bounce/VERP address that rotates every send
(`bounces+…@em6054.thenewscc.com.br`, `0100019f…@dailyupdate.tldrnewsletter.com`).
`FindByEmailSender` never matches → every email creates a brand-new source. The
Worker already forwards all headers in `payload.Headers`, but the parser ignores
them. See PRD **F3.3**.

### 10.1 Diagnose — DONE
- [x] `wrangler tail` — Worker returns `Ok`/200 per email (infra ruled out)
- [x] Root cause identified: envelope-from rotation → per-email source creation
      (`parser.go` keys on `payload.From`; `payload.Headers` ignored)

### 10.2 Fix: stable newsletter identity (see PRD F3.3)
- [x] Migration `000013_source_identity` — added `identity VARCHAR(255)` to
      `sources` (nullable; stable newsletter key) + matching `Identity *string`
      field on `domain.Source`. **Not yet applied to any database** — run
      `make migrate-up` when your dev/prod DB is reachable (not run from this
      session: no local Docker daemon, and this shouldn't be run against a
      possibly-live DB without you at the wheel)
- [x] `email-worker` payload already carries `headers` — confirmed no Worker
      change needed
- [x] `internal/ingestion/email/parser.go` — `resolveSender()` reads
      `payload.Headers`: `List-Id` → `From:`-header address (via `net/mail.ParseAddress`)
      → envelope `from`; `Article.Author` now set from the `From:` header, not
      the envelope
- [x] Source `Name` derived from the `From:` header display name (e.g. "TLDR")
- [x] `internal/storage/source_repo.go` — added `FindByIdentity(ctx, key)`
      returning `(nil, nil)` on not-found (mirrors `FindByURL`); removed the now-
      unused `FindByEmailSender` (had the ErrNoRows-as-error shape that caused
      the bug)
- [x] `internal/ingestion/email/webhook.go` — looks up by identity; creates only
      on a genuine `nil` miss (a real lookup error now correctly 500s instead of
      silently creating a duplicate source)
- [ ] **Known gap, not closed:** no DB-level uniqueness constraint or upsert on
      `identity` — two near-simultaneous emails from a brand-new newsletter could
      still race into two sources. Same risk class as the pre-existing
      `FindByURL`-based OPML import flow (not a regression), but worth a `UNIQUE`
      index + `ON CONFLICT` later if it proves to matter in practice.
- [x] Unit tests (`internal/ingestion/email/parser_test.go`) cover: List-Id
      preferred, From-header fallback, envelope fallback, malformed From header,
      missing display name, and identity staying stable across the 3 real
      rotating envelope addresses captured live via `wrangler tail`

### 10.3 Close the observability gaps (independent of root cause) — DONE
- [x] `internal/storage/worker.go` — logs `worker: skipped duplicate article
      external_url=... title=...` when `repo.Create` returns `id == "", nil`
- [x] `internal/broker/consumer.go` `worker()` — NACK log now includes a
      truncated (300-char) message body so a DLQ'd message is diagnosable from
      `docker logs` alone
- [x] `cmd/agregado/main.go` — `b.DeclareTopology()`'s error is now checked and
      `log.Fatal`'d, matching every other startup step in `main()`

### Phase 10 Verification
- [x] `go build ./...` && `go vet ./...` pass
- [x] `go test ./...` passes — identity resolution unit-tested, including
      stability across the 3 real rotating envelope addresses from `wrangler tail`
- [ ] **Needs your environment:** apply migration `000013` (`make migrate-up`),
      then send 2+ real emails from the same newsletter end-to-end → confirm
      **one** source, named after the newsletter, with a real `Article.Author`
- [ ] Force a duplicate insert against a live DB → confirm the new dedup log line
- [ ] Force a consumer handler error → confirm the NACK log line identifies
      which message failed and why, without needing the RabbitMQ UI

---

## Phase 11: Content Hygiene (Clean Ingest + 24h Retention)

**Goal:** Stop CSS/HTML noise from reaching the AI, and stop articles accumulating
forever. See `docs/PRD.md` F10.

**Decisions:** clean at ingest (store clean `content`); hard-delete unsaved articles
older than 24h (keep `is_saved`); learning survives in the weight tables.

### 11.1 Clean content at ingest (F10a)
- [ ] Create `internal/textutil/clean.go` — `Clean(html string) string`: strip
      `<style>`/`<script>`/tags/inline CSS with `goquery`, then `go-readability` for
      main-content extraction
- [ ] Call `Clean` in `internal/ingestion/rss/parser.go` before building the article
- [ ] Call `Clean` in `internal/ingestion/email/parser.go` before building the article
- [ ] Derive `summary` from the cleaned body (not raw HTML)
- [ ] Verify: ingest a noisy feed (e.g. beehiiv/Hacker News) → stored `content` has no
      tag/CSS soup

### 11.2 24h retention job (F10b)
- [ ] Migration `000013_parent_on_delete_set_null` — drop + re-add
      `articles.parent_article_id` FK as `ON DELETE SET NULL`
- [ ] Add `ARTICLE_RETENTION_HOURS` (default `24`) and `RETENTION_SCHEDULE`
      (default hourly) to `internal/config/config.go` + `.env.example`
- [ ] Add `DeleteOlderThan(ctx, cutoff time.Time) (int, error)` to `ArticleRepo`
      (`DELETE ... WHERE ingested_at < $1 AND is_saved = false`)
- [ ] Register a cron job that runs **after** the digest and calls `DeleteOlderThan`
- [ ] Add `POST /api/retention/run` manual trigger (mirror `/api/digest/send`)
- [ ] Verify: unsaved old article deleted; `is_saved=true` article and weight tables untouched

### Phase 11 Verification
- [ ] Noisy feed ingested → `content` is clean readable text
- [ ] Retention job deletes only unsaved articles past the window
- [ ] `go build ./...` && `go vet ./...` pass

---

## Phase 12: Personalization — Implicit Feedback & Topic Cloud

**Goal:** Learn from opened links (not just votes), build a keyword-level "nuvem de
assuntos", and feed it back into scoring. See `docs/PRD.md` F11.

**Decisions:** AI-extracted keywords per article (new `keyword_weights` layer beside
`topic_weights`); opens captured via `/r/{id}` redirect; cloud both visualizes AND
biases the `Score` prompt.

### 12.1 DB Schema — migration `000014`
- [ ] `keyword_weights` (keyword PK, weight FLOAT default 1.0, updated_at)
- [ ] `article_keywords` (article_id FK ON DELETE CASCADE, keyword, PK(article_id, keyword))
- [ ] Run migration; verify tables exist

### 12.2 AI keyword extraction
- [ ] Add `ExtractKeywords(ctx, title, content string) ([]string, error)` to
      `internal/ai/provider.go`
- [ ] Implement in `internal/ai/cloudflare.go` (prompt: return 3–5 salient
      keywords/entities, one per line); add default system prompt + admin-editable entry
- [ ] Add `SetKeywords(ctx, articleID string, keywords []string) error` to storage
- [ ] Wire extraction at ingest (RSS poller + email webhook) after `Create`

### 12.3 Weight tables + repos
- [ ] Create `internal/storage/keyword_weights_repo.go` — `Upsert(ctx, keyword, delta)`
      (clamp 0.1–2.0), `Top(ctx, n) ([]KeywordWeight, error)`
- [ ] Extend the existing feedback handler: on 👍/👎, also upsert `keyword_weights`
      for the article's keywords (+0.1 / −0.1)

### 12.4 Open tracking redirect
- [ ] Add `GET /r/{article_id}` handler: look up article → bump its `keyword_weights`
      (+0.05) and tag `topic_weights` → `302` to the real `external_url`
- [ ] Change article title links to route through `/r/{id}` in: digest email template,
      web article/home templates, and (later) the reading feed
- [ ] Verify: clicking a link records the open and lands on the real URL

### 12.5 Score prompt injection
- [ ] Inject top-N `keyword_weights` into the `Score` prompt (alongside `topic_weights`)
- [ ] Verify: a `score` AI-log row shows the keyword weights block

### 12.6 Topic cloud UI
- [ ] Add `GET /cloud` handler → query `keyword_weights.Top(n)`
- [ ] Create `templates/cloud.html` — font-size-weighted keyword cloud
- [ ] Add nav link to the cloud page

### Phase 12 Verification
- [ ] Ingest an article → `article_keywords` rows created
- [ ] Click a link → `keyword_weights` bumped, open redirects correctly
- [ ] Vote 👍/👎 → both `topic_weights` and `keyword_weights` update
- [ ] `/cloud` renders top keywords sized by weight

---

## Phase 13: Vocabulário Agregado (Daily English Word)

**Goal:** Each daily digest suggests one useful English word (meaning + example),
drawn from the day's content, in both email and web. See `docs/PRD.md` F12.

**Decisions:** AI-generated from digest content at compute time; `vocab_history`
prevents repeats; renders via the shared `DigestView`.

### 13.1 DB Schema — migration `000015`
- [ ] `vocab_history` (id, word, meaning, example, shown_on DATE, created_at)
- [ ] Run migration; verify table exists

### 13.2 AI word suggestion
- [ ] Add `SuggestWord(ctx, summaries []string) (Word, error)` to
      `internal/ai/provider.go` (`Word{Word, Meaning, Example}`)
- [ ] Implement in `internal/ai/cloudflare.go`; pass recent `vocab_history` words to
      avoid repeats; add default system prompt + admin-editable entry
- [ ] Add `vocab_history` repo: `RecentWords(ctx, n)`, `Insert(ctx, word)`

### 13.3 View integration
- [ ] Call `SuggestWord` during the digest compute (before the 24h retention job);
      persist to `vocab_history`; soft-fail (omit on AI error)
- [ ] Add `WordOfDay *Word` to `digest.DigestView` (populated in `BuildView`)
- [ ] Render the word block in `internal/digest/templates/digest.html` (email) and the
      web home template
- [ ] Verify: same word block appears in email and on `GET /`, and doesn't repeat

### Phase 13 Verification
- [ ] Digest compute produces + persists a word
- [ ] Word block renders in email and web, identical
- [ ] AI failure omits the block without breaking the digest

---

## Phase 14: Bookmarks Reading Feed (scrollable)

**Goal:** Flip through bookmarked articles in a full-screen, vertically-scrollable
feed. See `docs/PRD.md` F13.

**Prerequisite:** the bookmark API + `is_saved` from **Phase 4.1 / 4.8** must be done
first (`POST /api/bookmarks`, `GET /api/bookmarks`, `is_saved`, `FindSaved`).

**Decisions:** bookmarks-only source; text-only cards (title + source + summary); taps
open through the F11 `/r/{id}` redirect.

### 14.1 Reading feed
- [ ] Add `GET /feed` handler → reuse `ArticleRepo.FindSaved` (paginated)
- [ ] Create `templates/feed.html` — full-screen vertical scroll of saved-article cards
      (title + source + summary); card tap → `/r/{article_id}`
- [ ] HTMX infinite scroll / pagination for the feed
- [ ] Empty state when there are no bookmarks
- [ ] Add nav link to the feed

### Phase 14 Verification
- [ ] `/feed` renders saved articles as scrollable cards
- [ ] Tapping a card records an open and opens the real article
- [ ] Empty state shows when no bookmarks exist
- [ ] `go build ./...` && `go vet ./...` pass

---

## Phase 15: Article Open Redirect + Reader Page

**Goal:** Fix the "clicking an article does nothing" bugs and give newsletters a
home to open into. See `docs/PRD.md` **F5.3**.

**Root causes (verified):** (a) newsletter `external_url = newsletter:<uuid>` →
`html/template` emits `#ZgotmplZ`; (b) the title `<a>` carries `href` + `hx-post`
on one click and htmx `preventDefault`s anchors, so navigation never fires (breaks
RSS links too); (c) newsletters have no external page (body is in `Content`).

**Decisions:** route all title links through `GET /r/{id}` (record open + mark read
→ 302); newsletters redirect to an in-app reader `GET /articles/{id}` backed by the
existing `ArticleRepo.GetById`. Pulls a subset of F11 (`/r/`) + F5 detail forward.

### 15.1 Open-tracking redirect
- [x] `GET /r/{article_id}` handler in `internal/api` — `GetById` → mark read →
      `302`: RSS/manual → `external_url`; newsletter (`external_url` has `newsletter:`
      scheme) → `/articles/{id}`
- [x] Route in `internal/api/server.go` (top-level `GET /r/{id}`)
- [ ] (Defer F11 keyword/topic weight bumps to Phase 12 — this ships record+redirect)

### 15.2 Reader page
- [x] `GET /articles/{id}` handler → `GetById` → render reader template
- [x] `templates/reader.html` — renders `Article.Content` (plain text `pre-wrap`;
      Markdown once F14 lands), title, source name, date; "Read the original" link
      only for non-newsletter articles (guards against re-leaking the `newsletter:`
      scheme into `href`)
- [x] Add `GetById` to the `ArticleRepository` interface used by `ArticleHandler`
      (`internal/api/articles.go`)

### 15.3 Repoint title links (kills ZgotmplZ + the htmx-preventDefault bug)
- [x] `templates/articles.html`, `templates/article_list.html`, web
      `templates/digest.html`, email `internal/digest/templates/digest.html`,
      `templates/bookmarks.html` — changed `href="{{ .ExternalURL }}"` +
      `hx-post=".../read"` to `href="/r/{{ .ID }}"` (email link is absolute:
      `{{ $.DigestURL }}/r/{{ $item.ID }}`, since `$` is needed to reach the
      template root from inside the nested `range`); read-marking now happens
      in `/r/{id}`
- [x] Kept the optimistic `is-read` UI styling (`hx-on:click` adds the class),
      without any `hx-post`/`hx-trigger` intercepting the click

### Phase 15 Verification
- [x] `internal/api/articles_test.go` (new): table-driven tests for `Open`
      (RSS redirect, newsletter redirect, 404 on unknown id, mark-read failure
      doesn't block the redirect) and `GetPage` (renders title/source/original
      link for RSS, omits the original-link + leaks no `newsletter:`/`ZgotmplZ`
      for newsletters, 404 on unknown id) — fakes satisfy `ArticleRepository`
      and `NavQuerier`; `GetPage` cases `os.Chdir` to the repo root since
      `render()` resolves templates relative to cwd
- [x] Live-verified against the real local stack (already-running Postgres/
      RabbitMQ/Mailpit): a real RSS article's `/r/{id}` 302'd to its
      `external_url` and was marked read in the DB; a temporary newsletter-scheme
      test row (inserted then deleted) 302'd to `/articles/{id}` and rendered its
      full body with no `ZgotmplZ` and no leaked `newsletter:` URL; bad ids on
      both routes returned clean 404s with the server still healthy afterward
- [x] `go build ./...`, `go vet ./...`, `go test ./...` all pass
- [ ] **Not verified live:** the digest-email `/r/{id}` link, because the local
      DB currently has 0 articles clearing the relevance bar (`/api/digest/preview`
      returned "0 of 0 stories") — no live data to click through. The email
      template did parse successfully at process startup and `go vet` confirms
      it's syntactically valid, but this is template-parse confidence, not a
      click-through confirmation. Re-check next time real digest candidates exist.

---

## Phase 16: Newsletter AI Content Budget (cap + CSS strip + model) — CLOSED

> **Closed at the lean core.** The deferred round it planned (Markdown conversion,
> `distilled_content`, `Compress`) is **superseded by Phase 17** — see 16.1/16.2 below.

**Goal:** Let the AI score/categorize/summarize newsletters from their real
substance, not the first 500 chars of CSS + boilerplate. See `docs/PRD.md` **F14**.

**Root causes (verified):** `maxPromptContentChars = 500` (`internal/ai/cloudflare.go`)
truncates Score/Categorize/Reason; `textutil.Clean` strips tags but not
`<style>`/`<script>` *contents*; the configured model `@cf/moonshotai/kimi-k2.7-code`
is a **code** model.

**What shipped (16.0):** the two biggest, lowest-risk levers — configurable
`AI_MAX_CONTENT_CHARS` (was a hardcoded 500), `<style>`/`<script>` *content* stripping
in `textutil.Strip`, and the `.env` model corrected to an instruct model. No new
dependency, migration, or AI call.

**What did not ship:** the 2-stage Markdown + cheap-model-compression pipeline. Phase
17 covers that ground for RSS *and* newsletters and reaches different conclusions —
notably that distillation should be **algorithmic**, not an AI call, and that the
pipeline belongs in a **dedicated `articles.enrich` stage** rather than inline in the
storage worker.

### 16.0 Lean core — DONE (this session)
- [x] `internal/textutil/textutil.go` — `Strip` now removes whole
      `<style|script|head|noscript>…</…>` blocks (contents included, not just the
      tags) via a pre-pass regex before the existing tag-strip. RE2 has no
      backreferences, so the closing-tag match is an alternation over the same
      four names rather than a captured backreference. `internal/textutil/textutil_test.go`
      (new) covers CSS/script leakage, mixed case, multiple blocks, plaintext
      passthrough, entity unescaping, and rune-safe truncation
- [x] Retired the hardcoded `maxPromptContentChars = 500` — added
      `AI.MaxContentChars int` (`AI_MAX_CONTENT_CHARS`, default `8000`) to
      `internal/config/config.go`; `CloudflareProvider` takes a `maxContentChars`
      constructor param (`<=0` → `defaultMaxContentChars = 8000`, mirrors the
      `requestTimeout` guard); `Categorize`/`Reason`/`Score` use `p.maxContentChars`
- [x] `.env` `AI_MODEL` corrected from the Kimi **code** model to
      `@cf/google/gemma-4-26b-a4b-it` (an instruct model — matches the config
      default, which was already correct; only `.env` was wrong). Added
      `AI_MODEL` + `AI_MAX_CONTENT_CHARS` to `.env.example` (both were previously
      undocumented there)
- [x] `go build ./...`, `go vet ./...`, `go test ./...` pass
- [x] Live-verified against the real local stack: temporarily bumped one real
      unread article into the digest lookback window (`published_at`/`ingested_at`
      → now, `relevance_score` → above the min bar) and triggered
      `POST /api/digest/refresh` — fresh `categorize`/`summarize`/`digest` rows in
      `ai_logs` show `model = @cf/google/gemma-4-26b-a4b-it`, all `success = true`.
      Reverted the article to its original values afterward.
- [ ] **Not observed live:** a dramatic CSS-soup before/after, because this local
      DB's RSS sources only ever populate `Article.Summary` (clean, short feed
      descriptions) — no article currently has `Content` with real HTML/CSS. The
      bug this fixes is specifically a newsletter problem (no newsletter articles
      exist in this DB right now). The fix itself is proven by the new unit tests,
      which encode the exact `<style>…</style>` leak scenario directly.

### 16.1 / 16.2 — SUPERSEDED BY PHASE 17

> The deferred round (Markdown conversion, `distilled_content`, `Provider.Compress`)
> is **superseded by Phase 17**, which covers the same ground for RSS *and*
> newsletters and reaches two different conclusions:
> - distillation is **algorithmic** (`textutil.Distill`), not a cheap-model AI call —
>   a third AI call would compound the enrichment bottleneck, and *extractive*
>   summarization is classically an algorithm anyway;
> - the pipeline runs in a **dedicated `articles.enrich` stage**, not inline in the
>   storage worker.
>
> Do not implement 16.1/16.2 as written — see Phase 17 and `docs/PRD.md` F15.

### Phase 16 Verification
- [x] Cap + model corrected, live-verified (see 16.0)
- [ ] A `score`/`categorize` row in `/admin/logs` shows dense distilled content —
      no CSS/`<style>` soup, not truncated at 500 chars (needs a real newsletter
      article to observe; deferred round)
- [ ] Newsletter tags/scores visibly improve vs. title-only classification
- [ ] A compression failure degrades gracefully (article still scored on fallback)
- [ ] The reader page (Phase 15) shows the structured Markdown body
- [x] `go build ./...` && `go vet ./...` pass

---

## Phase 17: RSS Article Content Fetching + Enrichment Stage

**Goal:** Score/tag/summarize RSS articles from the *real article body* fetched via
`item.Link`, not the feed's teaser — and record content provenance so degradation is
countable. See `docs/PRD.md` **F15** and `docs/rss-content-fetching-plan.md`.

**Root causes (verified):** most feeds omit `<content:encoded>`, so `Content` is nil
(`poller.go:104-112`) and `worker.go:49-54` silently falls back to `Summary` (the
`<description>` teaser). `Summarize`/`Digest` (`cloudflare.go:195,206`) build prompts
from **titles only** — `AI_MAX_CONTENT_CHARS` reaches just 3 of 5 provider methods.
`word_count`/`estimated_read_minutes` have never been written by any code, yet the
digest renders a read-time from them.

**Decisions:** always fetch (feed = fallback); dedicated `articles.enrich` stage after
`Create` (newsletters ride it free); message carries `{article_id}` only; readability →
Markdown; algorithmic distillation (no `ai.Compress`); `content_source` provenance
column; honest UA + per-host serialization, no robots.txt v1; roundup link-following
and clustering explicitly out of scope. **Supersedes Phase 16.1/16.2.**

### 17.1 Schema + domain — migration `000014_article_content_source` — DONE
- [x] `migrations/000014_article_content_source.up.sql` / `.down.sql` — add
      `distilled_content TEXT` and `content_source VARCHAR(32) CHECK (content_source IN
      ('fetched','feed_content','feed_description','newsletter'))` to `articles`
- [x] `internal/domain/article.go` — add `DistilledContent *string`,
      `ContentSource *string` with `db:` tags (`FindUnreadSince` uses `SELECT *`
      (`article_repo.go:141`), so these scan through with no query edits)
- [x] Add `BestText() string` to `domain.Article` — precedence
      `DistilledContent → Content → Summary`. Unit-tested (`article_test.go`)
- [x] Replace the hand-written fallbacks in `worker.go:49-54` **and** `ranker.go:76-82`
      with `BestText()` (already duplicated; this phase would make it a third copy)
- [x] Run migration; verify columns + CHECK exist

### 17.2 Fetcher — new `internal/ingestion/fetch/` — DONE
- [x] `Fetcher.Fetch(ctx, url) (Result, error)`, `Result{Markdown, Title, Byline, Length}`
- [x] Own `http.Client`: explicit `Timeout`, `io.LimitReader` size cap,
      `Content-Type: text/html` gate, redirect cap ~5
- [x] Honest UA `Agregado/1.0 (+https://github.com/felipeafreitas/agregado)`;
      per-host serialization (mutex keyed on `url.Host`) + small delay;
      403/429 → `ErrBlocked`, never retry
- [x] Pipeline: fetch → readability → cleaned HTML → `JohannesKaufmann/html-to-markdown/v2`
      → Markdown. **Library swap from plan:** `go-shiori/go-readability` is deprecated
      upstream (points at a successor); used **`codeberg.org/readeck/go-readability/v2`**
      instead — same Mozilla-Readability-derived approach, actively maintained, verified
      to resolve cleanly against this repo's `go.mod` before committing
- [x] Title/Byline **fill gaps only**: `readability.Byline()` fills `Author` only when the
      feed left it nil; `Title` is never overridden by the fetched page's title
- [x] Config-injected timeout with a `<= 0 → default` guard (mirrors `cloudflare.go:19-31`)
- [x] Unit tests against `httptest.Server`: real article (nav/footer correctly excluded),
      consent-wall-length fallback, 403, 429, unsupported content-type, oversized body
      (capped, doesn't hang), per-host serialization (no concurrent requests observed)

### 17.3 Distiller — `internal/textutil/distill.go` — DONE
- [x] `Distill(markdown string, max int) string` — keep headings + lede + first
      sentence of each section, drop boilerplate (bare links/images, short button-like
      lines), cap to `max` runes
- [x] Dependency-free + deterministic, matching existing `textutil` style; unit-tested
      (`distill_test.go`)

### 17.4 Enrich stage — DONE
- [x] `internal/broker` — declared `articles.enrich` exchange + queue in
      `DeclareTopology()`, bound to the existing dead-letter exchange like `articles.store`.
      Confirmed live via `rabbitmqctl list_exchanges`/`list_queues`
- [x] `internal/broker/consumer.go` — `Consume` now takes `(queueName, prefetch,
      numWorkers, handler)`. **Fixed the latent bug predating this feature:**
      `Qos(1, 0, false)` with a single consumer tag was starving the 5 goroutines —
      ingest was serial. Storage keeps prefetch 1 (fast, Create-only); enrich gets 5/5
- [x] `internal/storage/worker.go` — slimmed to Create-only; publishes `{article_id}`
      (`enrichTrigger`) to `articles.enrich` on non-duplicate `Create`. `Score`/`Reason`
      moved into `enrich.go`
- [x] `internal/storage/enrich.go` (new) — `NewEnrichHandler`: `GetById` → skip fetch if
      `external_url` has the `newsletter:` scheme (Phase 15) → else `Fetch` → quality gate
      (`resolveContent`: keep whichever of fetched/feed is longer after `textutil.Strip`)
      → `Distill` → compute `word_count`/`estimated_read_minutes` → `UpdateContent` →
      `Score` → `Reason` (gated on `>= minScore`)
- [x] Added `ArticleRepo.UpdateContent(ctx, id, content, distilled, source string, wordCount, readMinutes int) error`
- [x] **Failure semantics implemented as planned:** fetch/quality-gate misses degrade to
      feed content and keep going (not an error); only `GetById`/`UpdateContent` errors
      NACK; AI failures soft-fail + ACK — all three paths observed live (see Verification)
- [x] Left `UpdateSummary` (zero callers) alone, as planned

### 17.5 Summarize sees substance — DONE
- [x] `internal/ai/cloudflare.go` `Summarize` — per article emits title + `RelevanceReason`
      ("Why it matters:") + a `textutil.Clean`'d excerpt of `BestText()` ("Excerpt:"),
      budgeted per article against `p.maxContentChars` (capped at 400 chars/article,
      shrinking further when `len(articles)` is large)
- [x] No new plumbing needed, as predicted: every digest article already has a
      `relevance_reason` from ingest
- [x] Updated the `summarize` default prompt in `internal/ai/prompts.go` for the new
      input shape

### 17.6 Config + wiring — DONE
- [x] New `Fetch` struct in `internal/config/config.go`: `FETCH_TIMEOUT` (15s),
      `FETCH_MAX_BYTES` (5MB), `FETCH_MIN_CONTENT_CHARS` (500), `FETCH_USER_AGENT`,
      `DISTILL_MAX_CHARS` (2000); documented in `.env.example`
- [x] `cmd/agregado/main.go` — builds the fetcher, wires the enrich handler, adds a
      second `broker.NewConsumer` (own channel, own Qos) consuming `articles.enrich`
- [x] **Dependency risk resolved:** `golang.org/x/net` bumped `v0.4.0 → v0.55.0` (also
      `x/sync`, `x/text`) — clean resolution, no conflicts, confirmed with `go build ./...`
      before writing any code against the new libs

### 17.7 Backfill — DONE
- [x] `POST /api/admin/enrich` (`AdminHandler.EnrichBackfill`) — `FindUnenriched`
      (new repo method, `content_source IS NULL`) → republish each onto `articles.enrich`.
      Mirrors the `/api/digest/refresh` + `/api/backup/send` handler shape

### Phase 17 Verification
- [x] `go build ./...` && `go vet ./...` && `go test ./...` — all green
- [x] Unit tests: `Distill` (7 cases), `Fetcher` against `httptest.Server` (7 cases,
      including per-host serialization and oversized-body capping), `BestText()`
      (6 cases) — all pass
- [x] **Live**, against the real local stack (Postgres + RabbitMQ, real network fetches):
      `POST /api/admin/enrich` on 45 pre-existing articles →
      `content_source` distribution **30 fetched / 15 feed_description**, zero NULL;
      `word_count`/`estimated_read_minutes`/`distilled_content` populated on all 45
      (previously 0). Observed real `ErrBlocked` (news.ycombinator.com 403s bots) and
      `ErrThinContent` (JS-rendered pages) fallbacks in the logs, both degrading cleanly
      to feed content as designed
- [x] `/admin/logs`: a live `score` row shows real Markdown prose (headings, bold,
      code fences) from a 68KB fetched article — no CSS, nowhere near truncated at 500
      chars (**the observation Phase 16 shipped without being able to make**)
- [x] `POST /api/digest/refresh` → live `summarize` row shows title + "Why it matters:"
      (the article's `relevance_reason`) + "Excerpt:" (real Markdown from
      `distilled_content`) — confirmed against the pre-fix titles-only shape
- [x] Enrich queue drained under real network conditions without hanging or crashing;
      one AI call timeout (`context deadline exceeded` against Cloudflare) soft-failed
      and logged correctly instead of blocking the queue
- [ ] **Not exercised:** true multi-host concurrency at scale (5 fetches in flight
      across 5 distinct hosts simultaneously) — the live backfill ran through real
      articles across many different hosts, so per-host serialization was exercised,
      but wall-clock concurrency across hosts wasn't separately measured

### Phase 17 Known gaps (deliberate)
- [ ] **No robots.txt** — proportionate for a single-user, subscriber-driven,
      once-per-URL fetch. Revisit if this ever serves more than one person
- [ ] **No enrichment retry** — a transient fetch failure leaves an article permanently
      on feed content. `content_source` makes it countable; the backfill endpoint
      re-drives it manually. Delayed retry needs a TTL+DLX trick RabbitMQ won't do natively
- [ ] **Roundups ingest as roundups** — link extraction / `parent_article_id` children
      stay deferred (Phase 2.5 + 5.6.3), but 17.2 builds the `Fetch` primitive they were
      always blocked on
- [ ] **`ai.Compress` not built** — if `Distill` proves too lossy, `distilled_content`
      is already the right seam to swap it in behind

---

## Phase 18: Close the Personalization Loop

**Goal:** Make 👍/👎 feedback and per-article tagging actually work — both were fully
built but silently non-functional. See `docs/PRD.md` **F16**.

**Root causes (verified live before any fix):** `topic_weights` 0 rows,
`article_feedback` 0 rows, `article_tags` 0 rows (8 tags defined), and every `score`
prompt in `ai_logs` ended with `Topic interest weights (...):` followed by nothing.
Five independent breaks, stacked: (1) the ranker assigned tags in memory only, no
`SetTags` existed; (2) → `article_tags` stayed empty; (3) → `GetById` never loaded
tags anyway (`db:"-"`, bare `SELECT *`); (4) → the 👍/👎 buttons POSTed to a route
that didn't exist, and the *previous* `GET /api/feedback` + HMAC route was itself
unreachable (its token generator had already been deleted); (5) →
`TopicWeightsRepo.Upsert`'s insert branch hardcoded `weight = 1.0`, discarding the
first vote on any topic. Also: the UI reported success (`hx-on::after-request`)
unconditionally, and untracked tags meant every digest compute re-categorized every
article from scratch (72 `categorize` calls logged for 45 articles).

**Decisions:** move `Categorize` from the digest ranker into the `articles.enrich`
stage (Phase 17) so it persists once per article via a new `ArticleRepo.SetTags`;
delete the dead GET+HMAC feedback route and replace with same-origin
`POST /api/articles/{id}/feedback`; fix the first-vote math; fix the false-success UI
report; also fix `?sort=relevant|recent` (same defect class — control wired to
nothing). **No migration needed** — every table already existed.

### 18.1 Persist tags — `internal/storage/article_repo.go`
- [x] Extract `FindUnreadSince`'s batch tag-load into a private
      `loadTags(ctx, ids []string) (map[string][]domain.Tag, error)`
- [x] Reuse `loadTags` in `GetById` too — it never loaded tags before, despite `Tags`
      being a struct field
- [x] Add `SetTags(ctx, articleID string, tagIDs []string) error` — delete-then-insert
      inside a transaction, so a reader never observes a half-written state

### 18.2 Move `Categorize` to the enrich stage
- [x] `internal/digest/ranker.go` — drop the lazy-categorize block, the `Categorizer`
      interface, and the `NewRanker` param; ranker now only reads persisted tags
- [x] `internal/storage/enrich.go` — add `Categorizer`/`TagQuerier`/`TagSetter` deps;
      `categorizeArticle` slots between `UpdateContent` and `Score`; soft-fails
      (unrecognized slug, AI error, or persist error all just leave the article
      uncategorized, matching how `Score`/`Reason` already degrade)
- [x] `cmd/agregado/main.go` — updated both `NewRanker` and `NewEnrichHandler` call sites
- [x] Pre-existing articles catch up via the already-existing `POST /api/admin/enrich`
      backfill (Phase 17)

### 18.3 Fix the first-vote math — `internal/storage/topic_weights_repo.go`
- [x] Insert branch now applies `GREATEST(0.1, LEAST(2.0, 1.0 + delta))` — same
      clamp formula as the conflict branch, instead of hardcoding `weight = 1.0`

### 18.4 Replace the feedback endpoint
- [x] Rewrote `FeedbackHandler.Handle` as `POST /api/articles/{id}/feedback`
      (`internal/api/feedback.go`), JSON body `{"vote":"up"|"down"}`, `204` on success
- [x] Deleted the HMAC/token machinery and the `secret` field/param entirely
- [x] `server.go` — registered the new route, removed `GET /api/feedback`
- [x] `templates/digest.html` — `hx-vals` changed `direction` → `vote` to match

### 18.5 Stop the UI reporting false success
- [x] `templates/digest.html` — `hx-on::after-request` now branches on
      `event.detail.successful` instead of firing the same message unconditionally

### 18.6 Make `?sort=` real
- [x] `ArticleRepository.List`/`ListBySource` (+ `article_repo.go` impl) take a
      `sort` param; `sortClause` whitelists to one of two fixed `ORDER BY` literals
      (`relevant` / default `recent`) — never interpolates the value into SQL
- [x] `articles.go` — `ListPage`, `SearchPage`'s no-query branch, and the JSON
      `List` handler all thread the query param through
- [x] Fixed `articles_test.go`'s fake to match the new interface signature

### 18.7 Tests
- [x] `internal/storage/article_repo_test.go` — `sortClause` (relevant/recent/empty/
      SQL-injection-shaped input, confirming it never reaches the query as text)
- [x] `internal/api/feedback_test.go` — up vote, down vote, invalid vote value,
      malformed JSON, repo error, untagged article (no weight bump but vote still
      recorded) — all via fakes, reusing `fakeArticleRepo` from `articles_test.go`

### Phase 18 Verification
- [x] `go build ./...` && `go vet ./...` && `go test ./...` — all pass
- [x] Live: `article_feedback` **0 → 1 row**; `topic_weights` **0 → 1 row at 1.1**,
      not 1.0 — confirms the first-vote fix (18.3) applies correctly on insert
- [x] Live: a `score` prompt captured after the vote reads
      `Topic interest weights (...): - tech: 1.1` — real content under the header
      that previously ended with nothing
- [x] Live: `article_tags` **0 → 5 rows** (first ever) after forcing 5 articles back
      through the enrich stage; re-enriching the same article twice more produced
      **zero** additional `categorize` calls and no duplicate `article_tags` row —
      the "skip already-tagged articles" guard (`enrich.go`'s `categorizeArticle`)
      works, closing the 72-calls-for-45-articles cost bug
- [x] Live: `/articles?sort=relevant` vs `?sort=recent` return genuinely different
      first-page orderings (confirmed via distinct article ID sequences)
- [ ] **Not click-tested:** the false-success UI fix (18.5) — Chrome extension
      wasn't connected this session. Verified instead by (a) confirming the
      `/feedback` endpoint returns a real `204`/`4xx`/`5xx` for htmx's
      `event.detail.successful` to key off, and (b) the exact same
      `event.detail.successful ? ... : ...` pattern already exists and works for
      the neighboring "Send email" button on the same page/line style
      (`templates/digest.html`)
- **Disclosure:** the live verification's vote (`article_feedback`/`topic_weights`)
  and 5 categorizations (`article_tags`) are real rows, not reverted — they're
  genuine proof the mechanism works, not corrupted test data, and the weight bump
  (1.0 → 1.1 on `tech`) is small enough not to distort future scoring meaningfully.
  Left in place, same as Phase 17's backfill was.

---

## Post-MVP Features (Pick as desired)

### Multi-Content Type Support
- [ ] Create migration to rename `articles` → `content_items`
- [ ] Add `content_type` column with CHECK constraint ('article', 'video', 'audio', 'pdf')
- [ ] Add nullable type-specific fields (`duration_seconds`, `page_count`, `thumbnail_url`)
- [ ] Update domain entities and repositories
- [ ] Create URL metadata fetcher service
- [ ] Integrate YouTube API for video metadata
- [ ] Add web scraping for generic video/audio pages
- [ ] Add PDF parsing for page count extraction
- [ ] Update digest template to show mixed content types
- [ ] Add content type filter to web UI

### Other Features
- [ ] Content-based deduplication (SimHash)
- [ ] Read time estimation
- [ ] AI-based tag classification (auto-assign tags based on content)
- [ ] AI summarization (for articles - separate from social)
- [ ] Phrase-level filtering
- [ ] Smart scheduling
- [ ] Local HTTPS for the dev server (e.g. `mkcert` + a local reverse proxy) —
      eliminates Chrome's "insecure download blocked" warning on any file
      download served from `http://localhost` (surfaced by OPML export, see
      Phase 4.10). Not needed for now — low priority, `docs/TODO.md` tracker only

---

## Learning Notes

Use this section to jot down concepts learned during implementation:

### RabbitMQ Concepts
-

### Go Patterns
-

### PostgreSQL
-

### Other
-
