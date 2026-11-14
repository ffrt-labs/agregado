# Runbook — production cutover to the new Reader

Issue #84 (parent #48). This switches production from the old single-Postgres
Agregado to the Reader:

- Miniflux owns Articles and subscriptions.
- Karakeep owns Bookmarks.
- n8n orchestrates.
- Agregado keeps only the Article Index, the Digest artifact, Opens, feedback
  and the preference profile, reachable only on the tailnet
  ([ADR-0008](../adr/0008-agregado-is-tailnet-only.md)).

`scripts/cutover-wizard.sh` walks this runbook stage by stage. It runs the
commands and logs everything. This document explains the order and how to
back out. Stage numbers match the wizard.

## Before you start

- **Where to run it.** Run it on the homelab host, from any checkout of this
  branch. It doesn't need to be merged or deployed first. The checkout's
  directory must be named `agregado` (or set `COMPOSE_PROJECT_NAME=agregado`),
  so that `docker compose --profile prod` resolves to the deploy's project and
  sees its running containers. The wizard checks this in stage 1.
- **Where its files live.** All state (`cutover.env`, the log, the OPML, the
  dump) goes to `CUTOVER_DIR`, which defaults to `~/agregado-cutover`. It sits
  outside the checkout because the deploy's `actions/checkout` deletes
  untracked files on every run, and stage 8 runs a deploy.
- **When.** Start after today's 08:00 Berlin Digest, and finish before
  01:00 Berlin. The Digest is keyed by UTC date, so an artifact created
  outside that window would be sent a second time by the next 08:00 schedule.
  Allow about two hours.
- **Prerequisites.** Tailscale Serve exposes Agregado on the tailnet, and the
  device you verify from is on it.

---

## Order, and why

| # | Stage | Reversible? |
| --- | --- | --- |
| 1 | Go / no-go: blockers closed, timing, tailnet, both test suites green | — |
| 2 | Final Karakeep backup (sidecar, `BACKUP_ONCE=1`) | yes |
| 3 | OPML export of the old Sources | yes |
| 4–5 | Migration prerequisites, dry run | yes |
| 6 | Apply the ownership migration (twice: the second run proves idempotency) | **no**: writes Karakeep and the Article Index |
| 7 | Final cold dump of the old Postgres | yes |
| 8 | Deactivate the RSS Sources (saving their ids), redeploy with `PUBLIC_BASE_URL` = the tailnet URL | yes |
| 9 | Import the n8n workflows, make sure a preference profile is stored, activate the webhook workflows | yes |
| 10 | Import the OPML into Miniflux, point its webhook at n8n | yes |
| 11 | Newsletter Sources onto the Bridge | yes |
| 12–14 | Verify ingestion, Open/feedback, Digest retry, preferences; then turn the schedules on | — |
| 15 | Rollback drill, then remove every tunnel route into Agregado | the route removal is the point of no easy return |

The order has four constraints:

- **The poller goes off before Miniflux subscribes (8 before 10).** The legacy
  in-process RSS poller still runs in the deployed binary until #85 removes it.
  If both run, every Article is ingested twice and enriched twice, so the model
  cost doubles too. The poller only reads Sources with `is_active = true`, so
  stage 8 saves the ids of the active RSS Sources to
  `$CUTOVER_DIR/active-rss-sources.txt` and then deactivates them all. That
  works on the currently deployed binary, with no code change. (Once this
  branch is deployed, `RSS_POLLER_ENABLED=false` does the same without
  touching the old data.)
- **n8n is ready before Miniflux sends anything (9 before 10).** The first
  fetch of every imported Source fires the webhook for its current entries.
  If the workflow isn't active yet, those entries are lost. Expect a burst of
  enrichment model cost at that point.
- **A preference profile is stored before enrichment runs (stage 9).**
  Enrichment refuses to score without one
  ([ADR-0010](../adr/0010-preference-profile-lives-in-agregados-database.md)).
  The wizard checks `GET /api/private/preferences` and, if no profile is
  stored, has you run `preferences-sync` once by hand.
- **The old public routes go last (stage 15).** ADR-0008 says the cutover
  fails closed: the public routes stay until the replacement path is proven
  and rollback has been exercised. Only then do the read hostname and every
  tunnel route whose origin is Agregado (`/webhook/email` among them) come
  out. The ingest hostname's `/webhook/resend-<suffix>` route to n8n stays
  ([ADR-0007](../adr/0007-resend-n8n-cloudflare-split-for-newsletter-ingestion.md)).

The migration itself (stages 4–7) is documented step by step in
[legacy-postgres-cutover.md](legacy-postgres-cutover.md). The migration
command loads the full Agregado config, so stage 4 also collects the
database credentials and `ENRICHMENT_SECRET`. RabbitMQ gets placeholders,
because the migration never touches it.

---

## Acceptance criteria → evidence

