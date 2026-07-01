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
- [ ] **Within tag, cluster by AI-detected topic** (Phase 5.5 — deferred)
- [ ] Apply relevance scoring to filter low-value articles (Phase 5.5 — deferred)
- [ ] Limit to configurable max articles (`maxArticles` field exists, not yet enforced)

### 3.2 Email Generation
- [x] Create `internal/digest/generator.go`
- [x] Create HTML email template with **Tag → Articles** structure (topic clustering deferred to Phase 5.5)
- [ ] Include AI-generated topic summaries in template (deferred to Phase 5.5)
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
- [ ] `POST /api/articles/{id}/bookmark` — toggle `is_saved` on article (star button in list)
- [ ] `POST /api/bookmarks` — save arbitrary URL as article (fetch title/summary)
- [ ] `DELETE /api/bookmarks/{id}` — remove bookmark
- [ ] `GET /api/bookmarks` — list saved articles

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
- [ ] Article detail page
- [x] Read/unread toggle (HTMX partial)
- [x] Filter by source dropdown
- [ ] Date range filter
- [ ] Sort by `?sort=relevant|recent` — wire `Sort` param in `ListPage` handler and pass to template

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

### 4.8 Bookmarks
- [ ] Add `is_saved BOOLEAN DEFAULT false` column to `articles` (migration)
- [ ] Add `IsSaved bool` to `domain.Article`
- [ ] `GET /bookmarks` page — renders `bookmarks.html` with saved articles
- [ ] Bookmark list query in `ArticleRepo` (`FindSaved`)
- [ ] Bookmark item needs `.SourceName`, `.SavedAt`, `.IsManual` — define a `BookmarkView` struct or add fields

### 4.9 Daily Digest Home Page
- [ ] Create `templates/home.html` — renders today's digest as a web page (uses `.digest-*` CSS classes already in `layout.html`)
- [ ] `GET /` route — query today's top articles, render home template
- [ ] Handler fetches articles since midnight, groups by tag (reuse ranker), passes to template

### 4.10 Source Backup & Restore
- [ ] Add `GET /api/sources/export` — returns all sources as OPML (RSS standard; importable into any feed reader)
- [ ] Add `POST /api/sources/import` — accepts an OPML file, bulk-creates missing sources (skip duplicates by URL)
- [ ] Add Export button to Sources page → triggers file download
- [ ] Add Import file input to Sources page → POSTs OPML, refreshes list

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
- [ ] `Categorize` integration — auto-assign tags to untagged articles at digest time
- [ ] Create `internal/ai/relevance.go` — score article relevance using blocklist
- [ ] Integrate blocklist from preferences table (`key='blocklist'`)

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

### 5.6.2 AI Scoring at Ingest
- [ ] Add `Score(ctx, title, content string, topicWeights map[string]float64) (int, error)` to `internal/ai/provider.go`
- [ ] Implement `Score` in `internal/ai/cloudflare.go` (prompt: rate 1–5 for quality + global importance, return integer only)
- [ ] Add `UpdateRelevanceScore(ctx, id, score)` to `internal/storage/article_repo.go`
- [ ] Wire `Score` call into RSS poller after `Create`
- [ ] Wire `Score` call into email webhook handler after article save
- [ ] For newsletter articles: also call `Summarize` to populate `summary` field

### 5.6.3 Newsletter Link Extraction
- [ ] Create `internal/newsletter/extractor.go` — `ExtractLinks(html string) []string` using goquery
  - Filter: `http/https` only; skip URLs containing `unsubscribe`, `pixel`, `mailto:`, social share links
- [ ] Create fetch helper in same package — `FetchArticle(ctx, url) (title, content string, err error)` using go-readability
- [ ] Wire extraction into email webhook handler: after saving newsletter article, extract links → fetch → create child Articles with `parent_article_id` set → score each

### 5.6.4 Ranker Update
- [ ] Add `DigestCap int` (`DIGEST_CAP`, default `20`) and `MinRelevanceScore int` (`DIGEST_MIN_SCORE`, default `3`) to `internal/config/config.go` `Digest` struct
- [ ] Update `GetDigestArticles` in `internal/digest/ranker.go`:
  - Filter: `relevance_score >= MinRelevanceScore OR relevance_score IS NULL`
  - Sort: score DESC NULLS LAST, published_at DESC
  - Limit: DigestCap

