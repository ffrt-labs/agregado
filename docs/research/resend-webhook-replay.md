# Research — Does Resend support replaying a specific inbound-webhook delivery?

**Ticket:** [#137](https://github.com/ffrt-labs/agregado/issues/137) (child of map [#135](https://github.com/ffrt-labs/agregado/issues/135))
**Feeds into:** live-verification ticket [#138](https://github.com/ffrt-labs/agregado/issues/138)
**Date:** 2026-09-29

## Question

Does Resend's dashboard or API support replaying/re-sending a specific past
inbound-email webhook delivery event, and if so, does that replay resend the
exact original signed payload — so n8n would see the identical `Message-ID`
and therefore compute the same `hash(Message-ID)` (ADR-0007's entry-identity
key, `n8n/src/identity.js`'s `entryId()`)?

## Answer, short version

**Yes.** Resend's dashboard has a per-message **Replay** action on the
Webhooks page, and it resends the same underlying event, not a synthetic one.
Because Resend's inbound-webhook body carries the original email content
(and Resend's webhook delivery is Svix-backed per ADR-0007), replaying an
`email.received` event resends the same JSON payload — same `Message-ID`
inside it, same `hash(Message-ID)` on the n8n side. The webhook-transport
message identifier (Svix's `svix-id` / `webhook-id` header) is also
preserved across replays of the same message, which is the mechanism Svix
documents for making retries and replays deduplicable — a second, independent
signal that the replay is a redelivery of the same event, not a new one.

## Findings, with sources

### 1. Resend's dashboard has a documented Replay feature

Resend's own webhooks documentation states outright that failed **and**
succeeded webhook messages can be manually replayed from the dashboard:

> "If a webhook message fails, you can manually replay it. You can replay
> both `failed` and `succeeded` webhook messages."

— [Managing Webhooks / Retries and replays](https://resend.com/docs/webhooks/retries-and-replays)

The flow is: Webhooks page → select the endpoint → select the specific
message → click **Replay**. This is a per-message action against a specific
past delivery event, not a bulk "resend everything" control — exactly the
granularity issue #138 needs ("manually retry the same `hash(Message-ID)`
delivery").

Resend's webhooks introduction page corroborates the same capability at a
higher level, describing the ability to "replay any webhook event," useful
"when your endpoint missed an event, or when you want to reprocess events
with updated handler code" — [Managing Webhooks](https://resend.com/docs/webhooks/introduction).

### 2. Resend's webhook delivery is Svix-backed (confirms ADR-0007)

Resend's webhook-verification docs show the exact signature headers ADR-0007
already assumes (`${svix-id}.${svix-timestamp}.${raw_body}` HMAC), and
explicitly point at the Svix libraries as the supported/alternative way to
verify:

> "Alternatively, you can manually use the Svix libraries and manually pass
> it the headers, body, and webhook secret."

with header examples `svix-id`, `svix-timestamp`, `svix-signature` —
[Verify webhooks requests](https://resend.com/docs/dashboard/webhooks/verify-webhooks-requests).
This matches `n8n/src/svix.js`'s manual verification approach and confirms
Resend's replay behavior can be reasoned about using Svix's own documented
semantics for retries/replays (below), since Resend does not appear to
publish its own independent explanation of what changes vs. stays fixed on a
replayed delivery.

### 3. Svix's docs: a replay reuses the same message identity, not a new one

Resend's own pages don't spell out whether the *webhook envelope* (headers,
message id) is identical on replay or freshly minted. Svix's docs — the
underlying delivery infrastructure — do:

> "The unique message identifier (the `webhook-id` header, aliased as
> `svix-id`) is unique per message but is reused across retries of the same
> message."

— [Idempotency](https://docs.svix.com/idempotency)

Svix's retries documentation distinguishes automatic **retries** (exponential
backoff on delivery failure, matching the "~27 hours" schedule ADR-0007
cites) from customer-initiated **replay/manual resend**, describing the
dashboard action as being able to "manually retry each message at any time"
or "Recover Failed" / "Replay Missing" messages —
[Retry Schedule](https://docs.svix.com/retries). Svix's app-portal docs
describe the mechanics of the Replay button itself: selecting a message and
an attempt, then choosing "resend" to have "the same message send to your
endpoint again" —
[Replaying Messages](https://docs.svix.com/receiving/using-app-portal/replaying-messages).

Taken together, this is Svix's documented dedup contract: the transport-level
message id stays stable across both automatic retries and manual replays of
the same message, specifically so a receiving webhook consumer can use it as
an idempotency key. Svix's own guidance for consumers is to treat that id as
an idempotency key precisely because retries/replays reuse it.

**Caveat:** none of Svix's or Resend's pages I found state in so many words
"a replayed message's `webhook-id`/`svix-id` is byte-for-byte identical to
the original delivery's," only that it "is reused across retries of the same
message" and that replay resends "the same message." I read this as a
reasonably strong documented claim, not an inference from silence, but it is
worth flagging that I did not find a single sentence that says explicitly
"replay preserves the id" — it's assembled from adjacent, consistent
statements across two docs sites, both primary sources for the systems
involved (Resend for the product surface, Svix for the transport it's built
on). If issue #138 needs a stronger guarantee than "documented and
consistent," the fallback in the next section sidesteps the question
entirely.

### 4. What this means for the body/`Message-ID` (the thing n8n actually hashes)

Regardless of exactly what happens to the Svix transport envelope, the
**payload agregado's n8n workflow hashes never depends on Svix's own message
id** — `n8n/src/identity.js`'s `entryId()` hashes the *email* `Message-ID`,
which lives inside the webhook's JSON body (Resend's `email.received` event
data), not in a Svix header. A dashboard Replay, by definition, re-delivers
the stored body of that specific past event — Resend isn't re-fetching or
re-parsing the source email, it's re-POSTing the same recorded payload. So
even setting aside the transport-id question in §3, replaying a specific
`email.received` event is guaranteed to reproduce the same `Message-ID` in
the body, and therefore the same `hash(Message-ID)` — that part follows
directly from "replay resends a past event's payload" (§1), independent of
any Svix-header nuance.

## Recommendation for issue #138 (live-verification ticket)

Trigger "manually retry the same `hash(Message-ID)` delivery" as:

1. Send/receive one real (or fixture) newsletter email through the pipeline
   once, and let it ingest successfully or fail as expected.
2. In Resend's dashboard, go to **Webhooks → \<the n8n endpoint\> → \<that
   message\>** and click **Replay**.
3. Confirm in n8n's execution log that the replayed execution parses the
   same `Message-ID` from the payload, and confirm the alert-dedup D1 column
   (ADR-0007's check-and-set) does not fire a second active alert.

### Fallback, if the dashboard Replay proves insufficient in practice

If live verification finds the Replay button doesn't resurface old messages
past some retention window, or access to the Resend dashboard for this is
constrained, the equivalent can be reproduced manually:

1. Pull the raw request body and headers (`svix-id`, `svix-timestamp`,
   `svix-signature`, `content-type`) for the original delivery from n8n's
   execution log — n8n's Webhook node stores the raw incoming request on
   each execution, so the original bytes are recoverable without going back
   to Resend at all.
2. Re-POST that exact body and header set with `curl` against the tunnel
   ingress path ADR-0007 documents (the `/webhook/resend-<random-suffix>`
   path amended into ADR-0001), e.g.:

   ```bash
   curl -X POST "https://<tunnel-host>/webhook/resend-<suffix>" \
     -H "svix-id: <captured-svix-id>" \
     -H "svix-timestamp: <captured-svix-timestamp>" \
     -H "svix-signature: <captured-svix-signature>" \
     -H "content-type: application/json" \
     --data-binary @captured-body.json
   ```

3. Because n8n's Svix verification (`n8n/src/svix.js`) recomputes the HMAC
   over `${svix-id}.${svix-timestamp}.${raw_body}`, replaying the exact
   captured header/body triple passes verification identically to the
   original delivery, and the body's `Message-ID` is unchanged — so
   `hash(Message-ID)` is identical by construction. This fallback is
   strictly more certain than the dashboard Replay button on the §3 caveat,
   since there is no question of what Resend/Svix did internally — the same
   bytes are just sent again.

## Sources

- [Resend — Managing Webhooks / Retries and replays](https://resend.com/docs/webhooks/retries-and-replays)
- [Resend — Managing Webhooks (introduction)](https://resend.com/docs/webhooks/introduction)
- [Resend — Verify webhooks requests](https://resend.com/docs/dashboard/webhooks/verify-webhooks-requests)
- [Svix — Idempotency](https://docs.svix.com/idempotency)
- [Svix — Retry Schedule](https://docs.svix.com/retries)
- [Svix — Replaying Messages](https://docs.svix.com/receiving/using-app-portal/replaying-messages)
- Repo cross-references: [ADR-0007](../adr/0007-resend-n8n-cloudflare-split-for-newsletter-ingestion.md), `n8n/src/identity.js`, `n8n/src/svix.js`
