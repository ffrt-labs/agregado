# Agregado - Product Requirements Document

> A newsletter/RSS aggregator with pub/sub architecture for learning and daily use.

## Technical Stack

| Component | Choice | Justification |
|-----------|--------|---------------|
| Backend | Go | Performance, simplicity, excellent concurrency |
| Message Broker | RabbitMQ | Classic AMQP patterns, enterprise-relevant, great learning |
| Database | PostgreSQL | Full-featured, scalable, industry standard |
| Frontend | HTMX + Go templates | Minimal JS, stays in Go ecosystem, modern hypermedia |
| Email Intake | Webhooks (Cloudflare Email Routing) | Event-driven, aligns with pub/sub goals, free tier |
| AI Inference | Ollama (local) | Free, privacy-preserving, learning value |
| Deployment | Docker Compose | Self-hosted, reproducible, simple |

---

## 1. Architecture Overview

### System Components

```
┌─────────────────────────────────────────────────────────────────────────────┐
│                              AGREGADO SYSTEM                                 │
├─────────────────────────────────────────────────────────────────────────────┤
│                                                                              │
│  ┌──────────────┐     ┌──────────────┐     ┌──────────────┐                │
│  │  RSS Poller  │     │   Webhook    │     │    CRON      │                │
│  │   Service    │     │   Handler    │     │  Scheduler   │                │
│  └──────┬───────┘     └──────┬───────┘     └──────┬───────┘                │
│         │                    │                    │                         │
│         │              ┌─────┴─────┐              │                         │
│         │              │   Link    │              │                         │
│         │              │  Fetcher  │              │                         │
│         │              └─────┬─────┘              │                         │
│         └────────────────────┼────────────────────┘                         │
│                              ▼                                              │
│                    ┌─────────────────┐                                      │
│                    │    RabbitMQ     │                                      │
│                    │                 │                                      │
│                    │  ┌───────────┐  │                                      │
│                    │  │ Exchange  │  │                                      │
│                    │  │ (fanout)  │  │                                      │
│                    │  └─────┬─────┘  │                                      │
│                    │        │        │                                      │
│                    │   ┌────┼────┐   │                                      │
│                    │   ▼    ▼    ▼   │                                      │
│                    │  ┌─┐  ┌─┐  ┌─┐  │                                      │
│                    │  │Q│  │Q│  │Q│  │  (store, digest, dedupe queues)     │
│                    │  └─┘  └─┘  └─┘  │                                      │
│                    └─────────────────┘                                      │
│                              │                                              │
│         ┌────────────────────┼────────────────────┐                         │
│         ▼                    ▼                    ▼                         │
│  ┌──────────────┐     ┌──────────────┐     ┌──────────────┐                │
│  │   Storage    │     │    Digest    │     │   Dedupe     │                │
│  │   Worker     │     │   Generator  │     │   Worker     │                │
│  └──────┬───────┘     └──────┬───────┘     └──────────────┘                │
│         │                    │                                              │
│         ▼                    │                                              │
│  ┌──────────────┐            │      ┌──────────────┐                       │
│  │  PostgreSQL  │            │      │    Ollama    │                       │
│  └──────────────┘            │      │  (local AI)  │                       │
│         ▲                    │      └──────┬───────┘                       │
│         │                    │             │                                │
│         │                    ▼             ▼                                │
│         │              ┌─────────────────────────┐                          │
│         │              │     AI Processing       │                          │
│         │              │  (categorize, summary,  │                          │
│         │              │   relevance scoring)    │                          │
│         │              └───────────┬─────────────┘                          │
│         │                          │                                        │
│         │                          ▼                                        │
│         │                   ┌──────────────┐                                │
│         │                   │ Email (SMTP) │                                │
│  ┌──────┴───────┐           └──────────────┘                                │
│  │   Web API    │◄────── HTMX Frontend                                     │
│  │  (HTTP/REST) │                                                          │
│  └──────────────┘                                                          │
│                                                                              │
└─────────────────────────────────────────────────────────────────────────────┘
```

### Component Responsibilities

