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

#### F3.2: Auto-Confirm Newsletter Subscriptions
**User Story:** As a user, when I subscribe to a newsletter using my Cloudflare email address, confirmation emails are automatically handled without me leaving the app.

**Description:**
Many newsletters require clicking a confirmation link before they start sending. Since subscription emails arrive at the Cloudflare-routed address and are forwarded to the webhook, the app can detect and auto-confirm them.

**Process:**
1. Incoming email arrives at webhook
2. Detect if it is a confirmation email (subject or body contains "confirm", "verify", "activate", "subscription")
3. Extract the confirmation link from the HTML body (look for prominent CTA links near confirmation keywords)
4. Make an HTTP GET to the confirmation URL
5. Log the attempt and result; process the email normally regardless of outcome

**Detection Heuristics:**
- Subject contains: `confirm`, `verify`, `activate`, `please confirm`, `subscription`
- Body contains a single prominent link near confirmation language
- Sender is not a known newsletter source (first-time sender)

**Error Handling:**
- If confirmation request fails (timeout, 4xx, 5xx), log and continue — do not block email processing
- If no confirmation link is found, treat the email as a normal article
- Set reasonable timeout (10s)

**Non-goals:**
- Multi-step confirmation flows (e.g. requiring form submission)
- Storing confirmation state per source

#### F3.3: Stable Newsletter Identity
**User Story:** As a user, all emails from one newsletter map to a single source,
not a new source per email.

**Motivation (production bug):** The webhook keys the source on `payload.From`
(`internal/ingestion/email/parser.go`), which is the SMTP **envelope** sender —
an ESP bounce/VERP address that rotates every send (e.g.
`bounces+27731166-…@em6054.thenewscc.com.br`,
`0100019f…@dailyupdate.tldrnewsletter.com`). So `FindByEmailSender` never matches
and **every email creates a brand-new source**. This is the true root cause of the
"newsletter subscribed but nothing coherent appears" incident (the Cloudflare
Worker returns `Ok`/200 for every email — the plumbing is fine; the *identity* is
wrong). The rotating address also leaks into `Article.Author`.

**Key insight:** the Cloudflare Worker (`email-worker/src/index.ts`) already
forwards *all* parsed headers in the JSON payload's `headers` map (lowercased
keys). The Go parser simply ignores `Payload.Headers`. The stable identity is
already in hand — it just isn't read.

**Design decisions (confirmed):**
- **Identity resolution order:** `List-Id` header (RFC 2919, the gold standard for
  mailing-list identity) → the `From:` **header** address (e.g. `dan@tldrnewsletter.com`,
  stable) → the envelope `from` (last-resort fallback for malformed mail).
- **Source `Name`** comes from the `From:` header display name (e.g. "TLDR"),
  not the bounce address.
- **`Article.Author`** uses the `From:` header (name/address), not the envelope.
- **Storage:** a new stable-key column on `sources` (e.g. `identity` / `list_key`),
  plus a matching `domain.Source` field. NOTE: all `SourceRepo` reads use
  `SELECT *` + `pgx.RowToStructByName`, so the struct MUST gain the field in
  lockstep with the migration or every read errors.
- **Lookup + dedup:** a stable-key lookup that returns `(nil, nil)` on not-found
  (mirroring `FindByURL`, *not* the `ErrNoRows`-as-error shape of
  `FindByEmailSender`), plus a uniqueness guard / upsert so a retry or race can't
  re-create the same source.

**Acceptance Criteria:**
- Ten emails from one newsletter (rotating envelope-from) map to **one** source.
- The source is named after the newsletter (From display name), not a bounce address.
- `Article.Author` is the human sender, not the envelope address.
- A newly-seen newsletter still auto-creates its source on first email.

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

#### F4.1: Unified Digest Templating (shared view-model + email-safe styling)
**User Story:** As the operator, the daily email looks like the web "Daily Digest" page, and the two aren't maintained as unrelated copies.

**Motivation:** Two digest templates had drifted apart — the web page (`templates/digest.html` + `layout.html`) is a polished, enriched view; the email (`internal/digest/templates/digest.html`) was bare `<h1>/<h2>/<h3>` with raw domain fields, no shared styling, funcs, Position, or SourceName. This unifies the *logic* and *view-model* while giving the email its own email-safe stylesheet.

**Design decisions (confirmed):**
- **Shared formatting funcs** — `add`, `excerpt`, `dots`, `scoreLabel` move from `internal/api/render.go` into a leaf package `internal/tmplfunc` (`tmplfunc.Map`), imported by both the web renderer and the email generator. Leaf placement avoids the import cycle (`internal/api` already imports `internal/digest`).
- **Shared view-model** — `DigestView` / `DigestGroupView` / `DigestItemView` + `BuildView(computed ComputedDigest, sourceNames map[string]string) DigestView` live in `internal/digest`. Both the web `HomePage` handler and the email generator build the *same* view structs (Position numbering, SourceName lookup, greeting/date). `BuildView` takes a plain map (not a repo interface) to stay cycle-free.
- **Styling is NOT shared verbatim** — email clients drop CSS custom properties, flexbox, web fonts, and `:hover`. The email gets its own `<style>` block mirroring the web `.digest-*` look, translated to email-safe CSS: hardcoded hex, table/inline-block layout, `Georgia,serif` + monospace fallbacks. The web keeps its `layout.html` CSS unchanged.
- **Email actions** — per-article ▲/▼ feedback links (existing HMAC-token `GET /api/feedback`) + a footer "browse all / read on the web" link. No HTMX (Save / more-like-this) in email. Links must be **absolute**, so a new `PUBLIC_BASE_URL` config value is introduced (`config.Digest.BaseURL`); the generator/scheduler also gains a source-name map to resolve `SourceName`.

