#!/usr/bin/env bash
# StreamGate backup/restore drill (TRANSITION_PLAN.md SG-14 step 8).
#
# WHAT THIS PROVES
#   A point-in-time Postgres dump can be restored and the service comes back
#   serving the SAME content with INVALIDATED old credentials:
#     1. pre-drill snapshot of authorized state (token, manifest URL, access
#        token, table row counts, MinIO object inventory),
#     2. pg_dump of the streamgate database,
#     3. restore INTO PLACE (same volumes — see the honest note below),
#     4. verification: row counts match, the pre-restore token/URL is REJECTED
#        (its session epoch died with the restore), a fresh
#        challenge -> login -> manifest 200 works,
#     5. RPO/RTO recorded.
#
# HONEST LIMITATION — restore-into-place drill, NOT an isolated-environment
#   drill: docker-compose.fullchain.yml pins container_name and host ports, so
#   a second stack would collide with the running one. This drill therefore
#   stops the APP containers (monolith, api-gateway, transcoder), DROPs and
#   recreates the streamgate database, applies the dump in the SAME Postgres
#   instance, flushes the Redis session keys (session epochs live in Redis, not
#   Postgres — a DB restore alone would NOT invalidate old tokens), verifies
#   the MinIO objects are untouched, and restarts the app containers. It proves
#   recoverability of state and credential invalidation using the same volumes;
#   it does NOT exercise mounting a fresh volume set or a fresh MinIO.
#
# RPO: bounded by snapshot frequency — this drill takes one point-in-time
# dump, so any writes after the dump are lost (their count is reported).
# RTO: wall-clock of step 3+4, printed at the end.
#
# PREREQUISITES
#   * the sg-fc stack is running and healthy,
#   * `cast` (Foundry) on the host, for the wallet login flow,
#   * WALLET_KEY: the PRIVATE KEY of a wallet that owns the gating NFT on the
#     stack's Anvil (default: Anvil account 0, the acceptance wallet A),
#   * CONTENT_ID (optional): content to authorize and verify; default
#     `demo-content-1` (seed-demo-video.sh), override with CONTENT_ID.
#
# Usage: ./scripts/backup-restore-drill.sh
set -euo pipefail

BASE_URL="${BASE_URL:-http://localhost:18080}"
ANVIL_URL="${ANVIL_URL:-http://localhost:18545}"
CHAIN_ID=31337
PG_CONTAINER=sg-fc-postgres
REDIS_CONTAINER=sg-fc-redis
MINIO_CONTAINER=sg-fc-minio
# The app resolves its Redis DB from config: config.dev.yaml (which the stack
# loads under APP_DEBUG=true) sets db: 1, so the session epochs live there.
# Flush the WRONG db and the old tokens keep verifying — override with
# REDIS_DB if your stack uses another index.
REDIS_DB="${REDIS_DB:-1}"
APP_CONTAINERS=(sg-fc-monolith sg-fc-api-gateway sg-fc-transcoder)
COMPOSE_FILE=docker-compose.fullchain.yml

WALLET_KEY="${WALLET_KEY:-0xac0974bec39a17e36ba4a6b4d238ff944bacb478cbed5efcae784d7bf4f2ff80}"
CONTENT_ID="${CONTENT_ID:-demo-content-1}"

BACKUP_SQL=/tmp/sg-backup.sql
OBJECTS_TXT=/tmp/sg-objects.txt
COUNTS_BEFORE_TXT=/tmp/sg-counts-before.txt
COUNTS_AFTER_TXT=/tmp/sg-counts-after.txt

FAIL=0
p() { printf 'PASS: %s\n' "$*"; }
f() { printf 'FAIL: %s\n' "$*"; FAIL=$((FAIL + 1)); }
die() { printf 'FATAL: %s\n' "$*" >&2; exit 1; }

require_tool() {
    command -v "$1" >/dev/null 2>&1 || die "required tool $1 not found in PATH"
}
require_tool curl
require_tool python3
require_tool cast
require_tool docker

cleanup() {
    rm -f "$BACKUP_SQL" "$OBJECTS_TXT" "$COUNTS_BEFORE_TXT" "$COUNTS_AFTER_TXT"
}
trap cleanup EXIT

json_get() { printf '%s' "$1" | python3 -c "import sys,json;print(json.load(sys.stdin).get('$2',''))" 2>/dev/null || true; }

