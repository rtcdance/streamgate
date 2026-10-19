#!/usr/bin/env bash
# StreamGate fresh-environment recovery drill (TRANSITION_PLAN.md §0.2 C).
#
# WHERE THIS RUNS
#   Inside the verification controller, driven by scripts/verify-docker.sh's
#   `recovery` stage. The stage brings up a DEDICATED SOURCE stack, seeds it
#   through the full acceptance, allocates the target project's front ports and
#   writes the chain-forwarding compose override, then calls this script.
#
# WHAT THIS PROVES
#   A point-in-time snapshot restored into an INDEPENDENT project with EMPTY
#   volumes serves the same content again, while every credential minted before
#   the restore is dead:
#     1. the source stack stops business writes, its registry publisher stops
#        broadcasting (the process that runs it is stopped) and the in-flight
#        submissions are drained before anything is captured,
#     2. one consistent Postgres dump, the actual MinIO objects (with a
#        per-object hash list) and the Redis snapshot are captured,
#     3. an independent project with EMPTY volumes restores the database and the
#        objects,
#     4. a NEW Postgres restore_epoch is generated before any traffic arrives and
#        the stale cache entries are cleared,
#     5. the registry broadcast stays paused while the current chain, the known
#        transactions, their receipts and the nonce reservations are reconciled,
#     6. broadcast resumes only when that state is explainable and consistent;
#        an unknown nonce consumption keeps it paused and is reported,
#     7. the old access/refresh/capability credentials are all refused, a fresh
#        login actually plays media, and the RPO/RTO are recorded.
#
# HARD RULE: the existing sg-fc-* stacks are never touched — no DROP, no write.
# This script only ever operates on the two projects it is told it owns.
#
# Usage (called by the recovery stage; all parameters are required):
#   scripts/backup-restore-drill.sh \
#     --src-project P --dst-project Q --compose FILE --dst-extra FILE \
#     --src-api http://localhost:PORT --dst-api http://localhost:PORT \
#     --artifacts DIR --controller NAME \
#     [--candidate-sha SHA] [--content-id ID]
set -euo pipefail

CHAIN_ID="${CHAIN_ID:-31337}"
HELPER_IMAGE="${HELPER_IMAGE:-alpine:3.21@sha256:ce64758a109eb420d874a118f87920e625e12d3634e03b4a5573fd9f6e5d3507}"
WALLET_KEY="${WALLET_KEY:-0xac0974bec39a17e36ba4a6b4d238ff944bacb478cbed5efcae784d7bf4f2ff80}"

SRC_PROJECT=""; DST_PROJECT=""; COMPOSE=""; DST_EXTRA=""
SRC_API=""; DST_API=""; ARTIFACTS=""; CONTROLLER=""
CONTENT_ID="${CONTENT_ID:-}"
CANDIDATE_SHA="${CANDIDATE_SHA:-unknown}"

usage() {
    sed -n '2,38p' "$0" | sed 's/^# \{0,1\}//'
    exit 2
}

while [ "$#" -gt 0 ]; do
    case "$1" in
        --src-project)  SRC_PROJECT="${2:-}"; shift 2 ;;
        --dst-project)  DST_PROJECT="${2:-}"; shift 2 ;;
        --compose)      COMPOSE="${2:-}"; shift 2 ;;
        --dst-extra)    DST_EXTRA="${2:-}"; shift 2 ;;
        --src-api)      SRC_API="${2:-}"; shift 2 ;;
        --dst-api)      DST_API="${2:-}"; shift 2 ;;
        --artifacts)    ARTIFACTS="${2:-}"; shift 2 ;;
        --controller)   CONTROLLER="${2:-}"; shift 2 ;;
        --candidate-sha) CANDIDATE_SHA="${2:-}"; shift 2 ;;
        --content-id)   CONTENT_ID="${2:-}"; shift 2 ;;
        -h|--help)      usage ;;
        *) printf 'unknown argument: %s\n' "$1" >&2; usage ;;
    esac
done

for required in SRC_PROJECT DST_PROJECT COMPOSE DST_EXTRA SRC_API DST_API ARTIFACTS CONTROLLER; do
    eval "value=\${$required}"
    [ -n "$value" ] || { printf 'missing --%s\n' "$(printf '%s' "$required" | tr '_' '-')" >&2; usage; }
done
[ "$SRC_PROJECT" != "$DST_PROJECT" ] || { printf 'the source and target projects must differ\n' >&2; exit 2; }

SRC_ANVIL="${SRC_PROJECT}-anvil-1"
SRC_PG="${SRC_PROJECT}-postgres-1"
SRC_REDIS="${SRC_PROJECT}-redis-1"
DST_PG="${DST_PROJECT}-postgres-1"
DST_REDIS="${DST_PROJECT}-redis-1"

PRIVATE_DIR=""
OBJECTS_BEFORE="${ARTIFACTS}/objects-before.json"
OBJECTS_AFTER="${ARTIFACTS}/objects-after.json"
COUNTS_BEFORE="${ARTIFACTS}/counts-before.txt"
COUNTS_AFTER="${ARTIFACTS}/counts-after.txt"
REGISTRY_STATE="${ARTIFACTS}/registry-state.tsv"
REPORT="${ARTIFACTS}/recovery-report.json"

FAIL=0
RESULTS_FILE="${ARTIFACTS}/checks.tsv"
p() { printf 'PASS: %s\n' "$*"; printf 'PASS\t%s\n' "$*" >> "$RESULTS_FILE"; }
f() { printf 'FAIL: %s\n' "$*"; printf 'FAIL\t%s\n' "$*" >> "$RESULTS_FILE"; FAIL=$((FAIL + 1)); }
die() { printf 'FATAL: %s\n' "$*" >&2; exit 1; }

cleanup_private() {
    local original=$? cleanup_rc=0
    trap - EXIT INT TERM
    if [ -n "$PRIVATE_DIR" ]; then rm -rf -- "$PRIVATE_DIR" || cleanup_rc=1; fi
    if [ "$original" = 0 ] && [ "$cleanup_rc" != 0 ]; then original=1; fi
    exit "$original"
}

require_tool() { command -v "$1" >/dev/null 2>&1 || die "required tool $1 not found"; }

# Everything below talks to the project network directly or through the
# controller-local front ports the stage allocated — no host port is published.
src_compose() { docker compose -p "$SRC_PROJECT" -f "$COMPOSE" "$@"; }
dst_compose() { docker compose -p "$DST_PROJECT" -f "$COMPOSE" -f "$DST_EXTRA" "$@"; }

# Registry / chain reads go through the source anvil container: the foundry image
# ships cast, and the chain is the one both stacks share, so there is nothing to
# publish and nothing to reach over HTTP.
anvil_cast() { timeout 20s docker exec "$SRC_ANVIL" cast "$@" --rpc-url "http://localhost:8545"; }

