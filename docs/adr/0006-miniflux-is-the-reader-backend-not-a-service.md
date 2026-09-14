# ADR 0006 — Miniflux is the Reader's backend, not a service the Reader survives around

**Status:** Accepted
**Date:** 2026-09-09
**Issue:** [#58](https://github.com/ffrt-labs/agregado/issues/58)

## Context

The pivot ([#48](https://github.com/ffrt-labs/agregado/issues/48)) needed a call on
Miniflux: adopt it as the whole reader (decorated with AI from outside), adopt it as a
fetch/dedupe/store backend only, fork it, or rebuild the reader from scratch.

[#53](https://github.com/ffrt-labs/agregado/issues/53) lived with a plain, undecorated
Miniflux for a week on real feeds and never once used it as a reading UI — only as
RSS-centralization/fetch backend. That trial predates decoration (the `PUT
/v1/entries/{id}` write-back path [#49](https://github.com/ffrt-labs/agregado/issues/49)
confirmed) being live, so it does not, by itself, rule out a decorated Miniflux changing
daily behaviour. Weighed against that: building decoration for real means standing up
most of the enrichment library first, just to re-test a hunch, and a second documented
data point already exists — the digest email's own tap-through rate is near zero (map
#40), suggesting engagement with a decorated *list* view is not a behaviour this dev
reliably exhibits regardless of surface. Backend-only was decided without a second trial.

## Decision

**Miniflux is adopted as backend only: fetch, dedupe, and store, for RSS and newsletter
Sources alike. It is never logged into as a UI.**

1. **Fork: rejected.** [#49](https://github.com/ffrt-labs/agregado/issues/49)'s research
   found decoration works without forking, but that a fork is the only way to get
   score-sorted browsing inside Miniflux's own UI. Since Miniflux's UI is not browsed at
   all under this decision, that capability buys nothing. No justification survives.
   Where ranked browsing (if any) lives is [#63](https://github.com/ffrt-labs/agregado/issues/63)'s
   question, not this one's.

2. **`internal/ingestion/rss` (the RSS poller) dies**, fully subsumed by Miniflux's own
   fetch/parse/dedupe.

3. **`internal/ingestion/email` stays, repurposed.** It keeps receiving newsletters via
   the Cloudflare Email Routing webhook — preserving the canonical-URL recovery
   (`Archived-At` header, scraped "view in browser" link) that
   [#52](https://github.com/ffrt-labs/agregado/issues/52) found no bridge tool matches —
   but instead of writing an article straight into Agregado's database, it converts each
   email into a feed entry that Miniflux polls like any other Source. This unifies RSS
   and newsletter intake behind one backend. (This component is expected to eventually
   spin out into its own standalone tool; not designed here — see the map's Not yet
   specified.)

4. **The enrichment library re-fetches every ordinary Article itself**, via the existing
   readability extractor (`internal/fetch`), rather than trusting whatever Miniflux
   already fetched via its own scraper rules. An Email-only Article is the exception: it
   has no ordinary canonical page, so the newsletter bridge may supply its readable
   content directly for transient enrichment. Keeps content quality — one of the map's
   "opinions that are the dev's own" — entirely in Agregado's hands, and keeps Miniflux a
   pure black box for the one job it is kept for.

5. **No article content is mirrored into Agregado's Postgres.** Miniflux is the sole
   store of article content, RSS and newsletter alike. Agregado instead keeps an
   **Article Index** — score, tags, distilled summary, feedback/read events, keyed by
   canonical URL with Miniflux's numeric entry id cached alongside for Decoration
   writeback. This is not "Agregado keeps its own copy" reasserting itself: it holds no
   article body. It exists because Miniflux prunes by default
   (`CLEANUP_ARCHIVE_READ_DAYS=60`, `CLEANUP_ARCHIVE_UNREAD_DAYS=180`) and has no field to
   filter or sort by Score — both the nightly `PREFERENCES.md` regeneration
   ([#59](https://github.com/ffrt-labs/agregado/issues/59)) and the digest ranker need
   Score-queryable history that outlives Miniflux's own retention window.

6. **The RabbitMQ pub/sub ingestion layer dies.** `internal/storage/worker.go` and the
   `articles.ingest`/`articles.enrich` exchange are replaced: n8n receives Miniflux's
   `new_entries` webhook directly and calls Agregado's enrichment library/endpoint per
   batch. No internal queue survives the pivot — consistent with the map's standing
   preference that orchestration moves to n8n and Agregado's job narrows to the library
   and scorer.

## Rationale / alternatives rejected

- **Whole reader, decorated (rejected).** The strongest datum on the whole map — "the
  sophisticated thing was built and then not used" — applies here too. A second,
  independent behavioural signal (near-zero digest tap-through) points the same way. The
  cost of building decoration just to confirm this was judged not worth it.
- **Rebuild the reader (rejected).** Only on the table if decoration proved impossible
  *and* backend-only proved insufficient. Neither held.
- **Trusting Miniflux's own fetched content (rejected).** Considered as a way to avoid a
  redundant fetch, but it ties article quality to Miniflux's scraper-rules configuration
  instead of Agregado's own readability extractor, for no real savings at single-user
  volume.
- **Keeping the internal RabbitMQ queue (rejected).** Would have decoupled Agregado's
  processing rate from Miniflux's webhook delivery pattern (fire-and-forget, no retries),
  but adds a second orchestration layer underneath n8n for no demonstrated need yet.

## Consequences

- **Where ranked/browsable access to the full stream lives, if anywhere, is still open**
  — [#63](https://github.com/ffrt-labs/agregado/issues/63).
- **A standalone newsletter-to-feed tool is now fogged** on the map, not designed. Until
  it exists, `internal/ingestion/email` keeps doing double duty inside Agregado.
- **The Article Index's schema is not yet designed** — this ADR settles what it must
  *not* contain (article bodies) and what it must answer (Score-filterable history), not
  its concrete shape.
- **What happens to `internal/ingestion` as a package, and what else in the current
  codebase survives the pivot**, is [#62](https://github.com/ffrt-labs/agregado/issues/62)'s
  question, unblocked by this decision landing.