**Acceptance Criteria:**
- Sent email visually mirrors the web Daily Digest (kicker, greeting H1, per-topic rules, meta line with source/date/read-time/dots, excerpt).
- `excerpt`, `dots`, `SourceName`, and `Position` render in the email.
- Web `/` renders identically to before the view-model move (no regression).
- Feedback links in the email are absolute and record a vote via `/api/feedback`.

#### F4.2: Digest Compute Resilience (per-call AI timeout + non-blocking homepage)
**User Story:** As the operator, a slow or capacity-limited AI provider degrades gracefully instead of silently dropping `summarize`/`digest` output and blocking the homepage for minutes.

**Motivation:** In production, `Categorize` → `Summarize` → `Digest` ran sequentially under one shared 3-minute deadline. A heavy model exhausted that deadline during categorization, so every later call failed instantly — and because the AI logger reused the same expired context, the failures didn't even show up as failed rows in the admin log table, making the pipeline look silently broken rather than visibly failing. The homepage also blocked on this same deadline when the daily cache was cold.

**Design decisions:**
- Each AI call gets its own bounded context (`config.AI.RequestTimeout`, env `AI_REQUEST_TIMEOUT`, default 30s) instead of inheriting whatever's left of a shared deadline; `CloudflareProvider`'s `http.Client` also carries an explicit `Timeout`.
- AI log writes use a context detached from the call's own cancellation (`context.WithoutCancel` + a short timeout), so a call that times out is still recorded as a failed row instead of disappearing from `/admin/logs`.
- `Scheduler.Today` (blocking) is reserved for callers that need real content regardless of wait time (digest send, preview). The web homepage uses `Scheduler.TodayOrTrigger`, which returns immediately on a cold cache and kicks off a background compute; concurrent callers join the same in-flight compute rather than duplicating AI calls.

**Acceptance Criteria:**
- A single slow/hung AI call cannot exhaust the budget for calls queued behind it.
- A timed-out or failed AI call always produces a row in the AI log table.
- `GET /` never blocks on a cold digest cache; it shows a "generating" state and the digest appears on a subsequent load once the background compute finishes.

#### F5: Web UI - Browse Articles
**User Story:** As a user, I can browse all my articles in a clean web interface.

**Acceptance Criteria:**
- List view of articles (title, source, date, summary preview)
- Pagination (20 articles per page)
- Filter by source/feed
- Filter by date range
- Search by title/content (basic PostgreSQL full-text search)
- Mark as read/unread
- Sort by relevance score or recency (`?sort=relevant|recent`)
- Star/bookmark articles from the list view
- Mobile-responsive layout

#### F5.1: Daily Digest Home Page (web view)
**User Story:** As a user, opening the app shows me today's digest — the same curated view I'd get in email, rendered in the browser.

**Acceptance Criteria:**
- `GET /` renders a digest-style page (not the raw article list)
- Shows articles ingested since midnight, grouped by tag
- Displays AI overview paragraph if available
- Uses the `.digest-*` layout already defined in `layout.html`
- Sidebar stat "cleared the bar" reflects today's article count

#### F5.2: Bookmarks
**User Story:** As a user, I can star articles to save them for later reading, and paste arbitrary URLs to bookmark external content.

**Acceptance Criteria:**
- Star button on every article row toggles `is_saved` on the article
- `GET /bookmarks` renders a reading-list page of all saved articles
- Form at top of bookmarks page accepts a URL, fetches title/summary, saves as article with `is_saved = true`
- Remove button un-saves the article (does not delete it)
- `is_saved BOOLEAN DEFAULT false` column on `articles` table

#### F5.3: Article Reader Page + Open Redirect
**User Story:** As a user, clicking any article title opens it — a real page for RSS
links, an in-app reader for newsletters — and the app records that I opened it.

**Motivation (two stacked bugs):**
1. **`#ZgotmplZ`** — newsletter articles have `external_url = newsletter:<uuid>`, an
   unknown scheme, so `html/template` refuses to emit it in `href` and substitutes
   its filter-failure sentinel `#ZgotmplZ` (`templates/articles.html`,
   `article_list.html`, `digest.html`). The link points nowhere.
2. **htmx swallows the click** — the title `<a>` carries both `href` AND
   `hx-post="/api/articles/{id}/read"` on the same click. htmx calls
   `preventDefault()` on anchors it's bound to, so the mark-as-read POST fires but
   the browser never navigates — this breaks **RSS** links too, even though their
   `href` is valid.
Additionally, newsletters have **no external page** — their body lives in
`Article.Content` — so even a valid link has nowhere to go.

