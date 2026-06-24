# Agregado

A newsletter/RSS aggregator with pub/sub architecture for learning and daily use.

## Overview

Agregado is a personal project to learn distributed systems patterns while building a useful tool. It aggregates content from RSS feeds and email newsletters, scores articles with AI at ingest time, and delivers a ranked daily digest email with a feedback loop that improves future scoring over time.

## Tech Stack

| Component | Technology | Why |
|-----------|------------|-----|
| Backend | Go | Performance, simplicity, excellent concurrency |
| Message Broker | RabbitMQ | Classic AMQP patterns, enterprise-relevant |
| Database | PostgreSQL | Full-featured, scalable, industry standard |
| Frontend | HTMX + Go templates | Minimal JS, stays in Go ecosystem |
| Email Intake | Webhooks (Cloudflare Email Routing) | Event-driven, aligns with pub/sub goals |
| AI Inference | Cloudflare Workers AI | Scoring, summarization, digest overview |
| Deployment | Docker Compose | Self-hosted, reproducible |

---

## Architecture

```
╔══════════════════════════════════════════════════════════════════╗
║                         INGESTION                                ║
╠══════════════════════════════════════════════════════════════════╣
║                                                                  ║
║  ┌─────────────┐          ┌──────────────────────────────────┐  ║
║  │  RSS Poller │          │      Email Webhook Handler        │  ║
║  │  (periodic) │          │   (Cloudflare Email Routing)      │  ║
║  └──────┬──────┘          └────────────┬─────────────────────┘  ║
║         │                              │                         ║
║         │                    ┌─────────▼──────────┐             ║
║         │                    │  Newsletter Article  │             ║
║         │                    │  (always created)   │             ║
║         │                    └────┬──────────┬─────┘             ║
║         │                         │          │                   ║
║         │            [summarize=true]  [extract_links=true]      ║
║         │                    ┌────▼───┐  ┌───▼───────────┐      ║
║         │                    │AI Sum. │  │ Link Extractor │      ║
║         │                    │→summary│  │ + Fetcher      │      ║
║         │                    └────────┘  └───────┬────────┘      ║
║         │                                        │               ║
║         │                               child articles           ║
║         │                                        │               ║
╠═════════╪════════════════════════════════════════╪══════════════╣
║         │              AI SCORING                │              ║
║         │          ┌──────────────────┐          │              ║
║         └─────────►│  Score(1–5)      │◄─────────┘             ║
║                    │  • quality       │                         ║
║                    │  • topic weights │◄── feedback loop        ║
║                    └────────┬─────────┘                         ║
║                             │ relevance_score stored on article  ║
╠═════════════════════════════╪════════════════════════════════════╣
║                       QUEUE │                                    ║
║                    ┌────────▼────────┐                          ║
║                    │    RabbitMQ     │                          ║
║                    │  articles.ingest│                          ║
║                    └────────┬────────┘                          ║
║                             │                                    ║
║                    ┌────────▼────────┐                          ║
║                    │  Storage Worker │                          ║
║                    │  (deduplicates) │                          ║
║                    └────────┬────────┘                          ║
║                             ▼                                    ║
║                       ┌──────────┐                              ║
║                       │PostgreSQL│                              ║
║                       └──────────┘                              ║
╠══════════════════════════════════════════════════════════════════╣
║                        DIGEST (daily CRON)                       ║
╠══════════════════════════════════════════════════════════════════╣
║                                                                  ║
║  Ranker: filter score ≥ 3, sort score DESC, cap N articles       ║
║     │                                                            ║
║     ▼                                                            ║
║  Generator:                                                      ║
║    1. Group articles by tag                                      ║
║    2. Per group → AI Summarize → group summary                   ║
║    3. All group summaries → AI Digest → overview paragraph       ║
║    4. Per article → HMAC token (for feedback links)              ║
║     │                                                            ║
║     ▼                                                            ║
║  Email (SMTP):                                                   ║
║    [Overview paragraph]                                          ║
║    ── Tag: Tech ──────────────────────────────────────           ║
║    AI summary of Tech articles                                   ║
║    • Article title  👍 👎                                        ║
║    ── Tag: Business ──────────────────────────────────           ║
║    ...                                                           ║
╠══════════════════════════════════════════════════════════════════╣
║                       FEEDBACK LOOP                              ║
╠══════════════════════════════════════════════════════════════════╣
║                                                                  ║
║  👍 / 👎 links in email                                          ║
║     │  GET /api/feedback?article_id=…&vote=up&token=…            ║
║     ▼                                                            ║
║  Validate HMAC token                                             ║
║     │                                                            ║
║     ├── Insert → article_feedback                                ║
║     └── Upsert → topic_weights (±0.1, clamped 0.1–2.0)          ║
║                       ▲                                          ║
║                       └── used in next Score() prompt            ║
╚══════════════════════════════════════════════════════════════════╝
```