| Component | Responsibility |
|-----------|----------------|
| **RSS Poller** | Periodically fetches RSS feeds, parses entries, publishes to RabbitMQ |
| **Webhook Handler** | Receives inbound emails from Cloudflare, parses content, publishes to RabbitMQ |
| **Storage Worker** | Consumes articles from queue, deduplicates, stores in PostgreSQL |
| **Digest Generator** | Scheduled job that queries articles + social digests, ranks them, generates and sends email digest |
| **Dedupe Worker** | (Post-MVP) Computes content hashes, detects near-duplicates across sources |
| **Social Pollers** | (Post-MVP) Fetches posts from social platforms (Bluesky, Reddit, Mastodon), stores in temp buffer |
| **AI Summarizer** | (Post-MVP) Groups social posts by topic, generates summaries, stores in social_digests |
| **Web API** | REST endpoints for UI, serves HTMX templates |
| **CRON Scheduler** | Triggers periodic jobs (RSS polling, social polling, digest generation) |

### Pub/Sub Strategy

**Why pub/sub fits this problem:**
1. **Decoupling** - Ingestors (RSS, email) don't need to know about consumers (storage, digest)
2. **Fan-out** - One article can trigger multiple actions (store, dedupe, future: AI summarize)
3. **Reliability** - Failed processing doesn't lose messages (dead-letter queues)
4. **Rate limiting** - Consumers process at their own pace
5. **Learning value** - Real patterns used in production systems

**RabbitMQ Topology:**

```
Exchange: articles.ingest (type: fanout)
    ├── Queue: articles.store      → Storage Worker
    ├── Queue: articles.dedupe     → Dedupe Worker (post-MVP)
    └── Queue: articles.enrich     → AI Enrichment (post-MVP)

Exchange: articles.dlx (type: fanout)  [Dead Letter Exchange]
    └── Queue: articles.failed     → Manual inspection / retry

Exchange: digest.trigger (type: direct)
    └── Queue: digest.generate     → Digest Generator
```

---

## 2. Feature Breakdown

### MVP Features

#### F1: RSS Feed Ingestion
**User Story:** As a user, I can add RSS feed URLs and have articles automatically fetched.

**Acceptance Criteria:**
- Add/remove RSS feed URLs via web UI
- Poller fetches feeds every 15 minutes (configurable)
- Parse standard RSS 2.0 and Atom feeds
- Extract: title, URL, content/summary, author, published date
- Handle feed errors gracefully (retry with backoff)
- Publish parsed articles to RabbitMQ

#### F2: Article Storage
**User Story:** As a user, my articles are stored persistently and deduplicated by URL.

**Acceptance Criteria:**
- Storage worker consumes from `articles.store` queue
- Deduplicate by article URL (same URL = same article)
- Store article metadata and content in PostgreSQL
- Track source feed and ingestion timestamp
- Acknowledge messages only after successful storage

#### F3: Email Newsletter Ingestion
**User Story:** As a user, I can forward newsletters to a dedicated email and have them appear as articles.

**Hybrid Processing Approach:**
Every newsletter is processed as BOTH:
1. **Main Article** — the newsletter email body itself (always created)
2. **Child Articles** — extracted links from the newsletter (configurable per source)

This handles the spectrum from essay-style newsletters (Stratechery, where links are references) to link-aggregation newsletters (TLDR, where links ARE the content).

**Per-Source Configuration (toggleable in UI):**

| Field | Type | Default | Behavior |
|-------|------|---------|----------|
| `extract_links` | `BOOLEAN` | `true` | If true, extract links from newsletter HTML and create child articles |
| `summarize` | `BOOLEAN` | `true` | If true, call AI summarizer on newsletter body and store result in `articles.summary` |

```sql
-- Actual migrations applied
ALTER TABLE sources ADD COLUMN extract_links BOOLEAN NOT NULL DEFAULT true;  -- migration 000007
ALTER TABLE sources ADD COLUMN summarize     BOOLEAN NOT NULL DEFAULT true;  -- migration 000008

-- Link child articles to their parent newsletter
ALTER TABLE articles ADD COLUMN parent_article_id UUID REFERENCES articles(id);
```

**Examples:**
| Newsletter | `extract_links` | `summarize` | Rationale |
|------------|-----------------|-------------|-----------|
| Stratechery | `false` | `true` | Essay-style; links are references; body IS the content worth summarizing |
| TLDR | `true` | `false` | Links are the content; newsletter body is just scaffolding |
| Morning Brew | `true` | `true` | Both body summary and extracted links have value |

