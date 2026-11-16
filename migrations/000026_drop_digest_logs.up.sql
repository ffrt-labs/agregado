-- digest_logs and digest_articles were meant as an audit trail of sent
-- Digests. digest_artifacts covers that (n8n sends from the persisted
-- artifact), and nothing reads or writes them (#194). Existing rows survive
-- only in the legacy cold dump (scripts/legacy-postgres-dump.sh), which must
-- be taken before this runs.
DROP TABLE IF EXISTS digest_articles;
DROP TABLE IF EXISTS digest_logs;
