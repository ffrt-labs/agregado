-- Remove manual from source type CHECK constraint
ALTER TABLE sources DROP CONSTRAINT sources_type_check;
ALTER TABLE sources ADD CONSTRAINT sources_type_check
    CHECK (type IN ('rss', 'newsletter'));

-- Make source_id nullable on articles
ALTER TABLE articles ALTER COLUMN source_id DROP NOT NULL;
