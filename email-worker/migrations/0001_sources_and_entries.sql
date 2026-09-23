-- Source registry (agregado#99) and entry metadata (agregado#97, #100).
-- Original HTML lives in R2, keyed by entries.id — this table holds only
-- what's needed to compute a feed and resolve a permalink.

CREATE TABLE sources (
	-- The inbound alias local-part (catch-all routing makes the alias the
	-- Source id — agregado#99), e.g. 'tldr' for tldr@<domain>.
	id TEXT PRIMARY KEY,
	display_name TEXT NOT NULL,
	-- Hashed, never the plaintext Basic-auth secret (agregado#98).
	basic_auth_secret_hash TEXT NOT NULL,
	-- 'pending' | 'active' (agregado#99) — n8n no-ops on inbound mail for a
	-- pending Source, so no entry ever exists for one; enforced by the
	-- ingest side, not a CHECK constraint here.
	status TEXT NOT NULL
);

CREATE TABLE entries (
	-- hash(Message-ID) — also the Atom <id> and, verbatim, the permalink
	-- UUID's source material (agregado#100). Deterministic, not random, so
	-- redelivery of the same message resolves to the same identity.
	id TEXT PRIMARY KEY,
	source_id TEXT NOT NULL REFERENCES sources(id),
	-- NULL when extraction recovered no canonical URL (agregado ADR-0004) —
	-- the permalink is the identity URL in that case (agregado#100).
	canonical_url TEXT,
	-- Derived from id (agregado#100), stored rather than recomputed per
	-- request since it's the primary key for permalink lookups.
	permalink_uuid TEXT NOT NULL UNIQUE,
	title TEXT NOT NULL,
	-- Readable extraction for the feed's <content> (agregado#101) — the
	-- *sanitized original* HTML this same entry's permalink serves lives in
	-- R2 under this row's id, not here.
	readable_content TEXT NOT NULL,
	-- Unix seconds. Governs the feed's rolling 30-day window (agregado#95);
	-- the R2/D1 rows themselves are never pruned (ADR-0003).
	published_at INTEGER NOT NULL
);

-- Serves the feed query: entries for one source, newest first, within the
-- rolling window — exactly the WHERE/ORDER BY #97 sized the whole design
-- around avoiding N R2 round-trips for.
CREATE INDEX entries_source_published_idx ON entries (source_id, published_at DESC);