### 5.6.5 Feedback Endpoint
- [ ] Create `internal/storage/feedback_repo.go` — `Create(ctx, articleID, vote)` inserting into `article_feedback`
- [ ] Create `internal/storage/topic_weights_repo.go` — `Upsert(ctx, topic string, delta float64)` (clamp weight 0.1–2.0)
- [ ] Add `GET /api/feedback` endpoint to `internal/api/server.go`:
  - Params: `article_id`, `vote` (`up|down`), `token`
  - Validate HMAC-SHA256 token (signed with `WEBHOOK_SECRET`, message = `article_id:vote`)
  - Insert feedback row
  - Fetch article tags → upsert topic_weights (up: +0.1, down: -0.1)
  - Return HTML confirmation page

### 5.6.6 Digest Template — Feedback Links
- [ ] Add a `DigestArticle` wrapper struct (or extend `domain.Article`) with `UpToken` and `DownToken` string fields
- [ ] Generate HMAC tokens per article in `internal/digest/generator.go` before template rendering
- [ ] Update `internal/digest/templates/digest.html` — add per-article: relevance score badge, 👍 link, 👎 link (separate from article title link)

### Phase 5.6 Verification
- [ ] Insert article with `relevance_score = 1` → confirm excluded from `/api/digest/preview`
- [ ] POST newsletter email → confirm newsletter article saved + child articles created from extracted links
- [ ] `/api/digest/preview` returns ≤ DigestCap articles, all score ≥ MinRelevanceScore (or unscored)
- [ ] Click 👍 feedback URL → `article_feedback` row created, `topic_weights` upserted
- [ ] Confirm topic weight used in subsequent `Score` prompt

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
- [ ] Create `internal/tmplfunc/funcmap.go` — move `funcMap` (`add`, `excerpt`, `dots`, `scoreLabel`) + `excerptChars` const out of `internal/api/render.go`; export as `tmplfunc.Map` (depends only on `internal/textutil`)
- [ ] Update `internal/api/render.go` — use `tmplfunc.Map` at both `.Funcs(...)` call sites; drop the moved var/const

### 8.2 Shared view-model — into `internal/digest`
- [ ] Create `internal/digest/view.go` — move `DigestGroupView`/`DigestItemView` from `api/digest.go`; add `DigestView` (Greeting, DeliveryTime, Date string, Intro, Groups)
- [ ] Add `BuildView(computed ComputedDigest, sourceNames map[string]string) DigestView` (Position/SourceName/Topic mapping + greeting/date formatting; plain map, no repo interface → no cycle)
- [ ] Slim `internal/api/digest.go` `HomePage` — build sourceMap → `digest.BuildView(...)` → render `{ DigestView; Nav }`; verify web `/` output unchanged

### 8.3 Config + wiring
- [ ] Add `BaseURL string \`env:"PUBLIC_BASE_URL"\`` to `config.Digest`
- [ ] Inject source-name map + base URL into the email path (generator/scheduler); mirror `api/digest.go` source lookup
- [ ] Update `cmd/agregado/main.go` (`NewDefaultGenerator` call) and `.env.example`

### 8.4 Email generator renders shared view
- [ ] Register `tmplfunc.Map` when parsing the embedded email template
- [ ] `Render` builds `DigestView` via `BuildView`, decorates items with absolute `UpURL`/`DownURL` (existing `tokenFor` + `BaseURL`) + footer browse URL (email-specific item struct embedding `DigestItemView`)
- [ ] Keep/lightly enrich the plain-text fallback

### 8.5 Email template rewrite (email-safe, mirrors web)
- [ ] Rewrite `internal/digest/templates/digest.html` — full document (`<!DOCTYPE html>…<head><style>…</style></head><body>`), table-based ~650px column
- [ ] Email-safe CSS mirroring `.digest-*`: hardcoded hex (no `var()`), table/inline-block (no flexbox), `Georgia,serif` + monospace fallbacks
- [ ] Structure parallels web: deliver line, kicker, H1 greeting, count line, intro, per-group topic-rule + summary, per-article meta (num/src/date/read-time/`dots`), title link, `excerpt`, ▲/▼ feedback links, footer browse link
- [ ] Decide: `.digest-count` (web uses Nav counts email lacks) → compute simple total or reword
- [ ] Optional polish: `<meta name="color-scheme" content="light dark">`

### Phase 8 Verification
- [ ] `go build ./...` && `go vet ./...`
- [ ] Web `/` renders identically to before the view-model move
- [ ] `POST /api/digest/preview` → inspect HTML in browser AND a mail client (Gmail draft / send to self)
- [ ] `dots`/`excerpt`/SourceName/Position appear in the email
- [ ] Click a feedback link from the email → absolute URL hits `/api/feedback`, records a vote

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
