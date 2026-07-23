-- Reverse Phase 21: restore the NOT NULL constraint by re-synthesising a
-- sentinel for every row that now holds NULL. gen_random_uuid() is available
-- (the initial schema already uses it for id defaults). This does not recover
-- the *original* UUIDs — those are gone — but the sentinels were never
-- referenced by value anywhere, only by their 'newsletter:' prefix, so a fresh
-- UUID restores a working pre-Phase-21 state.
UPDATE articles SET external_url = 'newsletter:' || gen_random_uuid() WHERE external_url IS NULL;

ALTER TABLE articles ALTER COLUMN external_url SET NOT NULL;
