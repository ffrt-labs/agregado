# ADR 0001 — The Cloudflare Tunnel ingress ACL is the auth boundary

**Status:** Accepted
**Date:** 2026-07-17
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
the requirement permits. As of Phase 19 the allowlist is:

```
^/(webhook/email|r/[0-9a-f-]{36}|articles/[0-9a-f-]{36})/?$
```

The UUID-shape requirement is load-bearing: it admits `/articles/{uuid}` (a
single article's reader page) while excluding `/articles` and `/articles/search`
(collection endpoints that would enumerate the full reading history).

**Any change to this regex is a security change.** It is the only thing standing
between the internet and an unauthenticated admin console.

## Consequences

**Good:**

- Fails closed at the edge, before a request touches the process. Router
  middleware cannot protect a route someone forgot to wrap; an allowlist protects
  everything nobody explicitly published.
- Reduces "should I add auth?" to "which paths does the outside world legitimately
  need?" — a smaller, more auditable question, answered in one config field
  rather than spread across a router.
- Zero code. No auth flow on cold clicks from the digest email.

**Bad / accepted:**

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

Related: setting `PUBLIC_BASE_URL` (Phase 19) stamps the tunnel hostname into
every digest email, where it lives in Gmail and leaks via any forward. That does
not create a hole — this ACL is what protects the app — but it does publish the
address of the front door, which is why the ingress scoping shipped in the same
phase.