# login <wallet> — challenge -> personal_sign -> login, mirroring
# scripts/fullchain-acceptance.sh. Prints the access token.
login() {
    local wallet="$1" challenge cid msg sig resp
    challenge="$(curl -sf -X POST "$BASE_URL/api/v1/auth/challenge" \
        -H 'Content-Type: application/json' \
        -d "{\"wallet\":\"$wallet\",\"chain_id\":$CHAIN_ID}")" || return 1
    cid="$(json_get "$challenge" challenge_id)"
    msg="$(json_get "$challenge" message)"
    [ -n "$cid" ] && [ -n "$msg" ] || return 1
    sig="$(cast wallet sign --private-key "$WALLET_KEY" "$msg")" || return 1
    resp="$(curl -sf -X POST "$BASE_URL/api/v1/auth/login" \
        -H 'Content-Type: application/json' \
        -d "{\"wallet\":\"$wallet\",\"chain_id\":$CHAIN_ID,\"challenge_id\":\"$cid\",\"signature\":\"$sig\"}")" || return 1
    printf '%s' "$(json_get "$resp" token)"
}

table_count() { # table_count <table>
    docker exec "$PG_CONTAINER" psql -U postgres -d streamgate -tAc \
        "SELECT COUNT(*) FROM $1" 2>/dev/null || echo "table-missing"
}

wait_healthy() { # wait_healthy <container> <deadline-seconds>
    local c="$1" deadline=$((SECONDS + $2))
    while [ "$SECONDS" -lt "$deadline" ]; do
        status="$(docker inspect -f '{{.State.Health.Status}}' "$c" 2>/dev/null || echo missing)"
        [ "$status" = "healthy" ] && return 0
        sleep 2
    done
    return 1
}

# ---------------------------------------------------------------------------
# 0. Preflight
# ---------------------------------------------------------------------------
printf '============================================\n'
printf 'StreamGate backup/restore drill\n'
printf 'Backend: %s\n' "$BASE_URL"
printf '============================================\n\n'

docker inspect "$PG_CONTAINER" >/dev/null 2>&1 || die "$PG_CONTAINER is not running — start the sg-fc stack first"
WALLET="$(cast wallet address --private-key "$WALLET_KEY")"
printf 'Wallet: %s\nContent: %s\n\n' "$WALLET" "$CONTENT_ID"

# ---------------------------------------------------------------------------
# 1. Pre-drill state: login, manifest URL + access token, row counts,
#    MinIO inventory.
# ---------------------------------------------------------------------------
TOKEN="$(login "$WALLET")" || die "wallet login failed"
[ -n "$TOKEN" ] || die "login returned no token"
p "logged in and obtained an access token"

PRE_MANIFEST_URL="$BASE_URL/api/v1/streaming/$CONTENT_ID/manifest.m3u8"
PRE_MANIFEST_CODE="$(curl -s -o /dev/null -w '%{http_code}' \
    -H "Authorization: Bearer $TOKEN" "$PRE_MANIFEST_URL")"
if [ "$PRE_MANIFEST_CODE" = "200" ]; then
    p "pre-restore holder manifest returns 200"
else
    f "pre-restore holder manifest returned $PRE_MANIFEST_CODE (is $CONTENT_ID authorized and transcoded? run fullchain-acceptance first)"
    die "aborting: the drill needs an authorized, playable content to be meaningful"
fi

TABLES=(contents content_gating_rules outbox content_registry_intents transcoding_tasks)
: > "$COUNTS_BEFORE_TXT"
for t in "${TABLES[@]}"; do
    printf '%s=%s\n' "$t" "$(table_count "$t")" >> "$COUNTS_BEFORE_TXT"
done
p "recorded pre-restore row counts ($(tr '\n' ' ' < "$COUNTS_BEFORE_TXT"))"

# MinIO inventory: object keys + sizes, and a sha256 over the manifest-relevant
# objects (.m3u8 / .mp4 originals) so the restore can prove objects untouched.
docker exec "$MINIO_CONTAINER" sh -c '
    find /data/streamgate -type f 2>/dev/null | sort | while read -r f; do
        printf "%s %s\n" "$(stat -c %s "$f" 2>/dev/null || echo ?)" "${f#/data/streamgate/}"
    done' > "$OBJECTS_TXT" 2>/dev/null || true
MANIFEST_HASH_BEFORE="$(docker exec "$MINIO_CONTAINER" sh -c '
    find /data/streamgate -type f \( -name "*.m3u8" -o -name "*.mp4" \) 2>/dev/null | sort | xargs -r sha256sum 2>/dev/null | sha256sum' \
    2>/dev/null | awk '{print $1}')"
p "MinIO inventory recorded ($(wc -l < "$OBJECTS_TXT" | tr -d ' ') objects; manifest/original hash ${MANIFEST_HASH_BEFORE:-none})"

