-- PREFERENCES.md moves from a bind-mounted file (PREFERENCES_PATH) into the
-- database (ADR-0009). Every accepted version is kept; the newest row is the
-- profile in force. The Google Doc stays authoritative: n8n hands each
-- version over through PUT /api/private/preferences.
CREATE TABLE preference_profiles (
    id         BIGSERIAL PRIMARY KEY,
    content    TEXT        NOT NULL,
    source     VARCHAR(16) NOT NULL CHECK (source IN ('manual', 'regenerated')),
    created_at TIMESTAMP   NOT NULL DEFAULT NOW()
);
