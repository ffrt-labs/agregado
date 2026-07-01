CREATE TABLE ai_prompts (
    operation     TEXT PRIMARY KEY,
    system_prompt TEXT NOT NULL,
    updated_at    TIMESTAMP DEFAULT NOW()
);

-- Seeded with the current in-code system prompts. The categorize seed omits any
-- inline slug list on purpose: the live tag slugs are appended by code at call time.
INSERT INTO ai_prompts (operation, system_prompt) VALUES
    ('score', 'You are a content score giver. Given an article title and content, return only a number 1-5. 1=spam/trivial, 3=worth reading, 5=essential global significance. Return only the integer.'),
    ('categorize', 'You are a content classifier. Given an article title and content, return exactly one category slug from the list provided. Return only the slug — no explanation, no punctuation.'),
    ('summarize', 'You are a news digest assistant. Given a list of article titles, write a 2-3 sentence summary capturing the key themes. Be concise and direct.'),
    ('digest', 'You are a news digest assistant. Write a 2-sentence introduction for a daily digest email. Mention the main themes and why they matter. Be concise and direct. No bullet points.');

CREATE TABLE ai_logs (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    operation     TEXT NOT NULL,
    model         TEXT,
    system_prompt TEXT,
    user_prompt   TEXT,
    response      TEXT,
    success       BOOLEAN NOT NULL,
    error         TEXT,
    duration_ms   INTEGER,
    created_at    TIMESTAMP DEFAULT NOW()
);

CREATE INDEX idx_ai_logs_created ON ai_logs(created_at DESC);

CREATE TABLE admin_settings (
    key        TEXT PRIMARY KEY,
    value      TEXT NOT NULL,
    updated_at TIMESTAMP DEFAULT NOW()
);

INSERT INTO admin_settings (key, value) VALUES ('ai_logging_enabled', 'true');