**Acceptance Criteria:**
- Webhook endpoint receives POST from Cloudflare Email Routing
- Parse email: subject → title, body → content, from → source
- Handle HTML emails (convert to readable text/markdown)
- **Always** create main article from newsletter body
- If `source.summarize = true`, call AI on newsletter body → store in `articles.summary`
- If `source.extract_links = true`, trigger link extraction pipeline
- Publish parsed articles to RabbitMQ
- Verify webhook authenticity (shared secret)
- Both toggles editable per-source in the Sources UI (newsletter sources only)

#### F3.1: Link Extraction Pipeline
**User Story:** As a user, links from my newsletters are automatically fetched and stored as separate articles.

**Description:**
When a newsletter source has `extract_links = true`, the system extracts links from the HTML body, fetches their content, and creates child articles linked to the parent newsletter.

**Process:**
1. Parse newsletter HTML with goquery
2. Extract `<a href>` links
3. Filter out noise (navigation, unsubscribe, social media, tracking links)
4. For each valid link:
   - Fetch URL content with timeout
   - Extract article content using readability algorithm
   - Create child article with `parent_article_id` set to newsletter
   - Publish to RabbitMQ

**Filtering Heuristics:**
- Exclude links containing: `unsubscribe`, `mailto:`, `twitter.com`, `facebook.com`, `linkedin.com/share`
- Exclude links in header/footer navigation
- Exclude tracking/redirect URLs (detect common patterns)

**Error Handling:**
- Skip inaccessible URLs gracefully (404, timeout, paywall)
- Log failures but don't block newsletter processing
- Set reasonable timeout (10s per link)

#### F4: Daily Digest Email
**User Story:** As a user, I receive a daily email with top articles from the last 24 hours, organized by topic.

**Structure:**
```
Daily Digest
├── Tag: Tech
│   ├── Topic: "AI Model Releases" (AI-detected cluster)
│   │   ├── [Article 1]
│   │   └── [Article 2]
│   └── Topic: "Cloud Infrastructure"
│       └── [Article 3]
├── Tag: Business
│   └── Topic: "Startup Funding"
│       └── [Article 4]
└── ...
```

**Acceptance Criteria:**
- Configurable digest time (default: 7:00 AM)
- Query articles from last 24 hours
- **Group by Tag first** (existing categorization)
- **Within each tag, AI clusters by specific topic** (e.g., "AI Model Releases", "Cloud Infrastructure")
- Filter articles with `relevance_score >= DIGEST_MIN_SCORE` (default 3); unscored articles (null) pass through
- Sort by relevance score DESC, then published_at DESC; cap at `DIGEST_CAP` articles (default 20)
- **Digest overview**: AI-generated 2–3 sentence introduction at top of email summarising the day's main topics (derived from per-group summaries; omitted gracefully if AI fails)
- Per article: title link (direct), 👍 and 👎 feedback links (HMAC-signed, separate from title)
- Generate HTML email with topic groupings and AI-generated topic summaries
- Send via SMTP (configurable provider)
- Track digest history (avoid re-sending same articles)

#### F5: Web UI - Browse Articles
**User Story:** As a user, I can browse all my articles in a clean web interface.

**Acceptance Criteria:**
- List view of articles (title, source, date, summary preview)
- Pagination (20 articles per page)
- Filter by source/feed
- Filter by date range
- Search by title/content (basic PostgreSQL full-text search)
- Mark as read/unread
- Mobile-responsive layout

#### F6: Source Management UI
**User Story:** As a user, I can manage my RSS feeds and see their status.

**Acceptance Criteria:**
- List all configured feeds
- Add new feed (validate URL, fetch title)
- Remove feed
- Show feed status (last fetch, error count)
- Manual "refresh now" button

#### F7: Article Tagging
**User Story:** As a user, I can categorize articles by topic (tech, business, personal, etc.) for better organization and filtering.

**Description:**
Articles can have multiple tags for categorization. Sources can have a default tag that new articles inherit automatically.

**Design Decisions:**
- Multiple tags per article (many-to-many relationship)
- Predefined tags with optional custom tags later
- Source-level default tag (articles inherit from source)
- Manual assignment initially, AI-based classification post-MVP

**Predefined Tags:**
- Tech, Business, Personal, Politics, Economy, Science, Health, Entertainment

**Acceptance Criteria:**
- Tags table with predefined entries (name, slug, color)
- Articles can have multiple tags via junction table
- Sources can have a default tag
- New articles inherit source's default tag
- Filter articles by tag in web UI
- Digest can filter by tag