# cast_keccak <data> — `cast keccak` takes NO `--rpc-url` (it needs no chain at
# all, and clap rejects the flag with exit 2), so it cannot go through
# `anvil_cast`. Routing it there produced an empty hash with the error swallowed
# by `2>/dev/null`, which read as a chain/record mismatch rather than a bug in
# this script.
cast_keccak() { docker exec "$SRC_ANVIL" cast keccak "$1"; }

src_psql() {
    timeout 15s docker exec "$SRC_PG" psql -v ON_ERROR_STOP=1 -U postgres -d streamgate -tAc "$1"
}
dst_psql() {
    timeout 15s docker exec "$DST_PG" psql -v ON_ERROR_STOP=1 -U postgres -d streamgate -tAc "$1"
}

json_get() {
    # Foundry 1.8.5 wraps tx/block in a v1 envelope; receipts and API responses
    # remain flat. A failed or unknown envelope is never a successful read.
    printf '%s' "$1" | python3 -c '
import json, sys
document = json.load(sys.stdin)
if not isinstance(document, dict):
    sys.exit(1)
if "schema_version" in document:
    if (type(document["schema_version"]) is not int or document["schema_version"] != 1
            or document.get("success") is not True or document.get("errors") != []
            or not isinstance(document.get("data"), dict)):
        sys.exit(1)
    document = document["data"]
value = document.get(sys.argv[1])
if not isinstance(value, (str, int, float)) or isinstance(value, bool):
    sys.exit(1)
print(value)
' "$2"
}

# normalize_number <value> — a chain JSON field may be a hex string ("0x2a") or
# a bare number depending on the cast build, so comparisons go through this
# rather than through a string equality that silently depends on the format.
normalize_number() {
    python3 -c "
import sys
value = sys.argv[1].strip()
try:
    print(int(value, 16) if value[:2].lower() == '0x' else int(value))
except Exception:
    sys.exit(1)
" "$1" 2>/dev/null || true
}

http_code() { # http_code <method> <url> [header...]
    local method="$1" url="$2"; shift 2
    curl -s -o /dev/null -w '%{http_code}' -X "$method" --max-time 20 "$@" "$url" 2>/dev/null || echo 000
}

http_body() { # http_body <method> <url> [args...]
    local method="$1" url="$2"; shift 2
    curl -s --max-time 20 -X "$method" "$@" "$url" 2>/dev/null || true
}

http_download() { # http_download <url> <file> — the body lands in the file, the code is printed
    local url="$1" file="$2"
    curl -s --max-time 30 -o "$file" -w '%{http_code}' "$url" 2>/dev/null || echo 000
}

login() { # login <base-url> — challenge -> personal_sign -> login; prints the token
    local base="$1" wallet challenge cid msg sig resp
    wallet="$(cast wallet address --private-key "$WALLET_KEY")" || return 1
    challenge="$(curl -sf --max-time 20 -X POST "$base/api/v1/auth/challenge" \
        -H 'Content-Type: application/json' \
        -d "{\"wallet\":\"$wallet\",\"chain_id\":$CHAIN_ID}")" || return 1
    cid="$(json_get "$challenge" challenge_id)"
    msg="$(json_get "$challenge" message)"
    [ -n "$cid" ] && [ -n "$msg" ] || return 1
    sig="$(cast wallet sign --private-key "$WALLET_KEY" "$msg")" || return 1
    resp="$(curl -sf --max-time 20 -X POST "$base/api/v1/auth/login" \
        -H 'Content-Type: application/json' \
        -d "{\"wallet\":\"$wallet\",\"chain_id\":$CHAIN_ID,\"challenge_id\":\"$cid\",\"signature\":\"$sig\"}")" || return 1
    json_get "$resp" token
}

# relative_url <absolute-url> — path plus query, so a capability captured on the
# source front can be replayed against the target front.
relative_url() {
    python3 - "$1" <<'PY'
import sys, urllib.parse
parts = urllib.parse.urlsplit(sys.argv[1])
print(parts.path + (("?" + parts.query) if parts.query else ""))
PY
}

# first_uri <playlist-body> — the first non-comment line, i.e. the first variant
# or segment URI the served playlist actually names.
first_uri() {
    python3 -c "
import sys
lines = [l.strip() for l in sys.stdin.read().splitlines() if l.strip() and not l.strip().startswith('#')]
print(lines[0] if lines else '')"
}

wait_health() { # wait_health <url> <seconds>
    local url="$1" deadline=$((SECONDS + $2))
    while [ "$SECONDS" -lt "$deadline" ]; do
        [ "$(http_code GET "$url")" = "200" ] && return 0
        sleep 2
    done
    return 1
}

