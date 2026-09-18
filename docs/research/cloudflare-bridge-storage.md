# Research: Cloudflare-native storage and feed serving for the bridge

**Issue:** [#97](https://github.com/ffrt-labs/agregado/issues/97) · child of [#95](https://github.com/ffrt-labs/agregado/issues/95)
**Date:** 2026-09-18
**Status:** Fact-finding. No decision taken — see "Open questions" at the end.

Every number below is quoted from Cloudflare's own docs, fetched on the date
above, with the URL that owns the claim. Where the docs do **not** state
something, this file says so explicitly rather than filling the gap from
memory. Per `email-worker/AGENTS.md`, nothing here is answered from model
knowledge.

**Docs reorganisation, noted because it affects citations:** the inbound Email
Workers reference now lives under `/email-service/` (`/email-routing/` pages
still resolve and still carry the limits table). Both are cited where relevant.

---

## 0. The workload, sized

The bridge's volume, as stated in #95 and #97:

| Quantity | Assumption |
|---|---|
| Inbound newsletters | ~3/day → ~1,100/year |
| Original HTML per message | 50–200 KB (take 125 KB mean) |
| Readable content per message | ~1/3 of the original (take 40 KB) |
| Retention | Originals and readable content **forever**; feed window 30 days |
| Feed reads | Miniflux polling each Source, ~1/hour |
| Sources | order of 10 |

Derived: **~137 MB/year** of original HTML, **~44 MB/year** of readable
content, ~180 MB/year total, growing linearly and never pruned. A 30-day feed
window holds ~90 entries; because #95 settled that *the feed entry carries the
readable content*, one feed document is roughly **90 × 40 KB ≈ 3.6 MB**.

That last number is the single most consequential fact in this file. It is what
decides the store, because it means generating a feed is not a metadata query —
it is a query that must return ~3.6 MB of bodies.

---

## 1. The four stores, against the two jobs

### 1.1 The subrequest rule that decides it

> "A subrequest is any request a Worker makes using the Fetch API or to
> Cloudflare services like R2, KV, or D1."
> — <https://developers.cloudflare.com/workers/platform/limits/>

Limits: **50 subrequests per invocation (Free)**, **10,000 (Paid)**
(same page). Calls to KV, R2, D1 and Cache all count. Service bindings
(Worker → Worker) also count.

So "without a second hop" is a literal, billable, capped property, and the
90-entry feed is the case that tests it:

| Store | Feed built from | Subrequests to build one feed |
|---|---|---|
| **D1** | one `SELECT … WHERE source_id = ? AND published_at > ? ORDER BY published_at DESC` returning 90 rows **including the content column** | **1** |
| **R2** | one `list()` + one `get()` per entry | **~91** — over the Free plan's 50 |
| **KV** | one `list()` + one `get()` per entry (see §1.4 on why metadata alone is not enough) | **~91** — over the Free plan's 50 |
| **Durable Object (SQLite)** | one RPC to the per-Source object, which queries its own SQLite locally | **1** (but that 1 is an extra network hop, and DO-internal SQLite reads are not Worker subrequests) |

D1's own per-invocation cap is on *queries*, not rows: "Queries per Worker
invocation: 1,000 (Workers Paid) / 50 (Free)"
(<https://developers.cloudflare.com/d1/platform/limits/>). One query is one
query no matter how many rows it returns.

### 1.2 R2 — right for blobs, wrong for the feed

- "Object size | 5 TiB per object"; "Number of objects per bucket | Unlimited";
  "Data storage per bucket | Unlimited"; "Object key length | 1,024 bytes";
  "Object metadata size | 8,192 bytes"
  (<https://developers.cloudflare.com/r2/platform/limits/>)
- "Maximum concurrent writes to the same object name (key) | 1 per second"
  (same page) — irrelevant at 3 writes/day to distinct keys.
- Class A (writes/lists) vs Class B (reads):
  > Class A: "ListBuckets, PutBucket, ListObjects, PutObject, CopyObject,
  > CompleteMultipartUpload, CreateMultipartUpload, …"
  > Class B: "HeadBucket, HeadObject, GetObject, UsageSummary, …"
  > — <https://developers.cloudflare.com/r2/pricing/>

  Note `ListObjects` is **Class A**, the expensive class. A feed built by
  listing R2 pays the write-class rate on every poll.
- R2 has no query surface at all: keys are listed by prefix, there is no
  filter, no ordering other than lexicographic key order, no join. A rolling
  window is only expressible by encoding the date into the key.

**Verdict:** R2 is the correct home for the **original HTML** — the provenance
artifact from ADR-0004's lesson. It is immutable, never on the hot path, and
potentially large (see §3: inbound messages may be up to 25 MiB, which exceeds
D1's 2 MB row cap). It is the wrong primary store for the feed.

### 1.3 D1 — right for queryable entry metadata, and big enough for the bodies

- "Maximum database size: 10 GB (Workers Paid) / 500 MB (Free)" — and the docs
  state the 10 GB cap **cannot be increased**.
- "Maximum string, BLOB or table row size: 2,000,000 bytes (2 MB)"
- "Maximum SQL statement length: 100,000 bytes"; "Maximum SQL query duration: 30
  seconds"; "Maximum number of columns per table: 100"; "Maximum bound
  parameters per query: 100"
- "Maximum storage per account: 1 TB (Workers Paid) / 5 GB (Free)"
- "Databases per account: 50,000 (Workers Paid) / 10 (Free)"
  — all <https://developers.cloudflare.com/d1/platform/limits/>

A 40 KB readable body is 2% of the 2 MB row cap. A 200 KB one is 10%. Bodies
fit comfortably; **originals do not reliably fit** and should not be attempted.

Row metering — this is what makes the feed query cheap or expensive:

> "how many rows a query reads (scans), regardless of the size of each row."
> "A query that filters on an unindexed column may return fewer rows to your
> Worker, but is still required to read (scan) more rows to determine which
> subset to return."
> "Defining indexes on your table(s) reduces the number of rows read by a query
> when filtering on that indexed field."
> — <https://developers.cloudflare.com/d1/platform/pricing/>

Writes pay an index tax: "at least one (1) additional row written to account for
updating the index" (same page).

So the feed needs a composite index on `(source_id, published_at DESC)`; with
it, a 30-day window query scans ~90 rows, not the whole table. Without it, rows
read grows without bound as the archive grows — **the one way this design can
silently become expensive.**

**Verdict:** D1 is the only one of the four that can hold queryable per-Source
entry metadata *and* return a rolling-window feed, bodies included, in a single
hop, on either plan.

### 1.4 KV — a cache, not a source of truth

- "Value size: 25 MiB"; "Key size: 512 bytes"; **"metadata: 1024 bytes"**;
  "Operations per invocation: 1,000"; keys per namespace unlimited
  (<https://developers.cloudflare.com/kv/platform/limits/>)
- Free plan: "100,000 reads per day", "1,000 writes per day" (different keys),
  "1 per second" writes to the *same* key — the same-key rate applies on Paid
  too (same page). Free storage: 1 GB per account/namespace; unlimited on Paid.
- Consistency: "KV achieves high performance by being eventually-consistent."
  Writes are "usually immediately visible" locally but "may take up to 60
  seconds or more to be visible in other global network locations as their
  cached versions of the data time out." Default `cacheTtl` is 60 s.
  (<https://developers.cloudflare.com/kv/concepts/how-kv-works/>)

The 1024-byte metadata field is the reason KV cannot serve the feed in one hop.
`list()` returns keys plus that metadata, which is enough for a title, a date
and a canonical URL — but #95 settled that the entry carries the readable
content, and 40 KB does not fit in 1024 bytes. So a KV-backed feed is
`list()` + N × `get()`, i.e. the 91-subrequest shape.

KV *is* well suited to holding **one pre-rendered feed document per Source** (a
3.6 MB blob, well under the 25 MiB value cap) — see §5.

### 1.5 Durable Objects — capable, but buying consistency this workload does not need

- Free plan supports DOs, **SQLite backend only**; the key-value backend
  requires Workers Paid
  (<https://developers.cloudflare.com/durable-objects/platform/pricing/>)
- SQLite-backed: "Storage per Durable Object | 10 GB"; key+value combined
  ≤ 2 MB. Key-value-backed: storage unlimited, but "Value size | 128 KiB
  (131072 bytes)" — which a 200 KB readable body would exceed.
  "Number of Objects | Unlimited". CPU per request defaults to 30 s, raisable
  to 5 min. (<https://developers.cloudflare.com/durable-objects/platform/limits/>)
- Billed on *duration* as well as requests: Free "100,000 requests/day" and
  "13,000 GB-s/day"; Paid "1 million/month, + $0.15/million" and
  "400,000 GB-s/month, + $12.50/million GB-s". "Durable Objects that are idle
  and eligible for hibernation are not billed for duration."
  (<https://developers.cloudflare.com/durable-objects/platform/pricing/>)

A DO-per-Source would give strong consistency, a natural place to serialise
writes, and its own SQLite to answer the feed. But this is a single-user
pipeline with ~3 writes/day and no concurrency to serialise; the strong
consistency is bought and unused. It also adds a mandatory hop: the entry
Worker must RPC into the object (which counts as a subrequest), and every
request for a given Source is pinned to one physical location, losing the edge
locality D1 read replicas or KV would give.

**Verdict:** correct tool, wrong problem. D1 is the same capability without the
hop or the duration meter.

### 1.6 Permalink, specifically

All four can serve a permanent UUID permalink in one hop:

- D1: `SELECT … WHERE id = ?` on the primary key — 1 query, 1 subrequest.
- R2: `get("entries/<uuid>.html")` — 1 Class B op, 1 subrequest.
- KV: `get("<uuid>")` — 1 read, 1 subrequest.
- DO: 1 RPC.

The permalink does not discriminate between the stores. **The feed does.**

---

## 2. Cost, at this volume

### 2.1 The prices

| Product | Free tier | Paid |
|---|---|---|
| **Workers** | 100,000 requests/day; 10 ms CPU/invocation | $5/mo min; 10M requests included, then $0.30/million; 30M CPU-ms included, then $0.02/million CPU-ms |
| **R2** (Standard) | 10 GB-month storage; 1M Class A/mo; 10M Class B/mo; egress free | $0.015/GB-mo; $4.50/M Class A; $0.36/M Class B; egress free |
| **D1** | 5M rows read/day; 100k rows written/day; 5 GB total storage; 500 MB max DB | 25B rows read/mo then $0.001/M; 50M rows written/mo then $1.00/M; 5 GB storage included then $0.75/GB-mo; 10 GB max DB |
| **KV** | 100k reads, 1,000 writes, 1,000 deletes, 1,000 lists per day; 1 GB storage | 10M reads/mo then $0.50/M; 1M writes/mo then $5.00/M; 1M deletes/mo then $5.00/M; 1M lists/mo then $5.00/M; 1 GB storage then $0.50/GB-mo |
| **Durable Objects** | 100k requests/day; 13,000 GB-s/day; SQLite only; 5M row reads/day; 100k row writes/day; 5 GB stored | 1M requests/mo then $0.15/M; 400k GB-s/mo then $12.50/M GB-s; 25B row reads/mo then $0.001/M; 50M row writes/mo then $1.00/M; 5 GB-mo then $0.20/GB-mo |

Sources: <https://developers.cloudflare.com/workers/platform/pricing/> ·
<https://developers.cloudflare.com/r2/pricing/> ·
<https://developers.cloudflare.com/d1/platform/pricing/> ·
<https://developers.cloudflare.com/kv/platform/pricing/> ·
<https://developers.cloudflare.com/durable-objects/platform/pricing/>

R2 Infrequent Access ($0.01/GB-mo storage, $0.01/GB retrieval, $9.00/M Class A,
$0.90/M Class B) has a **"30 day minimum storage duration"** and the free tier
"does not apply to Infrequent Access storage"
(<https://developers.cloudflare.com/r2/pricing/>,
<https://developers.cloudflare.com/r2/buckets/storage-classes/>). At 137 MB/year
the storage saving is fractions of a cent; IA is not worth its retrieval fee
here.

### 2.2 Where unbounded growth first costs money

Run the numbers against the §0 volume:

- **R2 storage.** 137 MB/year of originals against a 10 GB-month free tier:
  roughly **70 years** before the first cent. Even then, the 11th GB is
  $0.015/month.
- **R2 operations.** ~1,100 `PutObject` (Class A) per year against 1M/month
  free — three orders of magnitude of headroom. Permalink `GetObject` (Class B)
  against 10M/month free — likewise.
- **D1 rows written.** ~3 inserts/day plus index rows ≈ 10/day against 100,000/day
  free.
- **D1 rows read.** *This is the one that can run away.* With the composite
  index: 10 Sources × 24 polls/day × ~90 rows = **~21,600 rows/day** against
  5M/day free. **Without** the index, every poll scans the whole table, so
  rows/day grows as (archive size × polls) — at year 5 that is ~5,500 rows per
  poll, ~1.3M rows/day, and it keeps climbing. Still inside the free tier for
  years, but it is the only meter here whose growth is superlinear in the
  archive, and it is the one to watch.
- **D1 storage.** 44 MB/year of readable content hits the **Free plan's 500 MB
  hard database cap in ~11 years** and the Paid plan's uncuttable 10 GB cap in
  ~225 years. Note these are *caps*, not prices — the failure is a write error,
  not a bill.
- **KV** (if used as a materialised cache): 3–10 writes/day against 1,000/day
  free; one 3.6 MB value per Source against 1 GB free storage.
- **Workers requests.** 10 Sources × 24 polls/day = 240/day, plus a handful of
  email invocations, against 100,000/day free.

**Answer:** at this volume, *nothing costs money for decades.* The first thing
that actually bites is not a price but a **limit** — D1's 500 MB Free-plan
database cap (≈11 years out), and before that, the **Free plan's 10 ms CPU and
50-subrequest ceilings** (§3), which bite on day one for a 3.6 MB feed. The
bridge's real cost driver is the **$5/month Workers Paid plan**, and it is worth
it for CPU headroom alone.

*Not established:* whether an inbound Email Worker invocation counts against the
Workers 100,000/day free request limit. The pricing page enumerates billable
requests ("Inbound requests to your Worker", WebSocket upgrades, service-binding
invocations) and never mentions email
(<https://developers.cloudflare.com/workers/platform/pricing/>). At a few
messages a day it is immaterial either way, but it is genuinely undocumented.

*Not established:* whether R2 requires a payment method on file even to use only
the free tier. The billing docs say a failed payment method suspends
usage-based services including R2, but no page states the enrolment
precondition plainly.

---

## 3. Email Worker constraints on doing the work inline

### 3.1 CPU, memory, subrequests

From <https://developers.cloudflare.com/workers/platform/limits/>:

| | Free | Paid |
|---|---|---|
| CPU time per invocation | **10 ms** | **5 min max, 30 s default** |
| Memory per isolate | **128 MB** (both plans) — "This limit is per-isolate, not per-invocation." | same |
| Subrequests per invocation | **50** | **10,000** |
| Simultaneous open connections | six, both plans | same |

> "Waiting on network requests (such as `fetch()` calls, KV reads, or database
> queries) does not count toward CPU time." — same page

That last sentence matters: R2 puts and D1 inserts cost *wall clock*, not CPU.
What costs CPU in the email path is **parsing MIME, running ADR-0004's
extraction chain, and readability extraction over 200 KB of HTML**. On the Free
plan's 10 ms that is implausible; the docs say so, for this exact case:

> "Workers that handle incoming emails count toward the standard Workers CPU and
> memory limits. On the Workers Free plan, complex handlers may exceed these
> limits and fail to process a message." … "Failed invocations appear in Workers
> logs with the `EXCEEDED_CPU` error."
> — <https://developers.cloudflare.com/email-routing/limits/>

**Conclusion: the bridge needs the Workers Paid plan.** Not for volume — for the
10 ms CPU ceiling on a parse-and-extract handler.

`ctx.waitUntil()` extends work for "up to 30 seconds" after the response
(<https://developers.cloudflare.com/workers/platform/limits/>) — useful for
deferring the R2 put of the original, though at 3/day there is no reason to.

### 3.2 Message size

- "Inbound message size: 25 MiB" — messages exceeding this are rejected.
- "Routing rules per domain: 200"; "Destination addresses per account: 200";
  "Reply References entries: 100".
  — <https://developers.cloudflare.com/email-routing/limits/>

25 MiB comfortably covers 50–200 KB newsletters, but it is the reason originals
belong in R2 (5 TiB object cap) rather than D1 (2 MB row cap) or KV-backed DOs
(128 KiB value cap).

Note the 200-rules-per-domain figure against #95's "one inbound alias per
Source": #95 already settled on **catch-all routing**, so the Source count is
not bounded by the rules limit. Good — otherwise 200 would be the ceiling on
newsletters.

### 3.3 The handler surface

From <https://developers.cloudflare.com/email-service/api/route-emails/email-handler/>:

- `email(message, env, ctx)`, with `message` a `ForwardableEmailMessage`.
- Read-only properties: `from` (envelope MAIL FROM), `to` (envelope RCPT TO),
  `headers` (a `Headers`-like object, `.get()`), `raw` (a `ReadableStream` of
  the raw MIME), `rawSize` (bytes), `canBeForwarded` (boolean — the docs define
  it as "Whether the message can be forwarded" and **do not** say when it is
  false).
- Methods: `setReject(reason)` — "Reject with permanent SMTP error";
  `forward(rcptTo, headers?)`; `reply(message)`.
- `reply()` requires "a valid DMARC result", may be called once per event, must
  address the incoming sender, and the outgoing sender domain must match the
  receiving domain.

`raw` being a stream, plus `rawSize`, means the original HTML can be streamed
straight into an R2 `put()` without ever materialising 25 MiB in the 128 MB
isolate — worth doing even though newsletters are small.

### 3.4 What `forward` / throwing does to delivery — **partially unestablished**

What *is* documented:

- `setReject(reason)` produces "a permanent SMTP error" — a 5xx at the SMTP
  conversation, so the sending server generates a bounce and never retries.
- `forward(rcptTo)` delivers to a verified destination address; the docs show
  multiple forwards via `Promise.all()`.
- The Email Routing log dispositions are **Forwarded** ("Email successfully
  forwarded to destination address"), **Handled** ("Email processed by a Worker
  handler"), **Dropped**, **Rejected** ("Email rejected due to SPF, DKIM, or
  DMARC failures"), **Delivery failed**, and **Error** ("Email could not be
  processed due to an internal error")
  (<https://developers.cloudflare.com/email-service/observability/logs/>).
  The existence of a distinct **Handled** status confirms that a handler which
  returns without forwarding is a normal, non-error terminal outcome: the
  message is consumed by the Worker and not delivered anywhere. That is exactly
  what the bridge wants.
- Soft bounces (4xx) "are automatically retried with exponential backoff"; hard
  bounces (5xx) "are never retried"
  (<https://developers.cloudflare.com/email-service/concepts/deliverability/>).
  This is documented for Cloudflare's *sending* pipeline; it is not stated to
  govern inbound Worker failures.
- The docs' own error-handling example wraps the handler body in try/catch,
  forwards to an admin address as a fallback, and calls `setReject()` as a last
  resort — i.e. the recommended pattern assumes the platform's own behaviour on
  an unhandled throw is not something you want to rely on.

**What is NOT documented, after checking the email handler reference, the
Email Workers overview, the email lifecycle page, the troubleshooting page, the
logs page and `/workers/observability/errors/`:** Cloudflare nowhere states
whether an *unhandled exception* (or an `EXCEEDED_CPU` failure) in an inbound
`email()` handler results in a 4xx (sender retries), a 5xx (sender bounces), or
a silent drop. The limits page says only that the handler will "fail to process
a message". **Do not design around an assumption here.** Two consequences for
the spec:

1. Wrap the whole handler in try/catch and decide the failure policy
   explicitly — the safest for a *never-lose-a-newsletter* pipeline is to
   persist the raw message to R2 **first**, before any parsing, so that even a
   total downstream failure leaves the provenance artifact recoverable.
2. If the behaviour matters to the spec, it needs an **empirical test** against
   a deployed Worker (throw on a test alias, observe the sender's bounce),
   because the docs will not answer it.

---

## 4. One Worker or two?

**One Worker can hold both.** Handlers are methods on the default export
(<https://developers.cloudflare.com/workers/runtime-apis/handlers/>), and the
Email Service docs show the combined shape directly:

```ts
export default {
  async fetch(request, env, ctx): Promise<Response> { /* … */ },
  async email(message, env, ctx): Promise<void> { /* … */ },
} satisfies ExportedHandler<Env>;
```

(<https://developers.cloudflare.com/email-service/api/route-emails/email-handler/>)

The two triggers are wired independently: the `email()` handler is reached by
selecting **"Action: Send to a Worker"** in an Email Routing rule
(<https://developers.cloudflare.com/email-service/get-started/route-emails/>),
while `fetch()` needs a route or custom domain. Neither requires the other.

**Arguments for keeping one Worker** (the current `email-worker/` already has
the `fetch` surface wired via `assets`):

- Shared bindings, shared types, one deploy.
- ADR-0004's extraction chain and the readable-content renderer are shared code
  between write path and read path (the permalink renders what was stored).
- A service binding between two Workers **counts as a subrequest** and adds
  latency (<https://developers.cloudflare.com/workers/platform/limits/>).

**Arguments for splitting:**

- Blast radius: a deploy that breaks the feed also redeploys the handler
  currently receiving live mail, and inbound mail has no undo.
- The two paths want different CPU profiles and possibly different
  Smart Placement settings (the email path is write-heavy and location-
  insensitive; the feed path is read-heavy).

At this scale the shared-code argument wins. **Recommend one Worker**, with the
email path defensive per §3.4 so a feed-route regression cannot swallow mail.
Revisit only if the bridge spins out as a standalone tool (#95, "the spin-out's
own identity").

One repo-specific note: `email-worker/wrangler.jsonc` currently declares
`assets.directory: ./public`. Static assets are served *before* the `fetch()`
handler runs and, per the pricing page, are "free and unlimited"; the feed and
permalink routes must not collide with asset paths.

---

## 5. Generating the rolling window cheaply

Two shapes, and at this scale the cheap one is also the simple one.

### 5.1 Compute per request (recommended)

One indexed D1 query per poll:

```sql
SELECT id, title, canonical_url, published_at, content
  FROM entries
 WHERE source_id = ?1 AND published_at >= ?2   -- now - 30 days
 ORDER BY published_at DESC;
```

- **1 query, 1 subrequest**, ~90 rows scanned with the composite index on
  `(source_id, published_at)`.
- CPU cost is the XML serialisation of ~3.6 MB — string building and escaping.
  This is real CPU and is why the Free plan's 10 ms is not viable; on Paid's
  30 s default it is comfortable.
- The window is **always exactly right**, because it is computed from `now()` at
  read time. Nothing expires without a write event, which is precisely the
  failure mode a materialised feed has (§5.2).

Two cheap multipliers, both first-party:

- **Conditional responses.** Miniflux sends `If-None-Match` / `If-Modified-Since`.
  Derive a strong `ETag` from `(source_id, MAX(published_at), COUNT(*))` — a
  second, tiny indexed query — and return **304** when it matches. At ~3
  newsletters/day across ~10 Sources, the overwhelming majority of the 240
  daily polls are 304s that never serialise a byte. This is the single biggest
  win available and it costs one extra query.
- **Cache API.** `caches.default` lets a rendered feed be held at the edge;
  Cache API calls count as subrequests
  (<https://developers.cloudflare.com/workers/platform/limits/>), so this is a
  1-subrequest read on a hit. A TTL of minutes is safe given a 30-day window.

### 5.2 Materialise on write (not recommended here)

Re-render the Atom document into KV on each ingest (3 writes/day, against the
Free plan's 1,000/day) and serve it with a single KV `get()`.

Why it loses:

- **A rolling window expires without a write.** An entry leaves the 30-day
  window because time passed, not because anything happened. A materialised
  feed therefore goes stale at the tail and needs a **cron trigger** to
  re-render — and Free-plan cron triggers get the same 10 ms CPU
  (<https://developers.cloudflare.com/workers/platform/limits/>), so the cron
  re-render has the same CPU problem the request-time render has, with an extra
  moving part.
- **KV is eventually consistent**, "up to 60 seconds or more" for global
  visibility, with a 60 s default `cacheTtl`
  (<https://developers.cloudflare.com/kv/concepts/how-kv-works/>). Acceptable
  for a feed, but it is a second staleness source stacked on the first.
- It buys nothing the ETag path does not already buy, because the expensive
  case (a genuinely changed feed) still has to be rendered once either way.

**Materialise only if** measured CPU on the render turns out to be the binding
constraint — in which case the right materialisation is the Cache API keyed on
the ETag, not KV, because it invalidates by TTL rather than requiring a cron.

---

## 6. The shape this points at

Stated as a finding, not a decision — #97 is fact-finding.

| Concern | Store | Why |
|---|---|---|
| Original HTML (provenance, ADR-0004) | **R2**, key `sources/<source>/<uuid>.eml` or `.html` | 5 TiB object cap absorbs the 25 MiB inbound ceiling; never on the hot path; free-tier storage for decades; `message.raw` streams straight in |
| Entry metadata + readable content | **D1**, one `entries` table, index on `(source_id, published_at)` | the only store that answers a rolling-window query **with bodies** in one subrequest; 2 MB row cap is 10× the largest readable body |
| Rolling 30-day Atom feed | **computed per request from D1**, ETag + 304, optional Cache API | window is correct by construction; 1–2 queries per poll; most polls become 304 |
| Permanent UUID permalink | **D1** primary-key lookup (fall back to R2 for the original, if a "view original" surface is ever wanted) | 1 query, 1 subrequest |
| Plan | **Workers Paid ($5/mo)** | not volume — the Free plan's 10 ms CPU cannot run MIME parse + ADR-0004 extraction + readability; documented as `EXCEEDED_CPU` |

Durable Objects and KV both have a coherent story here and neither earns its
place: DO buys strong consistency for a workload with no concurrency, and KV
cannot carry the bodies in `list()` metadata (1024 bytes) so it degrades to the
91-subrequest shape.

---

## Open questions

1. **Unhandled-throw semantics on the inbound path.** Undocumented (§3.4).
   Needs an empirical test on a deployed Worker if the spec depends on it.
2. **Whether Email Worker invocations count against the Workers request meter.**
   Undocumented (§2.2). Immaterial at this volume; noted for completeness.
3. **Feed authentication.** #95 calls the feed "private" but the mechanism is
   out of scope of this ticket. It affects §5's caching story (a
   credential-bearing request is not trivially edge-cacheable).
4. **`canBeForwarded`.** The docs define the property and never say when it is
   false. Irrelevant if the bridge never forwards, which is the #95 shape.
5. **Whether R2 free-tier use requires a payment method on file.** Not stated
   plainly in the docs (§2.2).
6. **Feed document size.** ~3.6 MB per feed (§0) is large for an Atom document;
   whether Miniflux imposes its own response-size ceiling on a fetched feed was
   not investigated here and is not a Cloudflare question. Worth checking before
   the window width is fixed at 30 days.
