# Bridge n8n workflow

The n8n half of the Bridge (ADR-0007): receives Resend's `email.received`
webhook, extracts, and writes the Worker's D1/R2 stores. Spec:
[`docs/newsletter-ingestion-resend-n8n-plan.md`](../docs/newsletter-ingestion-resend-n8n-plan.md).

- `src/` — the workflow's logic as plain CommonJS modules (Svix check,
  identity, ADR-0004 canonical-URL chain, the two sanitizer policies,
  asset inlining, SQL). Unit-tested; `npm test`.
- `scripts/build-workflow.js` — bundles `src/` into each Code node and emits
  `workflows/newsletter-ingest.json`. **Edit `src/`, run `npm run build`, commit
  the JSON.** A test fails if the committed JSON drifts from the generator.
- `workflows/newsletter-ingest.json` — the committed export (#125). Sync to the
  live instance is manual: `n8n import:workflow --input=...`, or paste into the
  editor. (An API/CLI-driven sync is still unspecified — see the plan.)

## Flow

```
Webhook (raw body) → Verify Svix → [401 if bad]
  → Fetch email (Resend) → Alias query → Lookup source (D1)
  → Build entry → [200 no-op if unknown/pending]
  → Capture assets → Write R2 → Upsert D1 → 200
any node's error output → Claim alert (D1) → Notify (first time only) → Fail execution
```

Fail-loud: the last node returns non-2xx so Resend redelivers (~27h). Both
writes are keyed on `hash(Message-ID)`, so a retry overwrites, never duplicates.

## Enrichment (deviation from plan step 9)

The plan had n8n call `POST /api/private/articles/enrich` inline at ingest. That
endpoint requires a Miniflux `entry_id` (`internal/articleindex/service.go`),
which does not exist until Miniflux polls the feed. So this workflow does **not**
enrich. Enrichment runs on the existing Miniflux `new_entries` path (#77):

1. Miniflux polls `/feed/{source}.atom`, creates the entry, fires `new_entries`.
2. #77's workflow sees an entry whose URL is `{bridge origin}/p/{uuid}`
   (an email-only Article; `canonical_url` for the request is that permalink).
3. It reads `readable_content` for that permalink from D1
   (`SELECT readable_content FROM entries WHERE permalink_uuid = ?`) and sends
   it as `bridge_content`. Agregado never fetches the permalink.

Entries with a recovered canonical URL are enriched by the normal path: the
Article Index is keyed on that URL and the Reader fetches it.

## Configuration (n8n)

Environment on the n8n container:

| Variable | Purpose |
|---|---|
| `NODE_FUNCTION_ALLOW_EXTERNAL=cheerio` | Code nodes may `require('cheerio')` (#120) |
| `NODE_FUNCTION_ALLOW_BUILTIN=crypto` | Svix HMAC, hashing |
| `RESEND_WEBHOOK_SECRET` | `whsec_...` signing secret |
| `BRIDGE_PERMALINK_SECRET` | keys the permalink UUID (see below); generate once, never rotate |
| `CF_ACCOUNT_ID`, `BRIDGE_D1_DATABASE_ID`, `BRIDGE_R2_BUCKET` | D1 REST + R2 S3 endpoints |
| `BRIDGE_ALERT_URL` | plain-text POST endpoint (e.g. an ntfy topic) |

Credentials to select after import: HTTP Header Auth `Resend API`,
HTTP Header Auth `Cloudflare API` (D1 edit), AWS credential for R2.
Env access in Code nodes must not be blocked (`N8N_BLOCK_ENV_ACCESS_IN_NODE=false`).
Also set the webhook path suffix (`resend-REPLACE_WITH_RANDOM_SUFFIX`) to match the
tunnel rule, and apply `email-worker/migrations/0002_ingest_alerts.sql` to D1.

## Deliberate deviations from the plan

- **Permalink UUID is an HMAC of `hash(Message-ID)`**, not the bare hash. The
  publisher knows the Message-ID, so a bare hash would let the sender guess the
  "unguessable" permalink. Still deterministic, so retries agree.
- **Alerts are keyed on the webhook's `svix-id`** (stable across Resend
  redeliveries), not `hash(Message-ID)`, because a failure in content fetch
  happens before the Message-ID is known.

## Verification status

Unit tests cover everything with logic. **Not verified** against live n8n,
Resend, D1 or R2 — node parameters (raw-body binary property, `$env` access,
R2 S3 signing, error-output wiring) were written from n8n's documented shapes
and must be exercised by #126 before this is trusted.

## Known gaps (from review)

- **#77's `new_entries` workflow is not in this repo**, so the D1 read that supplies
  `bridge_content` (criterion 7) is unbuilt here; the API already accepts it.
- The permalink CSP is a `<meta>` tag; a response header from the Worker would be stronger.
  CSS `url()` and non-beacon tracking images can still leak viewer IPs.
- `aliasFromRecipients` uses the first recipient only and keeps `+tag`.
- If `Verify Svix` itself throws, the alert branch cannot read its `alertKey`; the
  execution still fails and Resend retries, but no alert fires.