---

## Data Flow

### Ingestion

**RSS feeds:** Poller fetches feeds on a configurable interval → parses entries → scores each article (1–5) using AI with topic weights → publishes to RabbitMQ → Storage Worker deduplicates by URL → saves to PostgreSQL.

**Email newsletters:** Cloudflare Email Routing → Webhook Handler → always creates a newsletter article. Two per-source toggles control additional processing:
- `summarize = true` → AI generates a summary of the newsletter body → stored in `articles.summary`
- `extract_links = true` → goquery extracts links from HTML → go-readability fetches and parses each → child articles created with `parent_article_id` pointing to the newsletter → each child is scored and published to the queue

### Daily Digest

CRON triggers the digest pipeline:
1. **Rank** — query articles from last 24h where `relevance_score >= 3` (or unscored), sorted by score DESC, capped at N
2. **Group** — articles grouped by tag (Tech, Business, etc.)
3. **Summarize** — AI generates a 2–3 sentence summary per tag group
4. **Overview** — AI generates a 2-sentence intro from all group summaries ("summaries of summaries")
5. **Tokens** — HMAC-SHA256 token generated per article per vote direction for feedback links
6. **Render** — HTML template + plain text fallback
7. **Send** — SMTP delivery

### Feedback Loop

Each digest article includes 👍/👎 links. Clicking one:
1. Validates the HMAC token (prevents tampering)
2. Records a row in `article_feedback`
3. Fetches the article's tags
4. Upserts `topic_weights` (up → weight += 0.1, down → weight -= 0.1, clamped 0.1–2.0)

Next time the AI scorer runs, the topic weights are passed in the prompt, biasing the score toward topics the user has engaged with.

---

## Getting Started

### Prerequisites

- Go 1.22+
- Docker & Docker Compose
- Make (optional)

### Development Setup

```bash
# Start dependencies (PostgreSQL + RabbitMQ)
make dev-deps

# Run database migrations
make migrate-up

# Start the application
make dev
```

### Docker Compose (Full Stack)

```bash
docker-compose up
```

---

## Project Structure

```
agregado/
├── cmd/agregado/          # Application entry point
├── internal/
│   ├── ai/                # AI provider interface + Cloudflare implementation
│   ├── api/               # HTTP handlers (sources, articles, feedback, digest)
│   ├── broker/            # RabbitMQ integration (publisher, consumer)
│   ├── config/            # Configuration (env vars → structs)
│   ├── digest/            # Ranker, generator, mailer, scheduler
│   ├── domain/            # Business entities (Article, Source, Tag)
│   ├── ingestion/
│   │   ├── email/         # Webhook handler, email parser
│   │   └── rss/           # Feed parser, poller
│   ├── newsletter/        # Link extractor + article fetcher
│   └── storage/           # PostgreSQL repositories
├── migrations/            # Database migrations (golang-migrate)
├── templates/             # HTMX + Go HTML templates
├── docker/                # Dockerfile
└── docs/                  # PRD, TODO, ADRs
```

---

## Documentation

- [Product Requirements (PRD)](docs/PRD.md) — Full feature specifications
- [TODO](docs/TODO.md) — Implementation checklist and progress

## Learning Goals

1. **Pub/Sub patterns** — Message queues, exchanges, consumers, dead-letter queues
2. **Go concurrency** — Goroutines, channels, graceful shutdown
3. **Database design** — Migrations, indexes, full-text search, upsert patterns
4. **API design** — RESTful patterns, HTMX hypermedia, HMAC token validation
5. **AI integration** — Provider interfaces, prompt engineering, feedback loops
6. **DevOps basics** — Docker, health checks, Cloudflare Workers

## License

MIT
