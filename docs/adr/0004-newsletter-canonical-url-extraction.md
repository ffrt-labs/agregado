# ADR 0004 — Newsletter canonical-URL extraction and raw-HTML persistence

**Status:** Accepted
**Date:** 2026-07-23
**Issue:** [#2](https://github.com/ffrt-labs/agregado/issues/2)

## Context

Newsletter articles opened inside Agregado's own reader page instead of the
source post. Not a routing bug — newsletters genuinely had nowhere else to go.
The email parser set each newsletter's `external_url` to a synthetic
`newsletter:<uuid>` placeholder (invented only to satisfy `NOT NULL UNIQUE`), so
`/r/{id}` special-cased the placeholder and fell back to the reader.

Compounding it, the parser ran `html2text` and **discarded the HTML** — the
links and structure needed to recover a canonical URL were destroyed at the
ingestion boundary, unrecoverable from the database.

## Decision

Extract each newsletter's canonical URL at parse time and stop discarding the
HTML it came from.

1. **Extraction chain** (`resolveCanonicalURL`), mirroring `resolveSender`'s
   priority idiom: `Archived-At` header (RFC 5064) > a "view in browser" anchor
   scraped from the HTML with goquery > nothing. A single acceptance predicate
   (`isCanonicalCandidate`) gates both branches: `http(s)` only; reject
   `unsubscribe`, `pixel`, `mailto:`, and social-share links.

2. **Storage — canonical URL.** A new nullable `canonical_url` column on
   `articles`, *parallel* to `external_url`. `external_url` keeps its
   `newsletter:<uuid>` placeholder.

3. **Storage — raw HTML.** A new `newsletter_raw_html` table keyed by article
   id, carrying the HTML through the queue on `domain.Article.RawHTML` and
   persisted by the storage worker after `Create` yields an id.

4. **Redirect precedence** (`/r/{id}`): `canonical_url` if set, else
   `external_url` when it is a real page, else the reader page.

## Rationale / alternatives rejected

- **Writing the real URL into `external_url` (rejected).** The `newsletter:`
  prefix is load-bearing in three files with three meanings — "where do I send
  the user?", "is there an original link?", and (in enrichment)
  "should I HTTP-fetch this?". The enrichment guard is
  `strings.HasPrefix(article.ExternalURL, "newsletter:")`; breaking the prefix
  would silently start HTTP-fetching newsletters and flip their content source
  from `newsletter` to `fetched` with no error (the CHECK constraint permits
  `'fetched'`). A parallel column confines this change to redirect behavior.
  The honest cost — two URL columns with overlapping meaning and a surviving
  sentinel — is accepted; removing the sentinel is deferred to #3.

- **Raw HTML as a column on `articles` (rejected).** `articles` is `SELECT *`'d
  at six call sites and would detoast-and-transfer 50-200KB per newsletter on
  every page load, for data read once at ingestion. Raw HTML is provenance, not
  article content: different lifetime, different access pattern, and it needs an
  independent retention lever (purge later while keeping article rows forever).

- **`List-Archive` (RFC 2369) (rejected).** Points at the archive *index*, not
  the specific issue — worse than the reader page, which at least holds the
  actual content.

## Consequences

- **Forward-only.** Existing newsletters cannot be backfilled — their HTML was
  discarded at parse time. They keep the placeholder and the reader-page
  fallback permanently. This is why the reader page cannot be deleted.
- The general lesson: **store the raw artifact at the ingestion boundary, derive
  downstream.** Text is derivable from HTML; HTML is never derivable from text.
  The RSS path got this right almost by accident; the email path never did.

## Verification

Extraction and redirect precedence are covered by table-driven tests at two
seams (`internal/ingestion/email` `Parse`, `internal/api` `GET /r/{id}`).
Raw-HTML persistence has no test seam (see ADR-0002, no database-backed tests)
and is **live-verified** by sending three crafted emails — one with an
`Archived-At` header, one with only a "view in browser" link, one with neither —
through the real Worker → webhook → parser path, then confirming `canonical_url`,
the raw-HTML row, and the `/r/{id}` redirect per variant. The crafted emails are
kept as fixtures — the newsletter corpus the project never had.
