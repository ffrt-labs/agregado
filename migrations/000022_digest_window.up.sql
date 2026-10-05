-- The Digest's candidate window is "successful Enrichment since the previous
-- successful Digest" (docs/architecture/agregado.md), not a UTC published_at
-- day. enriched_at is when an Article's Enrichment completed; updated_at is
-- not reused for this because any later write to the row would move it.
ALTER TABLE article_index ADD COLUMN enriched_at TIMESTAMP;
UPDATE article_index SET enriched_at = updated_at WHERE processing_status = 'complete';
CREATE INDEX idx_article_index_enriched_at ON article_index (enriched_at) WHERE processing_status = 'complete';

-- Each Digest records the window it drew from, so the next one starts exactly
-- where it ended. Rows written before this migration have neither; the window
-- query falls back to created_at for them.
ALTER TABLE digest_artifacts ADD COLUMN window_start TIMESTAMP;
ALTER TABLE digest_artifacts ADD COLUMN window_end TIMESTAMP;

-- Every artifact saved so far that selected nothing was produced by the
-- published_at-day window or #158's untagged Choice, not by an empty day.
-- Removing them lets those dates be regenerated; empty artifacts are no
-- longer persisted at all.
DELETE FROM digest_artifacts WHERE selected_count = 0;
