# Runbook — production cutover to the new Reader

Issue #84 (parent #48). This switched production from the old single-Postgres
Agregado to the Reader:

- Miniflux owns Articles and subscriptions.
- Karakeep owns Bookmarks.
- n8n orchestrates.
- Agregado keeps only the Article Index, the Digest artifact, Opens, feedback
  and the preference profile, reachable only on the tailnet
  ([ADR-0008](../adr/0008-agregado-is-tailnet-only.md)).

The cutover was run on 2026-11-14. This document records what was done, the
evidence for each acceptance criterion, and how to back out. It was first
written for a step-by-step wizard. The wizard and a `RSS_POLLER_ENABLED`
switch were removed afterwards, because production turned out to be mostly
cut over already (see below), so neither is needed again.

---

## What production looked like on the day

The new path had been live since about 2026-10-01:

- The old pipeline's last ingest was 2026-09-14. All 23 RSS Sources were
  already `is_active = false`, so the legacy poller was idle.
- Miniflux held 58 feeds with its webhook enabled. n8n's `newsletter-ingest`,
  `article-enrichment` and `daily-digest` workflows were active. The Article
  Index grew by about 100 rows a day, and a Digest was stored every day at
  07:00 UTC.
- `preferences-sync` was **not** active. That was not deliberate.
- No newsletter had moved to the Bridge yet. The 20 old newsletter Sources
  stopped receiving mail in mid-September.
- `PUBLIC_BASE_URL` was already `https://agregado-app.felipefreitas.dev`,
  served by the homelab-edge Caddy (see the ADR-0008 amendment).

So the cutover consisted of the irreversible steps (backup, migration, cold
dump) plus the missing pieces (preferences, newsletters).

## Order, and why

| # | Step | Reversible? |
| --- | --- | --- |
| 1 | Final Karakeep backup | yes |
| 2 | OPML export of the old Sources | yes |
| 3 | Ownership migration: dry run, then apply twice (the second run proves idempotency) | **no**: writes Karakeep and the Article Index |
| 4 | Final cold dump of the old Postgres, copied off-box | yes |
| 5 | Digest retry check | yes |
| 6 | Activate `preferences-sync` | yes |
| 7 | Newsletter Sources onto the Bridge | yes |
| 8 | Verify an email-only Article reaches the Digest | — |

The order has three constraints:

- **The poller stays off while Miniflux subscribes.** The legacy in-process
  RSS poller still runs in the deployed binary until #85 removes it, and it
  reads every Source with `is_active = true`. If it ran alongside Miniflux,
  every Article would be ingested and enriched twice. Deactivating the RSS
  Sources is all it takes, and they were already inactive.
- **n8n is ready before Miniflux sends anything.** The first fetch of a new
  feed fires the webhook for its current entries. If the workflow isn't active,
  those entries are lost.
- **A preference profile is stored before enrichment runs.** Enrichment refuses
  to score without one
  ([ADR-0010](../adr/0010-preference-profile-lives-in-agregados-database.md)).

The migration is documented step by step in
[legacy-postgres-cutover.md](legacy-postgres-cutover.md). The migration command
loads the full Agregado config, so it needs the database credentials and
`ENRICHMENT_SECRET`. RabbitMQ can get placeholders, because the migration
never touches it.

---

## Acceptance criteria → evidence

| AC | Evidence (2026-11-14) |
| --- | --- |
| Final Karakeep backup and cold dump verified | The Karakeep sidecar backup holds 704 Bookmarks and was uploaded to `r2:karakeep-backups`. The dump `agregado-legacy-20261114T102352Z.dump` has `restore: VERIFIED` in its manifest. The dump, the manifest and the OPML are off-box in `r2:karakeep-backups/agregado-legacy/` (`rclone check`: 0 differences). |
| Migration completes with reconciled counts | The first apply wrote 239 scores, 62 tags, 201 summaries, 23 Opens and 1 Bookmark, with `FAIL` 0. The second apply reported `DUP` everywhere except `saved_urls`, which reported the Bookmark as `DEST` again. Karakeep still holds exactly one copy, so that is a false report from the existence check, not a duplicate. |
| A live RSS Article and an email-only Article reach Miniflux, enrichment and the Digest | RSS: shown by the daily Article Index growth and the Digests since October. Email: pending the first issue on the Bridge. |
| `/r/{id}` records one Open and redirects; `/f/{id}/{up\|down}` replaces the vote | Production holds 40 Opens and 10 votes recorded through the tailnet URL since October. The repeat-Open and vote-replacement behavior is covered by the test suites and was not re-run by hand on the day. |
| Digest retry sends the same artifact without regenerating it | Two `POST /api/private/digests/{date}` calls returned 200 with identical bodies (sha256 `05cb066b41fb2abf…`). |
| Nightly preference proposal | `preferences-sync` was run once by hand and activated, and wrote preference profile versions 3 and 4. See AC 6 below. |
| Old stack stopped but recoverable; rollback exercised or verified | The old stack is idle (see below). The rollback is one `UPDATE`, plus a verified dump. |

