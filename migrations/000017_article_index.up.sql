-- The Article Index keeps durable derived data after Miniflux prunes Article
-- bodies. Deliberately no content/body column: Miniflux (or the bridge) owns it.
CREATE TABLE article_index (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    miniflux_entry_id BIGINT NOT NULL,
    canonical_url TEXT NOT NULL UNIQUE,
    title TEXT NOT NULL,
    author TEXT,
    published_at TIMESTAMP,
    summary TEXT,
    tags JSONB NOT NULL DEFAULT '[]'::jsonb,
    score SMALLINT CHECK (score BETWEEN 1 AND 5),
    processing_status TEXT NOT NULL CHECK (processing_status IN ('processing', 'complete', 'failed')),
    failure_reason TEXT,
    created_at TIMESTAMP NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMP NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_article_index_status ON article_index(processing_status);
