# Can Miniflux be decorated from outside, without forking it?

Research for [ffrt-labs/agregado#49](https://github.com/ffrt-labs/agregado/issues/49).

**Date:** 2026-08-22
**Sources:** primary only — the official docs at `miniflux.app/docs`, and the source
tree at `github.com/miniflux/v2` read at commit
[`106cdd0`](https://github.com/miniflux/v2/commit/106cdd09e1557222303f3ed1376e1dadf0638621)
(2026-08-11, `2.3.x-dev`, i.e. post-2.3.3). Version attributions were derived with
`git log -S <symbol>` + `git tag --contains`, cross-checked against
`gh api repos/miniflux/v2/releases` and the docs' own "available since" notes.
Issue/PR statements are quoted from the GitHub API, not from third-party write-ups.

**Short answer: yes — decoration is possible today without forking, and the maintainer
explicitly built the endpoint that makes it possible in response to a request that
named "summarize via llm" as the use case. But the decoration is confined to entry
*title* and *content* (plain text in the title, a restricted HTML subset in the body),
it is invisible in the entry *list* view without a Custom JS shim, and no supported
mechanism will make Miniflux sort by a score.**

---

## 1. REST API surface: can entry content or title be mutated?

### Yes — `PUT /v1/entries/{entryID}`, since v2.0.49

Route registration:
[`internal/api/api.go`](https://github.com/miniflux/v2/blob/main/internal/api/api.go) —
`mux.HandleFunc("PUT /v1/entries/{entryID}", handler.updateEntryHandler)`.

Request body
([`internal/model/entry.go`](https://github.com/miniflux/v2/blob/main/internal/model/entry.go)):

```go
type EntryUpdateRequest struct {
    Title   *string `json:"title"`
    Content *string `json:"content"`
}
```

Handler
([`internal/api/entry_handlers.go`](https://github.com/miniflux/v2/blob/main/internal/api/entry_handlers.go),
`updateEntryHandler`) validates, **sanitises the supplied content** with
`sanitizer.SanitizeHTML`, recomputes `reading_time` if the user has
`ShowReadingTime`, then calls `storage.UpdateEntryTitleAndContent`, which writes
`title`, `content`, `reading_time` and the full-text `document_vectors` column
([`internal/storage/entry.go`](https://github.com/miniflux/v2/blob/main/internal/storage/entry.go)).
Response is `201 Created` with the updated entry.

Official docs confirm the surface and the version:
<https://miniflux.app/docs/api.html> — *"Both fields `title` and `content` are
optional… Available since Miniflux v2.0.49."*

**Version:** introduced by commit *"Add API endpoint to update entry title and content"*
(2023-10-06), first released in **2.0.49** (2023-10-15). Go client method
`(*Client).UpdateEntry(entryID, *EntryModificationRequest)` in
[`client/client.go`](https://github.com/miniflux/v2/blob/main/client/client.go).

**Provenance worth knowing:** this endpoint exists *because someone asked for exactly
our use case.* [miniflux/v2#2034](https://github.com/miniflux/v2/issues/2034) ("API to
allow updating content of an entry", closed 2023-10-07) requested it to *"1) machine
translate, 2) summarize via llm, 3) add other information"* and to *"add visual cues or
numerical weights for the articles of interest."* The maintainer implemented it the
next day. This is the single strongest signal in the whole investigation.

### Other relevant endpoints

| Endpoint | What it does | Since |
|---|---|---|
| `PUT /v1/entries` | bulk set `status` and/or `starred` (`EntriesStatusUpdateRequest`) | long-standing; `starred` field added later |
| `PUT /v1/entries/{id}/bookmark`, `/star` | toggle starred | `/star` alias is recent |
| `POST /v1/entries/{id}/save` | trigger the "save to third party" integration path | 2.0.x |
| `GET /v1/entries/{id}/fetch-content` | server-side scrape of the entry URL | 2.0.x |
| `POST /v1/feeds/{feedID}/entries/import` | **inject a new entry into an existing feed**, with `url`, `title`, `content`, `author`, `comments_url`, `published_at`, `status`, `starred`, **`tags`**, `external_id` | **2.2.16** (2026-01-07) |
| `GET /v1/entries/ids` | cheap ID-only listing for polling, limit up to 10 000 (`model.MaxEntryIDsLimit`) | **2.3.2** (2026-06-27) |
| `POST/GET/DELETE /v1/api-keys` | per-application API keys | 2.0.21 (keys), CRUD endpoints later |

Auth for all of the above is `X-Auth-Token` (API key) or HTTP Basic
([`internal/api/middleware.go`](https://github.com/miniflux/v2/blob/main/internal/api/middleware.go)).
The web session cookie does **not** authenticate `/v1/*`.

### What you cannot mutate

`EntryUpdateRequest` has **only** `title` and `content`. There is no supported way to
set `tags`, `author`, `published_at`, or any custom field on an **already-fetched**
entry. `tags` is written only by `storage.createEntry` / `storage.updateEntry` (feed
refresh) and by `InsertEntryForFeed` (the import endpoint). Grep of
`internal/storage/entry.go` shows no other `tags=` write.

### Fragility: does a refresh clobber my edit?

[`internal/reader/handler/handler.go`](https://github.com/miniflux/v2/blob/main/internal/reader/handler/handler.go):

```go
updateExistingEntries := forceRefresh || (!originalFeed.Crawler && !originalFeed.IgnoreEntryUpdates)
```

`storage.updateEntry` matches on `(user_id, feed_id, hash)` and overwrites
`title`, `url`, `comments_url`, `content`, `author`, `reading_time`, `tags`, `language`.
The hash is derived from the feed item, so your API edit does **not** change the hash and
does **not** protect the entry.

So enriched content survives a refresh only if **either**:

- the feed has `crawler = true` ("Fetch original content"), **or**
- the feed has `ignore_entry_updates = true` — feed field `json:"ignore_entry_updates"`,
  added by *"feat(feed): add ignore_entry_updates option to feeds"*, first released in
  **2.2.18** (2026-03-15).

…**and** the user never presses the UI's force-refresh button. Scheduled refreshes
(`internal/worker/worker.go`), the CLI (`internal/cli/refresh_feeds.go`) and the API
(`internal/api/feed_handlers.go`) all pass `forceRefresh = false`; only
`internal/ui/feed_refresh.go` can pass `true`. This is the main operational hazard of
the write-back strategy, and it is fully mitigable via the API by setting
`ignore_entry_updates` on every feed.

### What HTML survives sanitisation

`internal/reader/sanitizer/sanitizer.go`, `allowedHTMLTagsAndAttributes`. Notable:

- **Allowed:** `a` (`href`, `title`, `id`), `aside`, `blockquote`, `p`, `h1`–`h6` (`id`),
  `ul`/`ol`/`li`/`dl`/`dt`/`dd` (`id`), `table`/`tr`/`td`/`th`, `hr`, `strong`, `em`,
  `code`, `pre`, `small`, `sup` (`id`), `time` (`datetime`), `img`, `iframe`, `video`,
  `audio`, plus MathML.
- **Not allowed:** `div`, `span`, `style`, `script`, and — critically — the attributes
  `class`, `style` and `data-*` on *any* tag.

The only styling hook available to injected content is the `id` attribute, and only on
`a`, `h1`–`h6`, `ul`, `ol`, `li`, `dl`, `dt`, `dd`, `sup`. So a summary block must look
roughly like:

```html
<aside><h3 id="agregado-score">Relevance 4/5</h3><p>…summary…</p></aside>
```

and can then be styled from user Custom CSS via `#agregado-score { … }`.

Entry **titles** are rendered with Go `html/template`'s default escaping
(`{{ .Title }}` in `internal/template/templates/views/unread_entries.html`), so a title
can carry a score only as **plain text** — e.g. `[4] Original headline`. No markup.

Entry **content** is rendered with `{{ safeHTML (proxyFilter .entry.Content) }}`
(`internal/template/templates/views/entry.html`), so the sanitised HTML above renders
as-is on the entry detail page.

---

## 2. Rewrite rules and scraper rules

**Scraper rules** (`internal/reader/scraper/scraper.go`): CSS selectors evaluated with
goquery against the fetched page, or Readability if no rule matches. They only run when
the feed has `crawler = true`. Note `sameSite := urllib.Domain(pageURL) == urllib.Domain(responseHandler.EffectiveURL())`
— custom rules are applied only when the effective URL is on the same domain as the
requested one. Docs: <https://miniflux.app/docs/rules.html>.

**Content rewrite rules** (`internal/reader/rewrite/content_rewrite.go`): a **closed
`switch` over a fixed vocabulary** of ~25 built-in function names —
`add_image_title`, `add_dynamic_image`, `add_youtube_video`, `nl2br`, `replace`,
`replace_title`, `remove`, `remove_tables`, `remove_clickbait`, `base64_decode`,
`fix_medium_images`, `fix_ghost_cards`, etc. All operate on the in-memory HTML with
goquery/regex. **None of them performs network I/O to an arbitrary user-specified
endpoint.** There is no `call(...)`, no shell-out, no scripting hook.

**URL rewrite rules** (`internal/reader/rewrite/url_rewrite.go`): a single regex form,
`rewrite("<search>"|"<replace>")`, applied to `entry.URL`.

### The one loophole: URL rewrite + crawler = a fetch of a URL you control

In `internal/reader/processor/processor.go` the order is:

```go
entry.URL = rewrite.RewriteEntryURL(feed, entry)      // line ~94
…
if feed.Crawler && (entryIsNew || forceRefresh) {
    …scraper.ScrapeWebsite(requestBuilder, entry.URL, feed.ScraperRules)   // line ~111
    entry.Content = minifyContent(extractedContent)
}
rewrite.ApplyContentRewriteRules(entry, feed.RewriteRules)                  // line ~144
```

Because the URL rewrite happens **before** the crawl, a rule like
`rewrite("^https://example\.com/(.*)"|"https://enricher.internal/enrich?u=https://example.com/$1")`
plus `crawler = true` will make Miniflux fetch *your* service and store whatever HTML it
returns as the entry content — a genuine, unforked, in-band enrichment path.

Costs: (a) `entry.URL` is **permanently** rewritten, so the "open original" link in the
UI now points at your proxy; (b) the `sameSite` guard means custom scraper rules stop
applying, so you must return content that Readability handles or that needs no rules;
(c) it is synchronous and inside the feed-refresh worker, so LLM latency directly stalls
refreshes; (d) per-feed configuration, one rule per feed.

`feed.proxy_url` / `feed.fetch_via_proxy` (`internal/reader/fetcher/request_builder.go`,
`WithCustomFeedProxyURL`) are transport-level HTTP proxies, not content hooks —
rewriting HTTPS responses through them would require MITM. Not a practical seam.

---

## 3. The integrations mechanism: general seam or hardcoded list?

**Hardcoded list.** `internal/integration/integration.go` is a flat `if` chain over
boolean fields of `model.Integration`, with a compile-time import of every provider
package. There are ~30 sub-packages: `apprise`, `archiveorg`, `betula`, `cubox`,
`discord`, `espial`, `instapaper`, `karakeep`, `linkace`, `linkding`, `linktaco`,
`linkwarden`, `matrixbot`, `notion`, `ntfy`, `nunuxkeeper`, `omnivore`, `pinboard`,
`pushover`, `raindrop`, `readeck`, `readwise`, `rssbridge`, `shaarli`, `shiori`,
`slack`, `telegrambot`, `wallabag`, `webhook`.

There are exactly two dispatch functions:

- `SendEntry(entry, userIntegrations)` — fired when the user clicks **Save**.
- `PushEntries(feed, entries, userIntegrations)` — fired during feed refresh with the
  newly created entries.

Adding a provider requires a Go file, a DB column, a settings form field and
translations. **There is no registration API, no dynamic loading, no `plugin` package
usage.** The only *general* member of the list is `webhook` — see next section.

---

## 4. Webhook / new-entry notification

`internal/integration/webhook/webhook.go`. Docs: <https://miniflux.app/docs/webhooks.html>
(*"since Miniflux 2.0.48"*). Introduced by *"Add generic webhook integration"*
(2023-09-09), released in **2.0.48** (2023-09-15); the `save_entry` event landed in the
same release.

**Two event types:**

- `new_entries` — fired during feed refresh when new entries are discovered.
- `save_entry` — fired when the user clicks Save.

**Trigger point** (`internal/reader/handler/handler.go`, ~line 347):

```go
go integration.PushEntries(originalFeed, newEntries, userIntegrations)
```

Key properties, all read from source:

- **Per-batch, per-feed, not per-entry.** `SendNewEntriesWebhookEvent(feed, entries)`
  posts one JSON body containing `{"event_type":"new_entries","feed":{…},"entries":[…]}`
  for all entries newly created in that one feed refresh. Returns early if
  `len(entries) == 0`.
- **Entries already carry their database `id`.** `createEntry` assigns the ID before the
  webhook fires, so the receiver can immediately `PUT /v1/entries/{id}` back. This makes
  the round trip work with no polling.
- **Fire-and-forget goroutine.** The refresh does not wait for or observe the result.
- **No retries.** Docs, verbatim: *"Miniflux does not retry webhook events on request
  failures."* Failures are only `slog.Warn`'d.
- **Payload** (`WebhookEntry`): `id`, `user_id`, `feed_id`, `status`, `hash`, `title`,
  `url`, `comments_url`, `published_at`, `created_at`, `changed_at`, **`content`**,
  `author`, `share_code`, `starred`, `reading_time`, `enclosures`, `tags`. The full
  content is included, so the enricher does not need to re-fetch to summarise.
- **Headers:** `X-Miniflux-Event-Type` and `X-Miniflux-Signature` =
  HMAC-SHA256(secret, body) (`crypto.GenerateSHA256Hmac`).
- **Per-feed URL override:** `PushEntries` uses `feed.WebhookURL` when non-empty, else
  the user-level `userIntegrations.WebhookURL`. Exposed as `webhook_url` in the feed API
  model (`internal/model/feed.go`) and in the edit-feed UI form
  (`internal/ui/form/feed.go`, `edit_feed.html`). Present since 2.0.48.

**Verdict on push vs poll:** push works, and it is the right primitive. Because there is
no retry and no delivery log, a **belt-and-braces poll** is still advisable —
`GET /v1/entries?changed_after=…` (timestamp filters since 2.0.49) or the cheap
`GET /v1/entries/ids` (**2.3.2**) to reconcile anything the webhook dropped.

---

## 5. Can entry metadata carry a score the UI displays or sorts on?

Short answer: **no, not usefully.** Detail:

### Tags

- Column `entries.tags text[]` added in the migration series first released in **2.0.47**
  (2023-08-21).
- **Writable only at ingest.** Feed refresh (`createEntry` / `updateEntry`) and the
  import endpoint (`POST /v1/feeds/{id}/entries/import`, **2.2.16**) set them. The
  `PUT /v1/entries/{id}` request body has no `tags` field. So tags **cannot** be attached
  to an entry that Miniflux fetched from a real feed.
- **Rendered only on the entry detail page** —
  `internal/template/templates/views/entry.html` renders up to 5 tags plus a
  `<details>` for the rest, linking to `/tags/{tag}/entries/all`. The tag-entries UI page
  arrived in **2.1.3** (2024-04-27).
- **Not rendered in list views.** `internal/template/templates/common/item_meta.html` —
  the shared list-item partial — renders feed title, timestamp, optional reading time,
  and the read/star/share/save/external-link/comments icons. **No tags, no excerpt,
  no content.**
- Not searchable as a first-class filter: `entries?search=` maps to
  `document_vectors @@ websearch_to_tsquery(...)`, and `document_vectors` is built from
  `setweight(to_tsvector(title),'A') || setweight(to_tsvector(content),'B')` only
  (`internal/storage/entry.go`). Tags are not indexed. There is an open request for
  `tags:` search on [#4289](https://github.com/miniflux/v2/pull/4289).

### Starred

Boolean only (`entries.starred`, since a 2.0.x migration). One bit. Google Reader
compatibility layer (`internal/googlereader/handler.go`, `checkAndSimplifyTags`) accepts
only `read`, `kept-unread` and `starred` streams — `broadcast`/`like` are explicitly
*"not implemented"*, and any other `user/-/label/...` is rejected with
`"unsupported tag type"`. So no user-labels via the GReader API either.

### Categories

A category belongs to a **feed**, not to an entry (`model.Feed.Category`). You would need
one feed per score bucket. Not viable for per-entry scores.

### Custom fields

None. `ALTER TABLE entries ADD COLUMN` history in `internal/database/migrations.go`:
`starred`, `comments_url`, `document_vectors`, `changed_at`, `share_code`,
`reading_time`, `created_at`, `tags`, `language`. There is no JSON/metadata column and
no extension table.

### Sorting — the hard blocker

Two different allow-lists:

- **API** (`internal/validator/entry.go`, `ValidateEntryOrder`):
  `id`, `status`, `changed_at`, `published_at`, `created_at`, `category_title`,
  `category_id`, `title`, `author`. Note **`title` is sortable via the API** — so a
  zero-padded score prefix in the title (`[04] …`) *is* API-sortable.
- **UI** (`internal/validator/user.go`, `validateEntrySortingOrder`): **only
  `published_at` or `created_at`**, with a comment that it *"must accept only the values
  of the entry_sorting_order enum type in the database."* The list pages
  (`internal/ui/unread_entries.go`, `starred_entries.go`) call
  `WithSorting(user.EntryOrder, user.EntryDirection)` then `WithSorting("id", …)`.

**Therefore Miniflux's own UI will never sort by your score**, no matter what carrier you
choose, short of a fork or a Custom JS client-side re-sort of the current page. The only
score-influenced ordering in the UI is the search page, which orders by
`ts_rank(document_vectors, websearch_to_tsquery(...)) - age*0.0000001` — so a distinctive
token embedded in the content *is* findable and rank-able via search, which is a
usable-but-clumsy escape hatch.

### Custom CSS and Custom JavaScript — the real UI lever

- `users.stylesheet` — long-standing user Custom CSS.
- `users.custom_js` — added by *"feat: add custom user JavaScript"* (2024-10-05), first
  released in **2.2.2** (2024-10-30). Settings form:
  `internal/template/templates/views/settings.html` (`form-custom-js`).

Injection, `internal/template/templates/common/layout.html`:

```html
{{ $cspNonce := nonce }}
<style nonce="{{ $cspNonce }}">{{ .user.Stylesheet | safeCSS }}</style>
<script type="module" nonce="{{ $cspNonce }}">{{ .user.CustomJS | safeJS }}</script>
```

CSP is generated per-request by `csp(user, nonce)` in
`internal/template/functions.go`:

```go
"default-src": "'none'", "script-src": "'nonce-…' 'strict-dynamic'",
"style-src": "'nonce-…'", "connect-src": "'self'",
"require-trusted-types-for": "'script'", "trusted-types": "html url",
```

Consequences for a Custom JS decorator:

- ✅ It runs on every page, including the list views, with full DOM access. Each list item
  carries `data-id="{{ .ID }}"`, so entries are individually addressable.
- ❌ **`connect-src 'self'`** — it **cannot** `fetch()` an external enricher directly. It
  can call the Miniflux API on the same origin (needs an embedded `X-Auth-Token`, since
  the session cookie doesn't authenticate `/v1/*`), or you can reverse-proxy your
  enricher under the Miniflux origin (e.g. `/agregado/*`) so it counts as `'self'`.
- ⚠️ **Trusted Types are enforced** (`require-trusted-types-for 'script'`, policies
  limited to names `html` and `url`; `app.js` already creates both, and duplicate policy
  names are rejected). Build DOM nodes with `document.createElement` + `textContent`
  rather than `innerHTML`.
- ⚠️ **Undocumented.** Neither `miniflux.app/docs/ui.html` nor the FAQ mentions Custom CSS
  or Custom JavaScript. It exists in the code and the settings screen but has no docs
  page, so it carries no stability promise.

---

## 6. Project position on plugins and extension points

This is unambiguous and comes straight from the project's own words.

**FAQ (<https://miniflux.app/faq.html>)** — there is no plugin system because
*"this software has a minimalist approach"*, *"implementing a plugin system increases the
complexity of the software"*, and *"people do not maintain their plugins after a while."*
The same page lists rejected-PR categories including *"making changes that conflict with
the software's philosophy"* and *"radical changes to the user interface."*

**Opinionated page (<https://miniflux.app/opinionated.html>)** —
*"Miniflux is a minimalist software. The purpose of this application is to read feeds.
Nothing else."* / *"the number of features is intentionally limited. Nobody likes
bloatware"* / *"Improving existing features is more important than adding new ones."*

**CONTRIBUTING.md** (repo root) restates the same: *"Miniflux follows a **minimalist
philosophy**. The feature set is intentionally kept limited to avoid bloatware"*, and
lists *"Conflicts with philosophy"* and *"Radical UI changes"* under "Pull Requests That
Cannot Be Accepted."

**Track record on exactly our kind of feature:**

| Item | Ask | Outcome |
|---|---|---|
| [#132](https://github.com/miniflux/v2/issues/132) *Extensible rewrite functions* | user-provided `call(/path/to/script)` rewrite hook | Closed 2022-06-03 without implementation. Maintainer: *"Calling an external command when processing each feed entries may have a performance impact."* |
| [#2034](https://github.com/miniflux/v2/issues/2034) *API to allow updating content of an entry* | LLM summarisation / weights via API | **Accepted and shipped in 2.0.49.** The one that landed. |
| [#2245](https://github.com/miniflux/v2/issues/2245) *[Idea] Recommendation Algorithm* | ranking | Closed 2026-01-05 by maintainer `fguillot`: *"Miniflux is an **opinionated project** and we intentionally keep the feature set **small**… Because of that, this won't be implemented."* |
| [#1493](https://github.com/miniflux/v2/issues/1493) *Summary mode / weighted ordering* | weighted unread ordering | Closed 2026-01-05, unimplemented |
| [#3926](https://github.com/miniflux/v2/pull/3926) *Add a voting feature (upvote/downvote)* | score carrier for a recommender | Closed 2026-02-08 by its own author after pushback (*"Sounds like feature creep to me"*). Author: *"I'll maintain my own fork."* |
| [#4101](https://github.com/miniflux/v2/pull/4101) *feat: add AI summarization, web scraper engine, headless JS rendering* | in-tree AI summaries | Closed 2026-03-16. Reviewers: *"Not a big fan of this. Also violates miniflux' aim for simplicity"*, *"This is unmergeable garbage, nobody is going to review 82 commits."* |
| [#4289](https://github.com/miniflux/v2/pull/4289) *feat(tags): Add minimal tag management* | add `tags` to `PUT /v1/entries/{id}` + a `/tags` page | **Open** (created 2026-05-04, last touched 2026-07-17, `+625-78`, mergeable_state `blocked`). Maintainer has engaged (asked for a rebase). Explicitly framed as *"an external service or reader client or greasemonkey script can interact with miniflux, saving metadata as tags… No bloat of miniflux features."* |

**Read of the pattern:** the project rejects *mechanisms* (plugin systems, scripting
hooks, in-tree AI, voting models, ranking) and accepts *small, generic API affordances*
that let an external system do the work — #2034 landed precisely because it added one
field pair to an existing endpoint and no new concepts. **Probability that a "plugin
system" or "extension point" PR lands: effectively zero.** Probability that a narrow,
one-field API extension lands (like #4289's `tags` on `PUT /v1/entries/{id}`): real but
unhurried — it has been open 3½ months with an engaged but unmerged maintainer.

**Design implication for Agregado:** do not plan around any upstream change. If you want
one, model it on #2034 — one field, one endpoint, no new concepts, no UI.

---

## Verdict per strategy

Version column = minimum Miniflux version required. All confirmed against
`2.3.x-dev` @ `106cdd0`.

### A. Mutate entry content via `PUT /v1/entries/{entryID}` — ✅ **the primary strategy**

- **Possible today:** yes. **Since:** 2.0.49 (2023-10-15).
- **What the user sees:** the summary/score renders in the **entry detail view** as
  sanitised HTML. Score can also be prefixed to the **title** as plain text, which makes
  it visible in **list views** with zero JS.
- **Cost:** must set `ignore_entry_updates` (2.2.18) or `crawler` per feed or refreshes
  overwrite the edit; UI force-refresh still clobbers. Restricted HTML subset — no `div`,
  `span`, `class`, `style`, `data-*`; `id` on a handful of tags is the only styling hook.
  Prefixing the title mutilates the original headline and is not reversible from the
  Miniflux side. `reading_time` is silently recomputed from your enriched content.
- **Fragility:** low-medium. Documented, stable since 2023, has an official Go client
  method, and was added for this exact use case. Main risk is the refresh-clobber
  interaction, which is fully controllable.

### B. Webhook-driven external enricher writing back — ✅ **the right trigger**

- **Possible today:** yes. **Since:** 2.0.48 (2023-09-15) for `new_entries`;
  per-feed `webhook_url` same release.
- **Shape:** one POST per feed-refresh batch containing full entry bodies **including
  database IDs**, HMAC-SHA256-signed, then write back via strategy A.
- **Cost / fragility:** fire-and-forget goroutine, **no retries**, no delivery log,
  failures only logged at warn level. Batch-per-feed, not per-entry — the enricher must
  fan out itself. Must be paired with a reconciliation poll
  (`GET /v1/entries?changed_after=` since 2.0.49, or `GET /v1/entries/ids` since 2.3.2)
  to catch drops. Medium fragility, entirely from the at-most-once delivery.

### C. Tags / starred / categories as score carrier — ❌ **do not build on this**

- **Tags:** cannot be set on feed-fetched entries at all (no `tags` field on
  `PUT /v1/entries/{id}`); settable only on entries you inject via
  `POST /v1/feeds/{id}/entries/import` (2.2.16). Even then, tags are **not rendered in
  list views**, only on the entry detail page and the `/tags/{tag}/entries/all` page
  (2.1.3), and are **not** covered by search.
- **Starred:** one boolean. Usable as a crude "above threshold" flag *and it does render
  in list views* (the star icon in `item_meta.html`) — the only metadata field that does.
  But it destroys the user's actual starring semantics.
- **Categories:** per-feed, not per-entry. Unusable.
- **Sorting:** the UI hard-caps `entry_order` to `published_at | created_at`. **No
  metadata carrier will ever produce a score-sorted list in Miniflux's UI.**
- Status could change if [#4289](https://github.com/miniflux/v2/pull/4289) merges — watch
  it, don't depend on it.

### D. Rewrite rules calling out — ⚠️ **possible via a loophole, not by design**

- Content rewrite rules are a **closed set of ~25 built-in functions with no network
  egress and no scripting hook**; #132 asked for `call(script)` and was closed. So: not
  by design.
- **But** `url_rewrite_rules` (regex on `entry.URL`) runs *before* the crawler fetch, so
  `rewrite("^https://site\.com/(.*)"|"https://enricher/enrich?u=…$1")` + `crawler = true`
  makes Miniflux fetch your service and store its HTML as the entry content.
- **Cost:** permanently rewrites `entry.URL` (the "open original" link now points at your
  proxy); the scraper's `sameSite` guard disables custom scraper rules; runs synchronously
  inside the refresh worker so LLM latency stalls feed refreshes; per-feed configuration.
- **Fragility:** high. Undesigned-for, and any change to the processor's ordering breaks
  it silently. Use A+B instead.

### E. User CSS / JS injection — ✅ **exists, and is how you fix the list view**

- **Custom CSS:** long-standing (`users.stylesheet`). **Custom JS:** since **2.2.2**
  (2024-10-30), `users.custom_js`, injected as a nonce'd `<script type="module">`.
- **Value:** this is the *only* supported way to render a score badge in the **entry
  list** and to re-order the visible page client-side. List items expose `data-id`.
- **Cost / fragility:** medium-high. `connect-src 'self'` blocks direct calls to an
  external enricher — you must either embed an API key and read from Miniflux's own API,
  or reverse-proxy the enricher onto the Miniflux origin. Trusted Types are enforced
  (`require-trusted-types-for 'script'`, `trusted-types html url`), so build DOM with
  `createElement`/`textContent`, not `innerHTML`. Both features are **undocumented** in
  `miniflux.app/docs` and the FAQ, so there is no compatibility promise; the CSP was
  itself refactored recently (#3731). Client-side sorting only ever reorders the current
  page, never the underlying pagination.

### F. Fork — ⚠️ **works, but the project's own history says it's a treadmill**

- Trivially possible; PR #3926's author explicitly went this route (*"I'll maintain my
  own fork"*), as did the author of #4101 (a published `ghcr.io/naiba-forks/miniflux`
  image).
- **Cost:** perpetual rebase against a fast-moving tree — 9 releases in the 12 months to
  2026-07, including schema migrations (`internal/database/migrations.go` grows most
  releases) and a UI/CSP refactor. You'd own the security posture too.
- Only justified if score-sorted list views are non-negotiable, since that is the one
  capability strategies A–E genuinely cannot deliver.

---

## Recommended shape

1. **Ingest trigger:** per-feed webhook (`feed.webhook_url`, 2.0.48) → Agregado, with a
   reconciliation poll on `GET /v1/entries/ids` (2.3.2) or `changed_after` (2.0.49) to
   cover the no-retry gap.
2. **Write-back:** `PUT /v1/entries/{id}` (2.0.49) with (a) a zero-padded score prefix in
   `title` so the score is visible in list views with no JS, and (b) an
   `<aside><h3 id="agregado-summary">…` block prepended to `content`.
3. **Protect the edit:** set `ignore_entry_updates: true` (2.2.18) on every feed via
   `PUT /v1/feeds/{id}`.
4. **Polish (optional):** user Custom CSS to style `#agregado-summary`, and Custom JS
   (2.2.2) if you want a proper badge instead of a title prefix — accepting the
   `connect-src 'self'` and Trusted Types constraints.
5. **Accept:** Miniflux will not sort by score. If ranked browsing is the product, that
   ranked view belongs in Agregado's own UI, with Miniflux as the "browse everything"
   surface — which is exactly the settled surface split.

**Minimum Miniflux version for the full recommended shape: 2.3.2** (for
`GET /v1/entries/ids`); **2.2.18** if you skip the ID-based reconciliation poll;
**2.0.49** for a bare webhook + write-back with no clobber protection.
