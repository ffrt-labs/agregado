# ADR 0008: Agregado is tailnet-only after the Reader cutover

**Status:** Accepted

**Date:** 2026-09-24

**Effective:** With the production cutover in issue #84

**Changes:** The Agregado and read-path portions of ADR-0001 after cutover

**Unaffected:** ADR-0001's Resend-to-n8n Bridge ingress and ADR-0007

## Context

ADR-0001 made Cloudflare Tunnel path allowlists the authentication boundary for
the legacy Agregado process. The public tunnel originally served two kinds of
traffic:

1. newsletter ingestion into Agregado; and
2. Digest Open, Article-reading, and feedback links.

ADR-0007 has since replaced the original newsletter route. Resend now calls an
n8n webhook through the ingest hostname. n8n parses the message, writes the
Bridge's D1 and R2 records, and calls Agregado's private Enrichment endpoint.
The slimmed Cloudflare Worker only serves Atom feeds and permalinks. The Bridge
still needs public Cloudflare ingress, but that ingress now terminates at n8n,
not Agregado.

The post-#48 target also deletes Agregado's Article-reading UI. The owner
accepts that the remaining Open and explicit-vote links work only from personal
devices connected to the tailnet.

The homelab already uses Tailscale for personal access. Agregado no longer
needs a public route once the legacy newsletter webhook and reading UI are
gone.

## Decision

After the production cutover in #84:

1. **Agregado is reachable only inside the Tailscale tailnet.** Tailscale Serve
   exposes the HTTP service to permitted tailnet devices. Tailscale Funnel is
   not used.
2. **Cloudflare Tunnel stops routing traffic to Agregado.** Remove the public
   read hostname and the old Agregado newsletter route. Keep the ingest
   hostname and its Resend webhook path because they route to n8n for the
   Bridge described by ADR-0007.
3. **Digest Open and explicit-vote links are tailnet-only.** They work only on
   a device connected to the tailnet. Forwarded Digest links and non-tailnet
   devices are unsupported.
4. **Private n8n calls keep a rotatable shared secret.** Tailscale decides which
   devices can reach Agregado. The secret authorizes the n8n workload to invoke
   private mutations.
5. **Agregado gets no user-account system.** It remains a single-user headless
   service.
6. **Cutover must fail closed.** Keep the current public routes until #84 proves
   the replacement path. Remove them only after rollback has been exercised.

## Target exposure

| Responsibility | Network boundary | Additional authorization |
|---|---|---|
| Enrich or retry Article | Tailnet | Shared secret |
| Create or retrieve Digest | Tailnet | Shared secret |
| Export preference signals | Tailnet | Shared secret |
| First-Open redirect | Tailnet | Unguessable Article Index ID |
| Explicit vote | Tailnet | Unguessable Article Index ID |
| Health and readiness | Tailnet | Tailnet ACL |

Tailscale ACLs should permit only the required users and workloads. Agregado
should bind so an unintended LAN or host-public interface cannot bypass the
proxy boundary.

## Consequences

### Positive

- Agregado has no public route after cutover.
- Personal access uses the same private network as the rest of the homelab.
- Adding an Agregado route cannot accidentally publish it through the old edge
  allowlist.
- n8n authorization remains explicit through the shared secret.
- Agregado needs no accounts, sessions, or passwords.

### Negative and accepted

- Open and vote links fail when Tailscale is disconnected or unavailable.
- Forwarded Digest emails have no working feedback links outside the tailnet.
- Tailscale availability and ACL correctness become runtime dependencies.
- n8n still needs a provisioned and rotated shared secret.
- Cloudflare Tunnel remains part of the wider system because the Resend Bridge
  webhook is public, even though it no longer routes to Agregado.

### Follow-up

- Set `PUBLIC_BASE_URL` to the Tailscale Serve hostname during #84.
- Verify Open, vote, health, and private n8n calls from allowed tailnet devices.
- Verify the same Agregado routes are unreachable outside the tailnet.
- Remove the public read hostname and every tunnel route whose origin is
  Agregado after rollback has been exercised.
- Preserve `/webhook/resend-<random-suffix>` to n8n and its scoped Cloudflare
  rate limit.
- Decide whether the shared `cloudflared` container remains in this repository
  or moves to the infrastructure repository. Its Bridge responsibility survives
  either way.
- Record Tailscale Serve and ACL configuration in the infrastructure repository.

## Alternatives considered

### Keep Cloudflare Tunnel for Open and vote routes

Rejected for the target. It preserves off-tailnet clicks but keeps a public
Agregado route for behavior used by one person whose reading devices are on
the tailnet.

### Use Tailscale Funnel

Rejected. Funnel restores public internet access and recreates the path
publication and abuse-control work that this decision removes from Agregado.

### Remove the shared secret because Tailscale is private

Rejected. Tailnet membership identifies devices that may connect. It does not
identify n8n as the workload allowed to run orchestration mutations.

### Add application-wide authentication

Rejected. There is no multi-user product after #85. Accounts would add state
and failure modes without meeting a current requirement.

## Related material

- [ADR-0001: Cloudflare Tunnel ingress ACL](0001-tunnel-ingress-is-the-auth-boundary.md)
- [ADR-0006: Miniflux is the Reader backend](0006-miniflux-is-the-reader-backend-not-a-service.md)
- [ADR-0007: Resend, n8n, and Worker Bridge split](0007-resend-n8n-cloudflare-split-for-newsletter-ingestion.md)
- [Agregado architecture](../architecture/agregado.md)
- [Reader ecosystem architecture](../architecture/ecosystem.md)
- [#84: Production cutover](https://github.com/ffrt-labs/agregado/issues/84)
- [#85: Remove the superseded system](https://github.com/ffrt-labs/agregado/issues/85)
