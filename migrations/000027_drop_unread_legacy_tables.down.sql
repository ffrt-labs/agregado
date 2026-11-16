-- Recreates the tables empty, exactly as their original migrations left them.

-- preferences, as 000001 defined it.
CREATE TABLE preferences(
    key VARCHAR(100) PRIMARY KEY,
    value JSONB NOT NULL,
    updated_at TIMESTAMP DEFAULT NOW()
);

-- newsletter_raw_html, as 000015 defined it.
CREATE TABLE newsletter_raw_html (
	article_id  UUID PRIMARY KEY REFERENCES articles(id) ON DELETE CASCADE,
	html        TEXT NOT NULL,
	received_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- topic_weights, as 000005 defined it.
CREATE TABLE topic_weights (
    topic       VARCHAR(100)    PRIMARY KEY,
    weight      FLOAT           NOT NULL DEFAULT 1.0,
    updated_at  TIMESTAMP       NOT NULL DEFAULT NOW()
);
