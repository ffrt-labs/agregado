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

**Active phase:** Phase 18 — Close the Personalization Loop — **DONE, live-verified**.
**Roadmap:** `docs/TODO.md` (phases + checkboxes). **PRD:** `docs/PRD.md`.
**Phase 18 plan:** PRD **F16** (no standalone plan doc this time — planned directly
into the shared plan file, then implemented in the same session).

> **Note on how Phases 17 and 18 were built:** the user explicitly asked for full,
> unattended implementations ("Exceptionally, implement this plan/task/phase
> completely for me, step-by-step") — the documented exception to the "user writes
> all code" rule above. This has now happened twice; if it keeps recurring, consider
> whether the exception should become the session default rather than something
> re-confirmed each time. Absent that instruction, the default guide-don't-do mode
> applies.

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
- **Phase 18: Close the Personalization Loop.** The app's core differentiator —
  digest personalization from 👍/👎 feedback — had **never once worked**. Live
  evidence before this phase: `topic_weights` 0 rows, `article_feedback` 0 rows,
  `article_tags` 0 rows, every `score` prompt ending
  `Topic interest weights (...):` followed by nothing. Five stacked breaks, each
  invisible alone: tags were assigned in the ranker **in memory only** (no
  `SetTags` existed); `article_tags` therefore stayed empty; `GetById` never
  loaded tags anyway (`db:"-"`, bare `SELECT *`); the 👍/👎 buttons POSTed to a
  route that didn't exist, and the *previous* GET+HMAC route was itself
  unreachable (its token generator had already been deleted); and
  `TopicWeightsRepo.Upsert`'s insert branch hardcoded `weight = 1.0`, silently
  discarding the first vote on every topic. Fixed: `Categorize` moved from the
  digest ranker into the `articles.enrich` stage (Phase 17) and now persists via
  new `ArticleRepo.SetTags`/`loadTags`; the dead feedback route was deleted and
  replaced with same-origin `POST /api/articles/{id}/feedback`; the first-vote
  math fixed; the UI's `hx-on::after-request` now gates its message on
  `event.detail.successful` instead of reporting success unconditionally. Also
  fixed `?sort=relevant|recent`, a second user-visible control wired to nothing
  (`List`/`ListBySource` took no sort argument at all). **No migration needed** —
  every table already existed; this was pure wiring.
  Covered by unit tests (`internal/storage/article_repo_test.go`,
  `internal/api/feedback_test.go`); **live-verified**: `article_tags` went 0 → 5
  (first ever) after forcing 5 articles back through enrich, with zero duplicate
  rows or repeat `categorize` calls on re-enrichment (proving the
  72-calls-for-45-articles cost bug is also fixed); a real 👍 click produced
  `article_feedback` 0 → 1 and `topic_weights` 0 → 1 **at exactly 1.1, not 1.0**;
  a subsequent `score` prompt read `- tech: 1.1` where it used to read nothing;
  `?sort=relevant` vs `?sort=recent` returned genuinely different article orders.
  **Not click-tested:** the UI false-success fix — Chrome extension wasn't
  connected this session; verified instead by confirming the endpoint's real
  status codes and matching an identical, already-working
  `event.detail.successful` pattern elsewhere on the same template.

### Next
- No active phase queued. Natural next candidates per `docs/TODO.md`: Phase 12's
  `keyword_weights` layer (now has a *verified working* `topic_weights` foundation
  to extend, instead of building on the silently-broken one), Phase 11.2 retention
  (nothing ever deletes an article — unbounded growth, more material now that
  Phase 17 grew average article size from a ~200-byte teaser to 10-70KB of
  Markdown), Phase 2.5 newsletter link extraction (Phase 17's `Fetcher` unblocked
  most of it, but needs real newsletter data in this DB to verify live), or
  Phase 5.1/5.2 observability (no slog, no metrics, a declared dead-letter queue
  that's never drained)
- Known gap: the CSS-soup fix from Phase 16 still hasn't been separately observed
  against a *newsletter* specifically (this local DB has no newsletter articles) —
  Phase 17's live verification covered RSS fetched-content, which exercises the
  same `textutil.Strip`/`Clean` path, but not the newsletter ingestion branch
- Known gap (carried from Phase 15): digest-email `/r/{id}` link only confirmed
  by template parse + `go vet`, not a live click-through — re-check next time
  real digest candidates exist
- Housekeeping note (Phase 17): 5 pre-existing articles had their
  `published_at`/`ingested_at` temporarily bumped to `NOW()` to pull them into the
  digest lookback window during live verification, then pushed back to
  `NOW() - 30 days` as an approximate (not exact) revert — original timestamps
  weren't captured first. Low-impact local test data, noted for honesty.
- Housekeeping note (Phase 18): live-verifying the feedback loop left real rows in
  place rather than reverting them — `article_tags` (5 rows from forcing
  re-categorization), one real `article_feedback` vote, and the resulting
  `topic_weights` bump (`tech` → 1.1). These are genuine proof the mechanism
  works, not corrupted test data (same reasoning as Phase 17's backfill), but
  worth knowing about if `tech`-tagged articles start scoring very slightly
  higher than expected.

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
