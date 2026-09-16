# Runbook — the ownership-based migration and the final legacy dump

Issue #83. This is the cutover from the old single-Postgres Agregado to the
shape [ADR-0005](../adr/0005-enrichment-is-a-library-scoring-is-not-in-it.md)
and [ADR-0006](../adr/0006-miniflux-is-the-reader-backend-not-a-service.md)
settled on: Miniflux owns Articles, Karakeep owns Bookmarks, and Agregado keeps
only the content-free Article Index.

Run it in the order below. Steps 1 and 2 are reversible; step 3 is not.

---

## Who ends up owning what

| Old data | New owner | Why |
| --- | --- | --- |
| Saved URLs (`articles.is_saved`) | **Karakeep** | A Save is a Bookmark, and the Bookmarker owns Bookmarks. |
| Scores, tags, summaries | **Article Index** | Enrichment belongs to the tool that owns the store. |
| Opens (`read_at` / `is_read`) | **Article Index** | A weak preference signal, deduplicated to the first Open per URL. |
| Current 👍/👎 votes | **Article Index** | `article_feedback` is a log; only the most recent vote per Article moves. |
| Source configuration (`sources`) | **nobody — left behind** | Subscriptions move to Miniflux, by hand, from the OPML backup (ADR-0006). |
| Article bodies (`content`, `distilled_content`, `newsletter_raw_html`) | **nobody — left behind** | The Article Index holds no content. Miniflux is the sole store. |

Two rules the migration enforces, and that the tests pin:

- **A Save never becomes a preference signal.** `is_saved`/`saved_at` produce a
  Karakeep Bookmark and nothing else. They never become an Open or a vote, so
  Bookmark data cannot reach `PREFERENCES.md` through
  `preferenceexport.Signals`, which reads exactly the rows carrying one. A
  saved Article that was *also* genuinely read keeps that Open — that is read
  data, not Bookmark data.
- **History never overwrites live data.** A canonical URL the live enrichment
  pipeline has already indexed keeps its own title, summary, tags and Score.
  The migration only fills gaps: a missing `opened_at`, a missing vote.

Articles with no web home of their own — newsletters that never had a page, and
the old `newsletter:<uuid>` sentinel — cannot move. Both destinations are keyed
by URL. They are reported as `skipped`, and they survive in the dump from step 3.

---

## Step 0 — Prerequisites

```bash
migrate -database "$POSTGRESQL_URL" -path migrations up   # through 000021
```

`000021` drops `NOT NULL` from `article_index.miniflux_entry_id`: a migrated
historical Article never existed in Miniflux, so there is no entry id to cache
for Decoration.

Set in `.env`:

```
KARAKEEP_ADDRESS=https://karakeep.example.com   # origin, no /api/v1
KARAKEEP_API_KEY=...                            # Settings → API Keys
```

Confirm Karakeep's own backup works first (#82) — you are about to write a few
hundred Bookmarks into it.

---

## Step 1 — Dry run

```bash
make migrate-ownership
```

Writes nothing. It still reads both destinations, so the `DUP` column is
measured rather than predicted.

```
DRY RUN — nothing was written to Karakeep or the Article Index.
The destination column is what an apply would create.

CLASS           DESTINATION     SOURCE  TRANSF    DEST    SKIP     DUP    FAIL
saved_urls      karakeep            84      84      84       0       0       0
scores          article_index     2311    2190       ...
...
source_config   (excluded)          31       0       0      31       0       0
article_bodies  (excluded)        2402       0       0    2402       0       0
```

Read it for:

- **`source_config` and `article_bodies` show `DEST` 0.** The exclusions are
  counted rather than merely absent — that is the evidence they were excluded
  on purpose.
- **`SKIP` on the enrichment classes** is mostly newsletters with no web home.
  A number far larger than your newsletter volume means URL resolution is
  wrong; stop and investigate.
- **`DUP`** counts two things: legacy rows merged into one canonical URL, and
  items the destination already had. Both mean "did not move, and should not".
- **`FAIL` must be 0** before you apply. The failure list below the table gives
  the reason verbatim.
- **The sample block** compares representative records against the destination.
  In a dry run they read `DIFF ... absent from the Article Index`, which is
  correct — nothing has been written yet.

Rerun the dry run as often as you like.

---

## Step 2 — Apply

```bash
make migrate-ownership-apply
```

Then read the sample block: every line should be `OK`, meaning the record in
the destination matches the row it came from, field by field. Any `DIFF` line
names the field and both values.

**Rerunning is safe.** Saved URLs are checked against Karakeep's
`/bookmarks/check-url` before any POST; Article Index rows are inserted
`ON CONFLICT (canonical_url)`; votes are inserted `ON CONFLICT DO NOTHING`. A
second apply reports every class with `DEST` 0 and the same totals under `DUP`.
That is the idempotency check — run it once more and confirm.

If the first run had failures, fix the cause and run it again. A partial run
leaves nothing to clean up.

---

## Step 3 — The final cold dump

Only after step 2 is verified.

```bash
scripts/legacy-postgres-dump.sh ./backups
```

The script:

1. stops the `agregado` container, so the dump is cold;
2. records row counts for every legacy table;
3. `pg_dump -Fc` (compressed custom format, restorable table-by-table);
4. **restores it into a scratch database on the same server and compares every
   row count against step 2** — this is the restore check, and it fails loudly;
5. drops the scratch database, restarts the app, and writes a manifest with the
   size, the SHA-256, the row counts, and `restore: VERIFIED`.

Then, by hand:

```bash
sha256sum -c <<< "$(grep sha256 backups/*.manifest.txt | awk '{print $2}')  backups/agregado-legacy-*.dump"
# copy both files off-box — a backup on the machine it backs up is not a backup
```

**Restoring it later**, in full or in part:

```bash
createdb agregado_restored
pg_restore -d agregado_restored --no-owner --no-acl agregado-legacy-<stamp>.dump
# or one table:
pg_restore -d agregado_restored --no-owner --no-acl -t sources agregado-legacy-<stamp>.dump
```

**Retention:** keep this dump indefinitely. It is not a rolling backup — it is
the one artifact the whole pivot is reversible against, and it is the only
remaining copy of everything the migration deliberately left behind. Storing it
alongside the nightly Karakeep dump (#82) is fine; expiring it with them is not.

---

## What is deliberately not automated

- **Re-subscribing your Sources in Miniflux.** Use the OPML the weekly backup
  already emails you (`internal/backup`). Miniflux owns subscriptions now; a
  script that wrote them would make Agregado the orchestrator again, which is
  exactly what #48 moved to n8n.
- **Deleting the old tables.** The migration only reads them. Dropping
  `articles`, `sources` and friends is a separate, later decision, and it
  should not happen until the dump has been restored somewhere at least once
  for real.
