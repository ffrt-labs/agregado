# ADR 0003 — Deliberate non-goals

**Status:** Accepted
**Date:** 2026-07-17 (consolidating decisions from Phases 15–19)

## Context

`docs/TODO.md` (now `docs/ROADMAP-ARCHIVE.md`) recorded deliberate non-goals as
unchecked checkboxes, e.g.:

```markdown
- [ ] **No robots.txt** — proportionate for a single-user, subscriber-driven,
      once-per-URL fetch. Revisit if this ever serves more than one person
```

These are decisions, not tasks. They will never be checked. Formatting them
identically to open work made the roadmap's checkbox counts meaningless and made
"what's left to do?" unanswerable — one symbol carrying three meanings (open
work / deliberate non-goal / acceptance criterion) with nothing enforcing which.

This ADR collects them so they stop masquerading as a backlog.

## Decision

The following are **deliberately not built**. Each records why, and what would
justify revisiting.

### No `robots.txt` handling in the article fetcher

Proportionate for a single-user, subscriber-driven, once-per-URL fetch: the app
fetches pages its owner already subscribed to, once each, at a rate a human
browsing would exceed. The fetcher sends an honest User-Agent identifying the
project.

**Revisit if** this ever serves more than one person, or fetches URLs the user did
not subscribe to.

### No enrichment retry

A transient fetch failure leaves an article permanently on feed content.
`content_source` makes this countable rather than invisible, and the admin
backfill endpoint re-drives it manually.

Not built because delayed retry needs a TTL + dead-letter-exchange trick that
RabbitMQ will not do natively, which is disproportionate to the impact (an
article scored on a teaser instead of full text).

**Revisit if** the `feed_content` / `feed_description` share of `content_source`
turns out to be materially driven by transient failures rather than real
`ErrBlocked` / `ErrThinContent` cases.

### `ai.Compress` not built

`Distill` is algorithmic (no AI call). If it proves too lossy, `distilled_content`
is already the right seam to swap a compression step in behind — the column and
the pipeline stage exist.

**Revisit if** score/reason quality visibly degrades on long articles.

### Roundup newsletters ingest as single articles

Link extraction and `parent_article_id` children stay deferred. Phase 17 built the
`Fetch` primitive they were always blocked on, so this is now unblocked but
unstarted (described in the archive as Phase 2.5).

Note this is **distinct** from newsletter *canonical URL* extraction (giving one
newsletter one URL of its own), which is tracked as a live issue.

### Article retention: nothing is ever deleted

No article is ever removed. Growth is unbounded, and Phase 17 made it materially
worse by growing average article size from a ~200-byte teaser to 10–70KB of
Markdown.

Accepted for now on the basis that a single user's feed volume is small relative
to disk. Described in the archive as Phase 11.2.

**Revisit when** disk becomes a real constraint, or sooner if raw newsletter HTML
persistence ships (each newsletter adds 50–200KB) — which is why that HTML lives
in its own table, so retention can be applied to it independently of article rows.

### No structured logging, metrics, or alerting

No slog, no metrics, and a declared dead-letter queue that is never drained.
Described in the archive as Phases 5.1/5.2.

The consequence is load-bearing elsewhere: because there is no channel that
reliably reaches the operator, warnings must be routed to where the operator
already is. This is why Phase 19's misconfiguration guard renders a banner **in
the digest email** rather than logging — the email is the one artifact guaranteed
to be seen.

**Revisit if** the app ever runs unattended for someone other than its author.

### App-level authentication

Not built. The Cloudflare Tunnel ingress ACL is the boundary. See **ADR 0001**.

## Consequences

- "What's left to do?" becomes answerable — `gh issue list` returns only real,
  closable work.
- Each non-goal now carries an explicit revisit trigger, so they are decisions
  with expiry conditions rather than indefinite silence.
- Risk: an ADR is easier to not-read than a checkbox is to not-see. Mitigated by
  `docs/agents/domain.md`, which instructs agents to read ADRs touching the area
  they are about to work in.

## Notes

Several of these are load-bearing for decisions documented elsewhere — the
observability gap justifies the Phase 19 banner; the retention gap justifies the
separate raw-HTML table in Phase 20. A non-goal is not inert. It is a constraint
that other designs have to route around, which is precisely why it deserves to be
written where those designs can find it.