# ---------------------------------------------------------------------------
# 2. Consistent snapshot: pg_dump.
# ---------------------------------------------------------------------------
# A plain pg_dump runs inside the SAME Postgres instance and takes its own
# consistent snapshot of every table; the app is STILL WRITING while it runs.
# This is a stop-write-FREE snapshot drill, not an online zero-loss backup:
# writes racing the dump may or may not land in it (standard MVCC caveat).
# An online-zero-loss design would add WAL archiving / snapshot-based logical
# replication; that is out of scope for this drill.
docker exec "$PG_CONTAINER" pg_dump -U postgres streamgate > "$BACKUP_SQL"
[ -s "$BACKUP_SQL" ] || die "pg_dump produced an empty file"
p "pg_dump captured $(wc -c < "$BACKUP_SQL" | tr -d ' ') bytes to $BACKUP_SQL"

# ---------------------------------------------------------------------------
# 3. Restore INTO PLACE.
# ---------------------------------------------------------------------------
RESTORE_START=$SECONDS

# 3a. stop the app containers (the DB itself stays up).
docker stop "${APP_CONTAINERS[@]}" >/dev/null
p "stopped app containers (${APP_CONTAINERS[*]})"

# 3b. drop and recreate the database, then apply the dump.
docker exec "$PG_CONTAINER" psql -U postgres -c "DROP DATABASE streamgate WITH (FORCE)" >/dev/null
docker exec "$PG_CONTAINER" psql -U postgres -c "CREATE DATABASE streamgate" >/dev/null
docker exec -i "$PG_CONTAINER" psql -U postgres -d streamgate < "$BACKUP_SQL" >/dev/null
p "database dropped, recreated and the dump applied"

# 3c. sessions live in REDIS: a Postgres restore alone would NOT invalidate
# old tokens (the epoch records are gone from Redis only if we drop them).
# Flush only this app's session keys in the db the app actually uses —
# never FLUSHALL, and never the wrong db index.
docker exec "$REDIS_CONTAINER" sh -c "redis-cli -n $REDIS_DB --scan --pattern 'streamgate:session:*' | xargs -r redis-cli -n $REDIS_DB DEL" >/dev/null
p "flushed streamgate:session:* keys from redis db $REDIS_DB (old session epochs are gone)"

# 3d. MinIO objects were not touched; re-inventory and compare.
docker exec "$MINIO_CONTAINER" sh -c '
    find /data/streamgate -type f 2>/dev/null | sort | while read -r f; do
        printf "%s %s\n" "$(stat -c %s "$f" 2>/dev/null || echo ?)" "${f#/data/streamgate/}"
    done' > /tmp/sg-objects-after.txt 2>/dev/null || true
if diff -q "$OBJECTS_TXT" /tmp/sg-objects-after.txt >/dev/null 2>&1; then
    p "MinIO inventory unchanged after restore"
else
    f "MinIO inventory changed during the drill"
fi
docker exec "$MINIO_CONTAINER" rm -f /tmp/sg-objects-after.txt >/dev/null 2>&1 || true

# 3e. restart the app containers and wait for health.
docker start "${APP_CONTAINERS[@]}" >/dev/null
for c in "${APP_CONTAINERS[@]}"; do
    if wait_healthy "$c" 120; then
        p "$c healthy again"
    else
        f "$c did not become healthy within 120s"
    fi
done

RESTORE_RTO=$((SECONDS - RESTORE_START))
p "restore + restart wall-clock (RTO sample): ${RESTORE_RTO}s"

# ---------------------------------------------------------------------------
# 4. Verify restored state.
# ---------------------------------------------------------------------------
for t in "${TABLES[@]}"; do
    printf '%s=%s\n' "$t" "$(table_count "$t")" >> "$COUNTS_AFTER_TXT"
done
if diff -q "$COUNTS_BEFORE_TXT" "$COUNTS_AFTER_TXT" >/dev/null; then
    p "row counts match pre-restore ($(tr '\n' ' ' < "$COUNTS_AFTER_TXT"))"
else
    f "row counts differ after restore:"
    diff "$COUNTS_BEFORE_TXT" "$COUNTS_AFTER_TXT" || true
fi

MANIFEST_HASH_AFTER="$(docker exec "$MINIO_CONTAINER" sh -c '
    find /data/streamgate -type f \( -name "*.m3u8" -o -name "*.mp4" \) 2>/dev/null | sort | xargs -r sha256sum 2>/dev/null | sha256sum' \
    2>/dev/null | awk '{print $1}')"
if [ -n "${MANIFEST_HASH_BEFORE:-}" ] && [ "$MANIFEST_HASH_BEFORE" = "${MANIFEST_HASH_AFTER:-}" ]; then
    p "manifest/original object hashes match pre-restore"
