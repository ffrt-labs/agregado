# ADR 0003 — Deliberate non-goals

**Status:** Accepted
**Date:** 2026-07-17 (consolidating decisions from Phases 15–19)
**Amended:** 2026-11-16 — the RabbitMQ, dead-letter and admin-endpoint
mechanics two entries relied on were removed (#85). See "Amendment,
2026-11-16" below.
**Amended:** 2026-11-26 — deleting a Newsletter Source is the one exception
to "nothing is ever deleted" (#230). See "Amendment, 2026-11-26" below.

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

One exception since 2026-11-26: deleting a Newsletter Source removes its
stored issues (see the amendment below).

**Revisit when** disk becomes a real constraint, or sooner if raw newsletter HTML
persistence ships (each newsletter adds 50–200KB) — which is why that HTML lives
in its own table, so retention can be applied to it independently of article rows.

### No structured logging, metrics, or alerting — *partially superseded (issue #4)*

Originally: no slog, no metrics, and a declared dead-letter queue that is never
drained. Described in the archive as Phases 5.1/5.2.

**The structured-logging and dead-letter halves are now built** (issue #4 /
Phase 22). The revisit trigger below fired: the app is being run in a homelab
alongside other personal apps, feeding a central log collector — i.e. it now
runs for an operator, not just its author. Agregado became a telemetry
*producer* — every diagnostic is a structured `slog` record emitted as JSON to
stdout (`internal/logging`), and `articles.dlq` now has a consumer that logs and
drains every dead-lettered message (`internal/broker/deadletter.go`). The
producer contract is deliberately narrow: structured JSON on stdout, nothing
more.

**Scope of the producer contract (amended, issue #83).** "Structured JSON on
stdout, nothing more" binds the **long-running service** — the process whose
stdout a log collector parses. It does not bind one-shot operator commands run
by hand at a terminal, whose stdout *is* the deliverable: `cmd/migrate-ownership`
prints a counts table and a before/after record comparison that a human reads
to decide whether to proceed. Rendering that as slog records would serve
nobody. The carve-out is enforced rather than assumed: `operatorTools` in
`internal/logging/unstructured_test.go` is an explicit per-file allowlist, and
`TestOperatorToolExemptionStaysNarrow` fails if an entry outlives its file or
if `cmd/agregado` is ever added to it.

**Still not built:** metrics (`/metrics`), alerting, and the central
Loki/Alloy/Grafana collector itself — those live in a separate repository and
are revisited alongside that stack.

Note the load-bearing consequence has shifted, not vanished. Phase 19's
misconfiguration guard still renders a banner **in the digest email**, and that
is still justified for the *email recipient* (who never sees stdout). But the
banner is no longer the *only* channel that reaches the operator: the same
condition is now also a `slog.Warn` (`internal/digest/generator.go`), visible in
the log stream.

**Revisit metrics/alerting if** stdout logs prove insufficient to diagnose a
failure after the fact — that is the trigger for the collector session.

### App-level authentication

Not built. The Cloudflare Tunnel ingress ACL is the boundary. See **ADR 0001**.

## Amendment, 2026-11-16: entries that named removed components

Recorded while removing the superseded system
([#85](https://github.com/ffrt-labs/agregado/issues/85)). The non-goals
stand; some of the reasons and mechanisms they cite are gone.

**No enrichment retry.** The legacy pipeline this entry described is gone:
RabbitMQ ([#236](https://github.com/ffrt-labs/agregado/issues/236)), the
storage worker ([#237](https://github.com/ffrt-labs/agregado/issues/237)) and
the admin endpoints ([#238](https://github.com/ffrt-labs/agregado/issues/238)),
including the backfill trigger. The "TTL + dead-letter-exchange" reason no
longer applies, because there is no broker. Enrichment now runs through the
private Article Index endpoint. A failed record is stored as failed with its
reason, and a repeat request for the same canonical URL returns the existing
record rather than retrying (`internal/articleindex/service.go`). n8n retries a
failed *call* (3 attempts, then an alert; `n8n/README.md`), but there is still
no retry of a failed *record*. The non-goal holds; the revisit trigger is now
"failed Article Index records turn out to be mostly transient failures."

**Structured logging and dead-letter queue.** The `articles.dlq` consumer
(`internal/broker/deadletter.go`) was deleted with the rest of the broker, so
the "dead-letter half" of the partial supersession no longer exists. The
structured-logging half and its producer contract are unchanged. The `broker`,
`deadletter`, `storage`, `backup` and `poller` log components went with their
code.

**The digest misconfiguration banner.** Agregado no longer emails a Digest.
n8n sends the persisted Digest artifact
([#239](https://github.com/ffrt-labs/agregado/issues/239)), rendered by
`internal/digestartifact`, which has no localhost banner. The banner and its
`slog.Warn` remain in `internal/digest/generator.go`, but no package imports
`internal/digest` any more, so the "banner in the digest email" consequence no
longer reaches anyone.

**App-level authentication.** Still not built. Agregado's boundary is now the
tailnet plus a shared secret on private routes
([ADR-0008](0008-agregado-is-tailnet-only.md)); ADR-0001's tunnel ACL covers
only the n8n ingest path.

## Amendment, 2026-11-26: deleting a Newsletter Source removes its issues

Recorded with Delete in the Newsletters app
([#230](https://github.com/ffrt-labs/agregado/issues/230), spec
[#225](https://github.com/ffrt-labs/agregado/issues/225),
[ADR-0012](0012-newsletter-sources-are-managed-by-a-tailnet-only-homelab-app.md)).

**Article retention: nothing is ever deleted** gains one exception. Deleting
a Newsletter Source (see **Delete** in `CONTEXT.md`) removes everything the
Bridge stored for it: its Miniflux feed, its issues' original HTML in R2
(keyed by `entries.id`), its D1 `entries` rows and its `sources` row. The
comment on `entries.published_at` in
`email-worker/migrations/0001_sources_and_entries.sql` ("never pruned") is
true of every Source except a deleted one.

The exception is narrow:

- It happens only when the owner deletes a Source by hand, one at a time.
  There is still no retention policy, age-based pruning or bulk delete.
- Articles that Source already produced stay: their Article Index rows,
  Scores and Bookmarks are not touched. Pause, unlike Delete, keeps
  everything.

**Accepted cost:** Bridge permalinks (`/p/<uuid>`) for that Source's issues,
including those linked from old Digests, return 404 afterwards, because the
`entries` row and the R2 object behind them are gone.

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
