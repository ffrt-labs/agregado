# Bridge and Agregado n8n workflows

Three workflows live here, all in this repo per
`docs/architecture/ecosystem.md`'s "n8n workflow source model": the Bridge
(ADR-0007), which receives Resend's `email.received` webhook and writes the
Worker's D1/R2 stores; Article enrichment (#144), which reacts to Miniflux's
`new_entries` webhook and calls Agregado's enrichment API; and Daily digest
(#145), which on a schedule fetches Agregado's persisted Digest artifact and
sends it as email. Bridge spec:
[`docs/newsletter-ingestion-resend-n8n-plan.md`](../docs/newsletter-ingestion-resend-n8n-plan.md).

- `src/` — all three workflows' logic as plain CommonJS modules (Svix/Miniflux
  signature checks, identity, ADR-0004 canonical-URL chain, the two sanitizer
  policies, asset inlining, SQL, the enrich request builder, the digest
  date/idempotency and request builder). Unit-tested; `npm test`.
- `scripts/build-workflow.js`, `scripts/build-article-enrichment.js`,
  `scripts/build-daily-digest.js` — bundle `src/` into each Code node and emit
  `workflows/newsletter-ingest.json`, `workflows/article-enrichment.json`, and
  `workflows/daily-digest.json` respectively (shared bundling logic in
  `scripts/lib/nodes.js`). **Edit `src/`, run `npm run build`, commit the
  JSON.** A test fails if any committed JSON drifts from its generator.
