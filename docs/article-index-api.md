# Article Index enrichment API

n8n calls `POST /api/private/articles/enrich` after it receives Miniflux's
`new_entries` webhook and has fetched the entry details. It sends the configured
`ENRICHMENT_SECRET` in `X-Enrichment-Secret`.

```json
{
  "entry_id": 42,
  "canonical_url": "https://example.com/articles/a-real-article",
  "title": "A real Article",
  "author": "Optional author",
  "published_at": "2026-09-14T12:00:00Z"
}
```

For an Email-only Article, n8n uses its unguessable bridge permalink as
`canonical_url` and adds `bridge_content`. Agregado does not fetch that URL;
the bridge-provided content is used only while producing derived fields and is
never stored in the Article Index.

The canonical URL is the idempotency key. A first request returns `201`; any
repeat returns `200` with the existing processing status and makes no fetch or
model call. The Index stores its identifying fields, summary, tags, score, and status only.

Agregado reads the hand-seeded PREFERENCES.md from the required `PREFERENCES_PATH`.
The profile is personal editable data outside this repository; scoring receives it
verbatim. In production, n8n is the sole writer for the read-only Agregado cache at
`/srv/agregado/preferences/PREFERENCES.md`: it syncs the accepted Google Drive document
every five minutes and atomically replaces the cache only after validation.

## Preference-regeneration evidence

n8n retrieves `GET /api/private/preferences/signals` with the same
`X-Enrichment-Secret`. The response contains one object per Article with usable
Reader evidence, including its title, canonical URL, compact derived fields,
first Open (if any), and current explicit vote (if any):

```json
{
  "signals": [{
    "article_id": "d9b6cfce-661c-4c71-8ff1-55b387ca3e6a",
    "canonical_url": "https://example.com/articles/a-real-article",
    "title": "A real Article",
    "tags": ["technology"],
    "opened_at": "2026-09-15T07:30:00Z",
    "vote": "up"
  }]
}
```

An Open is one weak positive signal: repeated opens preserve the first timestamp
and do not create rows or weight. `up` and `down` are the one current, strong
explicit signal; a later opposite vote replaces it. Articles without either
signal are omitted. The endpoint reads only the Article Index and its feedback
table, so silence, Bookmarks, and Karakeep data cannot influence a proposal.

n8n regenerates the whole derived profile nightly, directly replacing the
accepted Google Drive document only after validation. Validation requires the
Markdown sections `Topics wanted`, `Topics to skip`, and `Sources trusted`, and
limits the complete file to 8 KiB. A failed generation or validation leaves the
accepted Drive revision and the local cache unchanged; Drive revision history is
the rollback mechanism. A hand-seeded valid file is sufficient before evidence
exists.

## Live verification

After applying migrations `000017` and `000020`, send one real Miniflux Article through n8n
and verify a single `article_index` row has `processing_status = 'complete'`
with `summary`, `tags`, and `score` populated. Confirm no body/content column
exists in `article_index`, then replay the same request and confirm the row
count and AI request log are unchanged.

Open the resulting Digest `GET /r/{article-index-id}` link twice. Confirm that
`article_index.opened_at` is set by the first request and unchanged by the
second, then retrieve `/api/private/preferences/signals` and confirm the
Article appears once with that timestamp and its current vote. Reverse
`000020`, confirm the column and index are removed, then reapply it and repeat
the check. This is intentionally a live check per ADR-0002; Docker is not
configured in this workspace.
