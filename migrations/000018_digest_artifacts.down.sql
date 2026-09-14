DROP TABLE IF EXISTS digest_artifacts;
DROP INDEX IF EXISTS idx_article_index_digest;
ALTER TABLE article_index DROP COLUMN IF EXISTS source_id;
