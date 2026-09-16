-- Historical Articles migrated out of the old Agregado Postgres (issue #83)
-- never existed in Miniflux, so they have no entry id to cache for Decoration.
-- The column stays NOT NULL for everything the live pipeline writes, enforced
-- by articleindex.Request.validate rather than by the schema.
ALTER TABLE article_index ALTER COLUMN miniflux_entry_id DROP NOT NULL;