# wait_forwarder <container-name> — the chain forwarder carries no healthcheck
# (the socat image has no probe utility), so `docker compose up --wait` refuses
# it as "no healthcheck configured". Readiness is therefore asserted through the
# chain itself: cast, run from the controller, must reach the SOURCE chain BY THE
# FORWARDER'S CONTAINER NAME (both stacks' containers are reachable by name on
# the shared network).
wait_forwarder() {
    local host="$1" deadline=$((SECONDS + 60))
    while [ "$SECONDS" -lt "$deadline" ]; do
        if [ "$(cast chain-id --rpc-url "http://${host}:8545" 2>/dev/null | tr -d '[:space:]')" = "$CHAIN_ID" ]; then
            return 0
        fi
        sleep 2
    done
    return 1
}

wait_rows() { # wait_rows <psql-fn> <sql> <want> <seconds> <label>
    local fn="$1" sql="$2" want="$3" deadline=$((SECONDS + $4)) label="$5" got
    while [ "$SECONDS" -lt "$deadline" ]; do
        got="$($fn "$sql" 2>/dev/null || echo '?')"
        [ "$got" = "$want" ] && return 0
        sleep 2
    done
    printf '  %s: last observed value %s (wanted %s)\n' "$label" "${got:-none}" "$want" >&2
    return 1
}

quiesce_source() {
    # This dedicated stack has no published ports. The drill is its only writer
    # and stops issuing mutations here; keep the worker/publisher alive to drain.
    src_compose stop api-gateway >/dev/null || die "could not stop the source gateway"
    wait_rows src_psql "SELECT COUNT(*) FROM transcoding_tasks WHERE status NOT IN ('completed','failed','cancelled')" 0 60 "active transcodes" \
        || die "source transcodes did not drain within 60s"
    wait_rows src_psql "SELECT COUNT(*) FROM outbox WHERE status = 'pending'" 0 60 "pending outbox" \
        || die "source outbox did not drain within 60s"
    wait_rows src_psql "SELECT COUNT(*) FROM content_registry_intents WHERE chain_id = ${CHAIN_ID} AND status NOT IN ('confirmed','failed')" 0 60 "registry submissions" \
        || die "source registry submissions did not drain within 60s"
    src_compose stop monolith transcoder >/dev/null || die "could not pause source writers"
    p "source tasks/outbox/registry drained before stopping worker and publisher"
}

mkdir -p "$ARTIFACTS"
umask 077
mkdir -p "$ARTIFACTS/.private"
PRIVATE_DIR="$(mktemp -d "$ARTIFACTS/.private/recovery-XXXXXXXX")"
trap cleanup_private EXIT
trap 'exit 130' INT
trap 'exit 143' TERM
PG_DUMP="$PRIVATE_DIR/pg-dump.sql"
OBJECTS_BACKUP="$PRIVATE_DIR/objects"
REDIS_RDB="$PRIVATE_DIR/redis-dump.rdb"
: > "$RESULTS_FILE"

DRILL_START=$SECONDS
printf '============================================\n'
printf 'StreamGate fresh-environment recovery drill\n'
printf 'candidate: %s\n' "$CANDIDATE_SHA"
printf 'source project: %s  target project: %s\n' "$SRC_PROJECT" "$DST_PROJECT"
printf 'source API: %s  target API: %s\n' "$SRC_API" "$DST_API"
printf '============================================\n'

# ---------------------------------------------------------------------------
# 0. Preflight
# ---------------------------------------------------------------------------
require_tool docker
require_tool curl
require_tool python3
require_tool cast
require_tool ffmpeg
python3 - "$SRC_PROJECT" "$DST_PROJECT" <<'PY'
import re, sys
for project in sys.argv[1:]:
    if not re.fullmatch(r'[a-z0-9][a-z0-9-]{0,62}', project) or project.startswith('sg-fc'):
        raise SystemExit('recovery requires dedicated project names; sg-fc is forbidden')
PY
[[ "$CHAIN_ID" =~ ^[1-9][0-9]*$ ]] || die "chain ID must be a positive integer"
TARGET_CONTAINERS="$(docker ps -aq --filter "label=com.docker.compose.project=$DST_PROJECT")" \
    || die "cannot check target containers"
TARGET_VOLUMES="$(docker volume ls -q --filter "label=com.docker.compose.project=$DST_PROJECT")" \
    || die "cannot check target volumes"
[ -z "$TARGET_CONTAINERS$TARGET_VOLUMES" ] || die "target project is not empty"
for c in "$SRC_ANVIL" "$SRC_PG" "$SRC_REDIS"; do
    docker inspect "$c" >/dev/null 2>&1 || die "the source stack is not up: $c is missing"
    OWNER="$(docker inspect --format '{{index .Config.Labels "com.docker.compose.project"}}' "$c")"
    [ "$OWNER" = "$SRC_PROJECT" ] || die "source container ownership mismatch"
done
docker inspect "$HELPER_IMAGE" >/dev/null 2>&1 || die "helper image $HELPER_IMAGE is not present locally"
WALLET="$(cast wallet address --private-key "$WALLET_KEY")"
printf 'wallet: %s\n' "$WALLET"

if [ -z "$CONTENT_ID" ]; then
    CONTENT_ID="$(src_psql "SELECT id FROM contents WHERE published_generation > 0 AND length(file_hash) = 64 ORDER BY created_at DESC LIMIT 1" | tr -d '[:space:]')"
fi
[ -n "$CONTENT_ID" ] || die "the source database has no content to restore"
python3 - "$CONTENT_ID" <<'PY'
import sys, uuid
if str(uuid.UUID(sys.argv[1])) != sys.argv[1]:
    raise SystemExit('content ID must be a canonical UUID')
PY
printf 'content: %s\n\n' "$CONTENT_ID"

MANIFEST_FILE="${REGISTRY_MANIFEST:-deployments/content-registry.${CHAIN_ID}.json}"
[ -f "$MANIFEST_FILE" ] || die "deployment records are missing: $MANIFEST_FILE"
MANIFEST_ADDR="$(python3 -c "import json;print(json.load(open('$MANIFEST_FILE'))['address'])" 2>/dev/null || true)"
MANIFEST_CODE_HASH="$(python3 -c "import json;print(json.load(open('$MANIFEST_FILE'))['runtime_code_hash'])" 2>/dev/null || true)"
MANIFEST_ABI_HASH="$(python3 -c "import json;print(json.load(open('$MANIFEST_FILE'))['abi_hash'])" 2>/dev/null || true)"
[ -n "$MANIFEST_ADDR" ] || die "the deployment record has no address: $MANIFEST_FILE"

# ---------------------------------------------------------------------------
# Pre-restore state (step 2 captures it): the source is STILL serving, so the
# outstanding credentials this drill must invalidate can be captured at all.
# ---------------------------------------------------------------------------
printf '===== pre-restore credentials (the source is still serving) =====\n'
OLD_TOKEN="$(login "$SRC_API")" || die "wallet login against the source failed"
[ -n "$OLD_TOKEN" ] || die "the source login returned no token"
OLD_PROFILE_CODE="$(http_code GET "$SRC_API/api/v1/auth/profile" -H "Authorization: Bearer $OLD_TOKEN")"
[ "$OLD_PROFILE_CODE" = "200" ] \
    && p "the pre-restore access token is accepted by the source (HTTP 200)" \
    || f "the pre-restore access token answered HTTP $OLD_PROFILE_CODE on the source — the drill needs a live credential to invalidate"

OLD_MANIFEST_PATH="/api/v1/streaming/${CONTENT_ID}/manifest.m3u8"
OLD_MANIFEST_CODE="$(http_code GET "${SRC_API}${OLD_MANIFEST_PATH}" -H "Authorization: Bearer $OLD_TOKEN")"
[ "$OLD_MANIFEST_CODE" = "200" ] \
    && p "the pre-restore access token plays content ${CONTENT_ID} on the source (HTTP 200)" \
    || die "the source manifest answered HTTP $OLD_MANIFEST_CODE — run the seed acceptance against this stack first"

OLD_VARIANT_REL="$(relative_url "$(http_body GET "${SRC_API}${OLD_MANIFEST_PATH}" -H "Authorization: Bearer $OLD_TOKEN" | first_uri)")"
case "$OLD_VARIANT_REL" in
    *playback_token=*) p "captured a pre-restore capability (variant URI carries playback_token)" ;;
    *) f "the served playlist carries no playback_token — the capability to invalidate was not captured" ;;
esac

# ---------------------------------------------------------------------------
# 1. Stop business writes, pause the registry broadcast, drain in-flight work
# ---------------------------------------------------------------------------
printf '\n===== 1. source: stop writes, pause broadcast, drain in-flight =====\n'
quiesce_source
OUTBOX_PENDING=0

# ---------------------------------------------------------------------------
# 2. Consistent snapshot: Postgres, the actual objects, Redis, registry state
# ---------------------------------------------------------------------------
printf '\n===== 2. consistent snapshot =====\n'
docker exec "$SRC_PG" pg_dump -U postgres streamgate > "$PG_DUMP" || die "pg_dump failed"
[ -s "$PG_DUMP" ] || die "pg_dump produced an empty file"
p "Postgres snapshot captured ($(wc -c < "$PG_DUMP" | tr -d ' ') bytes, application stopped)"