### Post-MVP Features (Prioritized)

| Priority | Feature | Learning Value | Usefulness |
|----------|---------|---------------|------------|
| 1 | Multi-content type support | High (schema evolution, APIs) | High |
| 2 | Content-based deduplication | High (hashing, similarity) | High |
| 3 | Read time estimation | Low | High |
| 4 | AI-based tag classification | High (NLP/LLM integration) | High |
| 5 | AI summarization | High (LLM integration) | Medium |
| 6 | **Social Media Digest** | High (APIs, AI integration) | High |
| 7 | Phrase-level filtering | Medium (text processing) | Medium |
| 8 | Smart scheduling | Medium (user preferences) | Medium |
| 9 | Mobile reading view | Low | High |

#### F7: Multi-Content Type Support (Post-MVP)
**User Story:** As a user, I can save videos, audio, and PDFs alongside articles in my digest.

**Description:**
Extend the system to support multiple content types beyond articles. User provides a URL, and the system fetches metadata (title, author, duration/page count, thumbnail).

**Supported Content Types:**
- `article` - Text articles (current implementation)
- `video` - YouTube, Vimeo, etc.
- `audio` - Podcasts, audio files
- `pdf` - PDF documents

**Schema Migration Required:**
```sql
-- Rename articles → content_items
ALTER TABLE articles RENAME TO content_items;

-- Add content type discriminator
ALTER TABLE content_items
    ADD COLUMN content_type VARCHAR(20) DEFAULT 'article'
    CHECK (content_type IN ('article', 'video', 'audio', 'pdf'));

-- Add type-specific fields (nullable)
ALTER TABLE content_items
    ADD COLUMN duration_seconds INTEGER,      -- video/audio
    ADD COLUMN page_count INTEGER,            -- pdf
    ADD COLUMN thumbnail_url VARCHAR(2048);   -- video
```

**Acceptance Criteria:**
- User can submit a URL via web UI
- System detects content type from URL/response
- Metadata fetched automatically (YouTube API, web scraping, PDF parsing)
- Digest shows mixed content with appropriate labels (read time vs watch time)
- Filter by content type in web UI

#### F8: Social Media Digest (Post-MVP)
**User Story:** As a user, I want to follow social media accounts and receive AI-summarized highlights grouped by topic in my daily digest.

**Description:**
Aggregate posts from social platforms (starting with Bluesky), use AI to group by topic and summarize, then include highlights in the daily digest. Unlike RSS/email which stores individual articles, social media stores only AI-generated summaries.

**Key Differences from RSS/Email:**

| Aspect | RSS/Email | Social |
|--------|-----------|--------|
| Storage | Each article stored | Only AI summaries stored |
| Processing | Pass-through | AI summarization required |
| Volume | Low (10-50/day) | High (100-1000+/day) |
| Value | Individual items | Aggregated insights |

**Architecture:**
```
Social Pollers ──► Post Buffer (temp, 24h) ──► AI Summarizer ──► Social Digest
     │                                              │                  │
     │                                              │                  ▼
Bluesky/Reddit/                              Groups by topic    Merged into
Mastodon/X(?)                               + summarizes       Daily Email
```

**Platform Priority:**
1. **Bluesky** (Priority 1) - Free, open AT Protocol
2. **Reddit** (Priority 2) - Free API with rate limits
3. **Mastodon** (Priority 3) - Per-instance API
4. **X/Twitter** (Optional) - Expensive API or risky scraping

**AI Integration Options:**
- OpenAI API - Most capable
- Anthropic API - Good for summarization
- Local LLM (Ollama) - Free but slower
- Groq - Fast inference, free tier

**Acceptance Criteria:**
- Add/remove social accounts to follow via web UI
- Pollers fetch posts periodically throughout the day
- Posts stored in temporary buffer (cleaned after processing)
- AI summarizer runs before digest generation
- Groups posts by topic (Tech, Business, Politics, etc.)
- Generates 2-3 sentence summary per topic
- Identifies 2-3 highlight posts per topic
- Social section appears in daily digest email
- Old processed posts cleaned up automatically

---

## 3. Technical Specifications

### Database Schema

