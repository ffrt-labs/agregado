DROP INDEX IF EXISTS idx_article_index_opened_at;
ALTER TABLE article_index DROP COLUMN IF EXISTS opened_at;
