# Research: email-only newsletters → feed entries

**Issue:** [#52](https://github.com/ffrt-labs/agregado/issues/52)
**Date:** 2026-08-22
**Status:** Fact-finding. No decision taken — see "Open questions" at the end.

Every claim below cites a URL or a file path in a public repo. Where a project's
README is vague, the citation is the source line that actually does the thing,
because on this topic READMEs and source disagree often.

---

## 0. The shape of the problem

An email newsletter is a single MIME message. To become a feed entry it needs
four things a feed reader expects, and email supplies none of them cleanly:

1. **A body** — email has `text/html` and `text/plain` alternatives, usually
   quoted-printable encoded, usually table-layout HTML with tracking pixels.
2. **A stable id** — `Message-ID` works, but not every sender sets it.
3. **A timestamp** — `Date` header, or receipt time.
4. **A link** — *this is the hard one.* A newsletter is one email containing
   many links. There is no natural "the URL of this entry". Every option below
   resolves this differently, and most resolve it by inventing a URL.

The transport also has to be chosen: **SMTP** (run a mail server, receive push),
**IMAP** (poll someone else's mailbox), or **webhook** (a mail provider POSTs to
you). These have very different operational weights.

---

## 1. kill-the-newsletter (leafac/kill-the-newsletter)

Repo: <https://github.com/leafac/kill-the-newsletter> · Hosted:
<https://kill-the-newsletter.com>

### Maturity and licence

- TypeScript, MIT licence, ~3.1k stars, not archived; last push 2026-07-31.
  (`https://api.github.com/repos/leafac/kill-the-newsletter`)
- Latest release **v2.0.9, 2026-01-12**. Before that v2.0.8 was 2024-08-01 — so
  a long quiet stretch, and 2.0.9's only changelog line is "Enable compression
  on the server, which should reduce data transfer costs."
  (`CHANGELOG.md`)
- The whole application is **one file**, `source/index.mts`, 1999 lines. Runtime
  dependencies are just four packages: `@radically-straightforward/production`,
  `crypto-random-string`, `mailparser`, `smtp-server`. (`package.json`)

### How it works

It **runs its own SMTP server**. There is no IMAP, no webhook, no polling:

```
if (application.commandLineArguments.values.type === "email") {
  application.email = new SMTPServer({
    name: application.userConfiguration.hostname,
    size: 2 ** 19,
    disabledCommands: ["AUTH"],
    key: ..., cert: ...,
    onData: async (emailStream, session, callback) => {
```
(`source/index.mts:1588-1597`)

Inbound mail is parsed with `mailparser.simpleParser` (`source/index.mts:1629`),
stored in SQLite, and served as Atom at `/feeds/<publicId>.xml`.

Notable inbound behaviour:
- `AUTH` is disabled — it is an **open relay for delivery** by design; anyone who
  knows a feed address can post to that feed.
- Two senders are hardcoded-blocked: `blogtrottr.com` and `feedrabbit.com`
  (`source/index.mts:1602-1607`) — i.e. it refuses to be chained off other
  feed-to-email services.
- Max inbound message size is `2 ** 19` = **512 KiB** (`SMTPServer({ size: 2 ** 19 })`,
  `source/index.mts:1590`); over that, `throw new Error("Email is too big.")`
  (`source/index.mts:1630`). **This is small.** Many HTML newsletters with inline
  images exceed 512 KiB.
- Attachments are extracted to disk and exposed as Atom `rel="enclosure"` links
  (`source/index.mts:1633-1690`, `source/index.mts:630-641`).

### Per-feed address model

A feed is created by `POST /feeds` with a title; no account, no auth. The feed
id is a 20-char random lowercase-alphanumeric string:

```
${cryptoRandomString({ length: 20, characters: "abcdefghijklmnopqrstuvwxyz0123456789" })}
```
(`source/index.mts:858-864`)

The JSON response is exactly the model:

```json
{"feedId":"...", "email":"<feedId>@<hostname>", "feed":"https://<hostname>/feeds/<feedId>.xml"}
```
(`source/index.mts:872-880`; documented in `CHANGELOG.md` under 2.0.5/2.0.7)

So: **one opaque random address per feed, address = feed id, and the address is
the only credential.** Anyone with the address can write; anyone with the feed
URL can read. There is no per-sender filtering — a feed is whatever was mailed
to that address.

### Retention / entry limits

Retention is **size-based, not count-based, and evaluated on every delivery**.
After inserting a new entry it walks entries newest-first accumulating
`title.length + content.length`, and deletes everything past the point where the
running total exceeds `2 ** 19` (512 KiB):

```
let feedLength = 0;
while (deletedFeedEntries.length > 0) {
  const feedEntry = deletedFeedEntries.pop()!;
  feedLength += feedEntry.title.length + feedEntry.content.length;
  if (feedLength > 2 ** 19) break;
}
for (const deletedFeedEntry of deletedFeedEntries) { /* DELETE */ }
```
(`source/index.mts:1745-1770`)

The user-facing FAQ confirms this is deliberate: *"When Kill the Newsletter!
receives an email it may delete old entries to keep the feed under a size limit,
because some feed readers don't support feeds that are too big."*
(`source/index.mts:786-792`)

`CHANGELOG.md` for 2.0.8 records the limit being **halved** from `2 ** 20` to
`2 ** 19` "to try and reduce server costs 💀".

**Consequence for a downstream consumer:** since full newsletter HTML is often
100-300 KiB, a 512 KiB budget can mean **as few as 2-4 retained entries per
feed**. A poller that falls behind by a couple of issues loses them permanently.
This is the single most important operational fact about KTN for Agregado: it is
a *transient buffer*, not an archive. Self-hosting lets you raise the constant,
but it is a hardcoded literal in two places, not configuration.

### What it does to the HTML body — **preserved verbatim**

Storage takes the HTML with no sanitisation, no readability extraction, no
rewriting:

```
${typeof email.html === "string" ? email.html
  : typeof email.textAsHtml === "string" ? email.textAsHtml
  : "No content."}
```
(`source/index.mts:1720`)

Priority is `text/html` → mailparser's `textAsHtml` (plaintext wrapped in HTML) →
the literal string `"No content."`.

Serialisation into Atom uses the `@radically-straightforward/html` tagged
template, where `${...}` **escapes** and `$${...}` interpolates raw. The content
is interpolated with the single-`$` escaping form:

```
<content type="html">
  ${feedEntry.content}
  ...
</content>
```
(`source/index.mts:651-653`)

The library documents `${}` as sanitising: *"Sanitizes interpolations to prevent
injection attacks"*
(<https://github.com/radically-straightforward/radically-straightforward/blob/main/html/README.md>).

So the email HTML is **HTML-escaped into `<content type="html">`** — which is the
correct Atom encoding, and a conforming consumer unescapes it back to the
original bytes. **Fidelity is full: the enricher receives the complete original
newsletter HTML, links, tracking pixels and all.** This is the best content
fidelity of any option surveyed.

One caveat: KTN **appends its own footer inside `<content>`** — an `<hr>` plus a
"Kill the Newsletter! feed settings" link (`source/index.mts:653-666`). Any
downstream link extraction must strip this, or it will find the KTN settings URL
as a candidate.

### Canonical URL — **synthetic, points back at KTN**

```
<id>urn:kill-the-newsletter:${feedEntry.publicId}</id>
<link rel="alternate" type="text/html"
      href="https://<hostname>/feeds/<feedPublicId>/entries/<feedEntryPublicId>.html" />
```
(`source/index.mts:601-609`)

The link is a **synthetic URL on the KTN host** that renders the stored HTML
(handler at `source/index.mts:1307+`, served with a restrictive
`Content-Security-Policy`). KTN makes **no attempt whatsoever** to find the
newsletter's real web home — it does not read `Archived-At`, does not look for a
"view in browser" anchor, does not read `List-Archive`. The entry id is a
`urn:`, not a URL.

**This is exactly the problem ADR-0004 solved, left unsolved.** Adopting KTN
would mean Agregado still has to do its own canonical-URL extraction — on HTML
it now receives second-hand through Atom rather than first-hand from the Worker.

### Self-hosting story

Self-hostable in principle (MIT, and there is a `configuration/example.mjs` and a
systemd unit at `configuration/kill-the-newsletter.service`), but the
**operational weight is high and under-documented**:

- The README is **7 lines long** and delegates to two generic external guides in
  a different repo — `radically-straightforward/guides/deployment.md` and
  `.../development.md`. There is no project-specific self-hosting doc, no
  Dockerfile, no container image in the repo tree.
- You must **run an internet-facing SMTP server on port 25** with a dedicated
  hostname and MX records pointed at it. Port 25 inbound is blocked by many
  hosts; deliverability and abuse handling become your problem.
- It needs **TLS key and cert file paths handed to it directly**, and the config
  comments say these come from Caddy's ACME storage:
  *"Paths to the key and certificate generated by Caddy, which must be provided
  here because they're used for the email server."* (`configuration/example.mjs`)
  So the deployment is coupled to Caddy specifically.
- The systemd unit runs `User=root` (`configuration/kill-the-newsletter.service`).
- There is an older, dockerised community fork —
  <https://github.com/3nprob/kill-the-newsletter.com> — but it is **stale**
  (last push 2021-01-06, 13 stars) and tracks KTN v1, not the current v2.

---

## 2. Miniflux — **no email ingestion, definitively**

Repo: <https://github.com/miniflux/v2> · Docs: <https://miniflux.app/docs/>

Miniflux is Go, Apache-2.0, ~9.6k stars, actively maintained (last push
2026-08-19; latest release **2.3.3, 2026-07-24**).
(`https://api.github.com/repos/miniflux/v2`)

**Miniflux cannot ingest email in any form. Email must arrive as an RSS/Atom feed
produced outside it.** Evidence, four independent ways:

1. **The parser accepts only four formats, all XML/JSON over HTTP.**
   `internal/reader/parser/format.go` defines the complete set:
   ```go
   const (
       FormatRDF     = "rdf"
       FormatRSS     = "rss"
       FormatAtom    = "atom"
       FormatJSON    = "json"
       FormatUnknown = "unknown"
   )
   ```
   `DetectFeedFormat` sniffs for JSON, or the XML root elements `rss`, `feed`,
   `RDF`. Anything else is `FormatUnknown`.
   (<https://github.com/miniflux/v2/blob/main/internal/reader/parser/format.go>)

2. **No IMAP anywhere in the codebase.** A GitHub code search for `imap` scoped
   to `repo:miniflux/v2` returns **0 results**. A search for `newsletter`
   returns 3 results, all of which are *test fixtures* —
   `internal/reader/urlcleaner/urlcleaner_test.go`,
   `internal/reader/rss/parser_test.go`,
   `internal/reader/sanitizer/sanitizer_test.go` — i.e. the word appears in
   sample data, never in ingestion logic.

3. **Every integration is outbound.** `internal/integration/` contains 30
   integrations (apprise, archiveorg, betula, cubox, discord, espial, instapaper,
   karakeep, linkace, linkding, linktaco, linkwarden, matrixbot, notion, ntfy,
   nunuxkeeper, omnivore, pinboard, pushover, raindrop, readeck, readwise,
   rssbridge, shaarli, shiori, slack, telegrambot, wallabag, webhook).
   These are all *share/save this entry to X* destinations. There is no inbound
   email connector. (<https://github.com/miniflux/v2/tree/main/internal/integration>)

4. **The docs never mention it.** Neither the docs index
   (<https://miniflux.app/docs/index.html>), the FAQ
   (<https://miniflux.app/faq.html>), nor the third-party apps page
   (<https://miniflux.app/docs/apps.html>) mentions email, newsletter, IMAP or
   SMTP ingestion. The third-party apps page lists only readers/frontends
   (Fleuron, FluxNews, Microflux, Fluxjs, ReactFlux, Reminiflux, Miniflux-digest,
   Miniflux-ai).

**The community answer is to put kill-the-newsletter in front of Miniflux.** The
only newsletter-titled issue in the tracker is
[miniflux/v2#1496](https://github.com/miniflux/v2/issues/1496), *"Boxes around
elements of articles when using kill the newsletter"* — a closed rendering bug
from a user already running that exact pipeline. That is the established
pattern, and it confirms Miniflux's role is strictly the reader end.

**Implication for Agregado:** "use Miniflux" is not an alternative to Agregado's
email path — Miniflux would sit *downstream* of whatever email→feed bridge is
chosen, and would inherit that bridge's content fidelity and synthetic links
unchanged. It removes nothing from the problem.

---

## 3. Other self-hostable email→feed bridges

### 3a. LetterFeed (LeonMusCoden/LetterFeed) — IMAP polling

<https://github.com/LeonMusCoden/LetterFeed> · Python, MIT, 203 stars, last push
2026-07-06, latest release **v0.5.0 (2025-09-06)**.

**Transport: IMAP polling, not SMTP.** *"It periodically scans your email inbox
via IMAP for new emails from the senders you've configured."* (`README.md`)
Prerequisites are just *"An existing mailbox with IMAP over SSL on port 993"* plus
Docker Compose — **by far the lightest self-host story of any option here.** You
bring a mailbox you already own; nothing listens on port 25.

Architecture: FastAPI + SQLAlchemy + Alembic backend, React frontend, `docker
compose up -d`. It connects with `imaplib.IMAP4_SSL`, searches `(UNSEEN)`, and
dedupes on `Message-ID`, skipping messages that lack one
(`backend/app/services/email_processor.py`). Newsletters are grouped by sender,
with optional auto-add of new senders. It produces both per-newsletter feeds and
a master feed (`backend/app/services/feed_generator.py`).

**Content fidelity: substantially LOSSY — the worst of the surveyed options.**
`_get_email_body` correctly prefers `text/html` over `text/plain`, but
`_extract_and_clean_html` then does two destructive things:

```python
doc = Document(clean_html_str)
extracted_body = doc.summary(html_partial=True)
```

That is **readability** (`Document.summary()`) — a heuristic main-content
extractor that discards everything it judges to be boilerplate. Then an nh3
allowlist sanitiser:

```python
ALLOWED_TAGS = {"p","strong","em","u","h3","h4","ul","ol","li","a","img","br",
                "div","span","figure","figcaption"}
ALLOWED_ATTRIBUTES = {"a": {"href","title"}, "img": {"src","alt","width","height"}, "*": {"style"}}
cleaned_body = nh3.clean(extracted_body, tags=ALLOWED_TAGS, attributes=ALLOWED_ATTRIBUTES)
```
(both `backend/app/services/email_processor.py:112-140`)

Two consequences worth flagging:
- **`table` is not in the allowlist.** The overwhelming majority of HTML
  newsletters are table-based layouts. Readability plus table-stripping on
  table-layout mail is a lossy combination with unpredictable results.
- **`h1` and `h2` are not in the allowlist** either (only `h3`/`h4`), so heading
  hierarchy is mangled.
- `a[href]` *does* survive, so link extraction downstream is still possible on
  whatever readability kept.

**Canonical URL: none at all.** `_add_entries_to_feed` sets id, title, content,
published and updated — and **never calls `fe.link()`**:

```python
fe.id(f"urn:letterfeed:entry:{entry.id}")
fe.title(...)
fe.content(entry.body, type="html")
```
(`backend/app/services/feed_generator.py`)

Entries carry a `urn:letterfeed:entry:<id>` URN and **no `<link>` element**.
Feed-level `rel="alternate"` points at the LetterFeed app root. So a consumer
gets no per-entry URL whatsoever — strictly worse than KTN's synthetic-but-real
URL.

### 3b. Cloudflare Email Workers — the DIY path (what Agregado already does)

Official docs: <https://developers.cloudflare.com/email-routing/email-workers/>

The primitive: you add an `email()` handler to a Worker and Cloudflare invokes
it on inbound mail. The message is a `ForwardableEmailMessage` exposing sender,
recipient, headers, and **`raw` as a `ReadableStream` of the complete MIME
content**; the docs recommend parsing it with `postal-mime`.

Limits (<https://developers.cloudflare.com/email-routing/limits/>):

| Limit | Value |
|---|---|
| Inbound message size | **25 MiB** (vs KTN's 512 KiB — ~50× headroom) |
| Destination addresses | 200 per account |
| Routing rules | 200 per domain |
| Custom headers | 16 KB combined |
| Domains per zone | 30 |
| Cost | Email Routing is included with Cloudflare accounts |

This is the **best raw-material path**: full MIME, all headers, 25 MiB ceiling,
no SMTP server to operate, no port 25, no TLS cert plumbing. The cost is that
*you* write the parser — there is no feed generation, no storage, no retention.
Which is precisely the trade Agregado has already made.

Two public examples of this shape, useful as reference points rather than
candidates:

**yl8976/Email-to-RSS** — <https://github.com/yl8976/Email-to-RSS> · TypeScript,
MIT, 63 stars, last push 2026-02-09, **no tagged releases**. Uses **ForwardEmail
webhooks** (not Cloudflare Email Routing) POSTing to `/api/inbound` on a Worker,
with KV for storage and an admin UI. Per-feed addresses are human-memorable
triples like `apple.mountain.42@yourdomain.com` (`README.md`).
- *Content:* verbatim — `const content = payload.html || payload.text || '';`
  (`src/utils/email-parser.ts:42`), stored and emitted unmodified.
- *Canonical URL:* **synthetic and, as far as I can tell, dead.**
  `feed.addItem({ ..., link: `${baseUrl}/emails/${uniqueId}` })`
  (`src/utils/feed-generator.ts`) — but `src/index.ts` only registers `/api`,
  `/rss`, `/admin`, `/` and a catch-all `app.all('*', (c) => c.text('Not Found',
  404))`. There is **no `/emails/:id` route**, so every entry link 404s.
- Feed is capped at the **last 20 emails** (`emails.slice(0, 20)`,
  `src/routes/rss.ts`).
- *Verdict:* useful as a worked example of the Worker shape; not production-grade.

**bytemain/mail2rss** — <https://github.com/bytemain/mail2rss> · JavaScript,
73 stars, **last push 2023-03-26** (stale). A single 300-line
`mail2rss.js` Cloudflare Worker. **Not actually self-hosted**: mail is received
and stored by **testmail.app**, a third-party SaaS, and the Worker just queries
its GraphQL API. Per the README the free tier is *"100 emails per month, and the
email are saved for one day"* — **one-day retention**, disqualifying on its own.
- *Content:* full HTML in CDATA — `<description><![CDATA[${value.html ? value.html : value.text}]]></description>`,
  with `cid:` inline-image references rewritten to testmail download URLs.
- *Canonical URL:* `<link>${value.downloadUrl}</link>` — testmail's raw `.eml`
  download URL, which expires with the 1-day retention. `<guid isPermaLink="false">`.

### 3c. atomail (remko/atomail) — historical, do not use

<https://github.com/remko/atomail> · Python, 67 stars, **last push 2019-02-28**,
no releases, no licence file.

Single script, very flexible transports — pipe from procmail, mbox/maildir,
POP3, IMAP, or NNTP (`README.md`). Its own README says *"AtoMail is still in
alpha stage, and that it probably contains bugs."*

**It is Python 2 and will not run on Python 3.** The source uses `unicode(...)`,
`cgi.escape(...)` (removed in 3.8) and `contents.sort(lambda x, y: cmp(x,y))`
(`cmp` and 2-arg sort removed in Python 3) — `atomail.py:103`, `:249`, `:257`.

- *Content:* preserves HTML verbatim as an Atom text node with
  `content.setAttribute('type', content_type)` where `content_type` is `html`
  when a `text/html` part exists; plaintext-only mail is wrapped in `<pre>` and
  escaped (`atomail.py:245-262`). Fidelity would be good if it ran.
- *Canonical URL:* **entries have no `<link>` at all** — only a feed-level link
  set from the `--uri` flag (`atomail.py:186`). Same failure mode as LetterFeed.

### 3d. "Feedmail" — a naming trap, not an option

The ticket names "Feedmail". **Every project by that name goes the opposite
direction — RSS→email, not email→RSS.** Searching GitHub for `feedmail`:
`metachris/feedmailer` (RSS to Email, 2011), `zsau/feedmail` (*"RSS/Atom feed
client that converts items to emails"*, 2020), `leviself/Feedmailer` (2009),
`jpoehls/feedmailer` (*"An RSS to Email utility"*, 2015). Same for the
better-known `rss2email/rss2email`, `agorf/feed2email`, `fgeller/feeder`,
`ElliotKillick/rss2newsletter`. None is relevant. Worth recording so the name
isn't chased again.

### 3e. Also checked, also negative

- **FreshRSS** (15.8k stars, active) — an aggregator, not an email bridge; same
  position as Miniflux.
- **RSS-Bridge** (9.2k stars, active, PHP) — generates feeds by **scraping
  websites** that lack them. It does not receive email.
- **`0x2E/fusion`, `nkanaev/yarr`** — readers, no email ingestion.

---

## 4. Content quality delivered to a downstream AI enricher

Ranked best to worst:

1. **Cloudflare Email Workers (Agregado today)** — full MIME, all headers, 25 MiB.
   Nothing is mediated. *But see the caveat in §5 about what Agregado then does
   with it.*
2. **kill-the-newsletter** — complete original HTML, escaped into
   `<content type="html">`, no sanitisation. Only losses: the 512 KiB ingest
   ceiling (whole messages rejected), the aggressive size-based retention, an
   appended KTN footer, and the loss of every header except From/Subject/Date
   (so `Archived-At` is gone — see §5).
3. **yl8976/Email-to-RSS** — verbatim `payload.html`, capped at 20 entries.
4. **bytemain/mail2rss** — verbatim HTML in CDATA, but 1-day retention upstream.
5. **atomail** — verbatim HTML, if it ran on a modern Python. It does not.
6. **LetterFeed** — readability extraction *plus* an allowlist that drops
   `table`, `h1`, `h2`. Real, unpredictable content loss on exactly the
   table-layout HTML newsletters are made of.

The general rule this survey confirms is the one ADR-0004 already states:
*store the raw artifact at the ingestion boundary, derive downstream.* The
options that sanitise or extract at ingestion (LetterFeed) destroy information
irrecoverably; the ones that pass HTML through (KTN, the Worker paths) leave
every downstream choice open.

---

## 5. The canonical URL problem

### What each option produces for the entry link

| Option | Entry `<id>` | Entry `<link>` | Tries to find the real URL? |
|---|---|---|---|
| kill-the-newsletter | `urn:kill-the-newsletter:<id>` | `https://<host>/feeds/<feed>/entries/<entry>.html` — synthetic, real, renders stored HTML | **No** |
| LetterFeed | `urn:letterfeed:entry:<id>` | **absent** | **No** |
| yl8976/Email-to-RSS | `<ts>-<b64 subject>` | `<base>/emails/<id>` — synthetic and **404s** | **No** |
| bytemain/mail2rss | testmail id, `isPermaLink="false"` | testmail `.eml` download URL, expires in 1 day | **No** |
| atomail | `Message-ID` | **absent** | **No** |
| **Agregado (per ADR-0004)** | article id | `canonical_url` → real newsletter URL, else reader page | **Yes** |

**Not one third-party option attempts canonical-URL recovery.** Every one either
invents a URL pointing back at itself or omits the link entirely. This is the
clearest finding of the whole survey.

### What Agregado does, per ADR-0004 and the code today

`docs/adr/0004-newsletter-canonical-url-extraction.md` specifies
`resolveCanonicalURL` with a priority chain, implemented at
`internal/ingestion/email/parser.go:71`:

1. **`Archived-At` header (RFC 5064)** — the per-message archive URL, an exact
   answer when the sender provides one, trimmed of `<>`.
2. **A "view in browser" anchor scraped from the HTML with goquery** —
   `scrapeViewInBrowser` (`parser.go:90`) matches on the *anchor's visible text*
   rather than the href, deliberately, so a tracking-wrapped URL still resolves:
   *"Matched on the human label rather than the href so a tracking-wrapped URL
   still resolves — the label is what senders keep readable."* (`parser.go:116-118`)
3. **Nothing** → `nil`, and `/r/{id}` falls back to the reader page.

Both branches are gated by one acceptance predicate `isCanonicalCandidate`
(`parser.go:147`): `http(s)` only, rejecting `unsubscribe`, `pixel`, `mailto:`
and social-share links. `List-Archive` (RFC 2369) is deliberately *not* tried —
it points at the archive index, not the specific issue.

**Agregado is meaningfully ahead of the field here.** No surveyed option does
step 1 or step 2.

### Two consequences that matter for any adoption decision

**(a) Routing email through KTN would destroy the `Archived-At` signal.**
KTN's Atom output carries only what it stored: id, link, published, updated,
author, title, content (`source/index.mts:601-668`). Original email headers are
**not preserved anywhere** — `Archived-At` is read by nobody and dropped at
`simpleParser`. So the highest-priority, most reliable branch of Agregado's
extraction chain would become permanently unavailable. Only the HTML-scraping
fallback would survive, and it would run against KTN-appended-footer HTML.

**(b) The ADR has drifted from the code — worth noting for the downstream decision.**
ADR-0004 §Decision.2 says *"`external_url` keeps its `newsletter:<uuid>`
placeholder."* That is **no longer true.** The sentinel has been removed
(Phase 21, issue [#3](https://github.com/ffrt-labs/agregado/issues/3)):

- `internal/ingestion/email/parser.go:52-57`: *"ExternalURL is left nil: a
  newsletter has no web page of its own. ... Before Phase 21 this held a
  'newsletter:<uuid>' sentinel only to satisfy a NOT NULL column (issue #3)."*
- `internal/api/articles.go:50-55` resolves `CanonicalURL` first and returns
  `("", false)` otherwise.
- `internal/api/articles_test.go:246-251` asserts no `newsletter:` string leaks
  into any response body.

The rest of ADR-0004 holds: `newsletter_raw_html` is a separate table written by
the storage worker after `Create` yields an id
(`internal/storage/worker.go:77-79`, `internal/storage/raw_html_repo.go:21-23`,
migration 000015).

**(c) A subtlety about Agregado's own content fidelity.** The parser stores
`RawHTML: payload.Html` verbatim (`parser.go:59`) — good — but the `Content`
field the enricher reads is `html2text.FromString(payload.Html)`
(`parser.go:38`), i.e. **stripped plaintext**, falling back to `payload.Text`.
So the enricher today receives plaintext even though full HTML is sitting in
`newsletter_raw_html`. Adopting KTN would hand the enricher *HTML* where it
currently gets *text* — but Agregado could get the same win with no new
infrastructure by reading its own raw-HTML table. Worth weighing before treating
"better content for the enricher" as an argument for a bridge.

---

## 6. Comparison table

| Option | Self-hostable | Content fidelity to enricher | Canonical URL produced | Operational weight | Maturity |
|---|---|---|---|---|---|
| **Agregado today** (Cloudflare Email Routing → webhook) | Yes — already running | Raw HTML stored verbatim (25 MiB cap); enricher currently reads `html2text` **plaintext**, though raw HTML is available in `newsletter_raw_html` | **Real URL**: `Archived-At` > "view in browser" anchor > `nil` → reader page | Low — no SMTP, no port 25, no certs; Cloudflare-coupled | In production; ADR-0004 accepted 2026-07-23 (§Decision.2 now stale) |
| **kill-the-newsletter** | Yes, MIT — but needs own SMTP on :25, MX records, Caddy-issued TLS paths, `User=root` systemd unit; 7-line README delegating to external generic guides; no Dockerfile | **Best of the bridges** — full original HTML escaped into `<content type="html">`, unsanitised; **but 512 KiB ingest cap** rejects large newsletters, and a KTN footer is appended | **Synthetic only** — `https://<host>/feeds/<f>/entries/<e>.html`; no attempt at the real URL; **drops all original headers, so `Archived-At` is lost** | **High** — you operate a public mail server | Mature & popular (3.1k★, MIT, active); one 1999-line file, 4 deps; v2.0.9 Jan 2026 after an 18-month gap |
| **Miniflux** | Yes (reader) | **N/A — cannot ingest email.** RDF/RSS/Atom/JSON over HTTP only | N/A — inherits whatever the upstream bridge emits | N/A | Very mature, very active (9.6k★, 2.3.3 Jul 2026) |
| **LetterFeed** | **Yes, easiest** — Docker Compose + any IMAP mailbox; no port 25 | **Worst** — readability `Document.summary()` + nh3 allowlist dropping `table`/`h1`/`h2`; `a[href]` survives | **None** — entries have **no `<link>`**, only `urn:letterfeed:entry:<id>` | **Low** — polls IMAP you already own | Young (203★, v0.5.0 Sep 2025, active Jul 2026) |
| **Cloudflare Email Workers (DIY)** | Yes | **Best possible** — full MIME `raw` stream, all headers, 25 MiB | **Whatever you implement** — this is why Agregado can do `Archived-At` | Low infra, **high code ownership** — you write parser, storage, retention | Vendor-stable primitive; official docs |
| **yl8976/Email-to-RSS** | Yes (Cloudflare + ForwardEmail) | Verbatim `payload.html`; last 20 entries only | Synthetic **and broken** — `/emails/:id` route not registered, 404s | Low-medium | Immature — 63★, no releases, last push Feb 2026 |
| **bytemain/mail2rss** | **No** — mail lives in testmail.app SaaS | Verbatim HTML in CDATA, but **1-day retention**, 100 mails/mo free | testmail `.eml` URL that **expires in 1 day** | Very low | **Stale** — 73★, last push Mar 2023 |
| **atomail** | Yes in principle | Verbatim HTML — **but Python 2, will not run** | **None** — no entry `<link>` | Low | **Dead** — last push 2019, self-described alpha, no licence |
| **"Feedmail"** | — | — | — | — | **Does not exist** as email→feed; all such projects are RSS→email |

---

## 7. Summary of findings

1. **Agregado's current approach is not behind the state of the art — it is
   ahead of it on the hardest axis.** No third-party email→feed bridge attempts
   canonical-URL recovery. All of them either point the entry link back at
   themselves or emit no link at all.
2. **Miniflux is not an alternative.** It has no email ingestion of any kind and
   would sit downstream of a bridge, inheriting its synthetic links unchanged.
3. **kill-the-newsletter has the best content fidelity of any bridge** and is the
   only mature one, but adopting it costs: running a public SMTP server, a
   512 KiB message ceiling, aggressive size-based retention that can hold as few
   as 2-4 entries, and the **permanent loss of the `Archived-At` header** that is
   the top of Agregado's extraction chain.
4. **The content-quality gap Agregado has is self-inflicted and cheap to close.**
   The enricher reads `html2text` plaintext while the full HTML sits unused in
   `newsletter_raw_html`. That is a bug-shaped opportunity, not a reason to adopt
   a bridge.
5. **ADR-0004 §Decision.2 is stale** — the `newsletter:<uuid>` sentinel was
   removed in Phase 21 (issue #3); `external_url` is now `nil` for newsletters.

## 8. Open questions for the downstream decision

- Should the enricher read `newsletter_raw_html` instead of the `html2text`
  `Content` field? (Independent of any bridge choice.)
- Is a "view in browser" URL the *right* canonical URL at all, or is the real
  goal per-link fan-out — one email containing many links becoming *many* entries
  rather than one? Nothing surveyed does this; it is a genuinely open design space.
- Should ADR-0004 be amended (or superseded) to reflect the Phase 21 sentinel
  removal?

---

## Appendix: sources

**Primary repos/source read directly**
- `leafac/kill-the-newsletter` — `source/index.mts`, `package.json`,
  `CHANGELOG.md`, `README.md`, `configuration/example.mjs`,
  `configuration/kill-the-newsletter.service`
- `radically-straightforward/radically-straightforward` — `html/README.md`
- `miniflux/v2` — `internal/reader/parser/format.go`, `internal/integration/`
- `LeonMusCoden/LetterFeed` — `backend/app/services/email_processor.py`,
  `backend/app/services/feed_generator.py`, `README.md`
- `yl8976/Email-to-RSS` — `src/utils/email-parser.ts`,
  `src/utils/feed-generator.ts`, `src/routes/rss.ts`, `src/index.ts`, `README.md`
- `bytemain/mail2rss` — `mail2rss.js`, `README_EN.md`
- `remko/atomail` — `atomail.py`, `README.md`
- `ffrt-labs/agregado` — `internal/ingestion/email/parser.go`,
  `internal/api/articles.go`, `internal/storage/worker.go`,
  `internal/storage/raw_html_repo.go`, `docs/adr/0004-newsletter-canonical-url-extraction.md`

**Official docs**
- <https://developers.cloudflare.com/email-routing/email-workers/>
- <https://developers.cloudflare.com/email-routing/limits/>
- <https://miniflux.app/docs/index.html>, <https://miniflux.app/faq.html>,
  <https://miniflux.app/docs/apps.html>
- <https://kill-the-newsletter.com>

**Repo metadata** via `api.github.com/repos/<owner>/<repo>` and
`api.github.com/repos/<owner>/<repo>/releases`, retrieved 2026-08-22.