timeout 600s python3 scripts/cd/recovery_objects.py backup --container "${SRC_PROJECT}-minio-1" \
    --directory "$OBJECTS_BACKUP" --output "$OBJECTS_BEFORE" \
    || die "object backup failed"
OBJECT_COUNT="$(python3 -c 'import json,sys; print(len(json.load(open(sys.argv[1]))))' "$OBJECTS_BEFORE")"
[ "$OBJECT_COUNT" -gt 0 ] || die "the object store backup captured nothing — the restore would prove nothing"
p "logical S3 object backup captured ($OBJECT_COUNT object(s), each hashed in objects-before.json)"

docker exec "$SRC_REDIS" redis-cli SAVE >/dev/null 2>&1 || die "the Redis snapshot (SAVE) failed"
docker cp "${SRC_REDIS}:/data/dump.rdb" "$REDIS_RDB" >/dev/null || die "could not copy the Redis snapshot"
[ -s "$REDIS_RDB" ] || die "the Redis snapshot is empty"
p "Redis snapshot captured ($(wc -c < "$REDIS_RDB" | tr -d ' ') bytes)"

TABLES=(contents content_gating_rules content_registry_intents tx_nonce_reservations tx_signed_attempts http_idempotency outbox transcoding_tasks auth_control_state)
: > "$COUNTS_BEFORE"
for t in "${TABLES[@]}"; do
    ROW_COUNT="$(src_psql "SELECT COUNT(*) FROM $t")" || die "source table count failed: $t"
    [[ "$ROW_COUNT" =~ ^[0-9]+$ ]] || die "source table count is unverifiable: $t"
    printf '%s=%s\n' "$t" "$ROW_COUNT" >> "$COUNTS_BEFORE"
done
EPOCH_BEFORE="$(src_psql "SELECT restore_epoch FROM auth_control_state WHERE id = 1" | tr -d '[:space:]')"
printf 'restore_epoch before restore: %s\n' "${EPOCH_BEFORE:-<none>}"
[ -n "$EPOCH_BEFORE" ] || die "the source has no restore epoch row — migration 042 did not apply"

REGISTRY_ADDR="$(src_psql "SELECT registry_address FROM content_registry_intents WHERE chain_id = ${CHAIN_ID} LIMIT 1" | tr -d '[:space:]')"
[ -n "$REGISTRY_ADDR" ] || REGISTRY_ADDR="$MANIFEST_ADDR"
SIGNER_ADDR="$(src_psql "SELECT signer FROM tx_nonce_reservations WHERE chain_id = ${CHAIN_ID} ORDER BY nonce DESC LIMIT 1" | tr -d '[:space:]')"
if [ -z "$SIGNER_ADDR" ]; then
    # No registration was submitted yet. The registry signer is then still the
    # stack's deployer key, which is the wallet this drill signs in with.
    SIGNER_ADDR="$(cast wallet address --private-key "$WALLET_KEY")"
    printf '  note: no nonce reservation in the snapshot; using the deployer signer %s for the chain reads\n' "$SIGNER_ADDR"
fi
docker exec "$SRC_PG" psql -v ON_ERROR_STOP=1 -U postgres -d streamgate -tA -F $'\t' -c \
    "SELECT content_id, COALESCE(NULLIF(tx_hash, ''), '-'), COALESCE(block_number, 0), COALESCE(NULLIF(block_hash, ''), '-'), status, COALESCE(nonce::text, '-') FROM content_registry_intents WHERE chain_id = ${CHAIN_ID} ORDER BY content_id" \
    > "$REGISTRY_STATE" || die "could not read the registry state"
INTENT_ROWS="$(wc -l < "$REGISTRY_STATE" | tr -d ' ')"
[ "$INTENT_ROWS" -gt 0 ] || die "no registry history was captured; receipt reconciliation would prove nothing"
CONFIRMED_ROWS="$(src_psql "SELECT COUNT(*) FROM content_registry_intents WHERE chain_id = ${CHAIN_ID} AND status = 'confirmed' AND tx_hash <> '' AND block_hash <> ''")"
[[ "$CONFIRMED_ROWS" =~ ^[0-9]+$ ]] && [ "$CONFIRMED_ROWS" -gt 0 ] \
    || die "no confirmed registration receipt was captured"
p "registry state recorded ($INTENT_ROWS intent row(s), signer ${SIGNER_ADDR}, registry ${REGISTRY_ADDR})"

# The signer's nonce AS OF THE SNAPSHOT. Step 5 compares against it: with every
# writer stopped, the chain's nonce may not move at all, and any movement is a
# nonce consumption nothing in this snapshot can account for.
SNAPSHOT_NONCE="$(anvil_cast nonce "$SIGNER_ADDR" 2>/dev/null | tr -d '[:space:]' || true)"
[ -n "$SNAPSHOT_NONCE" ] || die "could not read the signer's nonce from the chain"
p "chain nonce for the signer recorded at the snapshot (${SNAPSHOT_NONCE})"

# The source has served its purpose: stop the rest of its infrastructure so the
# target's resources are not competing with it. The CHAIN KEEPS RUNNING — a real
# recovery restores state onto a chain that survives the failure, and the
# reconciliation in step 5 needs that very chain.
src_compose stop postgres redis minio nats >/dev/null || die "could not quiesce the source infrastructure"
p "quiesced the source database/redis/minio/queue (the chain stays up: it is the same chain the target must be reconciled against)"

# ---------------------------------------------------------------------------
# 3. Target project with EMPTY volumes: restore the DB and the objects
# ---------------------------------------------------------------------------
printf '\n===== 3. target project %s with empty volumes =====\n' "$DST_PROJECT"
# `create` materialises the containers AND their volumes without starting them,
# so the restored data can be written into the volumes before the services that
# read it come up.
dst_compose create --pull never postgres redis minio nats consul anvil >/dev/null \
    || die "could not create the target project's containers and volumes"

docker run --rm --label "com.docker.compose.project=$DST_PROJECT" \
    -v "${DST_PROJECT}_redis-data:/data" -v "${PRIVATE_DIR}:/backup:ro" "$HELPER_IMAGE" \
    sh -c 'cp /backup/redis-dump.rdb /data/dump.rdb' || die "Redis restore failed"
p "the Redis snapshot restored into a FRESH target volume"

dst_compose up -d --no-build --no-deps --pull never --wait --wait-timeout 240 postgres redis minio nats consul >/dev/null \
    || die "the target infrastructure did not become ready"
timeout 600s python3 scripts/cd/recovery_objects.py restore --container "${DST_PROJECT}-minio-1" \
    --directory "$OBJECTS_BACKUP" || die "logical object restore failed"
p "logical objects restored through S3 into the fresh target bucket"
# The chain forwarder is started WITHOUT --wait: it carries no healthcheck, and
# `--wait` refuses a service that has none. Its readiness is asserted through the
# chain itself, which is the only question that matters for the apps.
dst_compose up -d --no-build --no-deps --pull never anvil >/dev/null \
    || die "the target chain forwarder did not start"