| AC | Where it's proven |
| --- | --- |
| Final Karakeep backup and cold dump verified | Stage 2 sidecar output; stage 7 manifest says `restore: VERIFIED` and `sha256sum -c` passes |
| Migration completes with reconciled counts | Stage 6: `FAIL` 0; second apply shows `DEST` 0 everywhere; `SOURCE = DEST + SKIP + DUP` |
| A live RSS Article and an email-only Article reach Miniflux, enrichment and the Digest | Stage 12 |
| `/r/{id}` records one Open and redirects; `/f/{id}/{up\|down}` replaces the vote | Stage 13: two GETs to `/r/` both give 302 and `opened_at` is unchanged after the second; down then up leaves one `article_index_feedback` row, `up`, with `created_at` unchanged; any other vote gives 404 |
| Digest retry sends the same artifact without regenerating it | Stage 14: two `POST /api/private/digests/{date}` bodies hash the same (201 then 200), no second `digest_select` trace in Langfuse |
| Nightly preference proposal | Stage 14, see below |
| Old stack stopped but recoverable; rollback exercised or verified | Stages 8 and 15 |

Stage 13 records a real Open and a real vote, so pick an Article you would
open and upvote anyway.

**AC 6 vs ADR-0010.** The issue asks for a *reviewable* proposal. ADR-0010
(accepted after this issue was written) removed the review gate: the nightly
`preferences-sync` regenerates the profile, validates it, and writes it back
to the Google Doc. Review happens afterwards: the n8n alert shows what
changed, and the Doc's revision history is the rollback. The wizard asks you
to accept that as the evidence. If you don't, stop and open a follow-up issue.
Either way, update the AC on #84 so it matches.

---

## Rollback

### Triggers

Roll back if, within the observation window (~48 h, before #85 starts):

- no Digest arrives at 08:00, or it arrives twice;
- enrichment fails for most new Articles (n8n `article-enrichment` executions
  red, or `article_index` stops growing while Miniflux has new entries);
- Open or vote links from the Digest don't work from a tailnet device;
- the migration turns out to have lost or duplicated Bookmarks in Karakeep.

### Steps

There's no separate old image to boot. The deploy rebuilds with `--no-cache`,
so rolling back means turning the old poller back on in the running binary.

1. In n8n, deactivate `article-enrichment`, `newsletter-ingest`,
   `daily-digest` and `preferences-sync`. In Miniflux, disable the webhook
   integration. Nothing new reaches Agregado now.
2. Reactivate exactly the RSS Sources stage 8 deactivated. No redeploy is
   needed: the poller picks them up within `RSS_POLL_INTERVAL` (15 m).

   ```sh
   ids=$(sed "s/.*/'&'/" ~/agregado-cutover/active-rss-sources.txt | paste -sd,)
   docker exec -i agregado-db psql -U "$DATABASE_USER" -d "$DATABASE_DB" \
     -c "UPDATE sources SET is_active = true WHERE id IN ($ids)"
   ```

   The migration only reads the old tables, so the old app resumes where it
   stopped. Anything published during the cutover is picked up on the next
   poll, because each Source keeps its `last_fetched_at`.
3. If the rollback happens after stage 15, also restore the tunnel routes you
   removed. Then set `PUBLIC_BASE_URL` back to the old public read URL and
   re-run the latest deploy (`deploy.yml` has no `workflow_dispatch`).
4. If the old tables are damaged, restore the off-box dump. Postgres runs in
   the `agregado-db` container, so go through `docker exec`:

   ```sh
   docker exec -i agregado-db createdb -U "$DATABASE_USER" agregado_restored
   docker exec -i agregado-db pg_restore -U "$DATABASE_USER" -d agregado_restored \
     --no-owner --no-acl < agregado-legacy-<stamp>.dump
   ```

   Compare the row counts with the manifest, then point the app at it.
5. Karakeep: Bookmarks the migration created are harmless to keep. If you need
   to remove them, restore the stage 2 backup (homelab-apps
   `docs/karakeep-backup.md`).
6. Preferences: restore an earlier revision of the `PREFERENCES` Doc, then run
   `preferences-sync` once.

The Article Index and Digest tables are additive, so leave them in place.

### What the drill covers

- **Reactivating the Sources** is verified rather than exercised: running it
  would start the old poller next to Miniflux. Stage 15 checks that the saved
  list exists and has you copy it off-box next to the dump. The step itself
  is a single `UPDATE` of a column stage 8 just changed.
- **Stage 15** also restores the dump copied *back from off-box* into a scratch
  database, compares every table's row count with the manifest, then drops
  the scratch database. It also confirms the Doc has a revision to go back to.

---

## What stays running after cutover

Until #85, the deployed image still contains the old components. With the
poller off and the tunnel routes gone, they are idle or tailnet-only:

- **RabbitMQ and its consumers** receive nothing, because the poller was the
  only RSS publisher and `/webhook/email` is no longer routed.
- **The legacy Digest** never had its own schedule. It only ever ran on
  `POST /api/digest/send` (the old HTMX button), so nothing sends it.
- **The old HTML pages** are still served, but only on the tailnet.
- **The weekly OPML backup mail** keeps running. It's harmless, and #85
  removes it.

Before removing the `/webhook/email` route, stage 15 counts its hits over the
last 24 h. A hit means a publisher still mails an old address. Onboard that
Source onto the Bridge first, using
[newsletter-source-onboarding.md](newsletter-source-onboarding.md).

Do **not** drop the old tables. That's #85, and only after the dump has been
restored somewhere for real at least once.

---

## Afterwards

Comment the cutover log summary on #84, update AC 6 to match ADR-0010, and
close the issue. After the observation window, start #85.