```sql
-- Tags for article categorization
CREATE TABLE tags (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name VARCHAR(50) UNIQUE NOT NULL,
    slug VARCHAR(50) UNIQUE NOT NULL,     -- URL-friendly (e.g., "tech", "personal-finance")
    color VARCHAR(7),                      -- Hex color for UI (#FF5733)
    created_at TIMESTAMP DEFAULT NOW()
);

-- Sources (RSS feeds and newsletter senders)
CREATE TABLE sources (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name VARCHAR(255) NOT NULL,
    type VARCHAR(50) NOT NULL CHECK (type IN ('rss', 'newsletter')),
    url VARCHAR(2048),                    -- RSS feed URL (null for newsletters)
    email_sender VARCHAR(255),            -- Newsletter sender email (null for RSS)
    default_tag_id UUID REFERENCES tags(id) ON DELETE SET NULL,  -- Default tag for new articles
    priority INTEGER DEFAULT 5,           -- 1-10, higher = more important
    is_active BOOLEAN DEFAULT true,
    last_fetched_at TIMESTAMP,
    last_error TEXT,
    error_count INTEGER DEFAULT 0,
    created_at TIMESTAMP DEFAULT NOW(),
    updated_at TIMESTAMP DEFAULT NOW()
);

-- Articles (source_id NULL = manually added by user)
CREATE TABLE articles (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    source_id UUID REFERENCES sources(id) ON DELETE CASCADE,  -- NULL = manually added
    external_url VARCHAR(2048) NOT NULL UNIQUE,  -- Dedupe key
    title VARCHAR(500) NOT NULL,
    author VARCHAR(255),
    summary TEXT,                         -- Short preview
    content TEXT,                         -- Full content if available
    content_hash VARCHAR(64),             -- SHA-256 for content deduplication
    published_at TIMESTAMP,
    ingested_at TIMESTAMP DEFAULT NOW(),
    is_read BOOLEAN DEFAULT false,
    read_at TIMESTAMP,
    word_count INTEGER,
    estimated_read_minutes INTEGER,
    created_at TIMESTAMP DEFAULT NOW()
);

-- Junction table for article-tag relationship (many-to-many)
CREATE TABLE article_tags (
    article_id UUID REFERENCES articles(id) ON DELETE CASCADE,
    tag_id UUID REFERENCES tags(id) ON DELETE CASCADE,
    PRIMARY KEY (article_id, tag_id)
);

-- Index for full-text search
CREATE INDEX idx_articles_search ON articles
    USING GIN (to_tsvector('english', title || ' ' || COALESCE(content, '')));

-- Index for common queries
CREATE INDEX idx_articles_source_date ON articles(source_id, published_at DESC);
CREATE INDEX idx_articles_unread ON articles(is_read, published_at DESC) WHERE NOT is_read;

-- Index for efficient tag lookups
CREATE INDEX idx_article_tags_tag_id ON article_tags(tag_id);

-- Digest history
CREATE TABLE digest_logs (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    sent_at TIMESTAMP DEFAULT NOW(),
    article_count INTEGER,
    recipient_email VARCHAR(255)
);

CREATE TABLE digest_articles (
    digest_id UUID REFERENCES digest_logs(id) ON DELETE CASCADE,
    article_id UUID REFERENCES articles(id) ON DELETE CASCADE,
    PRIMARY KEY (digest_id, article_id)
);

-- User preferences (single user for MVP)
CREATE TABLE preferences (
    key VARCHAR(100) PRIMARY KEY,
    value JSONB NOT NULL,
    updated_at TIMESTAMP DEFAULT NOW()
);

-- ============================================
-- SOCIAL MEDIA TABLES (Post-MVP)
-- ============================================

-- Social accounts to follow
CREATE TABLE social_sources (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    platform VARCHAR(50) NOT NULL CHECK (platform IN ('bluesky', 'reddit', 'mastodon', 'twitter')),
    handle VARCHAR(255) NOT NULL,           -- @username or subreddit
    display_name VARCHAR(255),
    profile_url VARCHAR(2048),
    is_active BOOLEAN DEFAULT true,
    last_fetched_at TIMESTAMP,
    last_error TEXT,
    error_count INTEGER DEFAULT 0,
    created_at TIMESTAMP DEFAULT NOW(),
    updated_at TIMESTAMP DEFAULT NOW(),
    UNIQUE(platform, handle)
);

-- Temporary post buffer (cleaned after processing)
CREATE TABLE social_posts (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    social_source_id UUID REFERENCES social_sources(id) ON DELETE CASCADE,
    platform_post_id VARCHAR(255) NOT NULL,  -- Original post ID on platform
    author_handle VARCHAR(255) NOT NULL,
    content TEXT NOT NULL,
    post_url VARCHAR(2048),
    posted_at TIMESTAMP,
    fetched_at TIMESTAMP DEFAULT NOW(),
    processed BOOLEAN DEFAULT false,
    UNIQUE(social_source_id, platform_post_id)
);

-- AI-generated social digests
CREATE TABLE social_digests (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    digest_date DATE NOT NULL,
    topic VARCHAR(100) NOT NULL,            -- AI-detected topic
    tag_id UUID REFERENCES tags(id),        -- Link to existing tags
    summary TEXT NOT NULL,                  -- AI-generated summary
    highlight_posts JSONB,                  -- Key posts that contributed
    post_count INTEGER,                     -- How many posts summarized
    created_at TIMESTAMP DEFAULT NOW(),
    UNIQUE(digest_date, topic)
);

-- Indexes for social tables
CREATE INDEX idx_social_posts_unprocessed ON social_posts(processed, fetched_at) WHERE NOT processed;
CREATE INDEX idx_social_digests_date ON social_digests(digest_date DESC);
```

