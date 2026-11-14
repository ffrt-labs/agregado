# Runbook — production cutover to the new Reader

Issue #84 (parent #48). This switches production from the old single-Postgres
Agregado to the Reader: Miniflux owns Articles and subscriptions, Karakeep owns
Bookmarks, n8n orchestrates, and Agregado keeps only the Article Index, the
Digest artifact, Opens, feedback and the preference profile.

`scripts/cutover-wizard.sh` walks this runbook stage by stage, runs the
commands, and logs everything to `backups/cutover-<stamp>.log`. Run it on the
homelab host, from the repo checkout the deploy uses. This document explains
the order and how to back out. Stage numbers match the wizard.

Allow about two hours, outside 08:00 Berlin (the Digest's hour).

---

## Order, and why

| # | Stage | Reversible? |
| --- | --- | --- |
| 1 | Go / no-go: blockers closed, both test suites green | — |
| 2 | Final Karakeep backup (sidecar, `BACKUP_ONCE=1`) | yes |
| 3 | OPML export of the old Sources | yes |
| 4–5 | Migration prerequisites, dry run | yes |
| 6 | Apply the ownership migration (twice: the second run proves idempotency) | **no**: writes Karakeep and the Article Index |
| 7 | Final cold dump of the old Postgres | yes |
| 8 | Deploy with `RSS_POLLER_ENABLED=false` | yes |
| 9 | Import the n8n workflows, make sure a preference profile is stored, activate the webhook workflows | yes |
| 10 | Import the OPML into Miniflux, point its webhook at n8n | yes |
| 11 | Newsletter Sources onto the Bridge | yes |
| 12–14 | Verify ingestion, Open/feedback, Digest retry, preferences; then turn the schedules on | — |
| 15 | Stop the old stack's remaining paths, drill the rollback | — |

The order has three constraints:

- **The poller goes off before Miniflux subscribes (8 before 10).** The legacy
  in-process RSS poller still runs in the deployed binary until #85 removes it.
  If both run, every Article is ingested twice and enriched twice, so the model
  cost doubles too. `RSS_POLLER_ENABLED` (default `true`) is the switch; the
  deploy logs `legacy RSS poller disabled` when it is off.
- **n8n is ready before Miniflux sends anything (9 before 10).** The first
  fetch of every imported feed fires the webhook for its current entries.
  If the workflow isn't active yet, those entries are lost. Expect a burst of
  enrichment model cost at that point.
- **A preference profile is stored before enrichment runs (stage 9).**
  Enrichment refuses to score without one (ADR-0010). The wizard checks
  `GET /api/private/preferences` and, on 404, has you run `preferences-sync`
  once by hand.

The migration itself (stages 4–7) is documented step by step in
[legacy-postgres-cutover.md](legacy-postgres-cutover.md).

---

## Acceptance criteria → evidence

| AC | Where it's proven |
| --- | --- |
| Final Karakeep backup and cold dump verified | Stage 2 sidecar output; stage 7 manifest says `restore: VERIFIED` and `sha256sum -c` passes |
| Migration completes with reconciled counts | Stage 6: `FAIL` 0; second apply shows `DEST` 0 everywhere; `SOURCE = DEST + SKIP + DUP` |
| Live RSS Article and a URL-less newsletter reach Miniflux, enrichment and the Digest | Stage 12 |
| `/r/{id}` records one Open and redirects; `/f/{id}/{up\|down}` replaces the vote | Stage 13 (two Opens → one row; up then down → one row, `first_voted_at` unchanged; any other vote → 404) |
| Digest retry sends the same artifact without regenerating it | Stage 14: two `POST /api/private/digests/{date}` bodies hash the same (201 then 200), no second `digest_select` trace in Langfuse |
| Nightly preference proposal | Stage 14, see below |
| Old stack stopped but recoverable; rollback exercised | Stage 15 |

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
- `/r/` or `/f/` links from the Digest don't work from the phone;
- the migration turns out to have lost or duplicated Bookmarks in Karakeep.

### Steps

1. In n8n, deactivate `article-enrichment`, `newsletter-ingest`,
   `daily-digest` and `preferences-sync`. In Miniflux, disable the webhook
   integration. Nothing new reaches Agregado now.
2. Turn the legacy poller back on and redeploy:

   ```sh
   gh variable set RSS_POLLER_ENABLED --body true
   gh run rerun <latest "Deploy to local server" run id>
   ```

   The old tables are untouched by the migration, so the old app resumes
   where it stopped. Anything published during the cutover is picked up on
   the next poll, because each Source keeps its `last_fetched_at`.
3. If the old tables are damaged, restore from the off-box dump:

   ```sh
   createdb agregado_restored
   pg_restore -d agregado_restored --no-owner --no-acl agregado-legacy-<stamp>.dump
   ```

   Compare the row counts with the manifest, then point the app at it.
4. Karakeep: Bookmarks the migration created are harmless to keep. If you need
   to remove them, restore the stage 2 backup (homelab-apps
   `docs/karakeep-backup.md`).
5. Preferences: restore an earlier revision of the `PREFERENCES` Doc, then run
   `preferences-sync` once.

The Article Index and Digest tables are additive, so leave them in place.

---

## What stays running after cutover

Until #85, the reduced Agregado image still contains the old components.
With the poller off they are idle:

- **RabbitMQ and its consumers** receive nothing, because the poller was the
  only RSS publisher.
- **The legacy Digest** never had its own schedule. It only ever ran on
  `POST /api/digest/send` (the old HTMX button), so nothing sends it.
- **`/webhook/email`** (the old newsletter ingress) gets traffic only if a
  publisher still mails an old address. Stage 15 counts the hits over 24 h.
  Onboard any such Source onto the Bridge with
  [newsletter-source-onboarding.md](newsletter-source-onboarding.md).
- **The weekly OPML backup mail** keeps running. It's harmless, and #85
  removes it.

Do **not** drop the old tables. That's #85, and only after the dump has been
restored somewhere for real at least once.

---

## Afterwards

Comment the cutover log summary on #84 and close it. After the observation
window, start #85.
