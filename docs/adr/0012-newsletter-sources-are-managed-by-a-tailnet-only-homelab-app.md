# ADR 0012: Newsletter Sources are managed by a tailnet-only homelab app, not the Bridge Worker

**Status:** Accepted

**Date:** 2026-11-26

**Issue:** [#228](https://github.com/ffrt-labs/agregado/issues/228) (spec [#225](https://github.com/ffrt-labs/agregado/issues/225))

**Unaffected:** ADR-0007's split. The Bridge Worker keeps its two read paths
and gains no write path. ADR-0008's boundary for Agregado itself.

## Context

Adding a newsletter to the Bridge took wrangler, the Resend dashboard and a
Miniflux subscription by hand, across two machines
([runbook](../runbooks/newsletter-source-onboarding.md)). Nothing showed which
Newsletter Sources exist, how often they publish, or whether Miniflux can still
read their feeds. Spec #225 settles a small **Newsletters** app that manages
Newsletter Sources end to end. This ADR records where it runs and how it is
reached.

Two places were possible: the Bridge Worker on Cloudflare, which already sits
next to D1 and R2, or the homelab, next to Miniflux and n8n.

- **Miniflux is tailnet-only.** Add must subscribe Miniflux to the new feed,
  and the List shows Miniflux's fetch health. A Worker cannot reach Miniflux
  without a new public route into the homelab.
- **The Worker has no write path, on purpose** (ADR-0007,
  `email-worker/src/index.ts`): it serves Atom feeds and permalinks, and n8n
  writes D1 and R2. An admin UI there would be the first public write surface
  on the Bridge, and would need real authentication.
- **Agregado is going headless** (#85) and has no UI left to host this in.
- **One user.** Agregado has no login and relies on the tailnet (ADR-0008).
  The same can hold for this app if its route only admits tailnet clients.

## Decision

1. **The Newsletters app is a separate Go service in this repository,
   `cmd/bridge-admin`**, with its own container in `docker-compose.yml`,
   built and started by the existing deploy pipeline. It renders HTML on the
   server (`html/template`) and uses no JavaScript beyond small conveniences
   (alias suggestion, copy button).
2. **It runs on the homelab and is served at `newsletters.felipefreitas.dev`
   by the homelab-edge Caddy.** The route admits only `remote_ip
   100.64.0.0/10` (the tailnet range). This is the first homelab-edge route
   with an address restriction: the app has no login, so the route, not DNS
   alone, is the boundary. The container publishes no host port.
3. **It reaches the Bridge's storage through Cloudflare's REST API**, not
   through the Worker: D1's query endpoint for Sources and entries (and R2 for
   Delete, #230). Miniflux is reached over the `edge` Docker network.
4. **Dedicated credentials**, each revocable without touching n8n or
   Agregado: a Cloudflare token scoped to D1 Edit on `bridge` (R2 write is
   added with Delete), a Miniflux API key, and from #229 a Resend key.
   They are separate from Agregado's Workers AI token. The boot log carries
   none of them (`cmd/bridge-admin/startup_test.go`).
5. **The app owns the feed password only for the length of an Add.** It
   generates 24 random bytes, writes their SHA-256 to D1 (the Worker's
   `verifySecret`), gives the plaintext to Miniflux as the feed's Basic-auth
   password, and keeps it nowhere.
6. **A failed Miniflux subscription rolls the Add back.** The Pending row is
   inserted first, because Miniflux fetches the feed while subscribing and the
   Worker 404s an unknown alias. If the subscription fails, the app deletes
   the row again (only while it is still Pending), so no Source is left with a
   feed nobody can read. The alternative, keeping the row and offering
   Reconnect, was rejected for Add: Reconnect (#231) exists for Sources that
   already have a subscription, and a half-added Source would show up in the
   List as something to clean up.
7. **The app's configuration names the Bridge's addresses**:
   `BRIDGE_ORIGIN` (the Worker's origin, as n8n uses it) and
   `BRIDGE_RECEIVING_DOMAIN` (the Resend catch-all). A Source's feed is
   `BRIDGE_ORIGIN/feed/<alias>.atom`, and the List finds its Miniflux feed by
   that exact URL.

## Consequences

### Good

- Adding a newsletter is one form. No wrangler, no script, no second machine.
- The Worker stays read-only and public; the only new write surface is on the
  tailnet.
- Miniflux, n8n and the app all see the same D1 rows, and n8n needs no change:
  it already ignores any status other than `active`.

### Bad, accepted

- A device on the home LAN cannot use the app, since the route admits only
  tailnet addresses. Requests from the server itself (Docker gateway
  addresses) are refused as well.
- Changing `BRIDGE_ORIGIN` (for example when `bridge.felipefreitas.dev` gets
  its record, ADR-0008) breaks the List's feed matching until every Miniflux
  feed URL is updated.
- If Miniflux creates the feed but its answer is lost (a timeout), the
  rollback leaves a Miniflux feed for an alias D1 no longer has. It shows as
  a 404 in Miniflux and is removed by hand.
- The Cloudflare token can edit the whole `bridge` database. D1 tokens cannot
  be scoped to tables.

## Alternatives considered

### An admin path on the Bridge Worker

Close to D1 and R2, but it cannot reach Miniflux, and it would put a write
path and a login on a public hostname. Rejected.

### A page inside Agregado

Agregado is becoming headless (#85), and the Bridge is not Agregado's
storage. Sharing a process would also share credentials and the deploy's
failure modes. Rejected.

### A CLI or script

Cheaper to build, but it brings back the two-machine problem, and it gives no
place to see Sources, their stats and their feed health. Rejected.

## Notes

- Vocabulary: **Newsletter Source**, **Delete** in `CONTEXT.md`.
- The Caddy route lives in `ffrt-labs/homelab-edge` and deploys separately.