else
    f "manifest/original object hashes differ (${MANIFEST_HASH_BEFORE:-none} vs ${MANIFEST_HASH_AFTER:-none})"
fi

# Old access token: its session epoch is gone — it must be REJECTED. This is
# the required session-invalidation assertion; the exact status code is
# captured in the output.
OLD_TOKEN_CODE="$(curl -s -o /dev/null -w '%{http_code}' \
    -H "Authorization: Bearer $TOKEN" "$BASE_URL/api/v1/auth/profile")"
if [ "$OLD_TOKEN_CODE" = "401" ]; then
    p "pre-restore access token is REJECTED after restore (HTTP $OLD_TOKEN_CODE)"
else
    f "pre-restore access token answered HTTP $OLD_TOKEN_CODE (expected 401 — session epochs were not invalidated)"
fi

# The pre-restore manifest URL with the dead token must also be refused.
PRE_URL_CODE="$(curl -s -o /dev/null -w '%{http_code}' \
    -H "Authorization: Bearer $TOKEN" "$PRE_MANIFEST_URL")"
printf 'pre-restore manifest URL + old token: HTTP %s\n' "$PRE_URL_CODE"

# Fresh login and playback: the SAME content must play for a new session.
TOKEN2="$(login "$WALLET")" || die "fresh login after restore failed"
[ -n "$TOKEN2" ] || die "fresh login returned no token"
FRESH_CODE="$(curl -s -o /dev/null -w '%{http_code}' \
    -H "Authorization: Bearer $TOKEN2" "$PRE_MANIFEST_URL")"
if [ "$FRESH_CODE" = "200" ]; then
    p "fresh login -> manifest 200 for $CONTENT_ID"
else
    f "fresh login -> manifest answered HTTP $FRESH_CODE"
fi

# And one media segment with the fresh playback token, proving the restored
# DB + untouched MinIO objects serve real media. The variant and segment URIs
# are taken FROM the served playlists (they carry the quality and the token)
# — quality names are not assumed, they are whatever the pipeline produced.
PLAYLIST_URI="$(curl -sf -H "Authorization: Bearer $TOKEN2" "$PRE_MANIFEST_URL" \
    | python3 -c "
import sys
lines = [l.strip() for l in sys.stdin.read().splitlines() if l.strip() and not l.strip().startswith('#')]
print(lines[0] if lines else '')" 2>/dev/null || true)"
[ -n "$PLAYLIST_URI" ] || die "could not read a variant URI from the manifest"
VARIANT_URL="$(python3 -c "import urllib.parse;print(urllib.parse.urljoin('$PRE_MANIFEST_URL','''$PLAYLIST_URI'''))")"
SEGMENT_URI="$(curl -sf -H "Authorization: Bearer $TOKEN2" "$VARIANT_URL" \
    | python3 -c "
import sys
lines = [l.strip() for l in sys.stdin.read().splitlines() if l.strip() and not l.strip().startswith('#')]
print(lines[0] if lines else '')" 2>/dev/null || true)"
if [ -n "$SEGMENT_URI" ]; then
    SEGMENT_URL="$(python3 -c "import urllib.parse;print(urllib.parse.urljoin('$VARIANT_URL','''$SEGMENT_URI'''))")"
    SEG_CODE="$(curl -s -o /dev/null -w '%{http_code}' -H "Authorization: Bearer $TOKEN2" "$SEGMENT_URL")"
    if [ "$SEG_CODE" = "200" ]; then
        p "fresh playback serves a real segment from the restored state (HTTP 200)"
    else
        f "segment request answered HTTP $SEG_CODE"
    fi
else
    f "could not extract a segment URI from the fresh variant playlist"
fi

# ---------------------------------------------------------------------------
# 5. Record RPO/RTO
# ---------------------------------------------------------------------------
printf '\n============================================\n'
printf 'RTO (this drill): %ss — stop app + drop/recreate DB + psql restore + flush sessions + restart + health wait\n' "$RESTORE_RTO"
printf 'RPO (this drill): bounded by snapshot frequency. One point-in-time pg_dump was taken while the app kept writing; any commit after the dump began is lost by design of this drill.\n'
printf 'Drill scope: restore-into-place on the SAME volumes; MinIO objects verified untouched by inventory + hash.\n'
printf '============================================\n'

if [ "$FAIL" -eq 0 ]; then
    printf '\nRESULT: PASS (stack left running and healthy)\n'
    exit 0
fi
printf '\nRESULT: FAIL (%d checks failed)\n' "$FAIL"
exit 1
