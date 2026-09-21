# Agregado

A single-user personal content pipeline: sources are ingested, enriched with AI,
filtered, and surfaced — as a curated daily digest and as a browsable feed.
Content that survives the filter can be promoted into a durable personal archive.

This glossary is the project's language. Use these terms exactly; prefer them
over the alternatives listed under _Avoid_.

## Language

### The stream

**Source**:
Something that emits content over time — a feed, or a newsletter arriving by email.
_Avoid_: Feed (ambiguous: a Source is one, but so is the browsable surface)

**Bridge**:
The Cloudflare Worker that converts one inbound newsletter email into a private Atom
feed entry the Reader can poll — one alias, one Source, one feed, per the newsletter's
onboarding. Recovers the canonical URL (ADR-0004), stores the original email durably,
and serves both the feed and a permalink reading surface. Sits upstream of the Reader,
never inside it: from Miniflux's perspective a Bridge-served feed is indistinguishable
from any other Source.
_Avoid_: Worker (names the Cloudflare mechanism, not the domain role), Gateway (implies
routing/proxying — the Bridge transforms and stores, it doesn't just pass through),
Ingestor (too generic — every Source ingests; "Bridge" names specifically the
email→feed conversion)

**Article**:
A single item that arrived through a Source. Lives in the stream and has a horizon:
the Reader prunes old ones.
_Avoid_: Entry, Item, Post

**Enrichment**:
AI-derived data about content — summary, tags, and (for Articles only) a Score.
Distinct from the content itself, and always derived, never authoritative.
_Avoid_: Metadata, Analysis, Processing

**Score**:
An Article's relevance to you, judged against the preference profile, with the reason
behind the judgement. Only Articles have one: a Bookmark has already survived the
judgement a Score exists to make.
_Avoid_: Rating, Rank, Priority

**Reader**:
The system that ingests and enriches Articles. Miniflux fetches, dedupes and stores
every Source (RSS and newsletter alike); Agregado enriches and scores. Miniflux's own
UI is never browsed day to day (ADR-0006) — whether a browsable surface exists anywhere,
and where, is unsettled (see #63).
_Avoid_: Aggregator, Feed reader (as a domain term), Miniflux (Miniflux is the Reader's
storage backend, not the whole Reader)

**Article Index**:
Agregado's own record of a processed Article: Score, tags, distilled summary, and
feedback/read events, keyed by the Article's canonical URL, with Miniflux's entry id
cached alongside for Decoration writeback. Holds no article content — Miniflux is the
sole store of that (ADR-0006). Exists because Miniflux prunes entries by default and has
no field to filter or sort by Score.
_Avoid_: Cache (it is the only durable record of Score and feedback, not a disposable
copy), Mirror

**Decorate**:
Writing Enrichment (Score, summary) back into Miniflux's own copy of an Article via its
API, so it is visible inside Miniflux even though Miniflux is otherwise never browsed.
_Avoid_: Sync, Update (too generic — Decorate is one-directional, Agregado to Miniflux)

**PREFERENCES.md**:
The preference profile a Score is judged against: structured prose (topics wanted,
topics skipped, sources trusted) describing your taste. Agregado reads it from the
explicit `PREFERENCES_PATH` outside this repository — the Bookmarker never scores, so
it has no reason to read it (ADR-0005). Google Drive is authoritative; n8n syncs it to
the homelab cache that Agregado reads. It is personal, editable data, not deployment
configuration. The nightly job directly replaces the accepted Drive document after
validation; Drive revision history is the rollback mechanism. Distinct from CONTEXT.md,
which is this project's glossary, not a taste profile.
_Avoid_: Preferences (too vague — always the file), Profile (ambiguous outside context), Weights (the old `topic_weights` shape this replaces)

**Digest**:
The curated daily email — AI-selected Articles, read every day. The only push surface.
_Avoid_: Newsletter (that is an inbound Source), Roundup

### The archive

**Bookmark**:
Something you **found** and chose to keep: an archived, readable copy of the content
at a URL you encountered. Its identity is that URL. The copy is immutable — it records
what you saved, not what the page says now.
_Avoid_: Save (as a noun), Favourite, Star, Hoard

**Save**:
The act of promoting content into the Bookmarker. Always creates or updates exactly one
Bookmark, keyed by URL — saving the same URL twice is an update, never a duplicate.
_Avoid_: Bookmark (as a verb), Star, Clip

**Bookmarker**:
The tool that stores Bookmarks. An archive with no horizon — it never prunes.
_Avoid_: Library, Vault, Read-later app

**The Pile**:
The Bookmarks you have not yet Archived — what you see by default in the Bookmarker.
It drains as you read. Nothing counts it, nudges you about it, or resurfaces it.
_Avoid_: Queue, Inbox, Backlog, Unread

**Archive** (verb):
Marking a Bookmark as read, removing it from the Pile. The only lifecycle a Bookmark has.
The Bookmark itself is retained forever; Archiving is not deletion.
_Avoid_: Done, Complete, Dismiss, Delete

**Save-signal**:
The event a Save produces for the Reader: that a URL was saved, and when. Carries no
Enrichment — never a summary, score, or tag — only the URL and a timestamp. The strongest
preference signal available, stronger than explicit feedback like a thumbs up/down.
_Avoid_: Webhook (a Save-signal's transport is an implementation detail; the event itself is the domain concept)

### Boundaries the language enforces

**Found, not made**:
A Bookmark has external provenance — you encountered it. Content you authored yourself
(notes, recordings, memos) is not a Bookmark and does not belong in the Bookmarker,
regardless of its file type.

**Enrichment belongs to the tool that owns the store**:
The Reader enriches Articles; the Bookmarker enriches Bookmarks, or does not. Enrichment
never crosses between them. A Save carries a URL and a title — never a summary, score, or tag.
What the two tools may share is *code*, never data: a library that summarizes and tags
(ADR-0005). Scoring is not in it — that is Agregado's alone.

**Retrieval is preserved, not built**:
Content is archived durably enough that an index could be built over it later. No search
surface is built until finding things is a demonstrated pain.
