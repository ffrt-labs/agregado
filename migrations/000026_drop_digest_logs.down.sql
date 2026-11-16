-- Recreates both tables empty, exactly as migration 000001 defined them.
CREATE TABLE digest_logs (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    sent_at TIMESTAMP DEFAULT NOW(),
    article_count INTEGER,
    recipient_email VARCHAR(255)
);

CREATE TABLE digest_articles(
    digest_id UUID REFERENCES digest_logs(id) ON DELETE CASCADE,
    article_id UUID REFERENCES articles(id) ON DELETE CASCADE,
    PRIMARY KEY (digest_id, article_id)
);
