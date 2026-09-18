# How does Miniflux subscribe to a private feed, and what are its hard constraints on the entries we hand it?

Research for [ffrt-labs/agregado#96](https://github.com/ffrt-labs/agregado/issues/96),
a child of the map [#95](https://github.com/ffrt-labs/agregado/issues/95) (*The newsletter bridge*).

**Date:** 2026-09-18
**Sources:** primary only — the `miniflux/v2` source tree read at commit
[`76889f0`](https://github.com/miniflux/v2/commit/76889f08b12c2577b37e524e645afa5dd46ff050)
(2026-09-11, `2.3.x-dev`, i.e. post-2.3.3, the latest release being
[2.3.3](https://github.com/miniflux/v2/releases) of 2026-07-24), plus the official docs
at `miniflux.app/docs`. Version attributions were derived with `git log -S <symbol>` +
`git tag --contains`. Line numbers are as of `76889f0`.

**Version the homelab runs: not determinable from this repo.** Miniflux is not in
`docker-compose.yml` and no Miniflux image tag, version pin or deployment manifest exists
anywhere in `ffrt-labs/agregado` (grep over all tracked files for `miniflux.*2\.\d+\.\d+`
returns nothing) — ADR-0006 treats it as an external black box. This note therefore reads
current `main` and flags, per fact, the release in which the behaviour arrived, so the
findings can be re-checked once the running version is known. Two of the facts below
changed recently enough to matter: **entry tombstones (2.3.0)** and **private-network
refusal (2.2.18)**.

---

## Short answer

A private feed is fine, but Miniflux's toolkit for it is thin: **HTTP Basic, a raw
`Cookie` header, a custom `User-Agent`, or nothing at all — there is no per-feed custom
request header field.** An unguessable URL alone is a shape Miniflux is perfectly happy
with: it only ever sends a plain `GET`, never `HEAD`, follows redirects, and needs no
conditional-GET support. The entry identity is **SHA-256 of `<id>` verbatim**, so the id
is a contract we must treat as immutable, whitespace and all. There is no per-entry size
cap, but a feed document larger than **15 MiB decompressed** is rejected *whole*. The
Atom parser's sharpest edges are the mandatory `xmlns`, the `<content type="html">`
single-unescape rule, and the fact that Miniflux **sanitises our HTML down to a fixed
tag allowlist that contains no `div`, `span` or `class`**.

---

## 1. Feed authentication Miniflux supports natively

Everything the fetcher can be told to send for a feed lives on one builder,
[`internal/reader/fetcher/request_builder.go`](https://github.com/miniflux/v2/blob/76889f08b12c2577b37e524e645afa5dd46ff050/internal/reader/fetcher/request_builder.go):

| Mechanism | Builder method | Per-feed field | Header actually sent |
| --- | --- | --- | --- |
| HTTP Basic | `WithUsernameAndPassword` (L100) | `username`, `password` | `Authorization: Basic base64(user:pass)` |
| Cookie | `WithCookie` (L93) | `cookie` | `Cookie: <verbatim string>` |
| Custom UA | `WithUserAgent` (L84) | `user_agent` | `User-Agent: <string>` |
| Proxy | `WithCustomFeedProxyURL` | `proxy_url` | — (transport-level) |
| Nothing | — | — | the URL is the only secret |

```go
func (r *RequestBuilder) WithUsernameAndPassword(username, password string) *RequestBuilder {
	if username != "" && password != "" {
		r.headers.Set("Authorization", "Basic "+base64.StdEncoding.EncodeToString([]byte(username+":"+password)))
	}
	return r
}
```

Note the guard: **Basic auth is skipped unless *both* username and password are
non-empty.** A bearer-token-in-the-password trick with an empty username silently sends
no `Authorization` header at all.

**There is no per-feed custom request header field.** `model.Feed`
([`internal/model/feed.go`](https://github.com/miniflux/v2/blob/76889f08b12c2577b37e524e645afa5dd46ff050/internal/model/feed.go)
L27–L65) has `UserAgent`, `Cookie`, `Username`, `Password`, `ProxyURL` and nothing
header-shaped; a grep for `custom.*header` across `internal/` returns only an unrelated
readability heuristic. Both the fetch paths that matter wire up exactly these four —
[`handler.go` L124–L133](https://github.com/miniflux/v2/blob/76889f08b12c2577b37e524e645afa5dd46ff050/internal/reader/handler/handler.go#L124)
(create) and
[L232–L241](https://github.com/miniflux/v2/blob/76889f08b12c2577b37e524e645afa5dd46ff050/internal/reader/handler/handler.go#L232)
(refresh).

So the practical options for the bridge are: **(a) Basic auth over TLS**, **(b) a shared
secret smuggled in the `Cookie` header**, **(c) a bearer token smuggled in
`User-Agent`** (ugly, but it is a free-form header), or **(d) an unguessable URL**. A
`Authorization: Bearer …` header is *not* reachable; Cloudflare Access service tokens
(`CF-Access-Client-Id` / `CF-Access-Client-Secret`) are *not* reachable either — that is
a hard no, and it is worth recording against the map's "no Cloudflare Access carve-out"
line.

Docs corroboration is thin but consistent: <https://miniflux.app/docs/api.html> documents
`username`, `password`, `user_agent` on `POST /v1/feeds`; <https://miniflux.app/docs/ui.html>
documents the cookie field as *"HTTP Cookies to add to the request, e.g.,
`name0=value0; name1=value1`."* `cookie` is undocumented in the API reference but is
accepted by `FeedCreationRequest` (`internal/model/feed.go` L157).

### Private-network refusal (since 2.2.18)

`FETCHER_ALLOW_PRIVATE_NETWORKS` defaults to **false**
([`internal/config/options.go` L223](https://github.com/miniflux/v2/blob/76889f08b12c2577b37e524e645afa5dd46ff050/internal/config/options.go#L223)),
and the check runs in the dialer's `Control` callback, after DNS resolution, so it is
rebinding-proof. Added by commit `2682421` *"feat: add FETCHER_ALLOW_PRIVATE_NETWORKS
option"* (2026-02-28), first released in **2.2.18**. A Cloudflare-hosted bridge is public
and unaffected; a bridge on the homelab LAN would be refused with
`fetcher: refusing to access private network host` unless the flag is flipped.

---

## 2. Entry identity and what happens when an entry changes

### The dedupe key

`entry.Hash = SHA-256(<id>)`, hex-encoded, taken from the Atom `<id>` **exactly as
parsed**, falling back to the alternate link's `href` only when `<id>` is empty
([`internal/reader/atom/atom_10_adapter.go` L170–L176](https://github.com/miniflux/v2/blob/76889f08b12c2577b37e524e645afa5dd46ff050/internal/reader/atom/atom_10_adapter.go#L170)):

```go
// Generate the entry hash.
for _, value := range []string{atomEntry.ID, atomEntry.Links.originalLink()} {
	if value != "" {
		entry.Hash = crypto.SHA256(value)
		break
	}
}
```

Verified against the project's own fixture: `atom_10_test.go` L60 asserts
`3841e5cf232f5111fc5841e9eba5f4b26d95e7d7124902e0f7272729d65601a6` for an entry whose id
is `urn:uuid:1225c695-cfb8-4ebb-aaaa-80da344efa6a`, and
`printf '%s' 'urn:uuid:1225c695-cfb8-4ebb-aaaa-80da344efa6a' | sha256sum` gives exactly
that. So: **the id string, no trimming, no normalisation, no URL canonicalisation.**

Consequences worth spelling out:

- **`<id>` is untrimmed chardata.** `<id>\n  urn:x\n</id>` hashes differently from
  `<id>urn:x</id>`. A pretty-printer that indents the feed will silently re-key every
  entry and duplicate the whole window. Emit `<id>` with no surrounding whitespace.
- **The entry URL is not part of the key.** `atom_10_test.go`'s
  `TestParseEntryWithHTMLContent` (L856) has three entries sharing one `<id>` and three
  different `<link href>`s; all three get the same hash and would collapse to one stored
  row.
- **Conversely, `<link>` changing is free.** The permalink can be rewritten without
  creating a duplicate, as long as `<id>` holds.
- **No `<id>` and no `<link>` ⇒ empty hash.** Two such entries in one feed collide on the
  `entries(feed_id, hash)` unique index and the transaction fails, aborting the whole
  refresh with a database error. Always emit `<id>`.

Storage looks the entry up on `(feed_id, hash)` only —
[`entryExists`, `internal/storage/entry.go` L218](https://github.com/miniflux/v2/blob/76889f08b12c2577b37e524e645afa5dd46ff050/internal/storage/entry.go#L218):
`SELECT true FROM entries WHERE feed_id=$1 AND hash=$2 LIMIT 1`. Hashes are scoped per
feed, so the same id in two Source feeds yields two entries — which is what the map's
"one feed per Source" shape wants anyway.

### Same id, changed content

`RefreshFeedEntries` (L320) splits on `entryExists`; for an existing entry it calls
`updateEntry` **only if `updateExistingEntries`** is true. From
[`handler.go` L327–L330](https://github.com/miniflux/v2/blob/76889f08b12c2577b37e524e645afa5dd46ff050/internal/reader/handler/handler.go#L327):

```go
updateExistingEntries := forceRefresh || (!originalFeed.Crawler && !originalFeed.IgnoreEntryUpdates)
```

So by default (no crawler, no `ignore_entry_updates`) **re-serving an entry with the same
`<id>` and new content does overwrite it.** `updateEntry`
([L169](https://github.com/miniflux/v2/blob/76889f08b12c2577b37e524e645afa5dd46ff050/internal/storage/entry.go#L169))
writes `title, url, comments_url, content, author, reading_time, document_vectors, tags,
language` and replaces the enclosures. What it does **not** touch:

- **`published_at`** — deliberate, per the function's own comment ("we do not update the
  published date because some feeds do not contains any date").
- **`status`** — an already-read entry stays read; an update never resurfaces it as unread.
- **`changed_at`** — not in the `SET` list, so it keeps its creation value.
- **`starred`, `share_code`, `id`** — stable. The numeric entry id the Article Index
  caches (ADR-0006 §5) survives a content update.

Turning this off per feed is `ignore_entry_updates` (`internal/model/feed.go` L46, UI
checkbox in `edit_feed.html` L107), added by `7b07b82` *"feat(feed): add
ignore_entry_updates option to feeds"* (2026-02-13), first released in **2.2.18**. On a
pre-2.2.18 instance the only way to suppress updates is to enable the crawler, which has
side effects we do not want.

### Tombstones — the resurrection guard (since 2.3.0)

`ArchiveEntries`
([L368](https://github.com/miniflux/v2/blob/76889f08b12c2577b37e524e645afa5dd46ff050/internal/storage/entry.go#L368))
no longer flags entries `removed`; it **deletes** them and records `(feed_id, hash)` in
`entry_tombstones`. `createEntry` (L81) carries a `WHERE NOT EXISTS (SELECT 1 FROM
entry_tombstones …)` guard and returns `ErrEntryTombstoned`, which `RefreshFeedEntries`
swallows silently. Introduced by `2916831` *"fix(storage): prevent deleted entries from
reappearing as unread"* (2026-04-13), first released in **2.3.0**.

**Nothing ever deletes a tombstone** — grep for `entry_tombstones` across `internal/`
finds four sites, all inserts or guards, and `runCleanupTasks`
(`internal/cli/cleanup_tasks.go`) has no tombstone pruning. So once an entry ages out
(defaults: read after `CLEANUP_ARCHIVE_READ_DAYS` = 60 days, unread after
`CLEANUP_ARCHIVE_UNREAD_DAYS` = 180 days — both measured from `created_at`, both settable
to `-1` to disable), **re-serving that `<id>` can never bring it back**, permanently.
`FlushHistory` tombstones too.

This directly touches the map's settled *"the permalink is permanent; only the feed window
rolls"*: it is safe, because Miniflux's own tombstones make the rolling window one-way by
construction. It also means a bug that re-keys entries (see the whitespace trap above)
creates duplicates that can never be reconciled by fixing the id later.

---

## 3. Size caps

**There is no per-entry content cap.** The `entries.content` column is plain `text`
(`internal/database/migrations.go` L85) — Postgres' ~1 GB field ceiling, never hit in
practice.

The only truncation is of the **full-text index input**, not the stored content
([`truncateTitleAndContentForTSVectorField`, L678](https://github.com/miniflux/v2/blob/76889f08b12c2577b37e524e645afa5dd46ff050/internal/storage/entry.go#L678)):

```go
// The length of a tsvector (lexemes + positions) must be less than 1 megabyte.
// We don't need to index the entire content, and we need to keep a buffer for the positions.
return truncateStringForTSVectorField(title, 200000), truncateStringForTSVectorField(content, 500000)
```

Title beyond 200 000 bytes and content beyond 500 000 bytes are dropped **from
`document_vectors` only**; `entry.Content` is inserted whole. A newsletter over ~500 KB is
stored and readable but only its first 500 KB is searchable in Miniflux.

**The binding cap is on the whole feed document: `HTTP_CLIENT_MAX_BODY_SIZE`, default
15 MiB** (`internal/config/options.go` L256; `HTTPClientMaxBodySize()` L775 multiplies by
`1024*1024`). It is enforced by `http.MaxBytesReader` wrapped *around* the brotli/gzip
decompressor
([`response_handler.go` `getReader`](https://github.com/miniflux/v2/blob/76889f08b12c2577b37e524e645afa5dd46ff050/internal/reader/fetcher/response_handler.go#L128)),
so **the limit is on decompressed bytes** — gzipping the feed buys transfer, not headroom.

**On exceeding it, the whole feed is rejected, not truncated and not partially parsed**
(`ReadBody`, L156):

```go
if err, ok := err.(*http.MaxBytesError); ok {
	return nil, locale.NewLocalizedErrorWrapper(fmt.Errorf("fetcher: response body too large: %d bytes", err.Limit), "error.http_response_too_large")
}
```

Miniflux never sees a single entry from that poll. Interesting detail: `handler.go` L289–L293
returns this error **without** calling `getTranslatedLocalizedError`, so unlike a parse
failure it does *not* increment `parsing_error_count` — the feed keeps being polled and
keeps failing, silently, forever. An oversized feed is therefore a quiet outage, not a
disabled feed.

Practical budget for the map's settled *"the feed entry carries the readable content"*:
a 30-day window of daily newsletters is ~30 entries, so the mean entry body must stay
under ~500 KB to be safe, and any single monster newsletter can blow the whole feed up.
Two mitigations are available and belong in a downstream ticket: shrink the window when
the rendered document approaches the cap, or raise `HTTP_CLIENT_MAX_BODY_SIZE` on the
homelab instance.

---

## 4. Is an unguessable feed URL a shape Miniflux is happy with?

Yes, with no caveats on Miniflux's side.

- **Method.** Feeds are fetched with a hardcoded `GET` and nothing else —
  `http.NewRequest("GET", requestURL, nil)` (`request_builder.go` L261). **Miniflux never
  issues a `HEAD`** for feeds; the only `MethodHead` reference in `internal/` is CSRF
  middleware. A bridge that only implements `GET` is complete.
- **Redirects.** Followed, using Go's default policy (10 hops). `withoutRedirects` is set
  in exactly one place — the well-known-path probe during *discovery*
  (`internal/reader/subscription/finder.go` L235) — and never on the refresh path.
  On subscribe, the **post-redirect** URL is what gets persisted:
  `subscription.FeedURL = responseHandler.EffectiveURL()`
  ([`handler.go` L181](https://github.com/miniflux/v2/blob/76889f08b12c2577b37e524e645afa5dd46ff050/internal/reader/handler/handler.go#L181)).
  On refresh, `feed_url` is never rewritten — only `etag`, `last_modified`, `language`
  and `icon_url` are copied back (L350–L353). So a stable-alias → secret-URL redirect
  would bake the secret into `feed_url` at subscribe time. Design around it, don't rely
  on it.
- **Content-Type is irrelevant.** The format is sniffed from the bytes —
  `DetectFeedFormat` (`internal/reader/parser/format.go`) tokenises the first 50 XML
  tokens and switches on the root element name. Serving `text/xml`,
  `application/atom+xml` or even `text/plain` all work. (Sending a correct
  `application/atom+xml` is still good manners; Miniflux's outgoing `Accept` header is
  `application/xml,application/atom+xml,application/rss+xml,application/rdf+xml,application/feed+json,text/html,*/*;q=0.9`.)
- **Conditional GET is optional.** `WithETag` / `WithLastModified` only set `If-None-Match`
  / `If-Modified-Since` when Miniflux has a stored value, which it only has if we sent one
  (`response_handler.go` L46–L52). `IsModified` (L100) returns `true` when neither header
  is present, so a bridge that ignores caching entirely just gets parsed every poll.
  Two traps if we *do* implement it: a response header `Expires: 0` makes Miniflux discard
  both `ETag()` and `LastModified()` outright; and a `304` short-circuits before `ReadBody`,
  so an ETag that doesn't change when the window rolls will freeze the feed.
- **Polling floor ≈ 60 minutes.** Two gates compose. The scheduler ticks every
  `POLLING_FREQUENCY` (default 60 min, `internal/cli/scheduler.go` L34) and selects feeds
  with `next_check_at < now()`; `ScheduleNextCheck` (`internal/model/feed.go` L123) sets
  `next_check_at = now + max(SCHEDULER_ROUND_ROBIN_MIN_INTERVAL, refreshDelay)`, default
  60 min, where `refreshDelay = max(feed TTL, Cache-Control max-age, Expires)`. Net
  effect: **one poll per feed per 60–120 minutes**, and we can *lengthen* but never
  shorten it via `Cache-Control: max-age`. Manual refresh is separately floored by
  `FORCE_REFRESH_INTERVAL` (default 30 min). Docs confirm every default:
  <https://miniflux.app/docs/configuration.html>.
- **Failure budget.** `POLLING_PARSING_ERROR_LIMIT` defaults to 3; the scheduler's batch
  query adds `parsing_error_count < $n` (`internal/storage/batch.go` `WithErrorLimit`,
  wired at `scheduler.go` L38). **Three consecutive parse/HTTP failures and the feed stops
  being polled** until a manual refresh succeeds (`ResetErrorCounter` on success,
  `handler.go` L373). A 401/403 counts. A body-too-large does *not* (see §3).
- **Rate limiting is respected.** A `429` makes Miniflux honour `Retry-After` (seconds or
  RFC1123 date) as the next interval (`response_handler.go` `ParseRetryDelay`).
- **One incidental fetch.** At subscribe time Miniflux runs an icon check against the
  feed's `SiteURL` (`internal/reader/icon/checker.go` L40), i.e. it will `GET` whatever
  `<link rel="alternate">` we advertise, plus `/favicon.ico`. If no alternate link is
  given, `SiteURL` falls back to the feed URL itself, so the secret URL gets one extra
  hit. Harmless, but worth knowing the bridge will be asked for a favicon.

---

## 5. Atom-parser traps for a feed we generate

Read with
[`atom_10.go`](https://github.com/miniflux/v2/blob/76889f08b12c2577b37e524e645afa5dd46ff050/internal/reader/atom/atom_10.go),
[`atom_10_adapter.go`](https://github.com/miniflux/v2/blob/76889f08b12c2577b37e524e645afa5dd46ff050/internal/reader/atom/atom_10_adapter.go)
and [`atom_common.go`](https://github.com/miniflux/v2/blob/76889f08b12c2577b37e524e645afa5dd46ff050/internal/reader/atom/atom_common.go).

### What is actually required

Almost nothing. Contrary to RFC 4287, Miniflux demands no element:

| Element | Missing ⇒ |
| --- | --- |
| feed `<title>` | falls back to the site URL (`atom_10_adapter.go` L46) |
| feed `<id>`, `<updated>`, `<author>` | ignored entirely |
| entry `<title>` | falls back to `TruncateHTML(content, 100)`, then to the entry URL (L107) |
| entry `<link>` | falls back to `<id>` if it is an absolute URL, else to the site URL (L86) |
| entry `<id>` | hash falls back to the link — **do not rely on this** (see §2) |
| entry `<updated>`/`<published>` | date defaults to `time.Now()` (L151) |
| entry `<author>` | inherits the feed-level author, else empty |

### The two things that *will* break the feed

1. **The `xmlns` is mandatory.** `atom10Feed` declares
   ``XMLName xml.Name `xml:"http://www.w3.org/2005/Atom feed"` ``, and every child tag is
   namespace-qualified. `DetectFeedFormat` matches on the *local* name `feed` regardless
   of namespace, so a `<feed>` without `xmlns="http://www.w3.org/2005/Atom"` is routed to
   the Atom 1.0 decoder and then fails its `XMLName` check, producing
   `atom: unable to parse Atom 1.0 feed: …` and a `parsing_error_count` bump.
   *Caveat: this follows from `encoding/xml`'s documented `XMLName` behaviour and the
   struct tags; I could not execute it — no Go toolchain in this environment — and there
   is no test in `atom_10_test.go` covering a namespace-less feed.*
2. **Never put `version="0.3"` on `<feed>`.** `DetectFeedFormat` switches to the Atom 0.3
   decoder purely on that attribute (`format.go`), which expects the
   `http://purl.org/atom/ns#` namespace and would silently yield an empty feed.

Everything else is forgiving: the decoder runs with `Strict = false` and
`Entity = xml.HTMLEntity` (`internal/reader/xml/*.go`), so bare HTML entities like
`&nbsp;` survive and mismatched tags are tolerated; invalid XML code points are stripped
in place rather than raising an error.

### Dates

`date.Parse` (`internal/reader/date/parser.go`) tries `RFC3339` second in a ~150-format
list, and it is the canonical Atom form. **Emit RFC 3339 with an explicit offset
(`2026-09-18T07:04:05Z`).** Note a live footgun in the list's structure: `RFC822`,
`RFC850` and `RFC1123` — the named-timezone variants — are segregated into
`dateFormatsLocalTimesOnly` and applied only for local times. A date that fails every
format is not an error; the entry silently gets `time.Now()` (`atom_10_adapter.go` L151),
which means a date bug shows up as "everything published at poll time", not as a failure.
`<published>` is tried before `<updated>` and the first parseable one wins.

### `<content type="html">` escaping

`atom10Text.body()` (`atom_10.go` L182) returns `strings.TrimSpace(a.CharData)` for
everything that isn't `type="xhtml"`. `CharData` is what Go's XML decoder produces, so it
has already been unescaped **exactly once** — and CDATA arrives as chardata too. The
project's own fixture `TestParseEntryWithHTMLContent` (`atom_10_test.go` L856) pins this:
three entries, written as `type="html"` escaped, `type="text/html"` escaped, and
`type="html"` in CDATA, all yield the identical string

```
AT&amp;T bought <b>by SBC</b>!
```

So the rule for the bridge is the plain one: **`<content type="html">` must contain the
HTML source escaped exactly once** (`&` → `&amp;`, `<` → `&lt;`), or the identical source
wrapped in `<![CDATA[…]]>`. Both produce the same stored content; escaping twice leaves
visible `&amp;lt;` in the reader. `type="text/html"` is accepted as a synonym, and
omitting `type` entirely takes the same branch.

`type="xhtml"` is handled separately — `xhtmlContent()` returns the inner XML of the
required `<div xmlns="http://www.w3.org/1999/xhtml">` wrapper — and is strictly more
fragile for newsletter HTML (must be well-formed XML). **Use `type="html"`.**

One asymmetry to know: `title()` (L194) *does* apply an extra `html.UnescapeString` when
the raw XML contained `<![CDATA[`, and `body()` does not. Titles in CDATA get one more
unescaping round than content does.

### What the sanitiser will do to our HTML

This is the biggest practical "mangle" risk, and it runs on every refresh, not just on
insert. `ProcessFeedEntries`
([`internal/reader/processor/processor.go` L163](https://github.com/miniflux/v2/blob/76889f08b12c2577b37e524e645afa5dd46ff050/internal/reader/processor/processor.go#L163))
ends with:

```go
// The sanitizer should always run at the end of the process to make sure unsafe HTML is filtered out.
entry.Content = sanitizer.SanitizeHTML(webpageBaseURL, entry.Content, …)
```

From [`internal/reader/sanitizer/sanitizer.go`](https://github.com/miniflux/v2/blob/76889f08b12c2577b37e524e645afa5dd46ff050/internal/reader/sanitizer/sanitizer.go):

- **Fixed tag+attribute allowlist** (L21–L115). It contains no `div`, `span`, `section`,
  `article`, `header`, `footer`, `nav`, `main`, `center`, `font`, `form`, `input`,
  `button`, `col`, `colgroup`, `tbody`. `table`/`tr`/`td` survive but `td` keeps only
  `rowspan`/`colspan`.
- **No `class`, no `style`, no `id` except on a fixed set** (`a`, headings, list items,
  `dl`/`dt`/`dd`, `sup`), no `data-*`, no `align`/`bgcolor`/`cellpadding`. Newsletter
  layout is table-and-inline-style soup; expect all of it to be discarded.
- **Disallowed tags are unwrapped, not dropped** — `filterAndRenderHTML` recurses into
  children for any tag not in the map (L252–L255), so text inside a `<div>` survives, the
  `<div>` itself does not.
- **Blocked tags lose their subtree**: `script`, `style`, `noscript` (`isBlockedTag`,
  L322), anything carrying a bare `hidden` attribute (`isHidden`, L339), and any
  `<img width="1" height="1">`-style pixel tracker (`isPixelTracker`, L348 — triggers on
  width *and* height being `"0"` or `"1"`).
- **Resources on a blocklist are stripped** — `feeds.feedburner.com`, `stats.wordpress.com`,
  share-button URLs for X/Facebook/LinkedIn/Pinterest (L132–L144).
- **Relative URLs are resolved** against `webpageBaseURL`, which is `entry.URL` — i.e. our
  permalink. Relative `<img src>` in newsletter HTML will be rewritten against the
  permalink's origin.
- `iframe`s survive only for a hardcoded domain allowlist (bandcamp, dailymotion, embedly,
  …) plus the configured YouTube/Invidious domain.

Nothing here rejects an entry — sanitisation is lossy, never fatal. But it means **the
readable content Miniflux stores is not byte-identical to what the bridge emits**, which
matters because the map settles that n8n hands Miniflux's copy to #77's bridge-content
endpoint. If enrichment needs the original markup, the bridge's own store is the only
faithful copy.

### The entry URL is rewritten too

Before sanitising, `ProcessFeedEntries` runs
`urlcleaner.RemoveTrackingParameters(feedURL, siteURL, entryURL)` (L88–L91, `urlcleaner`) and then
`rewrite.RewriteEntryURL`. The cleaner strips a long list of tracking params
(`utm_*`, `fbclid`, `mc_cid`, `_hsenc`, `gclid`, …) and, **when it strips anything, it
re-encodes the remaining query string with `url.Values.Encode()`, which sorts parameters
alphabetically**. Since the Article Index is keyed on canonical URL (ADR-0006 §5) and for
an email-only Article the permalink *is* that key, the safe shape is a **permalink with
no query string at all** — `https://bridge.example/a/<uuid>`. Then
`RemoveTrackingParameters` short-circuits on `RawQuery == ""` and the URL is passed
through untouched.

---

## 6. The alternative shape: push instead of poll

Worth recording because it sidesteps most of §3–§5. `POST /v1/feeds/{feedID}/entries/import`
([`internal/api/entry_handlers.go` L370–L462](https://github.com/miniflux/v2/blob/76889f08b12c2577b37e524e645afa5dd46ff050/internal/api/entry_handlers.go#L370),
**since 2.2.16**, 2026-01-07) injects one entry into an existing feed with `url`, `title`,
`content`, `author`, `comments_url`, `published_at`, `status`, `starred`, `tags` and
`external_id`:

```go
hashInput := importRequest.ExternalID
if hashInput == "" {
	hashInput = importRequest.URL
}
entry.Hash = crypto.HashFromBytes([]byte(hashInput))
```

Same hash discipline, same sanitiser, no 15 MiB document cap, no polling floor, no Atom
serialisation — but it needs a Miniflux API key held by the bridge, it still requires a
feed to exist to hang entries off, and it inverts ADR-0006's "Miniflux polls like any
other Source" framing. Also note it returns `400` on `ErrEntryTombstoned` rather than
swallowing it. Not a recommendation; a documented door.

---

## Summary table

| Question | Answer |
| --- | --- |
| Native feed auth | Basic (both user+pass non-empty), verbatim `Cookie` header, custom `User-Agent`, per-feed proxy. **No custom request headers.** |
| Dedupe key | `hex(SHA-256(<id>))` verbatim, scoped by `feed_id`; falls back to alternate `<link href>` |
| Same id, new content | Overwrites title/url/content/author/tags by default; keeps `published_at`, `status`, `changed_at`, numeric id. Suppress with `ignore_entry_updates` (2.2.18+) |
| Aged-out entries | Deleted + tombstoned permanently (2.3.0+); re-serving the id never brings them back |
| Per-entry size cap | None. Only the FTS index is truncated (200 KB title / 500 KB content) |
| Feed document cap | 15 MiB **decompressed** (`HTTP_CLIENT_MAX_BODY_SIZE`); exceeding ⇒ whole feed rejected, silently, without incrementing the error counter |
| Unguessable URL | Fine. `GET` only, never `HEAD`, redirects followed, Content-Type ignored, conditional GET optional |
| Poll interval floor | ~60 min (both `POLLING_FREQUENCY` and `SCHEDULER_ROUND_ROBIN_MIN_INTERVAL` default to 60); can only be lengthened by us |
| Failure budget | 3 parse/HTTP errors ⇒ polling stops (`POLLING_PARSING_ERROR_LIMIT`) |
| Atom must-haves | `xmlns="http://www.w3.org/2005/Atom"`; no `version="0.3"`; `<id>` with no stray whitespace; RFC 3339 dates; `<content type="html">` escaped exactly once |
| Guaranteed mangling | HTML sanitised to a fixed allowlist (no `div`/`span`/`class`/`style`); tracking params stripped from the entry URL and the query re-sorted |

## Open / unverified

- **The Miniflux version the homelab runs.** Not pinned anywhere in this repo; asserted
  facts are tagged with the release that introduced them so they can be re-checked.
- **The namespace-less `<feed>` failure mode** (§5) is derived from struct tags and
  `encoding/xml` semantics, not executed — no Go toolchain was available here.
- **Whether `document_vectors` truncation matters at all** depends on whether Miniflux's
  own search is ever used. Under ADR-0006 ("never logged into as a UI") it almost
  certainly does not.
