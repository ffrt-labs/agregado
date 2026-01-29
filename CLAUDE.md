# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Critical: Tutoring Mode

**READ AGENT.md FIRST.** This is a learning project. Do NOT write code for the user. Instead:
- Act as a tutor guiding step-by-step implementation
- Explain concepts and ask questions
- Review code the user writes
- Only write boilerplate when explicitly requested

## Project Overview

Agregado is a newsletter/RSS aggregator with pub/sub architecture. It aggregates content from RSS feeds and email newsletters, stores them in PostgreSQL, and delivers daily digest emails. The project follows a learning-focused approach where the user is developing both the software and their understanding of systems design.

## Tech Stack

- **Backend**: Go 1.22+
- **Message Broker**: RabbitMQ (AMQP, fanout exchanges)
- **Database**: PostgreSQL with full-text search
- **Frontend**: HTMX + Go templates
- **Email Intake**: Webhooks (Cloudflare Email Routing)
- **Deployment**: Docker Compose

## Development Commands

```bash
# Start dependencies (PostgreSQL + RabbitMQ)
make dev-deps

# Run database migrations
make migrate-up

# Start the application
make dev

# Full stack via Docker
docker-compose up
```

## Architecture

The system uses a pub/sub pattern with RabbitMQ:

1. **Ingestors** (RSS Poller, Webhook Handler) → publish to `articles.ingest` fanout exchange
2. **Workers** consume from queues: `articles.store`, `articles.dedupe`, `articles.enrich`
3. **Storage Worker** → PostgreSQL
4. **Digest Generator** → SMTP

Dead-letter exchange (`articles.dlx`) handles failed messages.

## Project Structure

```
cmd/agregado/        # Application entry point
internal/
├── config/          # Configuration (env vars → struct via caarlos0/env)
├── domain/          # Business entities (Article, Source)
├── broker/          # RabbitMQ integration (publisher, consumer, reconnection)
├── ingestion/       # RSS (gofeed) and email ingestion
├── storage/         # PostgreSQL repositories (pgx)
├── digest/          # Digest generation and SMTP
├── api/             # HTTP handlers (chi router)
└── web/             # Templates and static files
migrations/          # Database migrations (golang-migrate)
```

## Key Libraries

| Purpose | Library |
|---------|---------|
| RSS parsing | `github.com/mmcdole/gofeed` |
| RabbitMQ | `github.com/rabbitmq/amqp091-go` |
| PostgreSQL | `github.com/jackc/pgx/v5` |
| HTTP router | `github.com/go-chi/chi/v5` |
| Migrations | `github.com/golang-migrate/migrate` |
| SMTP | `github.com/wneessen/go-mail` |
| HTML parsing | `github.com/PuerkitoBio/goquery` |
| Cron | `github.com/robfig/cron/v3` |
| Config | `github.com/caarlos0/env/v10` |
| Logging | `log/slog` (stdlib) |

## RabbitMQ Topology

```
Exchange: articles.ingest (fanout)
├── Queue: articles.store      → Storage Worker
├── Queue: articles.dedupe     → Dedupe Worker
└── Queue: articles.enrich     → AI Enrichment

Exchange: articles.dlx (fanout) [Dead Letter]
└── Queue: articles.failed

Exchange: digest.trigger (direct)
└── Queue: digest.generate
```

## Error Handling Pattern

- **Success**: ACK message
- **Transient error** (DB timeout, network): NACK + requeue (retry up to 3 times)
- **Permanent error** (bad data, parse failure): NACK + dead-letter queue
