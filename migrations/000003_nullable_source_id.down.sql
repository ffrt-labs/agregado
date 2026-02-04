-- Restore original CHECK constraint
ALTER TABLE sources DROP CONSTRAINT sources_type_check;
ALTER TABLE sources ADD CONSTRAINT sources_type_check
    CHECK (type IN ('rss', 'newsletter', 'manual'));

-- Restore NOT NULL (will fail if NULL values exist)
ALTER TABLE articles ALTER COLUMN source_id SET NOT NULL;
