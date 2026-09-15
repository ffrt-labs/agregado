-- An Open is deliberately a single weak signal, not a click counter.
ALTER TABLE article_index ADD COLUMN opened_at TIMESTAMP;

CREATE INDEX idx_article_index_opened_at ON article_index (opened_at) WHERE opened_at IS NOT NULL;
