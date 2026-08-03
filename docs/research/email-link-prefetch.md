# Research — Do mail clients prefetch links in HTML email?

Resolves [#41](https://github.com/ffrt-labs/agregado/issues/41). Charted by map [#40](https://github.com/ffrt-labs/agregado/issues/40).
Date: 2026-08-03.

> **Location note.** The repo has no existing convention for research notes
> (`docs/` holds plans, ADRs, the PRD and the study log; the old
> `docs/wayfinder/tickets/` files are gone). This file establishes
> `docs/research/<topic>.md` as the place for them. Move it if a different
> convention lands.

---

## Bottom line

**No — a `GET /f/{uuid}/{up|down}` that writes directly is not dangerous for
this deployment.** Ship it.

**Confidence: high** for this deployment. **Medium** as a general statement
about email at large — in a Defender-licensed corporate tenant the same route
would be genuinely unsafe.

The decisive evidence is not a vendor document. It is that **this exact
experiment has already been running in production for months**: `GET /r/{uuid}`
is published on `read.<domain>`, appears as the headline link of every article
in every digest, and *writes* (`MarkRead`) before redirecting. If anything in
the path between the digest sender and this Gmail inbox issued unsolicited
`GET`s to anchor hrefs, every article in every digest would be marked read on
delivery and silently vanish from the next digest (`FindUnreadSince` filters
`is_read = false`). See [Local evidence](#6-local-evidence-the-canary-already-running)
— that behaviour has not been reported, and it would be hard to miss.

The general picture and the caveats that would flip this answer are below.

---

## 1. The general picture, in one table

| Actor | Fetches anchor `href`s the user did not click? | Applies to this deployment? | Source trust |
|---|---|---|---|
| Gmail image proxy (`googleusercontent.com`) | **No** — images only, on message open | Yes, but harmless | Primary (Google) |
| Gmail / Google Safe Browsing URL checks | **No** — hash-prefix database lookup, not a fetch | Yes, but harmless | Primary (Google) |
| Google Workspace pre-delivery scanning / Gmail Enhanced Safe Browsing | **Unspecified**; Workspace-admin features, not consumer Gmail | **No** — personal Gmail, no Workspace tenant | Primary but vague |
| Apple Mail Privacy Protection | **No** — "remote content", i.e. images/subresources | Not in the read path today | Primary (Apple), thin |
| **Microsoft Defender SafeLinks** | **Yes** — "detonated asynchronously in the background", pre-click | **No** — requires a Defender for Office 365 licence | Primary (Microsoft), explicit |
| Consumer Outlook.com Safelinks | Warning at click; no documented pre-click fetch | No | Primary (Microsoft), thin |
| Corporate gateways (Proofpoint, Mimecast, Barracuda) | Yes for some, at delivery and/or click | **No** — no gateway in front of this inbox | Vendor docs / marketing, **lower confidence** |
| Link-preview unfurlers (Slack, chat apps) | **Yes**, on post | Only if a digest is pasted into a chat | Primary (Slack) |
| Anti-spam software fetching **header** URIs | **Yes** — the documented reason RFC 8058 exists | Yes in principle — but headers, not body links | Primary (IETF) |

---

## 2. Gmail

### 2.1 The image proxy is images, and only images

Google's Workspace admin documentation for the image URL proxy:

> "When your users open email messages, Gmail uses Google's secure proxy servers
> to serve images that might be included in these messages."

> "Because of the image proxy, links to images that are dependent on internal
> IPs and sometimes cookies are broken."

Two facts follow. The proxy's subject is **images**; "links" appears only in the
phrase "links to images". And it fires **when the user opens the message**, not
at delivery. There is no statement anywhere in Google's proxy documentation
about anchor `href`s.

Source: [Set up an image URL proxy allowlist](https://knowledge.workspace.google.com/admin/gmail/advanced/set-up-an-image-url-proxy-allowlist)
(Google Workspace Admin Help). **Primary.**

### 2.2 Google's URL safety checks are lookups, not visits

Safe Browsing — the mechanism behind Gmail's "this link is suspicious" interstitials —
works by shipping a hashed blocklist to the client:

> "The Update API lets your client applications download hashed versions of the
> Safe Browsing lists for storage in a local database. URLs can then be checked
> locally."

> "To save bandwidth, clients download the hash prefixes of URLs rather than the
> raw URLs."

A hash-prefix comparison against a downloaded database issues **no request to
the URL being checked**. Even the fallback on a prefix collision is a request to
*Google*, not to the target host.

Source: [Safe Browsing Update API (v4)](https://developers.google.com/safe-browsing/v4/update-api),
Google for Developers. **Primary.**

### 2.3 What Google does *not* document

Two Google features are described in terms too vague to rule anything in or out:

- **Enhanced pre-delivery message scanning** (Workspace admin setting): "When
  Gmail detects suspicious content, message delivery is slightly delayed so that
  Gmail can do additional security checks on the message." It does not say what
  the checks are.
  ([source](https://knowledge.workspace.google.com/admin/security/help-prevent-phishing-with-pre-delivery-message-scanning))
- **Gmail Enhanced Safe Browsing** (Workspace admin setting): Gmail "takes
  additional steps to check messages for harmful content before delivering
  messages to recipients." Again, unspecified.
  ([source](https://knowledge.workspace.google.com/admin/gmail/advanced/turn-gmail-enhanced-safe-browsing-on-or-off))

Both are **Google Workspace administrator settings**. A personal `@gmail.com`
inbox has no Workspace tenant and no admin console, so neither is configurable
or applicable here. Whatever they do, they are out of scope for this deployment.

**Honest statement of the evidence.** Google nowhere publishes "we do not fetch
links in message bodies". The Gmail conclusion rests on (a) the documented scope
of the proxy being images, (b) Safe Browsing being a lookup rather than a fetch,
and (c) the local empirical evidence in §6. It is an argument from documented
scope plus observation, not from an explicit denial. Weight it accordingly —
though §6 is strong enough to carry it alone.

---

## 3. Apple Mail Privacy Protection

Apple's user documentation:

> "remote content is privately downloaded in the background when you receive a
> message (instead of when you view it)"

> "your IP address is hidden from senders"

The operative term is **remote content** — the subresources a mail renderer
loads to display the message: images, and anything else the HTML references for
rendering. An anchor `href` is not loaded to render a message; it is loaded when
followed. Apple's documentation never mentions links, and MPP's stated purpose —
defeating open-tracking pixels and IP geolocation — has nothing to do with
anchor targets. Fetching every link in every email would be a spectacular
bandwidth and correctness problem Apple would have had to document.

Source: [Protect email privacy in Mail on Mac](https://support.apple.com/guide/mail/protect-email-privacy-mlhlp1205/mac)
and [Use Mail Privacy Protection on iCloud.com](https://support.apple.com/guide/icloud/use-mail-privacy-protection-mm90f7d05c96/icloud).
**Primary, but thin** — these are consumer help pages, not a technical
specification. Apple publishes no engineering-level description of MPP's fetch
behaviour.

**Relevance here:** none directly. The digest lands in Gmail. MPP would only
enter the picture if the digest were forwarded to an iCloud address or the
inbox were added to Apple Mail — and even then the finding is "no".

---

## 4. Microsoft Defender SafeLinks — the strongest suspect, and it is real

This is the one actor with an explicit, first-party statement that it fetches
URLs before any user clicks them.

### 4.1 It detonates URLs pre-click

From Microsoft's Safe Links overview, describing what happens when Safe Links
scanning for email is turned on:

> "URLs that don't have a valid reputation are detonated asynchronously in the
> background."

And:

> "As long as Safe Links protection is turned on, URLs are scanned prior to
> message delivery, regardless of whether the URLs are rewritten or not."

"Detonated" is Microsoft's term for opening the URL in a sandbox. This is an
unambiguous, vendor-documented, **pre-click `GET` to a link in an email body**.
Note it is scoped to URLs "without a valid reputation" — a brand-new
`read.<domain>` tunnel hostname with no reputation history is precisely the
profile that gets detonated.

There is also an optional setting, **"Apply real-time URL scanning for
suspicious links and links that point to files"**, with a sub-option **"Wait for
URL scanning to complete before delivering the message"** — messages "are held
until scanning is finished". Both are on in the recommended (Standard/Strict)
configurations.

### 4.2 It follows redirects

Not stated as such in the overview, but implied by the design: Safe Links is a
redirector (`https://<region>.safelinks.protection.outlook.com`) whose entire
job is to resolve where a URL ends up before letting the user go there, and it
scans "links that point to files" — which requires following to the file. The
Teams section states the validation page "redirects to the target page". Treat
"SafeLinks follows redirects" as **likely but not directly quoted** from the
overview page.

Practical consequence for any design that leans on a redirect: a redirect does
not launder a write. If `GET /f/…` 302s to a confirmation page, a detonator that
follows the redirect has already triggered the write on the first hop.

### 4.3 It requires a Defender licence — this is the crux for us

Microsoft puts this at the very top of the article:

> "This article is intended for business customers who have Microsoft Defender
> for Office 365. If you're using Outlook.com, Microsoft 365 Family, or
> Microsoft 365 Personal, and you're looking for information about Safelinks in
> Outlook.com, see Advanced Outlook.com security for Microsoft 365 subscribers."

And on defaults:

> "Although there's no default Safe Links policy, the **Built-in protection**
> preset security policy provides Safe Links protection in e-mail messages …
> to all recipients **for customers that have at least one Defender for Office
> 365 license**."

So the accurate statement is: **there is no default Safe Links *policy*, but
Built-in protection turns it on for everyone in any tenant holding at least one
Defender for Office 365 licence.** Effectively on-by-default *given the licence*;
entirely absent without it.

The consumer article confirms the split: all Outlook.com users get "spam and
malware filtering", but "For Microsoft 365 Family and Microsoft 365 Personal
subscribers, Outlook.com performs extra screening", after which "links in your
email might look different … include text such as
`na01.safelinks.protection.outlook.com`". The consumer page describes a warning
**on click** and does not document background detonation.

Sources: [Complete Safe Links overview](https://learn.microsoft.com/en-us/defender-office-365/safe-links-about)
and [Advanced Outlook.com security for Microsoft 365 subscribers](https://support.microsoft.com/office/882d2243-eab9-4545-a58a-b36fee4a46e2).
**Primary (Microsoft Learn).**

**Relevance here:** **zero.** There is one recipient, on personal Gmail. No
Defender licence, no Exchange Online tenant, no Safe Links policy, no Built-in
protection. This is the textbook case of a risk that is entirely real in general
and entirely inapplicable to this deployment.

---

## 5. Everything else that fetches links in transit

### 5.1 Corporate gateways and antivirus

Proofpoint, Mimecast and Barracuda all rewrite URLs and sandbox them; Proofpoint
markets both click-time and "predictive" (pre-click) sandboxing. Their detailed
technical pages sit behind a customer login, and the openly available material
is data sheets and blog posts.

**Lower confidence — vendor marketing, not documentation.** Flagging it as the
ticket asked. The shape of the claim is not in doubt (SafeLinks proves the
category exists and Microsoft documents it plainly); only the per-vendor detail
is unverified.

**Relevance here:** none. There is no gateway, no corporate MX, no endpoint AV
mail plugin between the sender and this inbox — the digest goes straight to
Gmail.

### 5.2 Link-preview generators, and forwarding

This is the only general-population risk that could plausibly reach this
deployment, and it arrives through **forwarding**, not delivery.

Slack documents its unfurler explicitly:

> User agent: `Slackbot-LinkExpanding 1.0 (+https://api.slack.com/robots)`
>
> "This robot responds to links that Slack users post into their channels."
>
> "We do not currently honor `robots.txt` files." … "we're acting on behalf of a
> human"

Source: [Slack — robots and crawlers](https://api.slack.com/robots). **Primary.**

So: paste a digest link into Slack, Discord, WhatsApp, Teams or an iMessage
thread and something will `GET` it to build a preview card, with no click and no
`robots.txt` recourse. Note Slack's own framing — "acting on behalf of a human" —
is precisely the reasoning that makes unfurlers *not* respect the safe-method
contract.

What changes on forwarding, generally:

- **The digest's links go wherever the forward goes.** ADR-0001 already records
  this: `PUBLIC_BASE_URL` "stamps a tunnel hostname into every digest email,
  where it lives in Gmail and leaks via any forward".
- **A forward into a Defender tenant re-enters SafeLinks' scope.** Microsoft:
  "If URL rewriting is enabled, the URL is rewritten even if the message is
  *manually* forwarded or replied to", and for *automatic* forwarding the URL is
  rewritten if "the recipient is also protected by Safe Links". So forwarding a
  digest to a work address in a Defender-licensed tenant would put every
  `/f/{uuid}/…` link in front of a background detonator.
- **UUIDs are the only secret.** Anyone holding a forwarded digest can vote,
  repeatedly, forever. That is not a prefetch risk — it is the pre-existing
  bare-UUID exposure ADR-0001 already accepts for `/r/` and `/articles/`, and
  #10's Q2 is where it belongs.

**Practical read for a single-user digest:** the forwarding risks are
self-inflicted and bounded. Felipe forwarding his own digest into a Slack
channel would cost a handful of spurious votes on his own feed. It is not a
security hole; it is a data-quality nuisance with a known trigger.

---

## 6. Local evidence: the canary already running

This is the strongest evidence in the file, and it is not a document.

`internal/api/server.go:117` routes `GET /r/{id}` to `ArticleHandler.Open`
(`internal/api/articles.go:214-237`), whose comment states the design plainly:
"it marks the article read, then redirects". The handler calls
`a.articles.MarkRead(...)` **before** issuing the 302.

ADR-0001 records that this path is published to the internet:

| Hostname | Ingress regex | Serves |
|---|---|---|
| `read.<domain>` | `^/(r\|articles)/[0-9a-f-]{36}/?$` | Digest click-through |

and states the consequence outright:

> "`/r/{uuid}` and `/articles/{uuid}` are not read-only: both call `MarkRead`.
> That is intended — the read signal is the point — but it means the public
> surface already includes a state mutation."

The digest email template links every article headline at
`{{ $.DigestURL }}/r/{{ $item.ID }}` (`internal/digest/templates/digest.html:110`,
embedded at `internal/digest/generator.go:17`).

And digest selection filters on read state — `ArticleRepo.FindUnreadSince`
(`internal/storage/article_repo.go:161-175`):

```sql
SELECT * FROM articles
WHERE COALESCE(published_at, ingested_at) > $1
  AND is_read = false
  AND relevance_score >= $2
```

**Therefore:** since Phase 19, an unauthenticated, publicly routable, writing
`GET` has been the headline link of every article in every digest delivered to
this Gmail inbox. If Gmail — or anything else in that path — prefetched anchor
hrefs, the failure would be loud and cumulative: every article marked read the
moment the digest arrived, articles showing as read in the web UI that were
never opened, and every subsequent digest silently dropping them.

That has not happened. **`GET /f/{uuid}/{up|down}` is the same experiment
already passing.**

> **Verify before trusting this, if you want certainty.** Two cheap checks:
> ① `SELECT COUNT(*) FROM articles WHERE is_read = true AND read_at < <digest send time + 5 min>`
> grouped by digest window — phantom reads clustering right after delivery is
> the signature; ② grep the access log for `GET /r/` requests whose user-agent
> is not Felipe's browser, or that arrive seconds after a digest send. Neither
> is required to accept the verdict, but ② in particular would upgrade this from
> "no reported symptoms" to "measured".
>
> One weakness worth naming: nobody has been *looking*. This is absence of a
> reported symptom, not a monitored null result. The symptom is conspicuous
> enough (a permanently empty digest) that it is unlikely to have gone unnoticed
> — but it is not the same as a measurement.

---

## 7. RFC 8058 — the prior art, and how far it transfers

RFC 8058 (*Signaling One-Click Functionality for List Email Headers*, IETF
Standards Track, 2017) exists for exactly the reason this ticket suspects.

### What the standard concluded

The problem statement, verbatim from §1:

> "But anti-spam software often fetches all resources in mail header fields
> automatically, without any action by the user, and there is no mechanical way
> for a sender to tell whether a request was made automatically by anti-spam
> software or manually requested by a user."

The industry's pre-RFC mitigation — and this is the direct ancestor of the
confirmation-page idea in #10's Q1:

> "To prevent accidental unsubscriptions, senders return landing pages with a
> confirmation step to finish the unsubscribe request. A live user would
> recognize and act on this confirmation step, but an automated system would
> not. That makes the unsubscription process more complex than a single click."

The RFC's answer was to switch the method rather than add a confirmation step:
`List-Unsubscribe-Post: List-Unsubscribe=One-Click`, a `POST` the sender can
distinguish from a scanner's `GET`. Related normative details worth carrying
over:

- **No redirects.** "The mail sender MUST NOT return an HTTPS redirect, since
  redirected POST actions have historically not worked reliably, and many
  browsers have turned redirected HTTP POSTs into GETs."
- **No ambient credentials.** "The POST request MUST NOT include cookies, HTTP
  authorization, or any other context information."
- **The URI must be unguessable.** §6 warns of "attacks in which a malicious
  party sends spam with List-Unsubscribe links for a victim list, with the
  intention of causing list unsubscriptions … or where the attacker does POSTs
  directly to the mail sender's unsubscription server", and requires an "opaque
  or hard-to-forge component".
- And, on the relative risk of the two methods (§6):
  > "it's been possible to provoke GET requests in a similar way for a long
  > time (and much easier, **due to spam filter auto-fetches**)"

Source: [RFC 8058](https://www.rfc-editor.org/rfc/rfc8058.txt). **Primary (IETF
Standards Track).** Google's bulk-sender requirements mandate it for senders of
>5,000 messages/day
([Email sender guidelines](https://support.google.com/a/answer/81126)) — a
threshold this deployment is roughly 5,000 messages short of.

### Does the reasoning transfer?

**Partly, and the part that does not is the important part.**

The scope RFC 8058 is written against is **URIs in mail header fields** — the
`List-Unsubscribe` header. Every problem statement in the RFC says "header
fields". Header URIs are a tiny, well-known, machine-readable set that a filter
can enumerate and fetch cheaply and safely; a filter that fetched *every anchor
href in every message body* would be a self-inflicted denial-of-service on the
whole web, and would trip every tracking pixel and unsubscribe link it touched.
**RFC 8058 is not evidence that body links are prefetched.** It is evidence that
*header* URIs are, which nobody disputes and which does not apply to a link in
the digest's HTML body.

What *does* transfer:

1. **The category is real.** Automated `GET`s to email URIs happen, are
   documented by the IETF, and defeat naive one-click designs. SafeLinks proves
   the body-link version of it exists too.
2. **The two mitigations are POST-with-distinguishable-method, or a
   confirmation page.** Those are the industry's whole menu, and #10's Q1 has
   already found both.
3. **Redirects do not help.** RFC 8058 rejects them for POST; §4.2 above shows a
   detonator following a 302 already triggered the write on hop one. Any design
   that hides the write behind a redirect is not mitigated.
4. **Whatever ships, the UUID must stay unguessable and the endpoint must not
   rely on cookies.** Both already hold.

And the standards-level framing that the map's closing note is really invoking —
RFC 9110 §9.2.1, *Safe Methods*:

> "The purpose of distinguishing between safe and unsafe methods is to allow
> automated retrieval processes (spiders) and cache performance optimization
> (pre-fetching) to work without fear of causing harm."

> "When a resource is constructed such that parameters within the target URI
> have the effect of selecting an action, it is the resource owner's
> responsibility to ensure that the action is consistent with the request method
> semantics. For example, it is common for Web-based content editing software to
> use actions within query parameters, such as `page?do=delete`. If the purpose
> of such a resource is to perform an unsafe action, then the resource owner
> **MUST** disable or disallow that action when it is accessed using a safe
> request method. Failure to do so will result in unfortunate side effects when
> automated processes perform a GET on every URI reference for the sake of link
> maintenance, pre-fetching, building a search index, etc."

Source: [RFC 9110 §9.2.1](https://www.rfc-editor.org/rfc/rfc9110.txt). **Primary.**

**Be honest about this: `GET /f/{uuid}/up` violates RFC 9110 §9.2.1, and so does
the `GET /r/{uuid}` already in production.** The finding of this ticket is not
that the design is correct — it is that the *specific harm* the rule exists to
prevent does not materialise in *this* delivery path, and that the existing
`/r/` route has been demonstrating that for months. That is a deliberate,
priced, documented deviation, which is a different thing from an oversight. It
should be recorded as such (ADR-0001 already set the precedent by naming
`MarkRead` on a `GET` explicitly rather than letting it pass unremarked).

---

## 8. Verdict, stated twice

### The general case

A publicly routable `GET` that writes, linked from an HTML email, **is unsafe in
the general population of inboxes.** Microsoft documents background detonation of
unreputed URLs; corporate gateways sandbox links; chat unfurlers fetch anything
pasted into them; and RFC 9110 says in normative language not to do this.
Anyone shipping a digest to a mixed audience should use `POST` from a
confirmation page, or accept spurious votes.

### This deployment

**Not dangerous. Ship the direct-writing `GET`.** One recipient, one personal
Gmail inbox, no Workspace tenant, no Defender licence, no mail gateway, and an
identically-shaped writing `GET` (`/r/{uuid}`) already published and already not
being prefetched.

Concretely, this means the confirmation-page mitigation — which per the map's
Established fact 4 would require publishing the **first ever unauthenticated
`POST`** on `read.<domain>`, widening the ACL that ADR-0001 calls the entire
security boundary — is an expensive answer to a problem that does not exist
here. The mitigation is a larger security change than the risk it mitigates.

### Confidence and residual caveats

**High** for this deployment, resting mainly on §6 rather than on any vendor's
promise. Residual caveats, in descending order of realism:

1. **The canary is unmonitored** (§6). Absence of a reported symptom, not a
   measured null. Cheap to upgrade; see the verification box.
2. **Forwarding is the live risk, not delivery.** Pasting a digest into Slack or
   forwarding it to a Defender-protected work address will fetch the vote links.
   Cost is spurious votes on a personal relevance model, recoverable by deleting
   rows. Bound it further if desired: see below.
3. **Vendors change.** Gmail could add link detonation tomorrow and would not
   necessarily document it. The canary would show it first — which is an
   argument for actually watching the canary.
4. **The corporate-gateway section is the weakest** (vendor marketing behind
   logins). It does not affect the verdict, since no gateway is in this path.
5. **This verdict is scoped to one recipient.** The moment the digest goes to a
   second person, especially at a company address, re-open this. The general
   case above becomes the operative one.

### Cheap hedges worth considering anyway (not required by this finding)

These are for #10 to decide; none of them are prefetch mitigations, and none
change the verdict.

- **Make the vote idempotent** (map Established fact 3 — needs a migration).
  Turns "an unfurler voted N times" into "an unfurler voted once", and makes the
  route idempotent in the RFC 9110 §9.2.2 sense even though it stays unsafe.
  This is the highest-value hedge for the effort.
- **`Cache-Control: no-store` on the response**, so no intermediary caches a
  vote result.
- **Log the user-agent on `/f/` hits.** Turns the next occurrence of this
  question from research into a query, and doubles as the §6 canary
  instrumentation.

---

## Sources

**Primary — standards bodies**
- [RFC 8058 — Signaling One-Click Functionality for List Email Headers](https://www.rfc-editor.org/rfc/rfc8058.txt) (IETF Standards Track)
- [RFC 9110 §9.2.1 — HTTP Semantics, Safe Methods](https://www.rfc-editor.org/rfc/rfc9110.txt) (IETF Internet Standard)

**Primary — vendor documentation**
- [Complete Safe Links overview for Microsoft Defender for Office 365](https://learn.microsoft.com/en-us/defender-office-365/safe-links-about) (Microsoft Learn)
- [Advanced Outlook.com security for Microsoft 365 subscribers](https://support.microsoft.com/office/882d2243-eab9-4545-a58a-b36fee4a46e2) (Microsoft Support)
- [Set up an image URL proxy allowlist](https://knowledge.workspace.google.com/admin/gmail/advanced/set-up-an-image-url-proxy-allowlist) (Google Workspace Admin Help)
- [Safe Browsing Update API (v4)](https://developers.google.com/safe-browsing/v4/update-api) (Google for Developers)
- [Help prevent phishing with pre-delivery message scanning](https://knowledge.workspace.google.com/admin/security/help-prevent-phishing-with-pre-delivery-message-scanning) (Google Workspace Admin Help)
- [Turn Gmail Enhanced Safe Browsing on or off](https://knowledge.workspace.google.com/admin/gmail/advanced/turn-gmail-enhanced-safe-browsing-on-or-off) (Google Workspace Admin Help)
- [Email sender guidelines](https://support.google.com/a/answer/81126) (Google Workspace Admin Help)
- [Protect email privacy in Mail on Mac](https://support.apple.com/guide/mail/protect-email-privacy-mlhlp1205/mac) (Apple Support)
- [Use Mail Privacy Protection on iCloud.com](https://support.apple.com/guide/icloud/use-mail-privacy-protection-mm90f7d05c96/icloud) (Apple Support)
- [Slack — robots and crawlers](https://api.slack.com/robots) (Slack API docs)

**Lower confidence — vendor marketing / data sheets**
- Proofpoint Targeted Attack Protection data sheets and blog posts (detailed
  URL Defense documentation is behind a customer login; claims about predictive
  pre-click sandboxing are marketing-sourced and unverified)

**Primary — this repository**
- `internal/api/server.go:117`, `internal/api/articles.go:214-237` (`GET /r/{id}` writes)
- `internal/storage/article_repo.go:161-175` (`FindUnreadSince` filters `is_read = false`)
- `internal/digest/templates/digest.html:110`, `internal/digest/generator.go:17` (digest links)
- `docs/adr/0001-tunnel-ingress-is-the-auth-boundary.md` (ingress regex; the "not read-only" note)
