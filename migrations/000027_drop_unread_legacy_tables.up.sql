-- Legacy tables nothing reads or writes any more (#241):
--   topic_weights       only the removed POST /api/articles/{id}/feedback wrote it.
--   newsletter_raw_html only the removed articles.store worker (#237) wrote it.
--   preferences         created by 000001 and never used; preference_profiles
--                       holds the preference profile.
-- Existing rows survive only in the legacy cold dump
-- (scripts/legacy-postgres-dump.sh), which must be taken before this runs.
--
-- Deliberately kept: sources, articles, tags, article_tags and
-- article_feedback. The ownership migration (internal/ownership,
-- cmd/migrate-ownership, #227) reads them, GET /r/{id} still falls back to
-- legacy articles, and the AI categorizer lists slugs from tags.
DROP TABLE IF EXISTS topic_weights;
DROP TABLE IF EXISTS newsletter_raw_html;
DROP TABLE IF EXISTS preferences;
