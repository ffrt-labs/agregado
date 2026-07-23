# Newsletter fixtures (issue #2)

The newsletter corpus this project never had. Each file is a webhook `Payload`
(the exact JSON the Cloudflare Email Routing Worker POSTs to `/webhook/email`),
crafted to exercise one branch of the canonical-URL extraction chain:

| Fixture | Proves | Expected `canonical_url` |
| --- | --- | --- |
| `01-archived-at-header.json` | `Archived-At` header wins over an HTML link | `https://dispatch.example.com/p/weekly-dispatch-42` |
| `02-view-in-browser.json` | no header → scrape the "view in browser" anchor, reject pixel + share link | `https://overflow.example.com/p/issue-108` |
| `03-no-web-home.json` | neither present → reader-page fallback | `NULL` (redirects to `/articles/{id}`) |

## Live verification (the acceptance criterion — still outstanding)

The parser and redirect logic are unit-tested (`parser_test.go`,
`articles_test.go`). What remains is proving the **whole path** — real Worker →
tunnel → webhook → parser → queue → worker → DB → `/r/{id}` — against real mail.
This needs the live infra from issue #1 (base URL set, tunnel publishing
`/r/{id}` and `/articles/{id}`) and cannot be run from unit tests.

Two ways to drive it:

1. **Through the real Worker (full acceptance):** send three actual emails
   matching these fixtures to the Cloudflare Email Routing address so they flow
   through the deployed Worker.
2. **Straight at the webhook (path from the webhook onward):**

   ```sh
   for f in 01-archived-at-header 02-view-in-browser 03-no-web-home; do
     curl -sS -X POST "$BASE_URL/webhook/email" \
       -H "X-Webhook-Secret: $WEBHOOK_SECRET" \
       -H "Content-Type: application/json" \
       --data-binary @"$f.json"
   done
   ```

Then confirm, per variant:
- `articles.canonical_url` matches the table above;
- a `newsletter_raw_html` row was written (`SELECT article_id FROM newsletter_raw_html`);
- `GET /r/{id}` redirects to the canonical URL (variants 1-2) or to
  `/articles/{id}` (variant 3);
- opening variant 3's reader page off-network shows **no** nav counts and **no**
  `/admin` links in source.
