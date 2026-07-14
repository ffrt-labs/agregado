# Agent Instructions

## Project Overview

Agregado is a newsletter/RSS aggregator with pub/sub architecture. It aggregates content from RSS feeds and email newsletters, stores them in PostgreSQL, and delivers daily digest emails. The project follows a learning-focused approach where the user is developing both the software and their understanding of systems design.

## ⚠️ Critical Rules — Read Before Doing Anything

> **The user writes ALL code.** Never use Write, Edit, or Bash to create or modify source files.
> Your role: explain concepts, ask guiding questions, review code the user writes, and suggest fixes.
> No exceptions unless the user explicitly says "just write it" — and even then, confirm first.
> This applies to everything: `go get`, new files, edits to existing files. Guide, don't do.

---

## Current State (update this each session)

**Active phase:** Phase 17 — RSS Article Content Fetching + Enrichment Stage —
**DONE, live-verified**. **Superseded Phase 16's deferred round** — TODO 16.1/16.2
were never implemented as originally written; Phase 17 replaced them.
**Roadmap:** `docs/TODO.md` (phases + checkboxes). **PRD:** `docs/PRD.md`.
**Phase 17 plan:** `docs/rss-content-fetching-plan.md` + PRD **F15**.

> **Note on how Phase 17 was built:** the user explicitly asked for a full,
> unattended implementation of the already-grilled plan ("Exceptionally, implement
> this plan step-by-step for me") — the documented exception to the "user writes
> all code" rule above. This was a one-time deviation for this phase; the default
> guide-don't-do mode applies again starting next session unless asked again.

### Completed
- Phases 1–9: Foundation, email ingestion, digest pipeline (ranker/generator/mailer/
  scheduling), web UI (Chi + HTMX article/source/digest views), AI scoring + relevance
  reasons, context-exhaustion fix
- Phase 10: Stable newsletter source identity (`sources.identity`, keyed on
  `List-Id` → `From:` header → envelope fallback, not the rotating SMTP envelope
  sender) + silent-failure-gap fixes in broker/storage
- Sources: OPML export/import, periodic backup email
- Digest: dedicated reading-focused email template, relevance labels, timeAgo helper
- Phase 15: Article open redirect (`GET /r/{id}`) + reader page (`GET /articles/{id}`)
  — fixes the "clicking any article title does nothing" bug (htmx `hx-post`
  `preventDefault`-ing the anchor's `href`, plus `ZgotmplZ` on newsletter
  `external_url`s). Covered by `internal/api/articles_test.go`; live-verified
  against the local dev stack (real RSS redirect + a temporary newsletter test row)
- Phase 16 lean core: `textutil.Strip` now removes `<style>`/`<script>` element
  *contents*, not just the tags (was leaking CSS/JS into every AI prompt and
  every excerpt); the hardcoded 500-char AI content cap is now configurable
  (`AI_MAX_CONTENT_CHARS`, default 8000); `.env`'s `AI_MODEL` corrected from a
  code model to the instruct model the config default already specified.
  Covered by `internal/textutil/textutil_test.go`; live-verified via a real
  digest refresh (fresh `ai_logs` rows show the corrected model)
- **Phase 17: RSS Article Content Fetching + Enrichment Stage.** RSS items mostly
  carry a link + a `<description>` teaser, not `<content:encoded>`, so the old
  worker silently scored the teaser. Now: `internal/ingestion/fetch` fetches
  `item.Link` (readability via `codeberg.org/readeck/go-readability/v2` — a
  maintained fork of the originally-planned, since-deprecated
  `go-shiori/go-readability` — → `html-to-markdown/v2`), a new `articles.enrich`
  RabbitMQ stage (own exchange/queue, own prefetch=5/workers=5) runs
  fetch → quality-gate (keep longer of fetched-vs-feed) → `textutil.Distill`
  (algorithmic, no AI call) → `UpdateContent` → `Score`/`Reason`, and every article
  now records `content_source` (`fetched|feed_content|feed_description|newsletter`)
  so teaser-scoring is countable instead of invisible. `Summarize` was rewritten to
  use title + `relevance_reason` + a real content excerpt instead of titles alone.
  Also fixed two latent bugs found while building this: `Qos(1, 0, false)` behind a
  single consumer tag was starving `consumer.go`'s 5 goroutines (`Consume` now takes
  explicit prefetch/worker params); `word_count`/`estimated_read_minutes` had never
  been written by any code despite the digest template rendering a read-time from
  them. New `POST /api/admin/enrich` backfills pre-Phase-17 rows.
  Covered by unit tests (`internal/textutil/distill_test.go`,
  `internal/ingestion/fetch/fetch_test.go`, `internal/domain/article_test.go`);
  **live-verified** against the real local stack — backfilled 45 pre-existing
  articles (30 fetched successfully, 15 fell back to feed content on real
  `ErrBlocked`/`ErrThinContent` cases), confirmed a live `/admin/logs` `score` row
  showing dense fetched Markdown (not CSS soup, not truncated at 500 chars), and
  confirmed a live `summarize` row showing title + reason + excerpt instead of a
  bare title list

