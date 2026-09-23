# ADR 0007 — Newsletter ingestion splits across Resend, n8n, and a slimmed Cloudflare Worker

**Status:** Accepted
**Date:** 2026-09-23
**Supersedes the ingestion half of:** [ADR-0004](0004-newsletter-canonical-url-extraction.md)'s implementation (not its decision — see Consequences), the Cloudflare Email Routing + single-Worker design [Map #95](https://github.com/ffrt-labs/agregado/issues/95) chartered and [#105](https://github.com/ffrt-labs/agregado/issues/105) never got to build
**Related:** [ADR-0001](0001-tunnel-ingress-is-the-auth-boundary.md) (amended alongside this ADR), [ADR-0006](0006-miniflux-is-the-reader-backend-not-a-service.md) (unaffected — see Consequences)
**Map:** [Map — Newsletter ingestion redesign: Resend + n8n replace Cloudflare Email Routing](https://github.com/ffrt-labs/agregado/issues/110)

## Context

The Bridge (CONTEXT.md) turns one inbound newsletter email into a private Atom
feed entry the Reader can poll. [Map #95](https://github.com/ffrt-labs/agregado/issues/95)
chartered it as a single Cloudflare Worker: receive via Email Routing, parse,
extract, store to R2/D1, serve. That design was never built — its build ticket,
[#105](https://github.com/ffrt-labs/agregado/issues/105), was superseded before
execution.

Two things changed the plan before it shipped. First, Map #95 assumed an inbound
mail provider that was already in place and free of DX cost; using it meant a
second personal Gmail just to click newsletter subscription-confirmation links,
since Cloudflare Email Routing has no dashboard rendering of received mail.
Second, the dev wants hands-on experience with Resend, a popular transactional/inbound
email API, and Resend's dashboard renders received HTML directly — the Gmail
hop disappears regardless of which service receives mail, because Resend's own
UI is a substitute for it.

Adopting Resend for the *receive* step alone would leave parsing and storage
writes stranded on Cloudflare's Workers runtime, reachable only via Resend's
webhook — a second cross-service hop for no architectural gain. The map's
standing preference (carried from ADR-0006 and the pivot maps before it) is
that orchestration lives in n8n and framework-owned code narrows to what needs
a specific runtime. Parsing and storage writes are exactly the kind of
per-message orchestration n8n already does for Miniflux's `new_entries`
webhook; there was no structural reason to keep them in a Worker once the
receive step moved off Cloudflare.

## Decision

**Newsletter ingestion splits across three services, replacing the single-Worker
design:**

1. **Resend receives.** Inbound mail lands on Resend, addressed on the
   project's own domain (an MX record makes Resend catch-all for any
   local-part — no per-Source registration with Resend itself, matching the
   Source ≜ alias model). A `<alias>@<domain>.resend.app`-style managed
   subdomain was rejected: it reads as disposable to newsletter publishers
   and risks getting flagged, where the project already controls DNS for a
   real domain.

2. **n8n parses, extracts, and writes.** n8n's Webhook node is Resend's direct
   receiver — no intermediate relay. It verifies Resend's Svix-signed
   `email.received` webhook by hand (HMAC-SHA256 over
   `${svix-id}.${svix-timestamp}.${raw_body}`; n8n has no native Svix
   support), calls Resend's content-fetch API for full headers/HTML/text,
   runs the cheerio-based extraction chain (below), captures subresources,
   and writes to R2 and D1 via Cloudflare's plain REST APIs — no Worker
   binding needed for writes from outside Cloudflare's own runtime.

3. **A slimmed Worker serves.** Storage and serving stay Cloudflare-native.
   The Worker keeps exactly two read paths — `GET /feed/{source-id}.atom`
   (Basic-Auth checked against D1) and `GET /p/{uuid}` (serves sanitized HTML
   from R2) — and its `email()` handler is deleted entirely. Feed/permalink
   serving needs uptime independent of the homelab (the trigger for this
   whole redesign was the homelab being unreachable), and [#97](https://github.com/ffrt-labs/agregado/issues/97)'s
   research already proved the per-request-from-D1 feed computation cheap
   enough; redoing that in n8n would re-solve a solved problem and tie
   serving-uptime to homelab-uptime, the opposite of the goal.

**Extraction chain: reimplemented with cheerio, not ported to `HTMLRewriter`.**
`HTMLRewriter` — the choice [#102](https://github.com/ffrt-labs/agregado/issues/102)
made when the plan was still a Worker — is Workers-runtime-only and unavailable
in n8n's Node.js Code node. Cheerio is the closest conceptual port from the
original Go/goquery implementation ADR-0004 was built against: both are
CSS-selector DOM traversal over static HTML. ADR-0004's crafted-email fixtures
port against cheerio's API instead of `HTMLRewriter`'s. Per
[research #114](https://github.com/ffrt-labs/agregado/issues/114): cheerio is
not built into n8n's Code node on self-hosted instances (n8n's own docs use it
as the canonical example of a module needing explicit configuration) and must
be added via a custom Docker image (`npm install cheerio` into the image that
actually executes Code-node scripts — the **task-runner** image on n8n 2.0+,
not the main `n8n` container, since Task Runners are default-on and relocate
Code-node execution to a separate process) plus
`NODE_FUNCTION_ALLOW_EXTERNAL=cheerio` set on that same process. Pure-JS
packages like cheerio have no known sandbox-incompatibility once installed and
allowlisted; the failure mode everyone hits is "module not found," not a
sandbox restriction. n8n's built-in HTML Extract node (cheerio internally, zero
config) is available for any part of the chain expressible as plain CSS
selectors.

**Subresource capture moves with extraction.** The permalink's
"self-contained forever" guarantee ([#101](https://github.com/ffrt-labs/agregado/issues/101),
inherited unchanged) requires fetching and inlining every remote asset at
ingest time. Was a Worker doing subrequests; now an n8n HTTP Request node per
subresource, before the R2/D1 write. Same guarantee, different execution
location.

**Error handling — whole-execution retry, not resumable, with active alerting.**
Any chain failure (webhook verification, content fetch, cheerio extraction,
R2/D1 write) fails the n8n execution loudly. Resend's Svix-backed webhook
retries for **~27 hours** (5s, 5m, 30m, 2h, 5h, 10h, 10h) before giving up —
n8n re-runs the whole chain from scratch on redelivery, with no internal
resume/state tracking. This is safe because both writes are keyed on
`hash(Message-ID)` (the inherited entry-identity scheme) with upsert/overwrite
semantics (`INSERT OR REPLACE` for D1, plain overwrite for R2): a retried
execution redoes the single-email work and lands on the same key. n8n sends an
active alert on the first failure per `hash(Message-ID)` — not passive-log-only,
and not repeated across retries, tracked via a check-and-set D1 column — because
a silently-dropped newsletter is invisible in a single-user system until
someone notices a Source went quiet. A subresource-fetch failure (one image
404s) degrades gracefully: skip and log the asset, proceed with the write,
rather than failing ingestion. This puts a soft edge on the permalink
guarantee — see Consequences.

**n8n's Resend-webhook ingress reuses the vacated ingest hostname on a new,
unguessable path, and amends ADR-0001.** See that ADR's
`## Amendment, 2026-09-23` section for the full ingress decision. In short:
the hostname that used to carry `^/webhook/email/?$` now routes a new
`/webhook/resend-<random-suffix>` path to n8n's origin instead of the app's;
Svix signature verification is the real auth (Cloudflare Access would block
Resend's own deliveries), backed by a scoped Cloudflare rate limit; any future
n8n workflow needing public ingress gets its own hostname, not a second path
here.

**Source onboarding drops the Gmail-forward hack.** No more `FORWARD_EMAIL`.
n8n no-ops on inbound mail for `pending` Sources, keeping unconfirmed noise out
of D1. Flipping `pending` → `active` stays manual — read the confirmation
email directly in Resend's dashboard, then flip the row — deliberately not
auto-detected, since confirmation-email shapes vary too much across publishers
and a false-positive auto-flip starts ingesting garbage from an unconfirmed
subscription.

**The n8n workflow is committed to this repo as exported JSON**, same
review/diff/CI bar [#104](https://github.com/ffrt-labs/agregado/issues/104)
set for the Worker's code. The sync mechanism between the committed JSON and
the live workflow (manual export vs. n8n API/CLI on deploy) is not settled by
this ADR — see the map's Not yet specified.

## Rationale / alternatives rejected

- **Keep everything in one Cloudflare Worker, just swap Email Routing for a
  Resend-to-Worker webhook (rejected).** Would have kept parsing/extraction on
  the Workers runtime for no reason once receiving moved off Cloudflare — the
  map's standing n8n-orchestrates preference applies here exactly as it does
  to Miniflux's `new_entries` handling.
- **A relay Worker in front of n8n, or n8n Cloud, so n8n's ingress stays off
  the homelab (rejected).** Either costs a second n8n environment or
  reintroduces the "concentrate everything in Resend/n8n" compromise this
  redesign is trying to avoid; ADR-0001's amendment accepts the homelab-uptime
  cost this creates because Resend's ~27h retry window is a real buffer
  Cloudflare Email Routing's retry behavior never documented.
- **Moving storage/serving into n8n and Cloudflare REST calls end-to-end
  (rejected).** [#97](https://github.com/ffrt-labs/agregado/issues/97)'s
  research already solved cheap per-request feed computation from D1;
  reimplementing it in n8n would tie serving-uptime to homelab-uptime, exactly
  what this redesign is trying to avoid for the one surface (feed/permalink)
  that most needs independence from it.
- **Auto-detecting Source confirmation instead of a manual dashboard read
  (rejected).** Confirmation-email shapes vary too much across publishers; a
  false-positive auto-flip starts ingesting garbage from an unconfirmed
  subscription, and this is a single-user system where a manual step costs
  nothing.

## Consequences

- **ADR-0004's extraction *decision* is unchanged; only its implementation
  moves.** The chain order (`Archived-At` → scraped view-in-browser anchor →
  nothing) and its acceptance predicate carry over verbatim to cheerio.
  ADR-0004 itself is **not amended** — cheerio vs. `HTMLRewriter` vs. the
  original goquery is implementation, not a fresh trade-off on the decision
  ADR-0004 recorded.
- **ADR-0006 is unaffected.** Storage/serving stay Cloudflare-native exactly
  as ADR-0006 assumed when it said n8n orchestrates *between* already-built
  pieces; this ADR is the first time n8n itself parses newsletter email,
  which is why it earns its own ADR rather than landing as an ADR-0006
  amendment.
- **ADR-0001 is amended, not superseded.** Its ingress-ACL-is-the-boundary
  decision holds; this ADR adds a new ingress path and corrects a
  now-false "never changes again" claim in ADR-0001's own Decision text.
- **Ingestion uptime now depends on the homelab; storage/serving uptime does
  not.** This is a deliberate asymmetry, not an oversight — see Decision's
  ingress-amendment summary and ADR-0001's amendment for the accepted-cost
  reasoning.
- **The "permalink is self-contained forever" guarantee gets a soft edge.**
  A subresource-fetch failure no longer fails ingestion; a permalink can
  render with a gap where one asset failed to capture. This guarantee was
  never written into CONTEXT.md or a prior ADR (it lived only in the closed
  map #95's ticket [#101](https://github.com/ffrt-labs/agregado/issues/101)),
  so nothing prior needs correcting — this ADR is where the caveat is first
  recorded.
- **A custom n8n Docker image is now infrastructure this project depends on**
  (cheerio baked into whichever image runs Code-node scripts). The exact
  image/Dockerfile lives in a separate homelab-edge repo, not this one; the
  buildable issues this map produces include verifying the live instance's
  n8n version and Task Runner configuration before authoring that Dockerfile
  change (per [research #114](https://github.com/ffrt-labs/agregado/issues/114)'s
  open TODOs).
- **The n8n workflow JSON's sync mechanism against the live instance is not
  yet specified** — carried on the map's Not yet specified list.

## Verification

Same live-verification bar ADR-0004 set: the three crafted-email fixtures
(`Archived-At` header, "view in browser" link only, neither) are replayed
through the real Resend → n8n webhook → extraction → R2/D1 → serving path
post-deploy, confirming `canonical_url` recovery, the R2 object, the D1 row,
and both the feed entry and `/p/{uuid}` permalink render correctly per
variant. Failure-path verification (a deliberately malformed fixture) confirms
the active-alert-on-first-failure behavior and that a Resend-triggered retry
does not double-write R2/D1.
