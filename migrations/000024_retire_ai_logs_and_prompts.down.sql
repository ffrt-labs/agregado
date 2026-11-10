-- Recreates the tables empty, as migration 000011 defined them. Rows are not
-- restored: ai_prompts is not re-seeded and ai_logs history is gone.
CREATE TABLE ai_prompts (
    operation     TEXT PRIMARY KEY,
    system_prompt TEXT NOT NULL,
    updated_at    TIMESTAMP DEFAULT NOW()
);

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

INSERT INTO admin_settings (key, value) VALUES ('ai_logging_enabled', 'true')
ON CONFLICT (key) DO NOTHING;
