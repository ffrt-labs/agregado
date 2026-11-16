# ADR 0001 — The Cloudflare Tunnel ingress ACL is the auth boundary

**Status:** Accepted
**Date:** 2026-07-17
**Amended:** 2026-07-21 — the allowlist was split across two public hostnames
(ingest vs. read) rather than widening the single existing regex. See Decision.
**Amended:** 2026-08-30 — Tailscale was reconsidered and adopted, but for
personal access to the homelab as a whole, not as a replacement for this
tunnel. See "Amendment, 2026-08-30" below.
**Amended:** 2026-09-23 — the ingest hostname gained a second path, routing to
n8n instead of the app, for the Resend-webhook newsletter-ingestion redesign.
See "Amendment, 2026-09-23" below.
**Amended:** 2026-11-16 — the superseded app surface this ADR protected was
deleted (#85), and the tunnel connector no longer runs from this repository.
See "Amendment, 2026-11-16" below.
**Supersedes the implicit reading of:** PRD F9 ("admin console unauthenticated in v1")

## Context

Agregado runs in a homelab behind NAT with no public IP and no inbound ports
open. A `cloudflared` daemon dials out to Cloudflare and holds a persistent
connection; Cloudflare routes public requests down that pipe. The tunnel exists
because the Cloudflare Worker that bridges Email Routing to the app runs on
Cloudflare's edge and must reach the app over the public internet — a private
LAN IP is not internet-routable, and the Worker's `global_fetch_strictly_public`
flag explicitly blocks private/loopback fetches.

The app has **no authentication middleware**. The router applies only
`RequestID`, `Logger`, and `Recoverer`. The admin console, all `/api/*` mutation
endpoints, source deletion, prompt editing, and digest triggering are all
unauthenticated.

This was safe because the tunnel's ingress rule was regex-scoped to a single
path:

```
^/webhook/email/?$
```

Everything else was denied at Cloudflare's edge and never reached the process.

The decision needed making because publishing digest links off-network requires
`/r/{id}` to be publicly routable, which means widening that regex — and
widening it is exactly what would expose the unauthenticated surface.

## Decision

**The tunnel ingress ACL is the security boundary. Keep it there. Do not add
app-level authentication.**

Access is granted by adding paths to the ingress allowlist, scoped as tightly as
the requirement permits.

**As of Phase 19 the boundary is split across two public hostnames on the same
tunnel**, both resolving to the same origin (`http://agregado:8080`), each with
its own independent path filter:

| Hostname | Ingress regex | Serves |
|---|---|---|
| *(the original tunnel hostname)* | `^/webhook/email/?$` | Worker → app ingestion |
| `read.<domain>` | `^/(r\|articles)/[0-9a-f-]{36}/?$\|^/f/[0-9a-f-]{36}/(up\|down)/?$` | Digest click-through and explicit feedback |

The UUID-shape requirement is load-bearing: it admits `/articles/{uuid}` (a
single article's reader page) while excluding `/articles` and `/articles/search`
(collection endpoints that would enumerate the full reading history).

The **split itself** is load-bearing for a different reason. `PUBLIC_BASE_URL`
stamps its hostname into every digest email, where it persists in Gmail and
leaks via any forward. Publishing the read paths on the *existing* hostname
would have made the leaked address and the ingestion address the same string.
With the split, a harvested digest reveals `read.<domain>` and reveals nothing
about where email ingestion answers. ~~The ingest surface's regex is now
expected to never change again.~~ **Correction, 2026-09-23: it changed.** See
"Amendment, 2026-09-23" below — this sentence was invalidated by the newsletter-
ingestion redesign and is struck rather than left standing as a silent-false
claim; see this ADR's own Notes section for why that specific failure mode is
worth guarding against.

**Any change to either regex is a security change.** They are the only thing
standing between the internet and an unauthenticated admin console.

A single wider regex on one hostname was the obvious alternative and was
rejected. It is not meaningfully less secure — both fail closed at the edge —
but it forfeits the compartmentalization above for no saving: a second public
hostname on an existing tunnel is one dashboard entry and an auto-created
proxied DNS record.

## Consequences

**Good:**

- Fails closed at the edge, before a request touches the process. Router
  middleware cannot protect a route someone forgot to wrap; an allowlist protects
  everything nobody explicitly published.
- Reduces "should I add auth?" to "which paths does the outside world legitimately
  need?" — a smaller, more auditable question, answered in one config field
  rather than spread across a router.
- Zero code. No auth flow on cold clicks from the digest email.
- The two surfaces are compartmentalized. The address that leaks through forwarded
  digests is not the address that accepts ingestion, and the ingest regex is frozen.
- No inbound port is opened. `cloudflared` holds an outbound connection; NAT and
  the router firewall are untouched. "Publicly reachable" and "zero open ports"
  are not in tension.

**Bad / accepted:**

- **Explicit feedback is a public state mutation.** Digest links use
  `GET /f/{uuid}/{up|down}` and are allowed only on `read.<domain>`. The route records
  one current vote per Article: repeating the direction is idempotent and the opposite
  direction replaces it. It returns a confirmation only, never a reading surface.
  Opens remain the separate, weak signal at `/r/{id}`.
- The reader page renders the full nav (`templates/layout.html`), leaking article,
  source, and bookmark **counts** and the existence of `/admin/*` to anyone holding
  a UUID. The paths themselves stay denied at the edge; only metadata escapes.
  Accepted to unblock the fix — tightening is tracked against Phase 20, when real
  newsletter articles first exist to test a stripped layout against.
- Two hostnames now have to stay conceptually straight. The failure mode is
  publishing a path on the ingest surface out of habit.

- **The boundary is invisible from the repository.** The tunnel is token-managed
  (`CLOUDFLARE_TUNNEL_TOKEN`, `command: tunnel run`), so ingress rules live in the
  Cloudflare dashboard, not in version control. Nothing in a code review can show
  you the ACL. This ADR exists partly to compensate.
- Anyone holding an article's UUIDv4 can read that article and mark it read.
  UUIDv4 is not guessable; the consequence is negligible.
- The reader page's nav renders dead links when opened off-network — those routes
  are denied at the edge. Accepted deliberately (Phase 19); reading works,
  navigating away does not.
- If the app is ever hosted anywhere without an equivalent ACL in front of it, it
  is **immediately and fully exposed**. This decision does not travel with the
  code.

## Alternatives considered

- **Cloudflare Access in front of everything.** Rejected: imposes an auth flow on
  every cold click from the digest email, and `/webhook/email` would need carving
  out (or a service token) or newsletter ingestion breaks.
- **App-level auth middleware in Chi.** Rejected: more code, weaker guarantee than
  an edge ACL (a route you forget to wrap is exposed; a path you forget to publish
  is not), and PRD F9 already deferred it.
- **Tailscale instead of a public tunnel.** Rejected at the time for the question
  this ADR was answering — anonymous public reach for digest links and the email
  Worker, which Tailscale cannot serve at all. That rejection stands for that
  question. It does not stand for personal access to the homelab in general;
  see "Amendment, 2026-08-30" below for what changed and why the tunnel was
  kept anyway.
- **Tailscale Funnel instead of the tunnel, once Tailscale was adopted for
  personal access.** Rejected: Funnel would mean rebuilding, in front of an
  app with no auth middleware at all, the exact path allowlist this ADR calls
  the auth boundary — trading a boundary that already exists and works for one
  that would have to be reconstructed on a different platform. See
  "Amendment, 2026-08-30" below.

## Notes

The trap this ADR guards against: **"unauthenticated in v1" was correct when
written, and was silently invalidated later by an unrelated change.** The tunnel
was added for email ingestion, by someone not thinking about the admin console.
The two decisions were only reconnected when Phase 19 needed to publish a new
path — and only because someone happened to check.

Related: setting `PUBLIC_BASE_URL` (Phase 19) stamps a tunnel hostname into every
digest email, where it lives in Gmail and leaks via any forward. That does not
create a hole — this ACL is what protects the app — but it does publish the
address of a front door, which is why the ingress scoping shipped in the same
phase, and why Phase 19 chose to make it a *second, separate* door rather than
the one ingestion already uses.

A note on what this ADR does **not** protect. The boundary is a path allowlist,
so it is only as good as the assumption that a published path is safe to expose.
`/r/{uuid}` and `/articles/{uuid}` are not read-only: both call `MarkRead`. That
is intended — the read signal is the point — but it means the public surface
already includes a state mutation. Any future "just publish one more path"
should be checked for writes, not assumed idempotent because it is a `GET`.

## Amendment, 2026-08-30: Tailscale reconsidered, tunnel retained

`homelab-edge` has since put the host this app runs on onto a Tailscale
tailnet, and moved the wildcard DNS record for personal access to a tailnet
address (`homelab-edge#10`, recorded in that repo's
`docs/adr/0001-tailnet-membership-is-the-access-boundary.md`). That is exactly
the "Tailscale instead of a public tunnel" alternative this ADR rejected above
— reconsidered, and this time adopted. A future reader landing on the flat
"rejected" line in Alternatives considered would be misled into thinking
Tailscale was tried and dismissed for this purpose; it wasn't. It was adopted,
for a different purpose, and this tunnel was kept anyway. Both are true at
once, which is precisely the trap this ADR's own Notes section already warns
about.

**The tunnel is retained deliberately, not by inertia.** Tailscale cannot
serve the consumers this ADR's tunnel exists for: the Cloudflare Worker that
bridges Email Routing to the app, and anyone who clicks a digest link without
being an enrolled tailnet device — both are, by definition, off the tailnet.
Collapsing onto Tailscale Funnel instead was considered and rejected; see
Alternatives considered above.

**This changes nothing about the boundary this ADR defines.** The ingress
allowlist — the two-hostname split, the frozen ingest regex, the UUID-scoped
read regex — is unchanged. This amendment widens nothing; it only records that
the personal-access question and the anonymous-access question this ADR
answers are separate questions, now visibly answered by separate mechanisms,
on purpose.

## Amendment, 2026-09-23: n8n's Resend-webhook ingress

[ADR-0007](0007-resend-n8n-cloudflare-split-for-newsletter-ingestion.md)
replaces Cloudflare Email Routing + a single Worker with Resend (receiving) +
n8n (parsing, extraction, storage writes) + a slimmed Worker (serving only).
n8n is self-hosted in the homelab, so its Resend-webhook endpoint needs a new
public ingress path — directly contradicting this ADR's own claim, struck
above, that the ingest regex was "now expected to never change again."

**The new rule reuses the existing ingest hostname**, not a third one. That
hostname vacates its old role (`^/webhook/email/?$` served the now-deleted
Worker `email()` handler) and keeps its conceptual one — "this hostname is the
ingestion surface" — rather than spending a fresh hostname for no security
gain. The 2026-07-21 hostname split (recorded above) was about separating
*read* from *ingest*; old-ingest and new-ingest are the same category of
surface and share its hostname.

**New path:** an unguessable random segment set as n8n's Webhook node custom
path (`/webhook/resend-<random-suffix>`). This is not a security boundary on
its own — Svix's signature is — but it is cheap friction against blind
scanning before signature verification runs.

**This path routes to a different origin than the rest of the tunnel.** Both
existing hostnames point at `http://agregado:8080` (the Go app). This path
routes to n8n's origin instead — Cloudflare Tunnel supports a different
backend per hostname/path within one tunnel natively.

**Defense in depth, not app-level auth.** Svix HMAC signature verification
(hand-rolled in an n8n Code node — no native n8n support) is the real
authentication, the same tier of guarantee Stripe- or GitHub-style webhook
auth relies on as its sole check. No Cloudflare Access in front: Resend's
webhook delivery cannot present custom auth beyond its own signature headers,
so Access would block legitimate deliveries, not just attackers. A Cloudflare
rate-limit rule scoped to this path is added as a free, zero-cooperation-
required backstop against flooding/scanning — consistent with this ADR's
edge-first posture, not a departure from it.

**No isolation needed today; a rule for tomorrow.** Neither the Miniflux
`new_entries` webhook (n8n calling Agregado's enrichment) nor the
`PREFERENCES.md` nightly Drive sync needs public ingress — both stay internal
to the homelab network. This Resend path is the only n8n workflow crossing the
public tunnel today. Continuing this ADR's hostname-split pattern
proactively rather than waiting for a second real case: **any future n8n
workflow needing public ingress gets its own hostname**, not a second path on
this one.

**What this does and doesn't change.** The boundary is still the tunnel
ingress ACL — this amendment adds a path to it, on the same terms as every
prior addition, and does not introduce app-level auth anywhere. It does
retire the "never changes again" expectation for the ingest hostname
specifically; that expectation is now understood to have been about the
*old* email-ingestion regex, not a permanent property of the hostname itself.

## Amendment, 2026-11-16: the protected surface is gone, and the connector moved out

Recorded while removing the superseded system
([#85](https://github.com/ffrt-labs/agregado/issues/85)). This amendment
narrows the picture; it adds no path to any allowlist.

**Most of what this ADR's Context and Consequences describe no longer exists.**
The admin console, Source management, bookmark pages, prompt editing, digest
triggering and the HTMX reader were deleted
([#238](https://github.com/ffrt-labs/agregado/issues/238),
[#239](https://github.com/ffrt-labs/agregado/issues/239)), along with
`templates/` and the `/articles/{uuid}` reader page. The
`^/webhook/email/?$` handler was deleted with the RabbitMQ wiring
([#236](https://github.com/ffrt-labs/agregado/issues/236)). The router
(`internal/api/server.go`) now serves only:

| Route | Protection beyond the network |
|---|---|
| `GET /health`, `GET /health/db` | none |
| `/api/private/*` (enrich, digests, preferences, preference signals) | `X-Enrichment-Secret` shared secret |
| `GET /r/{id}` | unguessable Article Index or legacy Article UUID |
| `GET /f/{id}/{up\|down}` | unguessable Article Index UUID |

So the Consequences bullets about the reader page leaking nav counts and
`/admin/*` links, and about dead nav links off-network, are moot: there is no
reader page and no nav. `GET /r/{id}` is a redirect only; for a legacy Article
with no web home it now returns 404 instead of falling back to a reader page.
It still records a read or an Open, so the Notes section's warning about
`GET` routes that write still applies.

**The tunnel no longer reaches Agregado at all.** [ADR-0008](0008-agregado-is-tailnet-only.md)
and its 2026-11-14 amendment moved Agregado to tailnet-only access behind the
homelab-edge Caddy, and the tunnel's Agregado paths were removed. The
`read.<domain>` row of the Decision table describes a route that no longer
exists. The only rule that survives is the ingest hostname's
`/webhook/resend-<random-suffix>` path to n8n (the 2026-09-23 amendment
above). This ADR's boundary now governs that one path; Agregado's boundary is
ADR-0008's.

**The `cloudflared` connector no longer runs from this repository.** The
bundled `cloudflared` Compose service, its `CLOUDFLARE_TUNNEL_TOKEN` variable
and deploy secret, and the token-rotation runbook were removed
([#240](https://github.com/ffrt-labs/agregado/issues/240)). The connector
that serves the ingest tunnel runs outside this repository, so nothing here
deploys or configures it. The "boundary is invisible from the repository"
consequence is now stronger: neither the ingress rules nor the connector are in
version control here.
