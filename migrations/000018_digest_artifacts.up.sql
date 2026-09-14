ALTER TABLE article_index ADD COLUMN source_id TEXT;
CREATE INDEX idx_article_index_digest ON article_index (processing_status, score DESC, published_at DESC);

CREATE TABLE digest_artifacts (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    digest_date DATE NOT NULL UNIQUE,
    subject TEXT NOT NULL,
    html TEXT NOT NULL,
    plain_text TEXT NOT NULL,
    selection JSONB NOT NULL,
    candidate_count INTEGER NOT NULL,
    floor_pass_count INTEGER NOT NULL,
    selected_count INTEGER NOT NULL,
    created_at TIMESTAMP NOT NULL DEFAULT NOW()
);