### API Endpoints

```
# Sources
GET    /api/sources              - List all sources
POST   /api/sources              - Add new source
GET    /api/sources/:id          - Get source details
PUT    /api/sources/:id          - Update source
DELETE /api/sources/:id          - Remove source
POST   /api/sources/:id/refresh  - Trigger immediate fetch

# Articles
GET    /api/articles             - List articles (paginated, filterable by tag)
GET    /api/articles/:id         - Get article details
PATCH  /api/articles/:id/read    - Mark as read
PATCH  /api/articles/:id/unread  - Mark as unread
POST   /api/articles/:id/tags    - Add tags to article
DELETE /api/articles/:id/tags/:tag_id - Remove tag from article

# Tags
GET    /api/tags                 - List all tags
POST   /api/tags                 - Create custom tag
GET    /api/tags/:id/articles    - List articles with tag

# Search
GET    /api/search?q=term        - Full-text search

# Digest
POST   /api/digest/preview       - Preview next digest
POST   /api/digest/send          - Manually trigger digest

# Webhook
POST   /webhook/email            - Cloudflare Email Routing webhook

# Social Sources (Post-MVP)
GET    /api/social-sources           - List social accounts
POST   /api/social-sources           - Add social account to follow
GET    /api/social-sources/:id       - Get social source details
PUT    /api/social-sources/:id       - Update social source
DELETE /api/social-sources/:id       - Remove social source
POST   /api/social-sources/:id/refresh - Trigger immediate fetch

# Social Digests (Post-MVP)
GET    /api/social-digests           - List social digests (by date)
GET    /api/social-digests/:date     - Get social digest for specific date

# Preferences
GET    /api/preferences          - Get all preferences
PUT    /api/preferences/:key     - Update preference

# Health
GET    /health                   - Service health check
GET    /health/rabbit            - RabbitMQ connection status
```

### Message Format (RabbitMQ)

```json
{
  "id": "uuid",
  "source_id": "uuid",
  "source_type": "rss|newsletter",
  "external_url": "https://...",
  "title": "Article Title",
  "author": "Author Name",
  "summary": "First 300 chars...",
  "content": "Full content...",
  "published_at": "2024-01-15T10:30:00Z",
  "ingested_at": "2024-01-15T10:35:00Z",
  "metadata": {
    "feed_url": "https://...",
    "email_from": "newsletter@example.com"
  }
}
```

### AI Infrastructure

**Processing Strategy:** At ingest time (per article), not batch at digest time.
- Score is a stable property of the article — compute once, reuse across digest runs
- Distributes AI load throughout the day
- Enables querying "top articles" from the web UI at any time

**Ollama Container:**
```yaml
# docker-compose.yml addition
ollama:
  image: ollama/ollama:latest
  ports:
    - "11434:11434"
  volumes:
    - ollama_data:/root/.ollama
  deploy:
    resources:
      reservations:
        devices:
          - driver: nvidia
            count: all
            capabilities: [gpu]  # Optional: for GPU acceleration
```

**Model Recommendations by Task:**

| Task | Model Size | Recommendations | Why |
|------|------------|-----------------|-----|
| Categorization | Small (1-3B) | Phi-3-mini, Llama-3.2-1B | Fast, accurate for classification |
| Summarization | Medium (7-8B) | Mistral-7B, Llama-3.1-8B | Balance of quality and speed |
| Topic Clustering | Small (1-3B) | Phi-3-mini | Just needs semantic similarity |

