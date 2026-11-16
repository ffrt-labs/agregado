-- Recreates the table empty, exactly as migration 000011 defined it. No row is
-- re-seeded: rolling back past 000024 re-inserts ai_logging_enabled itself.
CREATE TABLE admin_settings (
    key        TEXT PRIMARY KEY,
    value      TEXT NOT NULL,
    updated_at TIMESTAMP DEFAULT NOW()
);
