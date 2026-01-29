CREATE TABLE sources (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name VARCHAR(255) NOT NULL,
    type VARCHAR(50) NOT NULL CHECK (type IN ('rss', 'newsletter', 'manual')),
    url VARCHAR(2048),
    email_sender VARCHAR(255),
    priority INTEGER DEFAULT 5,
    is_active BOOLEAN DEFAULT true,
    last_fetched_at TIMESTAMP,
    last_error TEXT,
    error_count INTEGER DEFAULT 0,
    created_at TIMESTAMP DEFAULT NOW(),
    updated_at TIMESTAMP DEFAULT NOW()
);

CREATE TABLE articles (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    source_id UUID REFERENCES sources(id) ON DELETE CASCADE,
    external_url VARCHAR(2048) NOT NULL UNIQUE,
    title VARCHAR(500) NOT NULL,
    author VARCHAR(255),
    summary TEXT,
    content TEXT,
    content_hash VARCHAR(64),
    published_at TIMESTAMP,
    ingested_at TIMESTAMP DEFAULT NOW(),
    is_read BOOLEAN DEFAULT false,
    read_at TIMESTAMP,
    word_count INTEGER,
    estimated_read_minutes INTEGER,
    created_at TIMESTAMP DEFAULT NOW()
);

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

CREATE TABLE preferences(
    key VARCHAR(100) PRIMARY KEY,
    value JSONB NOT NULL,
    updated_at TIMESTAMP DEFAULT NOW()
);

-- Full-Text Search Index
CREATE INDEX idx_articles_search ON articles
    USING GIN (to_tsvector('english', title || ' ' ||
COALESCE(content, '')));

-- Composite Index for Source + Date
CREATE INDEX idx_articles_source_date ON articles(source_id, published_at DESC);

-- Partial Index for Unread Articles
CREATE INDEX idx_articles_unread ON articles(is_read, published_at DESC) WHERE NOT is_read;
