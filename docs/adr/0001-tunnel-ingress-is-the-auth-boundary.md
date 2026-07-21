# ADR 0001 — The Cloudflare Tunnel ingress ACL is the auth boundary

**Status:** Accepted
**Date:** 2026-07-17
**Amended:** 2026-07-21 — the allowlist was split across two public hostnames
(ingest vs. read) rather than widening the single existing regex. See Decision.
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
| `read.<domain>` | `^/(r\|articles)/[0-9a-f-]{36}/?$` | Digest click-through |

The UUID-shape requirement is load-bearing: it admits `/articles/{uuid}` (a
single article's reader page) while excluding `/articles` and `/articles/search`
(collection endpoints that would enumerate the full reading history).

The **split itself** is load-bearing for a different reason. `PUBLIC_BASE_URL`
stamps its hostname into every digest email, where it persists in Gmail and
leaks via any forward. Publishing the read paths on the *existing* hostname
would have made the leaked address and the ingestion address the same string.
With the split, a harvested digest reveals `read.<domain>` and reveals nothing
about where email ingestion answers. The ingest surface's regex is now expected
to **never change again**.

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

- **No 👍/👎 from the phone.** The feedback buttons live only on the web homepage
  (`templates/digest.html`) and POST to `/api/articles/{id}/feedback`. Serving them
  would require publishing `/` (which renders the reading history this ACL exists
  to hide) *and* an unauthenticated `/api/*` mutation. Only the **implicit** signal
  survives off-network: `/r/{id}` marks an article read before redirecting. If
  explicit feedback from email is wanted later, the cheap shape is a narrow
  `GET /f/{uuid}/{up|down}` published alongside `/r/`, not a wider ACL.
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
- **Tailscale instead of a public tunnel.** Rejected: the tunnel already exists and
  is required for the email Worker regardless, so Tailscale would be a second
  network path to maintain for no additional benefit.

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
