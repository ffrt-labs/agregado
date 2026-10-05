#!/usr/bin/env bash
# Live verification of newsletter ingestion (agregado#138): sends the 4 crafted
# fixtures through Resend -> n8n -> R2/D1 -> Bridge and checks each outcome.
#
# Required env:
#   RESEND_API_KEY   key allowed to send from FROM_ADDR
#   FROM_ADDR        verified Resend sender, e.g. "Agregado Test <test@send.example.com>"
#   INBOUND_DOMAIN   domain whose catch-all feeds the Bridge, e.g. read.example.com
#   BRIDGE_HOST      Bridge hostname serving /feed and /p, e.g. bridge.example.com
#   R2_BUCKET        bucket holding original HTML (keys are hash(Message-ID))
# Optional env:
#   D1_DB            D1 database name (default: bridge)
#   WRANGLER_DIR     dir with wrangler config + auth (default: ../../email-worker)
#   N8N_URL, N8N_API_KEY   if set, also checks n8n for failed executions
#   WAIT             seconds to wait for ingestion (default: 45)
#   CLEANUP=1        at the end, delete everything this run created
# Needs: curl, jq, openssl, sha256sum, npx (logged in to Cloudflare).
set -uo pipefail

: "${RESEND_API_KEY:?}" "${FROM_ADDR:?}" "${INBOUND_DOMAIN:?}" "${BRIDGE_HOST:?}" "${R2_BUCKET:?}"
D1_DB=${D1_DB:-bridge}
WAIT=${WAIT:-45}
HERE=$(cd "$(dirname "$0")" && pwd)
FIX="$HERE/../test/fixtures"
WRANGLER_DIR=${WRANGLER_DIR:-$HERE/../../email-worker}
RUN=$(date +%s)
START=$RUN
PASS=0; FAIL=0

ALIASES=(fx-archived fx-viewbrowser fx-noweb fx-malformed)
FILES=(01-archived-at-header.json 02-view-in-browser.json 03-no-web-home.json 04-malformed-html.json)
set_var() { printf -v "$1_${2//-/_}" %s "$3"; }
get_var() { local v="$1_${2//-/_}"; printf %s "${!v}"; }

ok()   { echo "  PASS  $*"; PASS=$((PASS+1)); }
bad()  { echo "  FAIL  $*"; FAIL=$((FAIL+1)); }
check() { if [ "$1" = "1" ]; then ok "$2"; else bad "$2"; fi; }

d1() { (cd "$WRANGLER_DIR" && npx wrangler d1 execute "$D1_DB" --remote --json --command "$1" 2>/dev/null) | jq -c '.[0].results'; }
r2_exists() { (cd "$WRANGLER_DIR" && npx wrangler r2 object get "$R2_BUCKET/$1" --remote --pipe >/dev/null 2>&1); }
r2_delete() { (cd "$WRANGLER_DIR" && npx wrangler r2 object delete "$R2_BUCKET/$1" --remote >/dev/null 2>&1); }
sha() { if command -v sha256sum >/dev/null; then printf %s "$1" | sha256sum | cut -d' ' -f1; else printf %s "$1" | shasum -a 256 | cut -d' ' -f1; fi; }

for t in curl jq openssl npx; do command -v "$t" >/dev/null || { echo "missing tool: $t"; exit 2; }; done
[ "$(d1 'SELECT 1 AS n' | jq -r '.[0].n' 2>/dev/null)" = 1 ] || {
  echo "D1 preflight failed. Run this to see the error:"
  echo "  (cd $WRANGLER_DIR && npx wrangler d1 execute $D1_DB --remote --command 'SELECT 1')"
  exit 2
}

echo "== Setup: Sources (run id $RUN)"
for a in "${ALIASES[@]}"; do
  set_var SECRET "$a" "$(openssl rand -hex 24)"
  d1 "DELETE FROM sources WHERE id = '$a'" >/dev/null
  d1 "INSERT INTO sources (id, display_name, basic_auth_secret_hash, status) VALUES ('$a', 'verify $a', '$(sha "$(get_var SECRET "$a")")', 'active')" >/dev/null
done
d1 "SELECT id, status FROM sources WHERE id LIKE 'fx-%'"

ALERTS_BEFORE=$(d1 "SELECT count(*) AS n FROM ingest_alerts" | jq '.[0].n')

echo "== Send fixtures"
for i in 0 1 2 3; do
  a=${ALIASES[$i]}; f="$FIX/${FILES[$i]}"
  set_var MSGID "$a" "verify-$RUN-$a@${INBOUND_DOMAIN}"
  payload=$(jq --arg from "$FROM_ADDR" --arg to "$a@$INBOUND_DOMAIN" --arg mid "<$(get_var MSGID "$a")>" \
    '{from: $from, to: [$to], subject: .subject, html: .html, text: .text,
      headers: ((.headers | del(.from)) + {"Message-ID": $mid})}' "$f")
  code=$(curl -s -o "${TMPDIR:-/tmp}/resend-$a.json" -w '%{http_code}' https://api.resend.com/emails \
    -H "Authorization: Bearer $RESEND_API_KEY" -H 'Content-Type: application/json' -d "$payload")
  echo "  $a -> HTTP $code $(jq -c . "${TMPDIR:-/tmp}/resend-$a.json" 2>/dev/null | head -c 160)"
done

echo "== Waiting ${WAIT}s for ingestion"; sleep "$WAIT"

