#!/usr/bin/env bash
#
# Takes the final cold dump of the old Agregado Postgres and proves it restores
# (issue #83). "Cold" is the point: the app is stopped first, so the dump is a
# consistent snapshot of a database nothing is writing to. This is the artifact
# the whole pivot is reversible against — it is taken once, verified once, and
# kept.
#
# Everything the ownership migration leaves behind — Sources, Article bodies,
# raw newsletter HTML, topic weights, digest history — survives only here.
#
#   scripts/legacy-postgres-dump.sh [output-directory]
#
# Reads DATABASE_USER / DATABASE_DB from .env, like the Makefile does.
set -euo pipefail

CONTAINER=${DB_CONTAINER:-agregado-db}
APP_SERVICE=${APP_SERVICE:-agregado}
OUTPUT_DIR=${1:-./backups}
STAMP=$(date -u +%Y%m%dT%H%M%SZ)

if [[ -f .env ]]; then
	set -a
	# shellcheck disable=SC1091
	source .env
	set +a
fi
: "${DATABASE_USER:?DATABASE_USER is not set (check .env)}"
: "${DATABASE_DB:?DATABASE_DB is not set (check .env)}"

DUMP="${OUTPUT_DIR}/agregado-legacy-${STAMP}.dump"
MANIFEST="${OUTPUT_DIR}/agregado-legacy-${STAMP}.manifest.txt"
SCRATCH="agregado_restore_check_${STAMP//[^0-9]/}"

mkdir -p "$OUTPUT_DIR"

psql() { docker exec -i "$CONTAINER" psql -v ON_ERROR_STOP=1 -U "$DATABASE_USER" "$@"; }

# --- 1. Go cold ------------------------------------------------------------
# Failing here is fine: if the app isn't running under compose there is nothing
# to stop, and the dump is already cold.
echo "==> stopping ${APP_SERVICE} so the dump is cold"
docker compose --profile prod stop "$APP_SERVICE" 2>/dev/null || echo "    (not running under compose — continuing)"

restart_app() {
	echo "==> restarting ${APP_SERVICE}"
	docker compose --profile prod start "$APP_SERVICE" 2>/dev/null || true
}
trap restart_app EXIT

# --- 2. Record what the source database holds ------------------------------
# These counts are the restore check's expected values. They are written to the
# manifest before the dump so a mismatch later is unambiguous.
echo "==> recording source row counts"
COUNT_SQL="
SELECT 'sources', COUNT(*) FROM sources UNION ALL
SELECT 'articles', COUNT(*) FROM articles UNION ALL
SELECT 'article_tags', COUNT(*) FROM article_tags UNION ALL
SELECT 'tags', COUNT(*) FROM tags UNION ALL
SELECT 'article_feedback', COUNT(*) FROM article_feedback UNION ALL
SELECT 'newsletter_raw_html', COUNT(*) FROM newsletter_raw_html UNION ALL
SELECT 'article_index', COUNT(*) FROM article_index UNION ALL
SELECT 'article_index_feedback', COUNT(*) FROM article_index_feedback UNION ALL
SELECT 'digest_logs', COUNT(*) FROM digest_logs
ORDER BY 1;"
SOURCE_COUNTS=$(psql -d "$DATABASE_DB" -At -F',' -c "$COUNT_SQL")

# --- 3. Dump ---------------------------------------------------------------
# Custom format (-Fc): compressed, and restorable table-by-table with pg_restore
# if only part of the old data is ever wanted back.
echo "==> dumping ${DATABASE_DB} to ${DUMP}"
docker exec "$CONTAINER" pg_dump -U "$DATABASE_USER" -d "$DATABASE_DB" -Fc --no-owner --no-acl > "$DUMP"

CHECKSUM=$(sha256sum "$DUMP" | cut -d' ' -f1)
SIZE=$(wc -c < "$DUMP")

# --- 4. Prove it restores --------------------------------------------------
# A dump that has never been restored is a guess. This restores into a scratch
# database on the same server and compares every row count against step 2.
echo "==> restoring into scratch database ${SCRATCH}"
psql -d postgres -c "DROP DATABASE IF EXISTS ${SCRATCH};" >/dev/null
psql -d postgres -c "CREATE DATABASE ${SCRATCH};" >/dev/null

drop_scratch() {
	psql -d postgres -c "DROP DATABASE IF EXISTS ${SCRATCH};" >/dev/null 2>&1 || true
	restart_app
}
trap drop_scratch EXIT

docker exec -i "$CONTAINER" pg_restore -U "$DATABASE_USER" -d "$SCRATCH" --no-owner --no-acl < "$DUMP"

echo "==> comparing restored row counts against the source"
RESTORED_COUNTS=$(psql -d "$SCRATCH" -At -F',' -c "$COUNT_SQL")

if [[ "$SOURCE_COUNTS" != "$RESTORED_COUNTS" ]]; then
	echo "RESTORE CHECK FAILED — the restored database does not match the source." >&2
	diff <(echo "$SOURCE_COUNTS") <(echo "$RESTORED_COUNTS") >&2 || true
	exit 1
fi

# --- 5. Manifest -----------------------------------------------------------
{
	echo "Agregado legacy Postgres — final cold dump"
	echo "taken:      ${STAMP}"
	echo "database:   ${DATABASE_DB}"
	echo "file:       $(basename "$DUMP")"
	echo "format:     pg_dump custom (-Fc), restore with pg_restore"
	echo "bytes:      ${SIZE}"
	echo "sha256:     ${CHECKSUM}"
	echo "restore:    VERIFIED — restored into a scratch database, row counts matched"
	echo
	echo "row counts at dump time:"
	echo "$SOURCE_COUNTS" | sed 's/^/  /'
} > "$MANIFEST"

echo
echo "==> done"
echo "    dump:     ${DUMP}"
echo "    manifest: ${MANIFEST}"
echo "    sha256:   ${CHECKSUM}"
echo
echo "Copy both off-box before proceeding. See docs/runbooks/legacy-postgres-cutover.md."
