# Newsletter fixtures (issue #2)

The newsletter corpus this project never had. Each file is a `Payload` in the
shape the old Cloudflare Email Routing Worker posted to Agregado's
`/webhook/email` route. That route was removed (#85), so today these fixtures
only feed `parser_test.go`, which tests the Go canonical-URL extraction chain.
The live chain is the Bridge's cheerio port in n8n (ADR-0007), tested against
identical copies in `n8n/test/fixtures/`.

Each fixture exercises one branch of the chain:

| Fixture | Proves | Expected `canonical_url` |
| --- | --- | --- |
| `01-archived-at-header.json` | `Archived-At` header wins over an HTML link | `https://dispatch.example.com/p/weekly-dispatch-42` |
| `02-view-in-browser.json` | no header → scrape the "view in browser" anchor, reject pixel + share link | `https://overflow.example.com/p/issue-108` |
| `03-no-web-home.json` | neither present → no canonical URL | `NULL` |

## History

The original plan was to live-verify the whole path (Worker → tunnel →
`/webhook/email` → parser → queue → storage worker → `newsletter_raw_html` →
`/r/{id}` → reader page) by posting these files to the webhook. Every hop after
the parser has since been removed: the webhook and queue (#236), the storage
worker (#237), the reader page (#238) and the `newsletter_raw_html` table
(#241). The equivalent live check now runs through the Bridge; see
`n8n/README.md` and `n8n/scripts/verify-ingest.sh`.