**AC 6 vs ADR-0010.** The issue asks for a *reviewable* proposal. ADR-0010,
accepted after the issue was written, removed the review gate. The nightly
`preferences-sync` regenerates the profile, validates it, and writes it back
to the Google Doc. Review happens afterwards: the n8n alert shows what
changed, and the Doc's revision history is the rollback.

---

## Newsletters on the Bridge

Ten newsletters moved: `tldr`, `hackernewsletter`, `theberliner`,
`smartliving`, `germanpedia`, `syntax`, `dw-berlin-briefing`, `timferriss`,
`hubermanlab` and `jamesclear`. Each one has a D1 Source row, a confirmed
subscription to `<alias>@<receiving-domain>`, and a Miniflux subscription to
`https://email-worker.f11s.workers.dev/feed/<alias>.atom`. The SQL is in
`scripts/newsletter-bridge/`. Mail that reaches a Source before it is
`active` is ignored, not held.

Adding one later follows
[newsletter-source-onboarding.md](newsletter-source-onboarding.md). #225
tracks a small app to automate it.

---

## Rollback

### Triggers

Roll back if, within the observation window (before #85 starts):

- no Digest arrives at 08:00, or it arrives twice;
- enrichment fails for most new Articles (n8n `article-enrichment` executions
  are red, or `article_index` stops growing while Miniflux has new entries);
- Open or vote links from the Digest don't work from a tailnet device;
- the migration turns out to have lost or duplicated Bookmarks in Karakeep.

### Steps

There's no separate old image to boot. Rolling back means turning the old
poller back on in the running binary.

1. In n8n, deactivate `article-enrichment`, `newsletter-ingest`,
   `daily-digest` and `preferences-sync`. In Miniflux, disable the webhook
   integration. Nothing new reaches Agregado now.
2. Reactivate the RSS Sources. No redeploy is needed: the poller picks them up
   within `RSS_POLL_INTERVAL` (15 m).

   ```sh
   docker exec -i agregado-db psql -U "$DATABASE_USER" -d "$DATABASE_DB" \
     -c "UPDATE sources SET is_active = true WHERE type = 'rss'"
   ```

   The migration only read the old tables, so the old app resumes where it
   stopped.
3. If the old tables are damaged, restore the off-box dump. Postgres runs in
   the `agregado-db` container, so go through `docker exec`:

   ```sh
   docker exec -i agregado-db createdb -U "$DATABASE_USER" agregado_restored
   docker exec -i agregado-db pg_restore -U "$DATABASE_USER" -d agregado_restored \
     --no-owner --no-acl < agregado-legacy-<stamp>.dump
   ```

   Compare the row counts with the manifest, then point the app at it.
4. Karakeep: Bookmarks the migration created are harmless to keep. To remove
   them, restore the pre-migration backup (homelab-apps
   `docs/karakeep-backup.md`).
5. Preferences: restore an earlier revision of the `PREFERENCES` Doc, then run
   `preferences-sync` once.

The Article Index and Digest tables are additive, so leave them in place.

The rollback was verified rather than exercised: reactivating the Sources would
start the old poller next to Miniflux. The dump's restore was exercised by the
dump script, into a scratch database with matching row counts.

---

## What stays running after cutover

Until #85, the deployed image still contains the old components. They are idle
or tailnet-only:

- **RabbitMQ and its consumers** receive nothing. The poller was the only RSS
  publisher, and the Cloudflare tunnel no longer routes `/webhook/email` to
  Agregado (every Agregado path on it returns 404).
- **The legacy Digest** never had its own schedule. It only ran on
  `POST /api/digest/send` (the old HTMX button), so nothing sends it.
- **The old HTML pages** are still served, but only behind Caddy.
- **The weekly OPML backup mail** keeps running. It's harmless, and #85
  removes it.

Do **not** drop the old tables. That's #85, and only after the dump has been
restored somewhere for real at least once.
