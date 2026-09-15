CREATE TABLE article_index_feedback (
    article_index_id UUID PRIMARY KEY REFERENCES article_index(id) ON DELETE CASCADE,
    vote             VARCHAR(4) NOT NULL CHECK (vote IN ('up', 'down')),
    created_at       TIMESTAMP  NOT NULL DEFAULT NOW(),
    updated_at       TIMESTAMP  NOT NULL DEFAULT NOW()
);