**AI Components:**
- `internal/ai/client.go` — Provider interface (swappable backends)
- `internal/ai/ollama.go` — Ollama HTTP client implementation
- `internal/ai/categorizer.go` — Batch tag assignment
- `internal/ai/summarizer.go` — Generate article/topic summaries
- `internal/ai/relevance.go` — Score article relevance

### Relevance Scoring

**Scoring Factors:**
1. **AI quality + global importance** — LLM rates each article 1–5 at ingest time using its own knowledge of what's globally significant. No external trend API needed.
2. **Topic weights** — `topic_weights` table (topic slug → float 0.1–2.0) biases the AI prompt toward user interests. Starts neutral (1.0 for all topics); adjusted by feedback over time.
3. **Digest cap** — configurable `DIGEST_CAP` (default 20) and `DIGEST_MIN_SCORE` (default 3) filter and limit the digest.

**Relevance Score Storage:**
```sql
ALTER TABLE articles ADD COLUMN relevance_score SMALLINT;  -- 1-5, nullable = not yet scored
```

**Topic Weights:**
```sql
CREATE TABLE topic_weights (
  topic VARCHAR(100) PRIMARY KEY,
  weight FLOAT NOT NULL DEFAULT 1.0,
  updated_at TIMESTAMP NOT NULL DEFAULT NOW()
);
```

**Feedback Loop:**
```sql
CREATE TABLE article_feedback (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  article_id UUID NOT NULL REFERENCES articles(id) ON DELETE CASCADE,
  vote VARCHAR(4) NOT NULL CHECK (vote IN ('up', 'down')),
  created_at TIMESTAMP NOT NULL DEFAULT NOW()
);
```

Each digest email includes per-article 👍/👎 links (HMAC-signed, separate from article link). Clicking feedback: inserts a row in `article_feedback`, fetches the article's tags, and upserts `topic_weights` (up → +0.1, down → -0.1, clamped 0.1–2.0).

**Blocklist Storage:**
```sql
-- Uses existing preferences table
-- key = 'blocklist', value = JSON array of terms
INSERT INTO preferences (key, value) VALUES (
  'blocklist',
  '["sponsored", "partner content", "advertorial"]'
);
```

**Blocklist Management:** Added to Phase 4 Web UI
- `GET /api/preferences/blocklist` — Retrieve current blocklist
- `PUT /api/preferences/blocklist` — Update blocklist

---

## 4. Key Go Libraries

| Purpose | Library | Why |
|---------|---------|-----|
| RSS parsing | `github.com/mmcdole/gofeed` | Handles RSS 2.0, Atom, JSON Feed |
| RabbitMQ | `github.com/rabbitmq/amqp091-go` | Official Go client |
| PostgreSQL | `github.com/jackc/pgx/v5` | Fast, feature-rich driver |
| HTTP router | `github.com/go-chi/chi/v5` | Lightweight, composable |
| Migrations | `github.com/golang-migrate/migrate` | CLI + library |
| SMTP | `github.com/wneessen/go-mail` | Modern, well-maintained |
| HTML parsing | `github.com/PuerkitoBio/goquery` | jQuery-like HTML parsing |
| Article extraction | `github.com/go-shiori/go-readability` | Readability algorithm for clean content |
| Cron | `github.com/robfig/cron/v3` | Standard cron library |
| Config | `github.com/caarlos0/env/v10` | Env vars to struct |
| Logging | `log/slog` | Standard library, structured |

---

## 5. Error Handling Strategy

```
Message arrives → Worker processes
      │
      ├── Success → ACK message (removed from queue)
      │
      ├── Transient error (DB timeout, network) →
      │       NACK + requeue (retry up to 3 times)
      │
      └── Permanent error (bad data, parse fail) →
              NACK + dead-letter queue

Dead-letter queue → Manual inspection / alerting
```

---

## 6. Metrics to Track

- `articles_ingested_total` (counter, by source type)
- `articles_stored_total` (counter)
- `articles_deduplicated_total` (counter)
- `digest_sent_total` (counter)
- `queue_depth` (gauge, by queue name)
- `feed_fetch_duration_seconds` (histogram)
- `feed_errors_total` (counter, by feed)