### Next
- No active phase queued. Natural next candidates per `docs/TODO.md`: Phase 17's
  own deliberate known gaps (no robots.txt, no enrichment retry, roundup link
  extraction still deferred, `ai.Compress` not built), or resume earlier deferred
  items (Phase 3.1 AI-detected topic clustering, Phase 4 polish, Phase 5 hardening)
- Known gap: the CSS-soup fix from Phase 16 still hasn't been separately observed
  against a *newsletter* specifically (this local DB has no newsletter articles) —
  Phase 17's live verification covered RSS fetched-content, which exercises the
  same `textutil.Strip`/`Clean` path, but not the newsletter ingestion branch
- Known gap (carried from Phase 15): digest-email `/r/{id}` link only confirmed
  by template parse + `go vet`, not a live click-through — re-check next time
  real digest candidates exist
- Housekeeping note: while live-verifying Phase 17, 5 pre-existing articles had
  their `published_at`/`ingested_at` temporarily bumped to `NOW()` to pull them into
  the digest lookback window (same technique Phase 16 used). Unlike Phase 16, the
  original timestamps weren't captured first, so they were pushed back to
  `NOW() - 30 days` afterward as an approximate (not exact) revert — low-impact
  since these are pre-existing local test/seed articles, not real production data,
  but noted here for honesty rather than silently claiming a clean revert

### Key library choices (from PRD §4)
| Purpose | Library |
|---|---|
| PostgreSQL | `github.com/jackc/pgx/v5` (pool via `pgxpool`) |
| RSS parsing | `github.com/mmcdole/gofeed` |
| HTTP router | `github.com/go-chi/chi/v5` |

### How to orient in a new session
1. Run `git log --oneline -5` to see latest work.
2. Skim `docs/TODO.md` — checked items = done; first unchecked = where we are.
3. Read the relevant existing package before touching it.

---

## Learning-First Approach

**IMPORTANT: This is a learning project.** The user is developing both the software AND their understanding of systems design.

### How to Assist

1. **DO NOT write code directly** - Act as a tutor, not a code generator
2. **Guide step-by-step** - Explain what needs to be done and why
3. **Ask questions** - Help the user think through problems
4. **Explain concepts** - When introducing new patterns or libraries, explain the reasoning
5. **Review code** - When the user writes code, review it and provide feedback
6. **Suggest improvements** - Point out issues and let the user fix them

### Interaction Pattern

Instead of:
```
Here's the code for config.go: [writes full implementation]
```

Do this:
```
Let's implement config.go together.

First, you'll need to:
1. Define a Config struct with fields for each environment variable
2. Use struct tags for the env library

What fields do you think we need based on the plan?
(Hint: database connection, RabbitMQ, HTTP server port...)
```

### When User Gets Stuck

- Provide hints, not solutions
- Ask leading questions
- Show small code snippets as examples (not full implementations)
- Explain the "why" behind patterns

### Exceptions

The user may explicitly ask for code in specific cases:
- Boilerplate (go.mod, .gitignore, docker-compose.yml)
- Complex configuration that's not educational
- When they say "just write it" or similar

Always confirm before writing substantial code.

### Documentation Updates

**The agent IS responsible for updating documentation files** (`docs/*.md`, `AGENTS.md`).
This includes:
- Updating `docs/TODO.md` when phases are completed or added
- Updating `docs/PRD.md` when schema or design changes
- Updating `docs/STUDY_LOG.md` with learning topics during the session
- Keeping `AGENTS.md` current state section accurate

Documentation maintenance is housekeeping, not a learning opportunity.

### End-of-Phase Study Recommendation

**At the end of every completed phase**, always offer a specific study topic recommendation based on the user's performance during that phase. The recommendation should:
- Be tied to a concrete mistake or pattern observed during the session (not generic advice)
- Name a specific book, chapter, search term, or resource
- Be scoped to 15–30 minutes of focused reading

Do this unprompted whenever the last TODO checkbox in a phase is checked off.

### Plan Mode

- Make the plan extremely concise. Sacrifice grammar for the sake of concision.
- At the end of each plan, give me a list of unresolved questions to answer, if any.

---

## Agent skills

### Issue tracker

Issues live in GitHub Issues (`felipeafreitas/agregado`). See `docs/agents/issue-tracker.md`.

### Triage labels

Default label vocabulary (needs-triage, needs-info, ready-for-agent, ready-for-human, wontfix). See `docs/agents/triage-labels.md`.

### Domain docs

Single-context: one `CONTEXT.md` + `docs/adr/` at the repo root. See `docs/agents/domain.md`.
