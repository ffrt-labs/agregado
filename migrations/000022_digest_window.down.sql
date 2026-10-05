ALTER TABLE digest_artifacts DROP COLUMN IF EXISTS window_end;
ALTER TABLE digest_artifacts DROP COLUMN IF EXISTS window_start;
DROP INDEX IF EXISTS idx_article_index_enriched_at;
ALTER TABLE article_index DROP COLUMN IF EXISTS enriched_at;