- `workflows/newsletter-ingest.json` — the committed export (#125). Sync to the
  live instance is manual: `n8n import:workflow --input=...`, or paste into the
  editor. (An API/CLI-driven sync is still unspecified — see the plan.)
- `workflows/article-enrichment.json` — the committed export (#144), same
  manual sync.
- `workflows/daily-digest.json` — the committed export (#145), same manual
  sync.

## Newsletter ingest: flow

```
Webhook (raw body) → Verify Svix → [401 if bad]
  → Fetch email (Resend) → Alias query → Lookup source (D1)
  → Build entry → [200 no-op if unknown/pending]
  → Capture assets → Sign R2 request → Write R2 → Upsert D1 → 200
any node's error output → Claim alert (D1) → Notify (first time only) → Fail execution
```

Fail-loud: the last node returns non-2xx so Resend redelivers (~27h). Both
writes are keyed on `hash(Message-ID)`, so a retry overwrites, never duplicates.

## Enrichment (deviation from plan step 9)

The plan had n8n call `POST /api/private/articles/enrich` inline at ingest. That
endpoint requires a Miniflux `entry_id` (`internal/articleindex/service.go`),
which does not exist until Miniflux polls the feed. So this workflow does **not**
enrich. Enrichment runs on the separate Miniflux `new_entries` workflow below:

1. Miniflux polls `/feed/{source}.atom`, creates the entry, fires `new_entries`.
2. `article-enrichment.json` sees an entry whose URL is `{BRIDGE_ORIGIN}/p/{uuid}`
   (an email-only Article; `canonical_url` for the request is that permalink).
3. It reads `readable_content` for that permalink from D1
   (`SELECT readable_content FROM entries WHERE permalink_uuid = ?`) and sends
   it as `bridge_content`. Agregado never fetches the permalink.

Entries with a recovered canonical URL are enriched by the normal path: the
Article Index is keyed on that URL and the Reader fetches it.

## Article enrichment: flow (#144)

```
Webhook (raw body) → Verify signature → [401 if bad]
  → Respond 200 (Miniflux gets no redelivery on failure, so ack immediately)
  → Parse entries (one item per Miniflux entry)
  → Determine bridge match
     bridge permalink → Lookup bridge content (D1) → Attach bridge content ┐
     ordinary Article  ───────────────────────────────────────────────────┤
                                                                            ↓
  → Build enrich request → Call POST /api/private/articles/enrich
any node's error output (post-Parse-entries) → Entry failed
  → Alert claim query (D1) → Notify (first time only) → Fail execution
Call enrich API's error output → Classify enrich failure
  → retryable & under 3 attempts → Wait (2s/4s/8s backoff) → Call enrich API again
  → terminal, or retries exhausted → Alert claim query (same as above)
```

Fire-and-forget, unlike Resend/Bridge: `docs/architecture/ecosystem.md`'s
reliability model notes Miniflux delivers `new_entries` with no redelivery on
failure, so this workflow acknowledges the webhook immediately and does its
own retrying, rather than relying on a non-2xx response. The enrich endpoint
is idempotent per `canonical_url` (#77's AC2), so a retried or duplicate
`new_entries` event for the same entry is a safe no-op on Agregado's side.

## Daily digest: flow (#145)

```
Schedule (0 8 * * *, matching DIGEST_SCHEDULE's default) → Build digest request
  → Get digest (POST /api/private/digests/{today}) → Send digest email
  → Check empty digest → [alert branch if the Digest selected nothing]
any node's error output → Digest fetch failed / Classify send failure
  → Alert claim query (D1) → Notify (first time only) → Fail execution
```

`internal/digestartifact`'s package comment says it directly: "n8n owns
delivery — it intentionally has no scheduler or delivery code." This workflow
is that delivery: no selection, ranking, or rendering logic here, only
fetching the already-built artifact and sending its `Subject`/`HTML`/`Text`
verbatim (all of that is #79's).

The endpoint is idempotent per date (#79's AC8) — calling it again for the
same date re-returns the already-persisted artifact rather than making
another model call. `Send digest email`'s failure branch exploits that
directly: `Classify send failure` counts the attempt and, under
`MAX_SEND_RETRIES` (3, with 2s/4s/8s backoff, the same shape as
`article-enrichment.json`'s retry loop), `Wait before retry` loops back to
**`Get digest`**, not to `Send digest email` — a retry re-fetches the
persisted artifact rather than resending whatever this execution happened to
be holding, satisfying the issue's "no duplicate model call, no regenerated
content" criterion as the literal control flow rather than as an incidental
property of idempotency. A `Get digest` failure (Agregado unreachable, bad
response) goes straight to the alert branch with no retry of its own, the
same precedent `article-enrichment.json`'s D1 lookups set: only the one step
whose failure the issue specifically calls out for backoff retries.

Exhausted retries and a `Get digest` failure both reach the same
D1-claim-then-notify alert branch as the other two workflows
(`BRIDGE_ALERT_URL`), satisfying docs/architecture/ecosystem.md's reliability
model (`SEND -->|failure| RETRY`, `ERROR --> ALERT`) and this ticket's reason
for existing: the digest-send path must never be as unobservable as #44
describes for the legacy scheduler.

An empty Digest is still sent (#79's AC12), then `Check empty digest` routes
it to the same alert branch with Agregado's `EmptyReason` (no Articles
finished Enrichment in the window, or the selection model chose none of
them). Agregado does not persist an empty artifact, so re-running the
workflow for that date after fixing the cause regenerates it instead of
re-sending the empty one. The alert claim is keyed on the date, so an empty
Digest and a send failure on the same day notify once between them.

## Configuration (n8n)

Environment on the n8n container, newsletter ingest:

| Variable | Purpose |
|---|---|
| `NODE_FUNCTION_ALLOW_EXTERNAL=cheerio` | Code nodes may `require('cheerio')` (#120) |
| `NODE_FUNCTION_ALLOW_BUILTIN=crypto` | Svix HMAC, hashing |
| `RESEND_WEBHOOK_SECRET` | `whsec_...` signing secret |
| `BRIDGE_PERMALINK_SECRET` | keys the permalink UUID (see below); generate once, never rotate |
| `CF_ACCOUNT_ID`, `BRIDGE_D1_DATABASE_ID`, `BRIDGE_R2_BUCKET` | D1 REST + R2 S3 endpoints |
| `BRIDGE_R2_ACCESS_KEY_ID`, `BRIDGE_R2_SECRET_ACCESS_KEY` | R2 SigV4, hand-signed in `Sign R2 request` (see below) |
| `BRIDGE_ALERT_URL` | plain-text POST endpoint (e.g. an ntfy topic) |

Credentials to select after import: HTTP Header Auth `Resend API`,
HTTP Header Auth `Cloudflare API` (D1 edit). `Write R2` needs no credential —
it sends a request pre-signed by `Sign R2 request` (see below).

Env access in Code nodes must not be blocked (`N8N_BLOCK_ENV_ACCESS_IN_NODE=false`).
Also set the webhook path suffix (`resend-REPLACE_WITH_RANDOM_SUFFIX`) to match the
tunnel rule, and apply `email-worker/migrations/0002_ingest_alerts.sql` to D1.

Environment on the n8n container, article enrichment (#144) — shares
`NODE_FUNCTION_ALLOW_BUILTIN`, `CF_ACCOUNT_ID`/`BRIDGE_D1_DATABASE_ID`, and
`BRIDGE_ALERT_URL` with newsletter ingest above:

| Variable | Purpose |
|---|---|
| `MINIFLUX_WEBHOOK_SECRET` | verifies `X-Miniflux-Signature` on the `new_entries` webhook |
| `BRIDGE_ORIGIN` | e.g. `https://bridge.example.com`; matches an entry's URL against `{BRIDGE_ORIGIN}/p/{uuid}` to recognise a Bridge permalink |
| `AGREGADO_BASE_URL` | Agregado's tailnet base URL (ADR-0008); `/api/private/articles/enrich` is appended |
| `ENRICHMENT_SECRET` | shared secret sent as `X-Enrichment-Secret` (`docs/article-index-api.md`) |

Credentials to select after import: none beyond `Cloudflare API` (D1 edit,
shared with newsletter ingest) — the enrich and Miniflux-facing calls use
plain header parameters, not an n8n credential.

Set the webhook path suffix (`miniflux-new-entries-REPLACE_WITH_RANDOM_SUFFIX`)
to match the tunnel rule and Miniflux's configured webhook URL, and set
Miniflux's webhook secret to the same value as `MINIFLUX_WEBHOOK_SECRET`.

Environment on the n8n container, daily digest (#145) — shares
`CF_ACCOUNT_ID`/`BRIDGE_D1_DATABASE_ID`, `BRIDGE_ALERT_URL`, `AGREGADO_BASE_URL`,
and `ENRICHMENT_SECRET` with article enrichment above (the digest artifact
endpoint checks the same `X-Enrichment-Secret` as the enrich endpoint —
`cmd/agregado/main.go` wires both handlers from the same `cfg.Enrichment.Secret`):

| Variable | Purpose |
|---|---|
| `DIGEST_RECIPIENT_EMAIL` | the digest's recipient address — never committed |
| `DIGEST_FROM_EMAIL` | the digest's from address |

Credentials to select after import: an SMTP credential (e.g. `Digest SMTP`) on
`Send digest email`, plus `Cloudflare API` (D1 edit, shared with the other two
workflows) on the alert branch. The exact SMTP/email provider is an explicitly
deferred decision (`docs/architecture/ecosystem.md`, "Explicitly deferred
decisions") — this workflow uses n8n's generic `emailSend` node rather than a
provider-specific one so that choice stays open.

The schedule node's cron expression (`0 8 * * *`) matches `DIGEST_SCHEDULE`'s
default (`internal/config/config.go`) — the legacy scheduler this workflow
replaces (#85, once #84 cuts over). It is not read from that env var; n8n's
Schedule Trigger has no `$env` access of its own, so keep the two in sync by
hand if `DIGEST_SCHEDULE` ever changes.

## Deliberate deviations from the plan

- **Permalink UUID is an HMAC of `hash(Message-ID)`**, not the bare hash. The
  publisher knows the Message-ID, so a bare hash would let the sender guess the
  "unguessable" permalink. Still deterministic, so retries agree.
- **Alerts are keyed on the webhook's `svix-id`** (stable across Resend
  redeliveries), not `hash(Message-ID)`, because a failure in content fetch
  happens before the Message-ID is known.
- **`Write R2` is hand-signed (AWS SigV4) in a Code node (`src/sigv4.js`)**,
  not authenticated via n8n's predefined AWS credential type. That built-in
  signer doesn't reliably add the `x-amz-content-sha256` header for
  S3-compatible hosts outside `*.amazonaws.com`, and R2 rejects requests
  missing it; n8n's own dedicated AWS S3 node also doesn't honor a custom S3
  endpoint at all, so it always targets real AWS. Hand-signing, the same
  approach this workflow already uses for the Svix signature, sidesteps both.
- **Article enrichment acknowledges the Miniflux webhook before enrichment
  completes**, and retries/backoff happen inside the workflow (a `Wait` node
  loop) rather than via webhook redelivery — Miniflux, unlike Resend/Svix,
  does not redeliver a failed `new_entries` webhook
  (`docs/architecture/ecosystem.md`'s reliability model, ADR-0006's rejected
  "trusting Miniflux's delivery pattern" alternative).
- **Only the `Call enrich API` step retries with backoff.** D1 lookup
  failures (`Lookup bridge content`) go straight to the alert branch, same as
  newsletter-ingest's D1/R2 calls — this workflow doesn't introduce a second,
  differently-shaped retry mechanism for those.
- **No separate "fetch entry details from Miniflux" call.** #144's step 2
  reads that way, but Miniflux's `new_entries` webhook payload already embeds
  each entry's `id`, `url`, `title`, `author`, and `published_at` in full —
  everything `Build enrich request` needs — so `Parse entries` reads them
  straight off the webhook body instead of making a redundant `GET` back to
  Miniflux for the same data.
- **`source_id` is never sent.** The enrich endpoint's body contract
  (`docs/article-index-api.md`) lists it as optional, and nothing in the
  `new_entries` payload maps cleanly to Agregado's own Source id (the
  newsletter alias local-part, `src/entry.js`'s `aliasFromRecipients`) for an
  RSS Article — Miniflux's own numeric `feed.id` is a different identity
  space. Left unset rather than sending a value that isn't actually the
  Source id.
- **A send-failure retry loops back to `Get digest`, not `Send digest
  email`.** The issue's acceptance criteria call for a retry to "re-fetch,
  not regenerate"; since the GET-digest-equivalent call is idempotent per
  date, re-fetching and resending identical in-memory data are
  behaviorally the same, but looping through the fetch node makes that the
  literal control flow the criteria describe rather than an incidental
  property of idempotency someone could break by refactoring the retry loop
  later.
- **`Classify send failure` counts attempts via self-reference
  (`$('Classify send failure')`), not via the error item.** Because the
  retry loops back through `Get digest`, a successful re-fetch replaces
  `$json` with the artifact response (`Subject`/`HTML`/`Text`/`ID`), which
  carries no `attempt` field — unlike `article-enrichment.json`'s retry loop,
  which loops back directly to the failing call and so can keep `attempt` on
  the error item itself. This node's own last output in the execution is the
  only value that survives a successful fetch sandwiched between failures,
  so it is the counter of record; self-reference throws on the first failure
  (the node has not run yet), which doubles as the "start at 0" case.
- **No separate terminal-vs-retryable classification for send failures**,
  unlike `article-enrichment.json`'s 4xx check in `classifyEnrichFailure`.
  The issue groups "SMTP auth, network, transient provider error" together
  under the same retry/backoff rule rather than carving out a terminal
  class, so `Classify send failure` only counts attempts.

## Verification status

Unit tests cover everything with logic, including `src/sigv4.js` against an
independently-computed (via `openssl`, not this module) signature,
`src/miniflux.js`/`src/enrich.js`'s Miniflux parsing, bridge-permalink
matching, and enrich-request building, and `src/digest.js`'s UTC date key and
persisted-artifact URL building.

**`daily-digest.json`'s happy path has been verified against the live
stack** (n8n, Agregado, and Resend's SMTP relay) — manually triggering the
workflow against real enriched content confirmed:

- `Get digest` reaching the real `POST /api/private/digests/{date}` endpoint
  with the correct `X-Enrichment-Secret` and idempotently returning the same
  persisted artifact across repeated calls for the same date.
- `Send digest email` delivering the fetched `Subject`/`HTML`/`Text`
  unmodified via a real SMTP credential (Resend's `smtp.resend.com` relay) —
  the email actually arrived.

This same live exercise found and fixed three real bugs along the way (all
on the Agregado side, not this workflow): `digestartifact`'s handler logged
nothing on failure (#152), `AI_REQUEST_TIMEOUT`'s 30s default was too tight
for the configured reasoning model (#153), and that model wraps its JSON
answer in a ` ```json ` code fence that the decoder choked on (#156) — plus
`DIGEST_RECIPIENT_EMAIL`/`DIGEST_FROM_EMAIL` were missing from n8n's own
deploy entirely (`ffrt-labs/homelab-apps#21`), easily confused with the
same-named, unrelated env var on Agregado's own (now-removed) legacy mailer.

**Still unverified**: the Schedule Trigger actually firing on its own cron
(every test so far has been a manual execution, not a real 8am trigger), and
the retry-with-backoff / alert branches (`Classify send failure` → `Wait
before retry` → re-fetch, and the D1-claim/Notify path) — no genuine SMTP or
fetch failure occurred during live testing to exercise them end to end.
Node parameters for the Schedule Trigger and the generic `emailSend` node
were otherwise written from n8n's documented shapes, now confirmed correct
for the paths actually exercised.

**`article-enrichment.json` has been verified against the live stack**
(n8n 2.39.6, Agregado, and the Bridge's D1) — a correctly HMAC-signed
synthetic `new_entries` webhook was fired at the deployed workflow and all of
the following were confirmed live, not just in unit tests:

- The happy path: fetch → preferences read → summarize → categorize → score
  → `article_index` row with `status: complete` and a populated
  `score`/`tags`/`summary`.
- Idempotency: resending the same `entry_id`/`canonical_url` returns
  `created: false` against the existing row rather than erroring or
  duplicating (the `canonical_url UNIQUE` constraint plus `Process()`'s
  early return in `internal/articleindex/service.go`).
- The terminal path: a bad `X-Miniflux-Signature` gets a `401` from
  `Verify signature` and never reaches `Call enrich API` or the alert branch.
- The retryable path: with Agregado briefly unreachable, `Call enrich API`
  retries 3 times (2s/4s/8s backoff) with `classification: "retryable"`, then
  `Classify enrich failure`/`Entry failed`'s approach of recovering
  `entry`/`body`/`attempt` from `$('Build enrich request')`/
  `$('Determine bridge match')` instead of trusting the error item works as
  designed, `Claim alert` and `Notify` both fire, and the execution ends
  failed for visibility. `n8n-nodes-base.if`'s `combineOperation` on
  `Retry enrich?` does AND its `boolean` and `number` conditions as intended.
- The bridge-permalink branch: resolving a real `permalink_uuid` from the
  Bridge's D1, attaching its `readable_content` as `bridge_content`, and
  completing enrichment without a live fetch. This live run caught a real
  bug — `Attach bridge content` read `entry` off `Lookup bridge content`'s
  HTTP response, which doesn't carry it (an HTTP node's output replaces
  `$json` rather than merging with it); fixed to recover `entry`/
  `permalinkUuid` from `$('Determine bridge match')` instead, the same
  pattern `Entry failed` already used correctly.

Two infra gaps surfaced during this same live exercise, both now fixed: the
`agregado` service's own compose/deploy never actually passed it
`ENRICHMENT_SECRET`/`PREFERENCES_PATH` (only n8n's side had been wired), and
nothing bind-mounted `PREFERENCES_PATH`'s directory into the container.

**`newsletter-ingest.json` remains unverified against a live n8n, Resend, D1
or R2** — node parameters (raw-body binary property, `$env` access,
error-output wiring, retry-loop item shape across `Wait`) were written from
each system's documented shapes and still need exercising live, per
#126/#138's precedent. The hand-signing approach in `Sign R2 request`
specifically replaced an earlier predefined-AWS-credential approach that
*was* exercised live and failed against a real R2 bucket (missing/wrong
`x-amz-content-sha256`); the replacement itself is still pending that same
live exercise.

## Known gaps (from review)

- The permalink CSP is a `<meta>` tag; a response header from the Worker would be stronger.
  CSS `url()` and non-beacon tracking images can still leak viewer IPs.
- `aliasFromRecipients` uses the first recipient only and keeps `+tag`.
- If `Verify Svix` (or article enrichment's `Verify signature`) itself
  throws, the alert branch cannot read its context; the execution still
  fails loudly, but no alert fires for that specific failure.
- Article enrichment's retry loop caps at 3 attempts (2s/4s/8s backoff) and
  then alerts; there is no separate reconciliation pass — deliberately out of
  scope per #144 (a follow-up ticket once this fast path is live and proven).
- Daily digest's schedule (`0 8 * * *`) is not read from `DIGEST_SCHEDULE` —
  n8n's Schedule Trigger node has no `$env` access, so the two must be kept in
  sync by hand if the env var ever changes. Same for a reconciliation pass:
  none exists (N/A per the issue — the daily schedule is the only trigger).
