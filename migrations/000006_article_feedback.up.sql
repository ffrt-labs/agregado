CREATE TABLE article_feedback (
    id          UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    article_id  UUID        NOT NULL REFERENCES articles(id) ON DELETE CASCADE,
    vote        VARCHAR(4)  NOT NULL CHECK (vote IN ('up', 'down')),
    created_at  TIMESTAMP   NOT NULL DEFAULT NOW()
);
