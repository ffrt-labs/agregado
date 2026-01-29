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
- [x] Added `manual` source type for user-submitted articles

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
- [ ] Create migration `000002_add_tags.up.sql` and `000002_add_tags.down.sql`
- [ ] Add `tags` table with predefined entries (Tech, Business, Personal, etc.)
- [ ] Add `article_tags` junction table for many-to-many relationship
- [ ] Add `default_tag_id` column to sources table
- [ ] Create `internal/domain/tag.go` entity
- [ ] Update `internal/domain/source.go` with `DefaultTagID` field
- [ ] Update `internal/domain/article.go` with `Tags` field
- [ ] Run migration and verify tables exist

### 1.5 RabbitMQ Integration
- [ ] Create `internal/broker/rabbitmq.go` - connection management
- [ ] Implement reconnection logic with backoff
- [ ] Create exchanges and queues on startup
- [ ] Create `internal/broker/publisher.go` - publish helper
- [ ] Create `internal/broker/consumer.go` - consume helper
- [ ] Set up dead-letter exchange and queue
- [ ] Test publish/consume round-trip

### 1.6 PostgreSQL Storage
- [ ] Create `internal/storage/postgres.go` - connection pool
- [ ] Create `internal/storage/source_repo.go` - CRUD for sources
- [ ] Create `internal/storage/article_repo.go` - CRUD for articles
- [ ] Implement URL-based deduplication (ON CONFLICT)
- [ ] Add repository interfaces for testing

### 1.7 RSS Poller
- [ ] Create `internal/ingestion/rss/parser.go` - feed parsing with gofeed
- [ ] Create `internal/ingestion/rss/poller.go` - polling service
- [ ] Implement periodic fetching (configurable interval)
- [ ] Publish articles to `articles.ingest` exchange
- [ ] Handle errors with exponential backoff
- [ ] Update source `last_fetched_at` and `error_count`

### 1.8 Storage Worker
- [ ] Create `internal/storage/worker.go`
- [ ] Consume from `articles.store` queue
- [ ] Store articles via repository
- [ ] Implement ACK/NACK based on success/failure
- [ ] Handle graceful shutdown

### 1.9 Health Endpoints
- [ ] Create `internal/api/server.go` - HTTP server setup
- [ ] Add `GET /health` - basic health check
- [ ] Add `GET /health/rabbit` - RabbitMQ status
- [ ] Add `GET /health/db` - PostgreSQL status

### 1.10 Main Entry Point
- [ ] Create `cmd/agregado/main.go`
- [ ] Wire up all components
- [ ] Implement graceful shutdown (SIGINT, SIGTERM)
- [ ] Start HTTP server, poller, and workers

### Phase 1 Verification
- [ ] `docker-compose up` starts all services
- [ ] Health endpoint returns 200
- [ ] RabbitMQ management UI shows exchanges/queues
- [ ] Add RSS feed via database → articles appear after poll

---

## Phase 2: Email Integration

### 2.1 Webhook Handler
- [ ] Create `internal/ingestion/email/webhook.go`
- [ ] Add `POST /webhook/email` endpoint
- [ ] Validate webhook secret header
- [ ] Parse Cloudflare Email Routing payload structure

### 2.2 Email Parsing
- [ ] Create `internal/ingestion/email/parser.go`
- [ ] Extract subject → title
- [ ] Extract from → source identifier
- [ ] Convert HTML body to text/markdown
- [ ] Handle multipart emails
- [ ] Extract links from newsletter content

### 2.3 Newsletter Source Management
- [ ] Auto-create source from new sender email
- [ ] Link newsletters to sources by `email_sender` field
- [ ] Publish parsed articles to RabbitMQ

### Phase 2 Verification
- [ ] POST to webhook endpoint returns 200
- [ ] Forwarded email creates article in database
- [ ] Source auto-created for new sender

---

## Phase 3: Digest Generation

### 3.1 Digest Query + Ranking
- [ ] Create `internal/digest/ranker.go`
- [ ] Query unread articles from last 24 hours
- [ ] Implement ranking algorithm (recency + priority + unread bonus)
- [ ] Limit to configurable max articles

### 3.2 Email Generation
- [ ] Create `internal/digest/generator.go`
- [ ] Create HTML email template
- [ ] Create plain text fallback
- [ ] Format article summaries and links

### 3.3 SMTP Integration
- [ ] Create `internal/digest/mailer.go`
- [ ] Send emails via go-mail library
- [ ] Log digest history to `digest_logs` table

### 3.4 Scheduling
- [ ] Add cron scheduler (robfig/cron)
- [ ] Schedule digest at configured time
- [ ] Add `POST /api/digest/send` for manual trigger
- [ ] Add `POST /api/digest/preview` for preview

### Phase 3 Verification
- [ ] Manual digest trigger sends email
- [ ] Email contains correct articles
- [ ] Scheduled digest fires at configured time

---

## Phase 4: Web UI

### 4.1 API Layer
- [ ] Set up Chi router with middleware
- [ ] Add JSON error response format
- [ ] Implement pagination helpers
- [ ] Create source handlers (CRUD)
- [ ] Create article handlers (list, read/unread)
- [ ] Create search handler

### 4.2 Templates Setup
- [ ] Create base layout template
- [ ] Set up template rendering helper
- [ ] Add HTMX library
- [ ] Add minimal CSS (or Tailwind)

### 4.3 Article Views
- [ ] Article list page with HTMX pagination
- [ ] Article detail page
- [ ] Read/unread toggle (HTMX partial)
- [ ] Filter by source dropdown
- [ ] Date range filter

### 4.4 Source Management
- [ ] Source list page
- [ ] Add source form (validates feed URL)
- [ ] Delete source with confirmation
- [ ] Show source status (last fetch, errors)
- [ ] Manual refresh button

### 4.5 Search
- [ ] Search input with HTMX
- [ ] PostgreSQL full-text search query
- [ ] Display search results

### 4.6 Polish
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

## Phase 6: Social Media Integration (Post-MVP)

**Prerequisites:** Phases 1-5 complete, AI summarization infrastructure

### 6.1 Social Sources Schema
- [ ] Create migration `000003_add_social_sources.up.sql`
- [ ] Create migration `000003_add_social_sources.down.sql`
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
