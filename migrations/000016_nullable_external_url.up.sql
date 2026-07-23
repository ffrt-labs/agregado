-- Phase 21 (issue #3): kill the newsletter: sentinel.
--
-- external_url held a synthetic 'newsletter:<uuid>' value for newsletter
-- articles only because the column was NOT NULL and the email parser needed
-- *something* to write. That prefix then grew three independent semantics
-- (redirect target, reader-link presence, enrichment fetch-guard). Type is
-- already modelled properly by sources.type; the prefix was a redundant, less
-- reliable duplicate.
--
-- Dropping NOT NULL lets external_url mean exactly one thing: a real web URL,
-- or NULL when the article has no web home. The UNIQUE constraint stays —
-- PostgreSQL treats NULLs as distinct, so multiple NULL newsletter rows
-- coexist without conflict (they never deduplicated on this column anyway;
-- every one carried a fresh UUID).
ALTER TABLE articles ALTER COLUMN external_url DROP NOT NULL;

UPDATE articles SET external_url = NULL WHERE external_url LIKE 'newsletter:%';
