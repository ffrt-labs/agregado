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

## Live verification

After applying migration `000017`, send one real Miniflux Article through n8n
and verify a single `article_index` row has `processing_status = 'complete'`
with `summary`, `tags`, and `score` populated. Confirm no body/content column
exists in `article_index`, then replay the same request and confirm the row
count and AI request log are unchanged. This is intentionally a live check per
ADR-0002; Docker is not configured in this workspace.