echo "== Fixtures 1-3"
EXPECT_CANON=("https://dispatch.example.com/p/weekly-dispatch-42" "" "")
for i in 0 1 2; do
  a=${ALIASES[$i]}; echo " [$a]"
  row=$(d1 "SELECT id, canonical_url, permalink_uuid, title, length(readable_content) AS len FROM entries WHERE source_id = '$a'")
  n=$(jq 'length' <<<"$row")
  check "$([ "$n" = 1 ] && echo 1 || echo 0)" "exactly one D1 entry (got $n)"
  [ "$n" = 1 ] || continue
  id=$(jq -r '.[0].id' <<<"$row"); canon=$(jq -r '.[0].canonical_url // ""' <<<"$row"); uuid=$(jq -r '.[0].permalink_uuid' <<<"$row")
  check "$([[ "$id" =~ ^[0-9a-f]{64}$ ]] && echo 1 || echo 0)" "entry id is a sha256 hex (Resend rewrites Message-ID, so it is not hash of the one we sent)"
  check "$([ "$(jq -r '.[0].len' <<<"$row")" -gt 0 ] && echo 1 || echo 0)" "readable_content non-empty"
  case $i in
    0) check "$([ "$canon" = "${EXPECT_CANON[0]}" ] && echo 1 || echo 0)" "canonical_url from Archived-At header (got '$canon')" ;;
    1) check "$([[ "$canon" == http* ]] && echo 1 || echo 0)" "canonical_url scraped from view-in-browser anchor (got '$canon')" ;;
    2) check "$([ -z "$canon" ] && echo 1 || echo 0)" "canonical_url empty (got '$canon')" ;;
  esac
  r2_exists "$id"; check "$([ $? = 0 ] && echo 1 || echo 0)" "R2 object $id exists"
  feed=$(curl -s -u "x:$(get_var SECRET "$a")" "https://$BRIDGE_HOST/feed/$a.atom")
  check "$(grep -q "$id" <<<"$feed" && echo 1 || echo 0)" "Atom feed contains the entry"
  page=$(curl -s -o "${TMPDIR:-/tmp}/p-$a.html" -w '%{http_code}' "https://$BRIDGE_HOST/p/$uuid")
  check "$([ "$page" = 200 ] && echo 1 || echo 0)" "/p/$uuid -> HTTP $page"
  check "$(grep -qi '<script' "${TMPDIR:-/tmp}/p-$a.html" && echo 0 || echo 1)" "permalink has no <script>"
  check "$(grep -qi 'Content-Security-Policy' "${TMPDIR:-/tmp}/p-$a.html" && echo 1 || echo 0)" "permalink has CSP meta"
done

echo "== Fixture 4 (failure path)"
a=${ALIASES[3]}
n=$(d1 "SELECT count(*) AS n FROM entries WHERE source_id = '$a'" | jq '.[0].n')
check "$([ "$n" = 0 ] && echo 1 || echo 0)" "no D1 entry (got $n)"
echo "  (R2 key is unknowable after Message-ID rewriting; failure is in Build entry, before Write R2: confirm in n8n that Write R2 did not run)"
ALERT_ROWS=$(d1 "SELECT id FROM ingest_alerts WHERE alerted_at >= $START")
na=$(jq 'length' <<<"$ALERT_ROWS")
check "$([ "$na" = 1 ] && echo 1 || echo 0)" "exactly one new ingest_alerts row (got $na)"
echo "  (check your BRIDGE_ALERT_URL channel: exactly one notification should have arrived)"
if [ -n "${N8N_URL:-}" ] && [ -n "${N8N_API_KEY:-}" ]; then
  fails=$(curl -s -H "X-N8N-API-KEY: $N8N_API_KEY" "$N8N_URL/api/v1/executions?status=error&limit=10" | jq '[.data[] | select((.startedAt | sub("\\.[0-9]+"; "") | fromdateiso8601) >= '"$START"')] | length')
  check "$([ "${fails:-0}" -ge 1 ] && echo 1 || echo 0)" "n8n shows >=1 failed execution since start (got ${fails:-?})"
else
  echo "  (set N8N_URL and N8N_API_KEY to check n8n automatically; otherwise confirm a failed execution in the n8n UI)"
fi

echo
echo "== MANUAL: in the Resend dashboard (Emails -> Receiving, or Webhooks -> the endpoint -> message log),"
echo "   find the failed delivery for alias $a and click Replay."
read -r -p "   Press Enter once you have replayed it... " _
echo "   Waiting ${WAIT}s"; sleep "$WAIT"

echo "== After replay"
na2=$(d1 "SELECT count(*) AS n FROM ingest_alerts WHERE alerted_at >= $START" | jq '.[0].n')
check "$([ "$na2" = 1 ] && echo 1 || echo 0)" "still exactly one ingest_alerts row (got $na2)"
n=$(d1 "SELECT count(*) AS n FROM entries WHERE source_id = '$a'" | jq '.[0].n')
check "$([ "$n" = 0 ] && echo 1 || echo 0)" "still no D1 entry (got $n)"
echo "  (confirm in the alert channel that no second notification arrived, and in n8n that the replay also failed)"

echo
echo "== Result: $PASS passed, $FAIL failed"

if [ "${CLEANUP:-0}" = 1 ]; then
  echo "== Cleanup"
  for a in "${ALIASES[@]}"; do
    for id in $(d1 "SELECT id FROM entries WHERE source_id = '$a'" | jq -r '.[].id'); do r2_delete "$id"; done
    d1 "DELETE FROM entries WHERE source_id = '$a'" >/dev/null
    d1 "DELETE FROM sources WHERE id = '$a'" >/dev/null
  done
  d1 "DELETE FROM ingest_alerts WHERE alerted_at >= $START" >/dev/null
  echo "  removed test entries, R2 objects, Sources and alert rows created since run start"
else
  echo "Not cleaned up: set status='pending' on the fx-* Sources (or delete them) so Resend's ~27h retries stop. Next time pass CLEANUP=1."
fi
[ "$FAIL" = 0 ]
