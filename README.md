# Agregado

A newsletter/RSS aggregator with pub/sub architecture for learning and daily use.

## Overview

Agregado is a personal project to learn distributed systems patterns while building a useful tool. It aggregates content from RSS feeds and email newsletters, stores them in a unified database, and delivers daily digest emails.

## Tech Stack

| Component | Technology | Why |
|-----------|------------|-----|
| Backend | Go | Performance, simplicity, excellent concurrency |
| Message Broker | RabbitMQ | Classic AMQP patterns, enterprise-relevant |
| Database | PostgreSQL | Full-featured, scalable, industry standard |
| Frontend | HTMX + Go templates | Minimal JS, stays in Go ecosystem |
| Email Intake | Webhooks (Cloudflare Email Routing) | Event-driven, aligns with pub/sub goals |
| Deployment | Docker Compose | Self-hosted, reproducible |

## Architecture

```
┌──────────────────┐          ┌────────────────────────────┐
│   RSS Poller     │          │      Webhook Handler       │
└────────┬─────────┘          └───────┬────────────────────┘
         │                            │
         │                     ┌──────┴──────┐
         │                     │    Link     │
         │                     │  Extractor  │  (newsletters)
         │                     └──────┬──────┘
         └───────────────┬────────────┘
                         │
               ┌──────────┴──────────┐      ┌─────────────────────┐
               │     AI Scorer       │◄────►│  Cloudflare         │
               │  (1–5 per article)  │      │  Workers AI         │
               └──────────┬──────────┘      └─────────────────────┘
                          │ relevance_score stored at ingest
                          ▼
                   ┌─────────────┐
                   │  RabbitMQ   │
                   │  (fanout)   │
                   └──────┬──────┘
                          │
           ┌──────────────┼──────────────┐
           ▼              ▼              ▼
     ┌──────────┐  ┌──────────┐  ┌──────────┐
     │ Storage  │  │  Digest  │  │  Dedupe  │
     │ Worker   │  │Generator │  │  Worker  │
     └────┬─────┘  └────┬─────┘  └──────────┘
          │              │ • filter score ≥ 3
          ▼              │ • cap at N articles
     ┌──────────┐        │ • HMAC feedback tokens
     │PostgreSQL│        ▼
     └──────────┘   ┌──────────┐
          ▲         │  SMTP    │
          │         └──────────┘
     ┌────┴──────────────┐
     │     Web API       │◄──── HTMX Frontend
     │  GET /api/feedback│◄──── 👍 / 👎 email links
     └───────────────────┘      (updates topic weights)
```

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

## Project Structure

```
agregado/
├── cmd/agregado/        # Application entry point
├── internal/
│   ├── config/          # Configuration management
│   ├── domain/          # Business entities
│   ├── broker/          # RabbitMQ integration
│   ├── ingestion/       # RSS and email ingestion
│   ├── storage/         # PostgreSQL repositories
│   ├── digest/          # Digest generation
│   ├── api/             # HTTP handlers
│   └── web/             # Templates and static files
├── migrations/          # Database migrations
├── docker/              # Dockerfile
└── docs/                # Documentation
```

## Documentation

- [Product Requirements (PRD)](docs/PRD.md) - Full feature specifications
- [TODO](docs/TODO.md) - Implementation checklist and progress

## Learning Goals

1. **Pub/Sub patterns** - Understanding message queues, exchanges, consumers
2. **Go concurrency** - Goroutines, channels, graceful shutdown
3. **Database design** - Migrations, indexes, full-text search
4. **API design** - RESTful patterns, HTMX integration
5. **DevOps basics** - Docker, health checks, observability

## License

MIT
