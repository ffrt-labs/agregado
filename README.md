# Agregado

A headless, single-user service that enriches and scores Articles and builds
the daily Digest for a personal reading workflow.

## Overview

Agregado started as a newsletter/RSS aggregator built to learn pub/sub
patterns. After the pivot ([#48](https://github.com/ffrt-labs/agregado/issues/48))
and the removal of the superseded system ([#85](https://github.com/ffrt-labs/agregado/issues/85)),
it is one component of a larger Reader workflow:

- **Miniflux** fetches, dedupes and stores every Source (ADR-0006).
- **The Bridge** (Resend, n8n and a Cloudflare Worker in `email-worker/`) turns
  newsletters into private Atom feeds that Miniflux polls (ADR-0007).
- **n8n** orchestrates: it calls Agregado's private API for each new entry,
  asks it for the daily Digest, and sends that Digest by email (`n8n/`).
- **Agregado** fetches and extracts readable text, summarizes, tags and scores
  each Article against `PREFERENCES.md`, and keeps a content-free Article
  Index. It builds immutable Digest artifacts and records Opens and explicit
  votes.

Agregado has no UI, no message broker, no RSS poller, no mail sending and no
scheduler. It is reachable only inside the tailnet (ADR-0008).

## Tech Stack

| Component | Technology |
|-----------|------------|
| Backend | Go (chi router) |
| Database | PostgreSQL (golang-migrate migrations) |
| AI Inference | Cloudflare Workers AI |
| Prompt management and tracing | Langfuse (optional) |
| Orchestration | n8n (separate deployment; workflows in `n8n/`) |
| Deployment | Docker Compose behind the homelab-edge Caddy proxy |

---

## HTTP surface

| Route | Caller | Protection |
|-------|--------|------------|
| `POST /api/private/articles/enrich` | n8n | `X-Enrichment-Secret` |
| `POST /api/private/digests`, `POST /api/private/digests/{date}` | n8n | `X-Enrichment-Secret` |
| `GET /api/private/preferences/signals` | n8n | `X-Enrichment-Secret` |
| `GET`/`PUT /api/private/preferences`, `POST /api/private/preferences/regenerate` | n8n | `X-Enrichment-Secret` |
| `GET /r/{id}` | Digest link | records the first Open, then redirects |
| `GET /f/{id}/{up\|down}` | Digest link | records or replaces the explicit vote |
| `GET /health`, `GET /health/db` | operator | none |

All of it sits behind the tailnet. See [the Article Index API](docs/article-index-api.md)
and [the architecture overview](docs/architecture/agregado.md).

---

## Getting Started

### Prerequisites

- Go 1.25+
- Docker & Docker Compose
- Make (optional)

### Development Setup

```bash
# Start PostgreSQL (Adminer: docker compose --profile dev up adminer)
make dev-deps

# Run database migrations
make migrate-up

# Start the application
make dev
```

### Production

`docker compose --profile prod up -d agregado` builds and starts the app. The
deploy workflow (`.github/workflows/deploy.yml`) does this on the home server.
The external `edge` and `langfuse-ingest` Docker networks must already exist.

---

## Observability

Agregado is a telemetry **producer**: every diagnostic is a structured record
emitted as **JSON on stdout**. Where those logs end up is a collector's problem —
the app ships no agent and stores no logs itself.

| Variable | Default | Values |
|----------|---------|--------|
| `LOG_LEVEL` | `info` | `debug`, `info`, `warn`, `error` — anything unrecognized falls back to `info` |
| `LOG_FORMAT` | `json` | `json` for the deploy, `text` for readable local development |

```bash
# readable lines while developing
LOG_FORMAT=text LOG_LEVEL=debug make dev

# what the deploy emits
{"time":"...","level":"INFO","msg":"agregado starting","component":"main","http_port":"8080"}
```

Every record carries a `component` field (for example `main`, `api`,
`articleindex`, `digestartifact`, `preferences`, `ai`, `langfuse`) —
deliberately low-cardinality, so it can become a log-store label.
High-cardinality identifiers like `article_id` and `url` are line fields,
never labels.

Failures surface at `ERROR` or `WARN` the moment they happen. Startup logs a
short list of non-sensitive fields; the `Config` struct is never serialized, so
the database password and Cloudflare API token stay out of the log stream.

See [F17 in the PRD](docs/PRD.md) for the design decisions behind this.

---

## Project Structure

```
agregado/
├── cmd/
│   ├── agregado/              # The HTTP service
│   ├── digest-preview/        # Render the Digest email from fixture data
│   ├── migrate-ownership/     # One-shot legacy-to-Article-Index migration (#83)
│   └── seed-langfuse-prompts/ # Seed the managed prompts into Langfuse
├── internal/
│   ├── ai/                # AI provider interface + Cloudflare implementation
│   ├── api/               # Router, Open redirect, explicit votes
│   ├── articleindex/      # Enrichment pipeline and Article Index API
│   ├── digestartifact/    # Digest selection, rendering and storage API
│   ├── preferences/       # Stored PREFERENCES.md versions and regeneration
│   ├── preferenceexport/  # Preference signal export and Open recording
│   ├── ingestion/fetch/   # Article fetcher + readable-text extraction
│   ├── logging/           # slog setup — the single logging seam
│   ├── langfuse/, telemetry/ # Prompt serving, tracing and scores
│   └── storage/           # PostgreSQL repositories
├── email-worker/          # The Bridge's Cloudflare Worker (feeds, permalinks)
├── n8n/                   # n8n workflows, their sources and tests
├── evals/                 # Python Langfuse experiment harness (see evals/README.md)
├── migrations/            # Database migrations (golang-migrate)
└── docs/                  # PRD, ADRs, architecture, runbooks, study log
```

Some legacy code is still in the tree with no production caller, such as
`internal/ingestion/email` (the old newsletter parser) and `internal/digest`
(the old Digest generator).

---

## Documentation

- [Product Requirements (PRD)](docs/PRD.md) — Original feature specifications; many describe removed parts (see its current-state note)
- [Issues](https://github.com/ffrt-labs/agregado/issues) — Live roadmap; what's actually being worked on
- [ADRs](docs/adr/) — Architectural decisions, including deliberate non-goals
- [Roadmap Archive](docs/ROADMAP-ARCHIVE.md) — Frozen history of Phases 1–18
- [Prompt experiments](evals/README.md) — `make experiment`: run a Langfuse prompt label against a dataset before promoting it

## Learning Goals

1. **Pub/Sub patterns** — Message queues, exchanges, consumers, dead-letter
   queues (the RabbitMQ layer that taught these was removed in #85)
2. **Go concurrency** — Goroutines, channels, graceful shutdown
3. **Database design** — Migrations, indexes, full-text search, upsert patterns
4. **API design** — RESTful patterns, shared-secret private APIs
5. **AI integration** — Provider interfaces, prompt engineering, feedback loops
6. **DevOps basics** — Docker, health checks, Cloudflare Workers

## License

MIT