wait_forwarder "${DST_PROJECT}-anvil-1" \
    || die "the target chain forwarder never forwarded to the chain"
p "target infrastructure up (postgres/redis/minio/nats/consul, and the chain forwarder reaches chain ${CHAIN_ID})"

# The target application is read through the controller-local front proxy, which
# resolves the project's SERVICE ALIASES ("monolith", "api-gateway", ...) on the
# project network at connection time. The controller therefore has to BE on that
# network; without this every request from here to the restored stack would be a
# resolution failure, indistinguishable from a health failure. (The source stack
# gets this in its bring-up; the target is created here, so it is attached here.)
docker network connect "${DST_PROJECT}_default" "$CONTROLLER" \
    || die "could not attach the controller to the target project network"
p "the controller is on the target project network (its front can reach the restored stack)"

docker exec -i "$DST_PG" psql -U postgres -d streamgate -v ON_ERROR_STOP=1 < "$PG_DUMP" >/dev/null \
    || die "the Postgres dump did not apply cleanly to the target"
RESTORED="$(dst_psql "SELECT COUNT(*) FROM contents")"
EXPECTED="$(sed -n 's/^contents=//p' "$COUNTS_BEFORE")"
[ "$RESTORED" = "$EXPECTED" ] \
    && p "the database restored into the empty target database (contents: $RESTORED row(s))" \
    || f "contents rows after restore: $RESTORED, expected $EXPECTED"

RESTORED_CODE="$(dst_psql "SELECT COUNT(*) FROM content_gating_rules")"
EXPECTED_CODE="$(sed -n 's/^content_gating_rules=//p' "$COUNTS_BEFORE")"
[ "$RESTORED_CODE" = "$EXPECTED_CODE" ] \
    && p "gating rules restored ($RESTORED_CODE row(s))" \
    || f "content_gating_rules rows after restore: $RESTORED_CODE, expected $EXPECTED_CODE"

RESTORED_EPOCH="$(dst_psql "SELECT restore_epoch FROM auth_control_state WHERE id = 1" | tr -d '[:space:]')"
[ "$RESTORED_EPOCH" = "$EPOCH_BEFORE" ] \
    && p "the restored snapshot carries the SOURCE restore epoch (${RESTORED_EPOCH:0:8}…) — the snapshot alone does not revoke anything" \
    || f "the restored restore epoch ($RESTORED_EPOCH) differs from the source one ($EPOCH_BEFORE)"

# ---------------------------------------------------------------------------
# 4. New restore epoch + stale caches cleared, BEFORE any traffic arrives
# ---------------------------------------------------------------------------
printf '\n===== 4. new restore epoch and no stale cache =====\n'
NEW_EPOCH="$(python3 -c 'import uuid; print(uuid.uuid4())')"
docker exec "$DST_PG" psql -U postgres -d streamgate -c \
    "UPDATE auth_control_state SET restore_epoch = '${NEW_EPOCH}', updated_at = NOW() WHERE id = 1" >/dev/null \
    || die "could not rotate the restore epoch"
READBACK="$(dst_psql "SELECT restore_epoch FROM auth_control_state WHERE id = 1" | tr -d '[:space:]')"
[ "$READBACK" = "$NEW_EPOCH" ] \
    && p "a NEW restore epoch was generated before traffic (${NEW_EPOCH:0:8}… vs ${EPOCH_BEFORE:0:8}…)" \
    || f "the rotated restore epoch did not persist ($READBACK)"

# Cache keys only: the SESSIONS from the snapshot must survive, otherwise the
# revocation proof would be trivial (nothing left to revoke) instead of the real
# question — can a restored Redis snapshot revive credentials?
SESSION_KEYS=0
for db in $(seq 0 15); do
    n="$(docker exec "$DST_REDIS" sh -c "redis-cli -n $db --scan --pattern 'streamgate:session:*' 2>/dev/null | wc -l" | tr -d '[:space:]')"
    SESSION_KEYS=$((SESSION_KEYS + ${n:-0}))
done
[ "$SESSION_KEYS" -gt 0 ] \
    && p "the restored Redis snapshot still carries $SESSION_KEYS session key(s) — revocation is not trivial here" \
    || f "the restored Redis snapshot carries no session keys; the credential proof would be vacuous"

CLEARED=0
for db in $(seq 0 15); do
    for pattern in 'streamgate:restore-epoch' 'streamgate:rulever:*'; do
        n="$(docker exec "$DST_REDIS" sh -c "redis-cli -n $db --scan --pattern '$pattern' 2>/dev/null | wc -l" | tr -d '[:space:]')"
        if [ "${n:-0}" -gt 0 ]; then
            docker exec "$DST_REDIS" sh -c "redis-cli -n $db --scan --pattern '$pattern' 2>/dev/null | xargs -r redis-cli -n $db DEL" >/dev/null
            CLEARED=$((CLEARED + n))
        fi
    done
done
p "stale cache entries cleared before traffic ($CLEARED key(s): restore-epoch and rule-version caches)"
REMAINING="$(docker exec "$DST_REDIS" sh -c "redis-cli -n 1 --scan --pattern 'streamgate:restore-epoch' 2>/dev/null | wc -l" | tr -d '[:space:]')"
[ "${REMAINING:-1}" = "0" ] \
    && p "no cached restore epoch is left to serve the pre-restore value" \
    || f "a cached restore epoch is still present in the target Redis"

# ---------------------------------------------------------------------------
# 5. Registry reconciliation with the broadcast still PAUSED
# ---------------------------------------------------------------------------
printf '\n===== 5. registry reconciliation (broadcast still paused) =====\n'
# Only the chain checks below may keep the broadcast paused. A failure anywhere
# else (an object count, a row count) is a drill failure, not an unexplainable
# chain, and must not be reported as one.
CHAIN_INCONSISTENT=0
CHAIN_SEEN="$(anvil_cast chain-id 2>/dev/null | tr -d '[:space:]')"
[ "$CHAIN_SEEN" = "$CHAIN_ID" ] \
    && p "the current chain is the chain the snapshot's history was written to (chain id $CHAIN_SEEN)" \
    || { f "the reachable chain id is $CHAIN_SEEN, expected $CHAIN_ID"; CHAIN_INCONSISTENT=1; }

REG_CODE="$(anvil_cast code "$REGISTRY_ADDR" 2>/dev/null || true)"
if [ -n "$REG_CODE" ] && [ "$REG_CODE" != "0x" ]; then
    p "the registry contract has runtime code at $REGISTRY_ADDR"
else
    f "no runtime code at the registry address $REGISTRY_ADDR"
    CHAIN_INCONSISTENT=1
fi
OBSERVED_CODE_HASH="$(cast_keccak "$REG_CODE" 2>/dev/null | tr -d '[:space:]' || true)"
if [ "$OBSERVED_CODE_HASH" = "$MANIFEST_CODE_HASH" ]; then
    p "the chain's runtime code hash matches the deployment record ($MANIFEST_CODE_HASH)"