**Design decisions (confirmed):**
- **`GET /r/{article_id}`** (a subset of F11's redirect, pulled forward): record the
  open + mark the article read, then `302` — RSS → `external_url`; newsletter →
  the in-app reader `GET /articles/{id}`. (The weight-bumping personalization of
  F11 lands later; this version just records + redirects.)
- **`GET /articles/{id}`** — an in-app reader page rendering `Article.Content`
  (Markdown for newsletters once F14 lands). Backed by `ArticleRepo.GetById`, which
  already exists (`internal/storage/article_repo.go`) but is currently unrouted for
  this purpose.
- **All title links route through `/r/{id}`** — a plain `http(s)` URL, so no
  `ZgotmplZ`; and the anchor no longer carries `hx-post`, so navigation works.
  Read-marking moves server-side into `/r/{id}` (it can still be reflected in the UI
  optimistically). Applies to `articles.html`, `article_list.html`, web
  `digest.html`, the email `internal/digest/templates/digest.html`, and
  `bookmarks.html`.

**Acceptance Criteria:**
- Clicking an RSS title opens the external article (new tab) AND marks it read.
- Clicking a newsletter title opens the in-app reader showing the full body.
- No `href="#ZgotmplZ"` remains in any rendered article list.
- Opening an article records it as read without a separate click.

#### F6: Source Management UI
**User Story:** As a user, I can manage my RSS feeds and see their status.

**Acceptance Criteria:**
- List all configured feeds
- Add new feed (validate URL, fetch title)
- Remove feed
- Show feed status (last fetch, error count)
- Manual "refresh now" button
- "Last activity" column is type-aware: RSS sources show last poll time (`last_fetched_at`); newsletter sources show when the last email arrived (`last_email_received_at`, stamped by the webhook on ingest)

#### F6.1: Source Backup & Restore
**User Story:** As a user, I can export my sources so I don't lose them if the database is wiped, and re-import them to restore everything.

**Format:** OPML — the industry-standard XML format for feed subscriptions, compatible with Feedly, NetNewsWire, Reeder, and others.

**Acceptance Criteria:**
- `GET /api/sources/export` returns a valid OPML 2.0 file (Content-Disposition triggers browser download)
- `POST /api/sources/import` accepts an OPML file upload, creates missing RSS sources, skips duplicates by URL
- Export and Import buttons available on the Sources page
- Newsletter-type sources are included in export but skipped on import (no URL to re-subscribe to)

#### F6.2: Periodic Automated Backup Email
**User Story:** As a user, I automatically receive my sources as an OPML backup by
email on a schedule, without remembering to export manually.

**Motivation:** The `agregado` app container has no persistent volume — anything
written to local disk is lost on redeploy/recreate. Rather than add new
infrastructure (mounted volume, cloud storage), the periodic backup reuses the
existing SMTP mailer to deliver the same OPML export (F6.1) to an email inbox,
which is durable and off-server by construction.

**Acceptance Criteria:**
- A scheduled job (`BACKUP_SCHEDULE`, cron syntax, default weekly `0 3 * * 0`)
  generates the same OPML export as F6.1 and emails it as an attachment to
  `BACKUP_RECIPIENT_EMAIL` via the existing SMTP mailer
- Manual trigger via `POST /api/backup/send` (mirrors `/api/digest/send`)
- If `BACKUP_RECIPIENT_EMAIL` is unset, the job logs and no-ops rather than erroring
- Scope: sources only (OPML). No article/bookmark backup, no full DB dump, no
  cloud storage, no retention policy for prior backup emails — out of scope for v1

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

**Current Implementation (and known gaps) — as of 2026-06**

How an article actually gets a tag today differs from the spec above. The only
code path that assigns tags is the digest ranker (`internal/digest/ranker.go`),
which calls the AI classifier (`CloudflareProvider.Categorize`,
`internal/ai/cloudflare.go`) at **digest compute time**:

1. `TagRepo.FindAll` loads the 8 seeded tags (`migrations/000002`), keyed by slug.
2. For each capped article, `Categorize(title, content)` sends a **fixed prompt**
   (`tech, business, personal, politics, economy, science, health, entertainment`)
   plus the title and the **first 500 chars** of cleaned content.
3. The returned slug is normalized (`TrimSpace` + `ToLower`) and looked up by
   **exact match**; a hit assigns the tag, a miss silently falls through to
   "uncategorized".

Gaps vs. the spec / things to know when debugging mis-tags:

- **Not persisted.** There is no `INSERT INTO article_tags` anywhere. Tags live
  only in memory for the duration of one digest compute, so the LLM **re-tags
  every article on every run** → the same article can get a *different* tag
  run-to-run (generative output isn't deterministic). The
  `if len(article.Tags) > 0 { continue }` guard in the ranker is therefore dead
  code (tags are never loaded from a populated table).
- **Single tag, not many-to-many.** The classifier returns exactly one slug, so
  the junction table's multi-tag capability is unused in practice.
- **`sources.default_tag_id` is never read** to tag articles (it is stored and
  editable in the UI only). The "new articles inherit source's default tag"
  criterion is unimplemented.
- **Mis-tags are usually accuracy, not a code bug.** A wrong-but-valid slug means
  the model genuinely chose wrong; unmappable output becomes *uncategorized*, not
  a wrong tag. Prime cause: weak input — only title + 500 chars, and some feeds
  (e.g. Hacker News) put boilerplate in `summary` (`Article URL: … Comments URL:
  …`), so the model effectively classifies on the title alone. The coarse 8-bucket
  taxonomy (business vs. economy, tech vs. science) compounds this.
- **No observability.** The success path logs nothing, so the raw classifier
  output isn't captured — the first debugging step is to log the returned slug
  per article.

Improvement directions (not yet done): assign + **persist** tags at ingest time
alongside relevance scoring (stable, deterministic, queryable in the web UI);
log raw classifier output; feed richer content; support multiple tags.

> **F14 supersedes the "feed richer content" item here.** The 500-char truncation
> and CSS-soup problem are addressed by the Newsletter Content Pipeline (F14):
> `Categorize`/`Score`/`Reason` consume the compressed, Markdown-derived
> `distilled_content` instead of the first 500 chars of raw HTML.

#### F9: Admin Console — AI Logs, Editable Prompts, Tag Settings
**User Story:** As the operator, I can see exactly what prompts are sent to the AI
and what it returns, tweak those prompts without redeploying, and manage the tag
taxonomy — all from an `/admin` area.

**Motivation:** AI behavior (e.g. the F7 "wrong tag" case) is currently
un-debuggable — prompts are hardcoded in `internal/ai/cloudflare.go` and nothing
records requests/responses. This adds observability + a control surface.

**Three areas:**
1. **AI logs** — every AI request/response persisted and browsable (operation,
   model, system + user prompt, raw response, success/error, duration).
2. **Prompt settings** — the 4 hardcoded **system** prompts (score, categorize,
   summarize, digest) move to the DB and become editable; injected at call time
   with the in-code strings kept as a fallback default.
3. **Tag settings** — CRUD the tag list; the categorize prompt's allowed slugs are
   generated from the live tags (edit tags → classifier updates).

**Design decisions (confirmed):**
- **System prompt only** is editable; the dynamic user prompt (title, cleaned
  content, weights, tag list) stays assembled in code.
- **Log all AI calls**, gated by a **live on/off toggle** in `/admin` (DB flag,
  default ON) plus a **Clear logs** action. No automatic retention.
- Categorize **injects live tag slugs** from the DB at call time (the stored
  categorize prompt therefore omits any inline slug list).
- `/admin` is **unauthenticated for v1**. ⚠️ Prod is publicly reachable (Cloudflare
  tunnel) — Basic Auth is a deliberate, scoped follow-up (one middleware).

**Architecture:** `ai.NewCloudflareProvider` is built once (`cmd/agregado/main.go`)
and injected into ranker, generator, worker, and server — so refactoring it there
gives every call site DB-backed prompts + logging. `internal/ai` stays free of
`internal/storage` (avoiding an import cycle) by defining small interfaces
(`PromptStore`, `TagLister`, `AILogSink`) that storage repos satisfy. The single
`complete()` chokepoint gains an `operation` label, times the call, and records a
log entry; the sink no-ops when the flag is off.

**Acceptance Criteria:**
- `ai_prompts`, `ai_logs`, `admin_settings` tables (migration `000011`), seeded
  with the 4 current prompts and `ai_logging_enabled = true`
- Every AI call (score/categorize/summarize/digest) writes an `ai_logs` row when
  logging is enabled; nothing when disabled
- `/admin/logs` — paginated, newest-first, filterable by operation; a logging
  toggle and a Clear-all action
- `/admin/prompts` — edit each system prompt; Reset restores the in-code default
- `/admin/tags` — create/edit/delete tags (name, slug, color); categorize uses the
  live list
- Editing a prompt or tag is reflected in the *next* AI call's log (no restart)

#### F10: Content Hygiene (Clean Ingest + 24h Retention)
**User Story:** As a user, my article store stays small and my AI prompts see clean
text, not CSS/HTML boilerplate.

**Motivation:** RSS/newsletter bodies frequently leak raw markup into `content`
(e.g. `.bh__table { border: 1px solid #C0C0C0; } … relembrar bom dia`), so the AI
`Score`/`Categorize`/keyword prompts classify on noise. Separately, articles
accumulate forever with no cleanup.

**F10a — Clean content at ingest**
- New `internal/textutil/clean.go` helper: strip `<style>`/`<script>`/tags/inline
  CSS with `goquery`, then run `go-readability` to extract main content.
- Called in the **RSS parser** and **email parser** *before* article `Create`/publish.
- `articles.content` stores clean text; `summary` derived from the cleaned body.
- **Acceptance:** no tag/CSS soup in stored `content`; AI prompts receive readable text.

**F10b — 24h retention job**
- Scheduled job (`robfig/cron`, `RETENTION_SCHEDULE`, default hourly) that runs
  **after** the digest so the digest + vocabulary still see the day's articles.
- `DELETE FROM articles WHERE ingested_at < now() - (ARTICLE_RETENTION_HOURS || '24h') AND is_saved = false`.
- **FK handling:** alter `articles.parent_article_id` to `ON DELETE SET NULL` so
  deleting a parent newsletter doesn't fail on referenced child articles.
  `digest_articles` already cascades.
- **Durability by design:** learning survives because it lives in the aggregate
  weight tables (`topic_weights`, `keyword_weights`), not in the articles. A vote or
  open bumps the weight immediately, before the article is ever deleted.
- **Acceptance:** unsaved articles older than the window are removed; saved
  (`is_saved = true`) articles and all weight tables are untouched. Manual trigger
  via `POST /api/retention/run`.

#### F11: Personalization — Implicit Feedback & Topic Cloud
**User Story:** As a user, the app learns what I like from what I *open* (not just
what I vote on), builds a "nuvem de assuntos" (topic cloud) of my interests, and
uses it to score future articles higher.

**Motivation:** Today the feedback loop is **explicit-only** (👍/👎) and **coarse**
(8 tags via `topic_weights`). This adds the **implicit** signal (opening a link = weak
positive) and **fine-grained** keywords, closing a real recommender loop.

**Design decisions (confirmed):**
- **Keyword layer, not a tag replacement.** A new `keyword_weights` table
  (keyword → float, mirrors `topic_weights`) sits alongside the existing tag layer.
- **AI extracts keywords per article at ingest** (`Provider.ExtractKeywords`) into
  `article_keywords`, so an open/vote can map back to the article's keywords.
- **Opens are captured via a redirect endpoint**, `GET /r/{article_id}`: records the
  open, then `302`s to the real URL. Article title links in the **digest email, web
  UI, and reading feed** all route through `/r/` so opens are tracked everywhere.
- **The cloud feeds scoring.** Top `keyword_weights` are injected into the `Score`
  prompt (exactly as `topic_weights` already are) — matching articles score higher.

**Weight update rules (all clamp 0.1–2.0):**
| Signal | Effect |
|--------|--------|
| Open (`/r/{id}`) | article's keywords **+0.05** (weak), tag weight bump |
| 👍 vote | article's keywords **+0.1**, topic weight **+0.1** (existing) |
| 👎 vote | article's keywords **−0.1**, topic weight **−0.1** (existing) |

**Acceptance Criteria:**
- Ingesting an article extracts keywords into `article_keywords`.
- Clicking an article link records an open and bumps the relevant weights, then lands
  on the real article.
- A 👍/👎 vote updates both `topic_weights` (existing) and `keyword_weights` (new).
- The `Score` prompt includes the top keyword weights.
- `GET /cloud` renders the topic cloud (keywords sized by weight).

#### F12: Vocabulário Agregado (Daily English Word)
**User Story:** As a user improving my English, each daily digest suggests one useful
word — with its meaning and an example sentence — drawn from the day's content.

**Design decisions (confirmed):**
- **AI-generated from today's digest content** (`Provider.SuggestWord`), so the word
  is contextual to what I read. Computed during the **digest compute** (before the 24h
  deletion runs).
- **No repeats:** a `vocab_history` table records shown words; recent words are passed
  to the prompt to avoid repetition.
- **Shown in both places:** `WordOfDay` is added to the shared `DigestView` (F4.1
  view-model) so it renders in the **digest email and the web home page**.

**Acceptance Criteria:**
- Each digest compute produces `{word, meaning, example}`, persisted to `vocab_history`.
- The word is not one shown in the recent history window.
- The word block renders identically in email and on `GET /`.
- If the AI call fails, the block is omitted gracefully (never blocks the digest).

#### F13: Bookmarks Reading Feed (scrollable)
**User Story:** As a user, I can flip through my bookmarked articles in a full-screen,
vertically-scrollable "TikTok-style" feed.

**Design decisions (confirmed):**
- **Source: bookmarks only** — reuses the `FindSaved` query (F5.2 / Phase 4.8).
- **Text-only cards** — title + source + summary (no image for v1).
- **Tap opens through `/r/{id}`** (F11 redirect) so feed opens feed the algorithm too.
- **Depends on** the bookmark API already scoped in Phase 4.1 / 4.8
  (`POST /api/bookmarks`, `is_saved`, `GET /api/bookmarks`).

**Acceptance Criteria:**
- `GET /feed` renders saved articles as full-screen scrollable cards.
- Infinite scroll / pagination via HTMX.
- Tapping a card records an open and navigates to the real article.
- Empty state when no bookmarks exist.

#### F14: Newsletter Content Pipeline for AI (Markdown + Extractive Compression)

> **Status: lean core DONE; deferred round SUPERSEDED BY F15.**
> The cap (`AI_MAX_CONTENT_CHARS`), the `<style>`/`<script>` content-strip and the
> model switch all shipped — items 3 and 4 below are **done**.
> Items 1 and 2 (Markdown conversion, `Provider.Compress`, `distilled_content`) are
> **superseded by F15**, which reaches different conclusions: distillation is
> **algorithmic, not an AI call**, and the pipeline runs in a **dedicated
> `articles.enrich` stage**, not inline in the storage worker. Read F15 instead —
> the design below is kept for history only.

**User Story:** As a user, the AI actually understands my newsletters — it scores,
categorizes and summarizes them from their real substance, not their first 500
characters of CSS and greeting boilerplate.

**Motivation:** Every body-consuming AI op (`Score`, `Categorize`, `Reason`) caps
content at `maxPromptContentChars = 500` (`internal/ai/cloudflare.go`), so a
newsletter (thousands–tens of thousands of chars) is judged on ~1–5% of itself —
usually the header/nav/greeting. `textutil.Clean` strips tags but **not** the
*contents* of `<style>`/`<script>`, so inline CSS soup eats even that tiny budget.
Full content already flows in from `worker.go`/`ranker.go`, so this is about *what*
we feed the model, not plumbing. Two additional facts: the configured model is
`@cf/moonshotai/kimi-k2.7-code` — a **code** model doing summarization — and
`Summarize`/`Digest` never see bodies at all.

**Design decisions (confirmed):** a 2-stage ingest preprocessing pipeline, run
**async in the storage worker** (off the webhook request path, alongside `Score`):

1. **Semantic Markdown Conversion** (rules-based, cheap, deterministic) — replace
   flat `html2text.FromString` (`parser.go`) with an HTML→Markdown pass (goquery
   pre-strip of `<style>`/`<script>`/tracking pixels/hidden preheader, then an
   HTML→Markdown library) that preserves headings, lists, links, emphasis. This
   improves the AI input **and** gives the F5.3 reader page nicely structured
   content — one change, two wins.
2. **Extractive Compression** (a cheap-model AI pass) — new `Provider.Compress`
   using a small/fast model (`AI_COMPRESS_MODEL`) that distills the Markdown to its
   salient, non-redundant content (drops ads, navigation, repeated boilerplate),
   preserving key facts/structure. Soft-fail: on error, fall back to truncated raw
   content so ingestion never blocks.
3. **Configurable budget** — replace the hardcoded `500` with `AI_MAX_CONTENT_CHARS`
   (large default, e.g. 8000). With compression the input is usually well under it.
4. **Model switch** — move the main `AI_MODEL` to a general instruct/summarization
   model (e.g. the config default `@cf/google/gemma-4-26b`, or a Llama-instruct);
   keep the cheap model only for the compression stage.

**Storage (lean default — confirm at implementation):** store the Markdown as the
display `content` (used by the F5.3 reader), and persist the compressed result in a
new `articles.distilled_content` column so both ingest-time `Score`/`Reason` and
digest-time `Categorize` reuse it instead of re-truncating raw HTML every run.

**Relationship to F10a:** this **supersedes/augments** the pure `go-readability`
cleaner idea — Markdown conversion + a cheap-model compression pass is the chosen
approach for the AI's content input.

**Acceptance Criteria:**
- A `score`/`categorize` entry in `/admin/logs` shows dense, readable distilled
  content (Markdown-derived) — no CSS/`<style>` soup, not truncated at 500 chars.
- Newsletter tags/scores visibly improve vs. title-only classification.
- A compression failure degrades gracefully (article still scored on fallback text).
- The reader page (F5.3) shows the structured Markdown body.

#### F15: RSS Article Content Fetching + Enrichment Stage

> **Status: DONE, live-verified.** Implemented and confirmed against the real local
> stack (see `docs/TODO.md` Phase 17 for the checklist). One deviation from the
> design below: `go-shiori/go-readability` is deprecated upstream, so the
> implementation uses **`codeberg.org/readeck/go-readability/v2`** instead (same
> Mozilla-Readability-derived approach, actively maintained). Live backfill of 45
> pre-existing articles: 30 fetched successfully, 15 fell back to feed content
> (blocked-by-origin and thin-content cases both observed and handled correctly);
> `word_count`/`estimated_read_minutes`/`distilled_content` went from 0 populated to
> 45/45.

**User Story:** As a user, the AI scores and tags my RSS articles by reading the
*actual article*, not the teaser blurb the feed happened to ship — and I can tell,
at a glance, which articles it only got a teaser for.

**Motivation.** RSS items overwhelmingly carry a link plus a short `<description>`;
the full body in `<content:encoded>` is the exception, not the rule.
`internal/ingestion/rss/poller.go:104-112` maps `<description>` → `Summary` and
`<content:encoded>` → `Content`. When a feed omits the latter, `Content` is nil and
`internal/storage/worker.go:49-54` **silently** falls back to `Summary`. The AI then
scores a ~200-char teaser with full confidence. Nothing anywhere records that this
happened.

This is the third instance of one defect class in this codebase — *a field that looks
populated but isn't, with no signal that it isn't*:
1. `Content ?? Summary` — no marker separating a real article from a teaser.
2. `word_count` / `estimated_read_minutes` — columns since migration `000001`, never
   written by any code, yet the digest template renders a read-time from them.
3. `Summarize` and `Digest` (`internal/ai/cloudflare.go:195,206`) build prompts from
   **titles only** — they never receive a body. `AI_MAX_CONTENT_CHARS` therefore
   reaches only 3 of 5 provider methods; F14 raised a cap two callers cannot use.

So F15 is not only "fetch the link." It is: get real content, *record its provenance*,
and let the summarizer finally see it.

**Design decisions (confirmed):**

1. **Always fetch** `item.Link` for each new article. Feed content is the *fallback*,
   not the trigger — length heuristics are fragile (a long teaser passes; a short
   real post fails). Cost is bounded: `ON CONFLICT (external_url) DO NOTHING`
   (`article_repo.go:76`) means each URL is fetched at most once, ever.
2. **A dedicated `articles.enrich` stage.** The storage worker shrinks to Create-only
   and publishes `{article_id}` on a successful (non-duplicate) insert. A separate
   consumer does fetch → extract → distil → `Score` → `Reason`. This separates
   *durability* (must succeed) from *enrichment* (best-effort) — today they share one
   handler and one failure path. **Newsletters ride this for free**: the email webhook
   already publishes to `articles.ingest`, so newsletters enter the same stage and
   simply skip the fetch step. One pipeline, two entry points.
3. **The message is a trigger, not a payload** — `{article_id}` only. Postgres stays
   the source of truth; the consumer re-reads via the existing `ArticleRepo.GetById`.
4. **Extraction:** `go-shiori/go-readability` (Mozilla Readability port — drops
   nav/ads/footers) → `JohannesKaufmann/html-to-markdown/v2`. Markdown preserves the
   headings, lists and links that `textutil.Strip` destroys, which helps both the AI
   and the F5.3 reader page.
5. **Quality gate:** readability's `.Length < ~500` chars ⇒ extraction failed.
   Otherwise keep whichever is longer, fetched vs feed. Consent walls, SPA shells and
   paywalls all return HTTP 200, so the transport layer cannot detect them.
6. **Provenance:** `articles.content_source` ∈
   `fetched | feed_content | feed_description | newsletter`, CHECK-constrained
   (mirrors `sources.type`). This is the column that turns invisible degradation into
   `SELECT content_source, count(*) ... GROUP BY 1`.
7. **Distillation is algorithmic, not an AI call** — `textutil.Distill` keeps headings,
   the lede and section leads, capped ~2000 chars, stored in `distilled_content`.
   Deterministic, unit-testable, and free. *Extractive* summarization classically means
   selecting existing sentences (an algorithm); calling a model is *abstractive*.
   Adding a third AI call to the enrichment stage would compound its bottleneck for
   little gain. `Provider.Compress` (F14 item 2) is **dropped** for now;
   `distilled_content` remains the seam to swap it in behind if quality demands it.
8. **`Summarize` finally sees substance** — title + the article's existing
   `relevance_reason` + a ~400-char excerpt of `distilled_content`, budgeted per
   article. `Reason` is already computed at ingest for every article at/above the score
   bar (`worker.go:70-77`), and `FindUnreadSince` filters `relevance_score >= $2`
   (`article_repo.go:144`) — `NULL >= 3` is false — so every digest article already has
   one. Free, dense, on-topic signal that `Summarize` currently ignores.
9. **Polite, honest fetching** — a truthful `User-Agent`
   (`Agregado/1.0 (+github.com/felipeafreitas/agregado)`), one in-flight request per
   host, explicit timeout, size cap, `text/html` only, no retry on 403/429. No
   browser-UA spoofing: misrepresenting the client to bypass a stated access decision
   is not a thing this project does. robots.txt is skipped in v1 as disproportionate
   for a single-user, subscriber-driven, once-per-URL fetch.

**Also fixed here (latent, predates this feature):** `internal/broker/consumer.go`
spawns 5 goroutines but calls `Qos(1, 0, false)`. Prefetch is scoped per *consumer
tag*, and there is exactly one `ch.Consume` call — so RabbitMQ never delivers message
N+1 until N is acked and the goroutines are starved. Ingest is effectively serial
today. `Consume` gains prefetch + worker-count parameters; enrich runs 5/5, storage
stays at 1. Without this, enrichment would run ~90s/article and a 200-item poll would
take ~5 hours — finishing long after the digest fired.

**Out of scope (deliberate):**
- **Link roundups.** An item linking to a *list* (TLDR-style) ingests as a roundup.
  Following those links means child articles, `parent_article_id`, per-child scoring
  and digest template changes — a much larger feature that stays deferred. F15 builds
  the `Fetch` primitive it has been blocked on since F3/Phase 2.5.
- **Clustering.** Grouping articles by topic is a *digest-time*, cross-article concern
  (Phase 3.1). It does nothing for a single article's prompt budget.

**Relationship to F10a / F14:** F15 supersedes both the pure-`go-readability` cleaner
idea (F10a) and F14's deferred round. Content is cleaned by *extraction at fetch*, not
by a separate hygiene pass.

**Acceptance Criteria:**
- `SELECT content_source, count(*) FROM articles GROUP BY 1` shows a real `fetched`
  majority; teaser-scored articles are countable rather than invisible.
- A `score` entry in `/admin/logs` shows dense article prose — no CSS, not truncated
  at 500 chars. (This is the observation F14 shipped without being able to make.)
- A `summarize` entry shows titles + reasons + excerpts, not a bare title list.
- `word_count` is populated and the digest's read-time stops being computed from nil.
- A fetch failure degrades gracefully: article still scored on feed content, with
  `content_source` recording the fallback.
- The enrich queue drains concurrently across distinct hosts, serially per host.

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
    email_sender VARCHAR(255),            -- Newsletter From-header address (STABLE; was envelope-from, see F3.3)
    identity VARCHAR(255),                -- F3.3: stable newsletter key (List-Id or From-header addr); lookup/dedup key
    default_tag_id UUID REFERENCES tags(id) ON DELETE SET NULL,  -- Default tag for new articles
    priority INTEGER DEFAULT 5,           -- 1-10, higher = more important
    is_active BOOLEAN DEFAULT true,
    last_fetched_at TIMESTAMP,            -- RSS: last successful poll
    last_email_received_at TIMESTAMP,     -- Newsletter: last inbound email (migration 000010)
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
    content TEXT,                         -- Display body (F14: semantic Markdown for newsletters)
    distilled_content TEXT,               -- F14: cheap-model extractive-compression output, fed to Score/Categorize/Reason
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
-- ADMIN CONSOLE TABLES (F9, migration 000011)
-- ============================================

-- Editable system prompts (one row per AI operation)
CREATE TABLE ai_prompts (
    operation     TEXT PRIMARY KEY,   -- 'score' | 'categorize' | 'summarize' | 'digest'
    system_prompt TEXT NOT NULL,
    updated_at    TIMESTAMP DEFAULT NOW()
);
-- Seeded with the 4 current strings; categorize seed omits the inline slug list
-- (the live tag slugs are appended by code at call time).

-- Persisted AI request/response log (gated by admin_settings.ai_logging_enabled)
CREATE TABLE ai_logs (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    operation     TEXT NOT NULL,
    model         TEXT,
    system_prompt TEXT,
    user_prompt   TEXT,
    response      TEXT,
    success       BOOLEAN NOT NULL,
    error         TEXT,
    duration_ms   INTEGER,
    created_at    TIMESTAMP DEFAULT NOW()
);
CREATE INDEX idx_ai_logs_created ON ai_logs(created_at DESC);

-- Generic key/value settings (seed: ai_logging_enabled = 'true')
CREATE TABLE admin_settings (
    key        TEXT PRIMARY KEY,
    value      TEXT NOT NULL,
    updated_at TIMESTAMP DEFAULT NOW()
);

-- ============================================
-- PERSONALIZATION & CONTENT TABLES (F10–F13)
-- ============================================

-- Fine-grained interest weights (F11) — mirrors topic_weights, keyword-level
CREATE TABLE keyword_weights (
    keyword    TEXT PRIMARY KEY,
    weight     FLOAT NOT NULL DEFAULT 1.0,   -- clamped 0.1–2.0
    updated_at TIMESTAMP NOT NULL DEFAULT NOW()
);

-- AI-extracted keywords per article (F11) — lets an open/vote map back to keywords.
-- Cascades on article delete; the weight was already banked, so no learning is lost.
CREATE TABLE article_keywords (
    article_id UUID NOT NULL REFERENCES articles(id) ON DELETE CASCADE,
    keyword    TEXT NOT NULL,
    PRIMARY KEY (article_id, keyword)
);

-- Daily vocabulary history (F12) — avoids repeating recently shown words
CREATE TABLE vocab_history (
    id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    word       TEXT NOT NULL,
    meaning    TEXT NOT NULL,
    example    TEXT NOT NULL,
    shown_on   DATE NOT NULL,
    created_at TIMESTAMP NOT NULL DEFAULT NOW()
);

-- F10b: retention job must delete parent newsletters without failing on child FKs.
-- Migration drops the existing articles_parent_article_id_fkey and re-adds it as:
--   REFERENCES articles(id) ON DELETE SET NULL
-- so deleting a parent orphans (rather than blocks) any surviving child articles.

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
GET    /api/sources/export       - Export all sources as OPML (F6.1)
POST   /api/sources/import       - Import sources from an OPML file (F6.1)

# Backup
POST   /api/backup/send          - Manually trigger the periodic OPML backup email (F6.2)

# Articles
GET    /api/articles             - List articles (paginated, filterable by tag)
GET    /api/articles/:id         - Get article details
GET    /articles/{id}            - In-app reader page for one article (F5.3; renders Content, Markdown for newsletters)
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

# Personalization & Reading (F10–F13)
GET    /r/{article_id}           - Record open + mark read, 302 → external_url (RSS) or /articles/{id} (newsletter). F5.3 ships record+redirect; F11 adds keyword/topic weight bumps.
GET    /cloud                    - Render the topic cloud from keyword_weights (F11)
GET    /feed                     - Full-screen scrollable reading feed of bookmarks (F13)
POST   /api/retention/run        - Manually trigger the 24h retention delete (F10b)

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

# Admin Console (F9) — unauthenticated in v1
GET    /admin/logs               - AI logs page (paginated, filter by operation)
POST   /api/admin/logs/toggle    - Enable/disable AI logging (admin_settings flag)
DELETE /api/admin/logs           - Clear all AI logs
GET    /admin/prompts            - Prompt settings page
PUT    /api/admin/prompts/{op}   - Update a system prompt
POST   /api/admin/prompts/{op}/reset - Reset a prompt to its in-code default
GET    /admin/tags               - Tag settings page
POST   /api/admin/tags           - Create tag
PUT    /api/admin/tags/{id}      - Update tag
DELETE /api/admin/tags/{id}      - Delete tag
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
0. **Content input (F14)** — the LLM scores/categorizes from `distilled_content`
   (Markdown conversion + cheap-model extractive compression), not the first 500
   chars of raw HTML. Governs input size via `AI_MAX_CONTENT_CHARS`. This is the
   single biggest lever on scoring quality for newsletters.
1. **AI quality + global importance** — LLM rates each article 1–5 at ingest time using its own knowledge of what's globally significant. No external trend API needed.
2. **Topic weights** — `topic_weights` table (topic slug → float 0.1–2.0) biases the AI prompt toward user interests. Starts neutral (1.0 for all topics); adjusted by feedback over time.
3. **Keyword weights (F11)** — `keyword_weights` table (keyword → float 0.1–2.0) is the fine-grained layer. Top weights are injected into the `Score` prompt alongside topic weights. Fed by both explicit votes and implicit opens (`/r/{id}`).
4. **Digest cap** — configurable `DIGEST_CAP` (default 20) and `DIGEST_MIN_SCORE` (default 3) filter and limit the digest.

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

**Implicit feedback (F11):** in addition to explicit votes, opening an article via the
`GET /r/{article_id}` redirect is a weak positive signal. It bumps `keyword_weights`
for the article's extracted keywords (**+0.05**) plus the article's tag weight, then
`302`s to the real URL. Explicit votes also update `keyword_weights` (up → +0.1,
down → −0.1) for the article's keywords, so both layers learn together.

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
