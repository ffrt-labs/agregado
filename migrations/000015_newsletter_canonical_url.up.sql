-- Canonical web home for newsletters, extracted at parse time (issue #2).
-- Nullable and parallel to external_url: the newsletter:<uuid> placeholder in
-- external_url stays load-bearing for the enrichment fetch-guard, so the real
-- URL lives in its own column rather than overwriting the sentinel.
ALTER TABLE articles ADD COLUMN canonical_url TEXT;

-- Raw email HTML, kept as ingestion provenance so the extraction heuristic can
-- be re-run against real stored mail. Its own table, not a column on articles:
-- articles is SELECT *'d at six call sites and would pay the transfer cost of
-- 50-200KB of HTML on every page load; and raw HTML needs an independent
-- retention lifetime (purgeable later while article rows are kept forever).
CREATE TABLE newsletter_raw_html (
	article_id  UUID PRIMARY KEY REFERENCES articles(id) ON DELETE CASCADE,
	html        TEXT NOT NULL,
	received_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