else
    f "runtime code hash mismatch: chain ${OBSERVED_CODE_HASH:-<none>} vs record $MANIFEST_CODE_HASH"
    # The chain is then running a different contract than the one the deployment
    # record names. That is an unexplainable environment, not a cosmetic delta:
    # resuming the broadcast would register content against code nobody verified.
    CHAIN_INCONSISTENT=1
fi
if [ "$(printf '%s' "$REGISTRY_ADDR" | tr '[:upper:]' '[:lower:]')" = "$(printf '%s' "$MANIFEST_ADDR" | tr '[:upper:]' '[:lower:]')" ]; then
    p "the registry address in the snapshot matches the deployment record"
else
    f "registry address mismatch: snapshot $REGISTRY_ADDR vs record $MANIFEST_ADDR"
    CHAIN_INCONSISTENT=1
fi

RECEIPT_MISMATCH=0
while IFS=$'\t' read -r cid txh blk bhash status nonce; do
    [ -n "$txh" ] && [ "$txh" != '-' ] || continue
    if ! [[ "$txh" =~ ^0x[0-9a-fA-F]{64}$ && "$nonce" =~ ^[0-9]+$ && "$blk" =~ ^[0-9]+$ ]]; then
        f "invalid recorded transaction identity for content $cid"
        RECEIPT_MISMATCH=$((RECEIPT_MISMATCH + 1))
        continue
    fi
    receipt="$(anvil_cast receipt --json "$txh" 2>/dev/null || true)"
    if [ -z "$receipt" ]; then
        f "recorded transaction $txh (content $cid) is not known to the chain"
        RECEIPT_MISMATCH=$((RECEIPT_MISMATCH + 1))
        continue
    fi
    r_blk="$(normalize_number "$(json_get "$receipt" blockNumber)")"
    r_status="$(normalize_number "$(json_get "$receipt" status)")"
    r_hash="$(printf '%s' "$(json_get "$receipt" blockHash)" | tr '[:upper:]' '[:lower:]')"
    b_hash="$(printf '%s' "$bhash" | tr '[:upper:]' '[:lower:]')"
    canonical="$(anvil_cast block --json "$blk" 2>/dev/null || true)"
    canonical_hash="$(printf '%s' "$(json_get "$canonical" hash)" | tr '[:upper:]' '[:lower:]')"
    chain_tx="$(anvil_cast tx --json "$txh" 2>/dev/null || true)"
    chain_tx_nonce="$(normalize_number "$(json_get "$chain_tx" nonce)")"
    chain_tx_from="$(printf '%s' "$(json_get "$chain_tx" from)" | tr '[:upper:]' '[:lower:]')"
    signer_lower="$(printf '%s' "$SIGNER_ADDR" | tr '[:upper:]' '[:lower:]')"
    ledger_matches="$(dst_psql "SELECT COUNT(*) FROM tx_signed_attempts a JOIN tx_nonce_reservations r ON r.chain_id = a.chain_id AND r.signer = a.signer AND r.nonce = a.nonce WHERE a.chain_id = ${CHAIN_ID} AND a.tx_hash = '${txh}' AND a.nonce = ${nonce} AND LOWER(a.signer) = '${signer_lower}' AND a.raw_tx <> ''")"
    printf '  reconciliation: tx=%s canonical=%s receipt=%s chainNonce=%s recordedNonce=%s signerMatch=%s ledgerMatches=%s\n' \
        "$txh" "${canonical_hash:-missing}" "${r_hash:-missing}" "${chain_tx_nonce:-missing}" "$nonce" \
        "$([ "$chain_tx_from" = "$signer_lower" ] && echo yes || echo no)" "$ledger_matches"
    if [ "${r_status:-x}" = "1" ] && [ "${r_blk:-x}" = "$blk" ] && [ "$r_hash" = "$b_hash" ] \
        && [ "$canonical_hash" = "$b_hash" ] && [ "$chain_tx_nonce" = "$nonce" ] \
        && [ "$chain_tx_from" = "$signer_lower" ] && [ "$ledger_matches" = 1 ]; then
        printf '  content %s: tx %s confirmed in block %s, receipt block hash still canonical\n' "$cid" "${txh:0:18}…" "$blk"
    else
        f "receipt mismatch for $txh: block ${r_blk:-none} hash ${r_hash:-none} status ${r_status:-none} (recorded block $blk hash $bhash)"
        RECEIPT_MISMATCH=$((RECEIPT_MISMATCH + 1))
    fi
done < "$REGISTRY_STATE"
if [ "$RECEIPT_MISMATCH" -eq 0 ]; then
    p "every recorded registration transaction is known to the chain and its receipt block is still canonical"
else
    f "$RECEIPT_MISMATCH recorded transaction(s) could not be reconciled"
    # A recorded registration the chain cannot account for (unknown, or confirmed
    # in a block that is no longer canonical) is exactly the "state is not
    # explainable" case step 6 is written around.
    CHAIN_INCONSISTENT=1
fi

# The ledger is read from the RESTORED database, not from the source: step 2
# stopped the source's Postgres, so a read there answers nothing (and its failure
# would be misread as a missing reservation rather than a stopped container).
EXPLAINED_MAX="$(dst_psql "
SELECT GREATEST(
  COALESCE((SELECT MAX(nonce) FROM tx_nonce_reservations WHERE chain_id = ${CHAIN_ID} AND signer = '${SIGNER_ADDR}'), -1),
  COALESCE((SELECT MAX(nonce) FROM tx_signed_attempts  WHERE chain_id = ${CHAIN_ID} AND signer = '${SIGNER_ADDR}'), -1),
  COALESCE((SELECT MAX(nonce) FROM content_registry_intents WHERE chain_id = ${CHAIN_ID}), -1))" | tr -d '[:space:]')"
[[ "$EXPLAINED_MAX" =~ ^-?[0-9]+$ ]] || die "restored nonce ledger is unverifiable"
CHAIN_NONCE="$(anvil_cast nonce "$SIGNER_ADDR" 2>/dev/null | tr -d '[:space:]' || true)"
printf 'nonce ledger: signer %s recorded through nonce %s; chain nonce at the snapshot %s, now %s\n' \
    "$SIGNER_ADDR" "${EXPLAINED_MAX:-?}" "${SNAPSHOT_NONCE:-?}" "${CHAIN_NONCE:-?}"
# The writers were all stopped, so the sole thing that may consume a nonce is a
# broadcast. The invariant is therefore "the chain's nonce did not move": any
# advance is a nonce nothing in the snapshot accounts for, and an unknown nonce
# consumption means an in-flight submission the restored state cannot explain.
if [ -n "${SNAPSHOT_NONCE:-}" ] && [ "${CHAIN_NONCE:-x}" = "${SNAPSHOT_NONCE}" ]; then
    p "the chain's nonce did not advance across the failover — no unknown nonce was consumed"
