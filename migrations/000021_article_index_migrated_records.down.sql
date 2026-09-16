-- Migrated records have no Miniflux entry; restoring NOT NULL means deciding
-- what they should hold. 0 is the sentinel the repo already reads them back as.
UPDATE article_index SET miniflux_entry_id = 0 WHERE miniflux_entry_id IS NULL;
ALTER TABLE article_index ALTER COLUMN miniflux_entry_id SET NOT NULL;
