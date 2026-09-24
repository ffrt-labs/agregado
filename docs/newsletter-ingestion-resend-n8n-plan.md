# Newsletter ingestion: Resend + n8n replace Cloudflare Email Routing

## Context

The Bridge (CONTEXT.md) turns one inbound newsletter email into a private
Atom feed entry the Reader can poll. This spec is the destination of
[Map #110](https://github.com/ffrt-labs/agregado/issues/110), which
redesigned newsletter *ingestion* — replacing Cloudflare Email Routing and a
single Worker with Resend (receiving) + n8n (parsing, extraction, storage
writes) + a slimmed Worker (serving only). Storage and serving (R2 + D1 + the
Worker's two read paths) carry over from
[Map #95](https://github.com/ffrt-labs/agregado/issues/95) unchanged, just
narrower in scope now that the Worker no longer parses anything.

The structural decision and its rationale live in
[ADR-0007](adr/0007-resend-n8n-cloudflare-split-for-newsletter-ingestion.md);
the ingress-security consequence lives in
[ADR-0001's 2026-09-23 amendment](adr/0001-tunnel-ingress-is-the-auth-boundary.md#amendment-2026-09-23-n8ns-resend-webhook-ingress).
This document is the buildable spec — what to build, not why.

**This document does not build anything.** It is followed by the buildable
issues listed at the end, ordinary agent work from here.

## Decisions (settled, inherited or fresh)

| Question | Decision | Source |
|---|---|---|
| Receiving | Resend, own domain (MX record), catch-all any local-part | map #110 |
| Parsing/extraction/writes | n8n Webhook node, direct Resend receiver, no relay | map #110 |
| Extraction chain | cheerio, ADR-0004's chain order verbatim | [#114](https://github.com/ffrt-labs/agregado/issues/114) |
| Storage/serving | R2 + D1 + slimmed Worker, unchanged from #95 | map #110 |
| Worker's role | Two read paths only; `email()` deleted | map #110 |
| Subresource capture | n8n HTTP Request node per asset, before write | map #110 |
| Ingress path | Reuse vacated ingest hostname, new random path, routes to n8n | [#111](https://github.com/ffrt-labs/agregado/issues/111) |
| Ingress auth | Svix HMAC signature check + Cloudflare rate limit; no Access | [#111](https://github.com/ffrt-labs/agregado/issues/111) |
| Failure handling | Whole-execution fail-loud; Resend's ~27h retry; upsert-keyed idempotency | [#113](https://github.com/ffrt-labs/agregado/issues/113) |
| Alerting | Active, once per `hash(Message-ID)`, on first failure | [#113](https://github.com/ffrt-labs/agregado/issues/113) |
| Subresource failure | Degrades gracefully — skip asset, proceed with write | [#113](https://github.com/ffrt-labs/agregado/issues/113) |
| Source onboarding | No `FORWARD_EMAIL`; n8n no-ops on `pending`; manual flip to `active` | map #110 |
| Enrichment input | n8n passes extracted content inline at ingest; enricher never reads Miniflux's copy | [#107](https://github.com/ffrt-labs/agregado/issues/107) |
| Digest link, email-only Article | Links to Index key verbatim — canonical URL or `/p/{uuid}` | [#108](https://github.com/ffrt-labs/agregado/issues/108) |
| Entry identity | `hash(Message-ID)` end-to-end, unchanged | inherited from #95 |
| Feed protection | Per-Source hashed Basic auth, `/feed/{source-id}.atom`, unchanged | inherited from #95 |
| Permalink | `/p/{uuid}`, self-contained, never redirects, unchanged | inherited from #95 |
| Workflow versioning | Exported JSON, committed to this repo, same bar as #104 set for Worker code | map #110 |
| Workflow sync mechanism | **Not yet specified** — manual export vs. API/CLI-driven | map #110, fog |

## Architecture

```
Resend (receive, own domain, MX record)
   │  Svix-signed `email.received` webhook
   ▼
n8n Webhook node — routed via Cloudflare Tunnel, new ingress path (ADR-0001 amendment)
   │  1. Verify Svix signature (Code node, hand-rolled HMAC-SHA256)
   │  2. Call Resend content-fetch API → full headers, HTML, text
   │  3. Look up Source by inbound alias in D1 — bounce if unknown, no-op if `pending`
   │  4. Extract canonical URL: `Archived-At` header → scraped "view in browser" → nothing (cheerio)
   │  5. Capture every remote subresource (HTTP Request node per asset; skip+log on failure)
   │  6. Sanitize: two policies off one sanitizer — permalink fidelity vs. feed-entry signal
   │  7. Write original HTML → R2 (key: hash(Message-ID))
   │  8. Write entry metadata + readable content → D1 (upsert on hash(Message-ID))
   │  9. (No enrichment call here — see "Enrichment call" below; it runs on Miniflux's new_entries)
   │ 10. On any failure in 1-4 or 7-8: fail the execution loudly, alert once per hash(Message-ID)
   ▼
D1 (Source registry, entry metadata) + R2 (original HTML, subresources)
   ▲  read
   │
Cloudflare Worker (slimmed — two read paths, no domain logic)
   ├── GET /feed/{source-id}.atom  — Basic-Auth checked against D1, computed per-request
   └── GET /p/{uuid}               — sanitized original HTML served from R2
   ▲
   │  polled                              │  clicked
Miniflux (Reader backend)          Human, via digest link or feed reader
```

## Component specs

### 1. Resend receiving

- MX record on the project's own domain, routed to Resend Inbound.
- Catch-all: any local-part is accepted, matching Source ≜ alias ≜ feed —
  adding a Source needs no Resend-side registration.
- No Resend-side filtering; unknown/unregistered aliases are bounced by n8n
  after lookup (step 3 above), not by Resend.

### 2. n8n workflow

**Trigger.** Webhook node, custom path `/webhook/resend-<random-suffix>`
(generate once at deploy; not derivable from anything public), POST only.

**Signature verification.** Code node, before any other step. Compute
HMAC-SHA256 over `${svix-id}.${svix-timestamp}.${raw_body}` using the Resend
webhook signing secret; reject (respond 401, do not proceed) on mismatch or
missing headers. Requires the **raw** request body — confirm n8n's Webhook
node config captures it unparsed, since JSON auto-parsing would break the
signature computation.

**Content fetch.** HTTP Request node against Resend's content-fetch API using
the `email.received` webhook payload's message reference, retrieving full
headers (including `Message-ID`), HTML body, and text body.

**Source lookup.** D1 query by inbound alias (the recipient local-part) via
Cloudflare's REST API. Three outcomes:
- No row: bounce, no-op, no write, no alert (not a failure — an unregistered
  address is not this system's problem).
- `status = 'pending'`: no-op, no write, no alert (keeps unconfirmed
  subscription noise out of D1; the human confirms manually in Resend's
  dashboard and flips the row).
- `status = 'active'`: proceed.

**Extraction (cheerio).** Port ADR-0004's `resolveCanonicalURL` chain and
`isCanonicalCandidate` predicate verbatim: `Archived-At` header (RFC 5064) →
scraped "view in browser" anchor → nothing. `http(s)` only; reject
`unsubscribe`, `pixel`, `mailto:`, and social-share links. Requires a custom
n8n image with cheerio installed and allowlisted — see Buildable issues,
this is infrastructure outside this repo.

**Subresource capture.** For every remote asset referenced in the extracted
HTML (images, etc.), an HTTP Request node fetches and inlines it, behind a
beacon filter (tracking pixels excluded, per the inherited permalink
guarantee). A single asset's fetch failure is caught and logged; the chain
proceeds without it (see Failure handling).

**Sanitization.** Two policies off one sanitizer, per the inherited decision:
the permalink gets a fidelity-preserving pass (original HTML, subresources
inlined); the feed `<content>` gets a signal-preserving pass (readable
extraction). Click-tracking wrappers are decoded locally in both, never
followed remotely.

**Writes.**
- R2: original HTML (post subresource-inlining), keyed on `hash(Message-ID)`.
  Plain overwrite on retry.
- D1: entry metadata (Atom `<id>` = `hash(Message-ID)`, permalink UUID
  derived the same way, canonical URL if recovered, readable content) via
  `INSERT OR REPLACE` keyed on `hash(Message-ID)`.
- Same-`Message-ID` redelivery (Resend retry) is dropped by the upsert — no
  duplicate entries, no double-write side effects, since both writes are
  idempotent by construction.

**Enrichment call.** Not made by this workflow. #77's endpoint requires a
Miniflux `entry_id`, which does not exist until Miniflux has polled the feed, so
an inline call at ingest cannot satisfy it. The entry is enriched when
Miniflux's `new_entries` webhook fires into #77's workflow, which recognises a
bridge permalink URL and supplies `readable_content` (read from D1 by
permalink UUID) as `bridge_content`. See `n8n/README.md`.

**Failure handling.**
- Any failure in signature verification, content fetch, extraction, or the
  R2/D1 writes: fail the n8n execution (do not catch and continue). Resend's
  retry redelivers the same webhook up to ~27h later; the re-run is safe
  because of the upsert keying above.
- On the **first** execution that fails for a given `hash(Message-ID)`
  (check a D1 column/table before alerting; set it on alert so subsequent
  retries of the same message don't re-alert): send an active notification
  (not passive-log-only).
- A subresource-fetch failure is caught locally, logged, and does **not**
  fail the execution — the write proceeds with that asset missing from the
  permalink.

### 3. Slimmed Worker

- Delete the `email()` handler entirely — no domain logic left in the
  Worker.
- `GET /feed/{source-id}.atom`: Basic-Auth checked against the D1 row's
  hashed secret; computes the Atom feed per request from D1 (unchanged from
  #95/#97's design — no materialization, ETag-cacheable).
- `GET /p/{uuid}`: serves the sanitized original HTML from R2, unchanged.
- No Worker code touches Resend, n8n, or the extraction chain.

### 4. Ingress (Cloudflare Tunnel)

Per [ADR-0001's 2026-09-23 amendment](adr/0001-tunnel-ingress-is-the-auth-boundary.md#amendment-2026-09-23-n8ns-resend-webhook-ingress):
add `/webhook/resend-<random-suffix>` on the existing ingest hostname,
routing to n8n's origin (not the app's). Add a Cloudflare rate-limit rule
scoped to this path. Do not add Cloudflare Access on this path.

### 5. Source onboarding (unchanged shape, changed mechanism)

1. Create the D1 row (`status = 'pending'`) for the new Source — same as
   #95/#99's design.
2. Subscribe at the publisher, using the Source's alias address on the
   project's own domain.
3. Confirmation mail arrives at Resend; n8n no-ops on it (Source is
   `pending`).
4. Human reads the confirmation email in **Resend's dashboard** (not Gmail —
   this is the whole point of the redesign) and manually flips the D1 row to
   `active`.
5. Subscribe to the feed in Miniflux, same as any other Source.

## Not yet specified (carried on the map, unaffected by this spec)

- **n8n workflow JSON sync mechanism** — manual export-and-commit vs.
  API/CLI-driven sync on deploy.
- **Sequencing against [#84](https://github.com/ffrt-labs/agregado/issues/84)**
  (cutover) and [#85](https://github.com/ffrt-labs/agregado/issues/85)
  (remove the superseded system).
- **Per-Source preference signal** in `PREFERENCES.md`'s nightly regeneration
  ([#59](https://github.com/ffrt-labs/agregado/issues/59)).

## Out of scope

- Building this spec's contents — see Buildable issues below for that work.
- Changing #77's enrichment endpoint.
- Backfilling newsletters that pre-date the Bridge.
- Migrating existing/future Sources off Cloudflare Email Routing for reasons
  other than this redesign.

## Buildable issues

Filed as GitHub issues, each independently buildable once its listed
dependency is closed:

1. **Verify the live n8n instance's version and Task Runner configuration**
   (homelab-edge repo or live instance — not this repo). Confirms where the
   cheerio allowlist env var needs to live, per
   [research #114](https://github.com/ffrt-labs/agregado/issues/114)'s open
   TODOs. Blocks everything downstream that touches the Code node.
2. **Build cheerio into the n8n image that executes Code-node scripts**
   (custom Dockerfile, `NODE_FUNCTION_ALLOW_EXTERNAL=cheerio`, restart).
   Depends on (1).
3. **Provision Resend**: own-domain MX record, catch-all inbound routing,
   webhook signing secret retrieval.
4. **Add the Cloudflare Tunnel ingress rule and rate-limit** for
   `/webhook/resend-<random-suffix>` → n8n's origin.
5. **Build the n8n workflow**: Webhook trigger, Svix verification Code node,
   Resend content-fetch, Source lookup against D1, cheerio extraction chain
   (ported from ADR-0004's fixtures), subresource capture, sanitization,
   R2/D1 writes, inline enrichment call, failure handling and alerting.
   Depends on (2), (3), (4).
6. **Slim the Worker**: delete `email()`, verify the two read paths are
   unaffected.
7. **Export and commit the n8n workflow JSON** to this repo. Depends on (5).
8. **Live-verify** with the three ADR-0004 crafted-email fixtures plus one
   deliberately-malformed fixture, per this ADR's Verification section.
   Depends on (5), (6), (7).
9. **Update Source onboarding docs/runbooks** to drop `FORWARD_EMAIL`
   references and describe the Resend-dashboard confirmation step.
