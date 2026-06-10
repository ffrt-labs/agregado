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
- [ ] Check `source.extract_links` setting (DEFERRED to later)
- [ ] If `extract_links=true`, trigger link extraction pipeline (Phase 2.4) (DEFERRED to later)

### 2.3 Newsletter Source Management
- [x] Auto-create source from new sender email
- [x] Link newsletters to sources by `email_sender` field
- [x] Publish parsed articles to RabbitMQ

### 2.4 Link Extraction Pipeline
- [ ] Create migration `000004_add_extract_links.up.sql` and `.down.sql`
  - Add `extract_links BOOLEAN DEFAULT true` to sources
  - Add `parent_article_id UUID REFERENCES articles(id)` to articles
- [ ] Create `internal/ingestion/email/link_extractor.go`
  - Parse HTML for `<a href>` links using goquery
  - Filter out navigation/unsubscribe/social links (heuristics)
- [ ] Create `internal/ingestion/email/article_fetcher.go`
  - Fetch URL content with timeout (10s)
  - Extract article content using go-readability
  - Handle errors gracefully (skip inaccessible URLs)
- [ ] Set `parent_article_id` linking child articles to newsletter
- [ ] Publish each extracted article to RabbitMQ

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

### 4.2 Templates Setup
- [x] Create base layout template
- [x] Set up template rendering helper
- [x] Add HTMX library
- [ ] Add minimal CSS

### 4.3 Article Views
- [x] Article list page with HTMX pagination
- [ ] Article detail page
- [x] Read/unread toggle (HTMX partial)
- [x] Filter by source dropdown
- [ ] Date range filter

### 4.4 Source Management
- [x] Source list page
- [x] Add source form (validates feed URL)
- [x] Delete source with confirmation
- [x] Show source status (last fetch, errors)
- [x] Manual refresh button

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

### Phase 4 Verification
- [ ] Can browse articles in browser
- [ ] Can add/remove sources
- [ ] Search returns relevant results
- [ ] Works on mobile viewport

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

### 5.5.1 Ollama Setup
- [ ] Add Ollama service to `docker-compose.yml`
- [ ] Pull recommended models (Phi-3-mini for categorization, Mistral-7B for summarization)
- [ ] Verify Ollama responds on `http://localhost:11434`

### 5.5.2 AI Client Layer
- [ ] Create `internal/ai/client.go` — provider interface
- [ ] Create `internal/ai/ollama.go` — Ollama HTTP client
- [ ] Add AI config to `internal/config/config.go` (model names, endpoint, timeout)

### 5.5.3 AI Features
- [ ] Create `internal/ai/categorizer.go` — batch assign tags to articles
- [ ] Create `internal/ai/summarizer.go` — generate topic summaries
- [ ] Create `internal/ai/relevance.go` — score article relevance using blocklist
- [ ] Integrate blocklist from preferences table (`key='blocklist'`)

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