else
    f "unknown nonce consumption: the chain's nonce for $SIGNER_ADDR moved from ${SNAPSHOT_NONCE:-none} to ${CHAIN_NONCE:-none} while every writer was stopped"
    CHAIN_INCONSISTENT=1
fi
if [ "${EXPLAINED_MAX:--1}" -lt 0 ]; then
    printf '  note: the snapshot records no registry nonce reservation (no registration was submitted yet)\n'
fi
printf '  the difference between the chain nonce and the recorded ledger is the chain setup itself\n'
printf '  (the deterministic DemoNFT + ContentRegistry deployments and the acceptance dev-mints).\n'
if [ "$CHAIN_INCONSISTENT" = "1" ]; then
    printf '\nRegistry broadcast STAYS PAUSED: the restored state is not explainable against the chain,\n'
    printf 'so the target application is not started and no registration will be broadcast from it.\n'
    printf 'Recorded registry state: %s\n' "$REGISTRY_STATE"
    exit 1
fi
p "state is explainable and consistent — the broadcast may resume"

# ---------------------------------------------------------------------------
# 6. Resume: start the target application (the broadcast resumes with it)
# ---------------------------------------------------------------------------
printf '\n===== 6. resume the target application =====\n'
dst_compose up -d --no-build --no-deps --pull never --wait --wait-timeout 300 monolith transcoder api-gateway >/dev/null \
    || die "the target application services did not become ready"
wait_health "${DST_API}/health" 120 || die "the target monolith is not health-ready"
p "target application up and healthy on the restored state"

docker logs "${DST_PROJECT}-monolith-1" 2>&1 | grep -q "On-chain content registry enabled" \
    && p "the target monolith constructed the durable registry hub (the broadcast has resumed)" \
    || f "the target monolith never reported the durable registry hub"

# ---------------------------------------------------------------------------
# 7. The old credentials are dead, a fresh login actually plays
# ---------------------------------------------------------------------------
printf '\n===== 7. credential invalidation and real playback =====\n'
OLD_PROFILE_BODY="$(http_body GET "$DST_API/api/v1/auth/profile" -H "Authorization: Bearer $OLD_TOKEN")"
OLD_PROFILE_CODE="$(http_code GET "$DST_API/api/v1/auth/profile" -H "Authorization: Bearer $OLD_TOKEN")"
if [ "$OLD_PROFILE_CODE" = "401" ] && printf '%s' "$OLD_PROFILE_BODY" | grep -q "restore epoch"; then
    p "the pre-restore access token is refused by the target for the RESTORE EPOCH reason (HTTP 401: $(printf '%s' "$OLD_PROFILE_BODY" | head -c 120))"
elif [ "$OLD_PROFILE_CODE" = "401" ]; then
    f "the pre-restore access token was refused (401) but not for the restore epoch: $(printf '%s' "$OLD_PROFILE_BODY" | head -c 160)"
else
    f "the pre-restore access token answered HTTP $OLD_PROFILE_CODE on the target — the restore did not revoke it"
fi

OLD_MANIFEST_CODE="$(http_code GET "${DST_API}${OLD_MANIFEST_PATH}" -H "Authorization: Bearer $OLD_TOKEN")"
[ "$OLD_MANIFEST_CODE" = "401" ] \
    && p "the pre-restore access token cannot play content on the target (HTTP 401)" \
    || f "the pre-restore access token answered HTTP $OLD_MANIFEST_CODE on the target manifest"

OLD_REFRESH_CODE="$(http_code POST "$DST_API/api/v1/auth/refresh" -H 'Content-Type: application/json' -d "{\"token\":\"$OLD_TOKEN\"}")"
[ "$OLD_REFRESH_CODE" = "401" ] \
    && p "the pre-restore token cannot be refreshed into a new credential (HTTP 401, its 2h TTL had not lapsed)" \
    || f "refresh accepted a pre-restore token (HTTP $OLD_REFRESH_CODE) — a restore must not mint a credential that outlives it"

# The pre-restore capability is refused too. Its own TTL is short by design, so
# the TTL-independent proof is the live rotation below.
OLD_CAP_CODE="$(http_code GET "${DST_API}${OLD_VARIANT_REL}")"
case "$OLD_CAP_CODE" in
    401|403) p "the pre-restore capability is refused by the target (HTTP $OLD_CAP_CODE)" ;;
    *) f "the pre-restore capability answered HTTP $OLD_CAP_CODE on the target" ;;
esac
if docker logs "${DST_PROJECT}-monolith-1" 2>&1 | grep -q "playback token validation failed"; then
    printf '  the target logged a capability refusal reason (playback token validation failed)\n'
fi

NEW_TOKEN="$(login "$DST_API")" || die "a fresh login against the restored target failed"
[ -n "$NEW_TOKEN" ] || die "the fresh login returned no token"
NEW_MANIFEST_CODE="$(http_code GET "${DST_API}${OLD_MANIFEST_PATH}" -H "Authorization: Bearer $NEW_TOKEN")"
[ "$NEW_MANIFEST_CODE" = "200" ] \
    && p "a fresh login plays the restored content (manifest HTTP 200)" \
    || f "the fresh login's manifest answered HTTP $NEW_MANIFEST_CODE"

NEW_VARIANT_REL="$(relative_url "$(http_body GET "${DST_API}${OLD_MANIFEST_PATH}" -H "Authorization: Bearer $NEW_TOKEN" | first_uri)")"
NEW_VARIANT_CODE="$(http_code GET "${DST_API}${NEW_VARIANT_REL}")"
[ "$NEW_VARIANT_CODE" = "200" ] \
    && p "the freshly minted capability alone (query token, no Authorization header) fetches the variant playlist" \
    || f "the variant playlist answered HTTP $NEW_VARIANT_CODE with the capability alone"

SEGMENT_REL="$(relative_url "$(http_body GET "${DST_API}${NEW_VARIANT_REL}" | first_uri)")"
SEG_FILE="${PRIVATE_DIR}/segment.ts"
SEGMENT_CODE="$(http_download "${DST_API}${SEGMENT_REL}" "$SEG_FILE")"
if [ "$SEGMENT_CODE" = "200" ]; then
    if ffmpeg -v error -i "$SEG_FILE" -f null - >/dev/null 2>&1; then
        p "a real media segment was fetched and DECODED from the restored state ($(wc -c < "$SEG_FILE" | tr -d ' ') bytes)"
    else
        f "the segment was fetched but ffmpeg could not decode it"
    fi
else
    f "the segment request answered HTTP $SEGMENT_CODE"
fi
rm -f "$SEG_FILE"

# TTL-independent capability revocation: rotate the epoch on the LIVE target and
# the capability minted moments ago must stop working inside the 5s cache bound,
# long before its own TTL lapses. The control below is what makes that claim
# attributable: the capability must be WORKING immediately before the rotation.
CAP_CONTROL_CODE="$(http_code GET "${DST_API}${NEW_VARIANT_REL}")"
[ "$CAP_CONTROL_CODE" = "200" ] \
    && p "control: the freshly minted capability works immediately before the rotation (HTTP 200, well inside its TTL)" \
    || f "control failed: the capability answered HTTP $CAP_CONTROL_CODE before the rotation, so the rotation check would prove nothing"

CAP_ROTATE_START="$(python3 -c 'import time;print(time.time())')"
CAP_ROTATE_EPOCH="$(python3 -c 'import uuid; print(uuid.uuid4())')"
docker exec "$DST_PG" psql -U postgres -d streamgate -c \
    "UPDATE auth_control_state SET restore_epoch = '${CAP_ROTATE_EPOCH}', updated_at = NOW() WHERE id = 1" >/dev/null \
    || die "could not rotate the restore epoch on the live target"
CAP_REFUSED_AT=""
for _ in $(seq 1 60); do
    LIVE_CAP_CODE="$(http_code GET "${DST_API}${NEW_VARIANT_REL}")"
    if [ "$LIVE_CAP_CODE" = "401" ] || [ "$LIVE_CAP_CODE" = "403" ]; then
        CAP_REFUSED_AT="$(python3 -c 'import time;print(time.time())')"
        break
    fi
    [ "$LIVE_CAP_CODE" = "200" ] || die "live capability revocation is unverifiable (HTTP $LIVE_CAP_CODE)"
    sleep 0.1
done
CAP_LATENCY="$(python3 -c "print(${CAP_REFUSED_AT:-0} - $CAP_ROTATE_START)")"
if [ -n "$CAP_REFUSED_AT" ]; then
    if python3 -c "import sys; sys.exit(0 if 0 <= $CAP_LATENCY <= 5 else 1)"; then
        p "a live restore-epoch rotation revoked a still-valid capability in ${CAP_LATENCY}s (5s cache bound)"
    else
        f "the capability survived the live rotation for ${CAP_LATENCY}s (5s cache bound exceeded)"
    fi
else
    f "a capability minted minutes ago still works after a live restore-epoch rotation"
fi
for endpoint in profile refresh; do
    if [ "$endpoint" = profile ]; then
        LIVE_TOKEN_CODE="$(http_code GET "$DST_API/api/v1/auth/profile" -H "Authorization: Bearer $NEW_TOKEN")"
    else
        LIVE_TOKEN_CODE="$(http_code POST "$DST_API/api/v1/auth/refresh" -H 'Content-Type: application/json' -d "{\"token\":\"$NEW_TOKEN\"}")"
    fi
    [ "$LIVE_TOKEN_CODE" = 401 ] && p "live epoch rotation invalidates the $endpoint credential" \
        || f "live epoch rotation: $endpoint returned HTTP $LIVE_TOKEN_CODE"
done

# ---------------------------------------------------------------------------
# 8. RPO / RTO and the machine-readable report
# ---------------------------------------------------------------------------
for t in "${TABLES[@]}"; do
    ROW_COUNT="$(dst_psql "SELECT COUNT(*) FROM $t")" || die "target table count failed: $t"
    [[ "$ROW_COUNT" =~ ^[0-9]+$ ]] || die "target table count is unverifiable: $t"
    printf '%s=%s\n' "$t" "$ROW_COUNT" >> "$COUNTS_AFTER"
done
if diff -q "$COUNTS_BEFORE" "$COUNTS_AFTER" >/dev/null 2>&1; then
    p "every business table holds the same row count as the snapshot ($(tr '\n' ' ' < "$COUNTS_AFTER"))"
else
    f "row counts changed across the restore"
    diff "$COUNTS_BEFORE" "$COUNTS_AFTER" || true
fi

timeout 600s python3 scripts/cd/recovery_objects.py inventory --container "${DST_PROJECT}-minio-1" --output "$OBJECTS_AFTER" \
    || die "could not re-inventory the restored objects"
OBJECT_HASH_PAIRS="$(diff -u "$OBJECTS_BEFORE" "$OBJECTS_AFTER" > "$ARTIFACTS/objects-diff.txt" 2>&1 && echo equal || echo differ)"
[ "$OBJECT_HASH_PAIRS" = "equal" ] \
    && p "all $OBJECT_COUNT restored objects hash identically to the snapshot, item by item" \
    || f "restored objects differ from the snapshot (see objects-diff.txt)"

RTO="$((SECONDS - DRILL_START))"
printf '\n============================================\n'
printf 'RTO (this drill): %ss — stop writes + drain + snapshot + empty-volume restore\n' "$RTO"
printf 'RTO (this drill): includes the credential invalidation and the real playback assertion\n'
printf 'RPO (this drill): 0 by construction — writes were stopped and drained BEFORE the snapshot,\n'
printf '                  so no committed write is missing from it. A production RPO is bounded by\n'
printf '                  the snapshot cadence, not by this drain.\n'
printf 'Objects: %s item(s), item-by-item hashes %s\n' "$OBJECT_COUNT" "$OBJECT_HASH_PAIRS"
printf 'Scope: the source stack is a dedicated one; nothing was dropped, the sg-fc-* stacks were\n'
printf '       never touched, and the target is an independent project on empty volumes.\n'
printf '============================================\n'

python3 - "$REPORT" "$RESULTS_FILE" "$CANDIDATE_SHA" "$SRC_PROJECT" "$DST_PROJECT" "$CONTENT_ID" \
    "$REGISTRY_ADDR" "$EPOCH_BEFORE" "$NEW_EPOCH" "$CAP_ROTATE_EPOCH" "$RTO" "$CAP_LATENCY" "$OBJECT_HASH_PAIRS" <<'PY'
import json, sys
report, checks, sha, src, dst, content, registry, epoch_before, epoch_new, epoch_live, rto, cap_latency, objects = sys.argv[1:15]
results = [line.split("\t", 1) for line in open(checks).read().splitlines() if line]
json.dump({
    "drill": "fresh-environment-recovery",
    "candidate_sha": sha,
    "source_project": src,
    "target_project": dst,
    "content_id": content,
    "registry_address": registry,
    "restore_epoch_before": epoch_before,
    "restore_epoch_after": epoch_new,
    "restore_epoch_live_rotation": epoch_live,
    "objects_item_hashes": objects,
    "capability_revocation_seconds": cap_latency,
    "rto_seconds": rto,
    "rpo": "0 for this drill (writes drained before the snapshot)",
    "passed": sum(1 for r in results if r[0] == "PASS"),
    "failed": sum(1 for r in results if r[0] == "FAIL"),
    "checks": [{"status": r[0], "check": r[1]} for r in results],
}, open(report, "w"), indent=2)
PY
printf '\nReport: %s\n' "$REPORT"

if [ "$FAIL" -eq 0 ]; then
    printf '\nRESULT: PASS (%d checks, no failure)\n' "$(wc -l < "$RESULTS_FILE" | tr -d ' ')"
    exit 0
fi
printf '\nRESULT: FAIL (%d check(s) failed)\n' "$FAIL"
exit 1
