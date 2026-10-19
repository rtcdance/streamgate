#!/usr/bin/env bash
# Durable fault acceptance (TRANSITION_PLAN.md section 0.2 D).
#
# Runs against a REAL durable-profile stack built from the candidate images:
# Postgres, JetStream, MinIO and Anvil all in play, the isolated transcoder
# worker executing real FFmpeg work. Every fault is injected at an OBSERVABLE
# component state, not after a guessed sleep: the candidate process announces
# the point it reached (a file in the barrier volume) and waits, this harness
# observes that marker, performs the fault (kill / restart), and then either
# releases the point or leaves the process dead for the recovery assertion.
#
# Every wait is bounded, and a bounded wait that expires is a FAILURE: a
# scenario that never fired must not be able to report success.
set -uo pipefail

PROJECT=""
COMPOSE=""
BASE_URL=""
ARTIFACT_DIR=""
HELPER_IMAGE=""
CANDIDATE_SHA=""
RUN_LABEL=""
PG_CONTAINER=""
MONO_CONTAINER=""
TC_CONTAINER=""
TC_CONTAINER_2=""
MINIO_CONTAINER=""
REDIS_CONTAINER=""
# The chain the registry publisher writes to, reached by SERVICE NAME: the
# controller is attached to the project network, so anvil is addressable without
# a published port.
ANVIL_URL="http://anvil:8545"
# One real wallet login is reused for every seed. The auth limiter is 10/min;
# re-logging in for each of the eight scenarios walks into a 429 late in the
# run, which used to surface as "the outbox point never fired".
TOKEN_A=""
# Scenario state, declared up front: `set -u` turns an unset variable in a
# diagnostic path into a dead harness, which is exactly when the diagnostics are
# needed most.
CONTENT_ID=""
TASK=""
SEED_TAG=""
SEED_PID=""
SQLLAST=""
STABLELAST=""
REG_LAST=""
WORKER_RECLAIM_BOUND="${WORKER_RECLAIM_BOUND:-60}"
# Bounded observation window that covers the broker's own redelivery timer
# (jsAckWait 30s in the candidate) plus a margin. A predicate is polled for the
# whole window; the harness never sleeps blindly and hopes.
SETTLE_WINDOW="${SETTLE_WINDOW:-40}"

while [ "$#" -gt 0 ]; do
  case "$1" in
    --project) PROJECT="$2"; shift 2 ;;
    --compose) COMPOSE="$2"; shift 2 ;;
    --base-url) BASE_URL="$2"; shift 2 ;;
    --artifact-dir) ARTIFACT_DIR="$2"; shift 2 ;;
    --helper-image) HELPER_IMAGE="$2"; shift 2 ;;
    --candidate-sha) CANDIDATE_SHA="$2"; shift 2 ;;
    --run-label) RUN_LABEL="$2"; shift 2 ;;
    --pg-container) PG_CONTAINER="$2"; shift 2 ;;
    --mono-container) MONO_CONTAINER="$2"; shift 2 ;;
    --minio-container) MINIO_CONTAINER="$2"; shift 2 ;;
    --redis-container) REDIS_CONTAINER="$2"; shift 2 ;;
    --transcoder-container) TC_CONTAINER="$2"; shift 2 ;;
    --transcoder-container-2) TC_CONTAINER_2="$2"; shift 2 ;;
    --anvil-url) ANVIL_URL="$2"; shift 2 ;;
    *) printf 'unknown argument: %s\n' "$1" >&2; exit 2 ;;
  esac
done
for required in PROJECT COMPOSE BASE_URL ARTIFACT_DIR HELPER_IMAGE CANDIDATE_SHA RUN_LABEL PG_CONTAINER MINIO_CONTAINER REDIS_CONTAINER MONO_CONTAINER TC_CONTAINER; do
  [ -n "${!required}" ] || { printf 'missing --%s\n' "$(printf '%s' "$required" | tr 'A-Z_' 'a-z-')" >&2; exit 2; }
done

CHECKS="${ARTIFACT_DIR}/checks.tsv"
: > "$CHECKS"
PASS=0
FAIL=0
HELPER="${PROJECT}-barrier"

p() { PASS=$((PASS + 1)); printf 'PASS: %s\n' "$*"; }
f() { FAIL=$((FAIL + 1)); printf 'FAIL: %s\n' "$*"; }
note() { printf 'NOTE: %s\n' "$*"; }
log() { printf -- '-- %s\n' "$*"; }

# record <id> <point> <status> <detail>
record() { printf '%s\t%s\t%s\t%s\t%s\n' "$1" "$2" "$3" "$CANDIDATE_SHA" "$4" >> "$CHECKS"; }

json_get() { printf '%s' "$1" | python3 -c "import sys,json;print(json.load(sys.stdin).get('$2',''))" 2>/dev/null || true; }

# seed_note — one diagnostic line per seed step. A scenario that could not be set
# up must say WHICH call failed and how, otherwise a failing harness reports only
# "the point never fired" and hides whether the candidate or the seed is at fault.
seed_note() { printf '  seed[%s]: %s\n' "${SEED_TAG:-?}" "$*" >&2; }

# api_post <path> <token-or-empty> <json-body-or-empty> — prints the response body
# on 2xx, and on any other outcome prints the status plus a body excerpt to stderr
# and returns non-zero. `curl -sf` alone swallows exactly that information.
api_post() {
  local path="$1" token="$2" body="$3"
  local out
  out="${WORKDIR}/resp.json"
  # `args` on its own statement: a local referenced by a sibling assignment in the
  # same `local` command is still unbound under `set -u` (same trap as `window`).
  local args=(-s -o "$out" -w '%{http_code}' -X POST "$BASE_URL$path")
  [ -n "$token" ] && args+=(-H "Authorization: Bearer $token")
  [ -n "$body" ] && args+=(-H 'Content-Type: application/json' -d "$body")
  local code
  code="$(curl "${args[@]}")" || { seed_note "POST $path: transport failure"; return 1; }
  case "$code" in
    2*) cat "$out"; return 0 ;;
  esac
  seed_note "POST $path -> HTTP $code: $(head -c 400 "$out" | tr '\n' ' ')"
  return 1
}

require_tool() {
  command -v "$1" >/dev/null 2>&1 || { printf 'FAIL: required tool %s not found in PATH (no skip by design)\n' "$1"; exit 1; }
}
require_tool curl
require_tool python3
require_tool cast
require_tool ffmpeg

# ---------------------------------------------------------------------------
# Barrier control: the volume is mounted into the app containers AND into a
# long-lived helper container here, so the harness reads and writes the very
# same files the candidate processes rendezvous on.
# ---------------------------------------------------------------------------
barrier_boot() {
  docker rm -f "$HELPER" >/dev/null 2>&1 || :
  docker run -d --name "$HELPER" \
    --label "com.streamgate.verify.run=${RUN_LABEL}" \
    --label "com.docker.compose.project=${PROJECT}" \
    -v "${PROJECT}_fault-barrier:/b" \
    --entrypoint sh "$HELPER_IMAGE" -c 'while :; do sleep 3600; done' >/dev/null \
    || return 1
  local deadline=$((SECONDS + 30))
  while [ "$SECONDS" -lt "$deadline" ]; do
    docker exec "$HELPER" sh -c 'test -d /b' 2>/dev/null && break
    sleep 0.3
  done
  docker exec "$HELPER" sh -c 'test -d /b' 2>/dev/null || return 1
  # Docker creates a fresh named volume root-owned, while every candidate
  # container runs as its own non-root user — so the directory has to be handed
  # to THAT user or the candidate cannot write a single marker (observed as
  # "cannot publish the reached marker: permission denied", which turns every
  # scenario into a silent no-op). The uid is read from the running worker
  # rather than hard-coded, so an image that changes its user cannot quietly
  # break the rendezvous again.
  local app_uid app_gid
  app_uid="$(docker exec "$TC_CONTAINER" id -u 2>/dev/null)" || return 1
  app_gid="$(docker exec "$TC_CONTAINER" id -g 2>/dev/null)" || return 1
  [ -n "$app_uid" ] && [ -n "$app_gid" ] || return 1
  docker exec "$HELPER" chown "${app_uid}:${app_gid}" /b >/dev/null 2>&1 || return 1
  docker exec "$HELPER" chmod 0770 /b >/dev/null 2>&1 || return 1
  return 0
}

barrier_reset() { docker exec "$HELPER" sh -c 'rm -f /b/*.armed /b/*.reached /b/*.release' 2>/dev/null; }

# arm <point> — select exactly one point for injection, clearing any stale
# markers first so what we observe next is this scenario's state.
arm() { docker exec "$HELPER" sh -c "rm -f /b/$1.reached /b/$1.release; : > /b/$1.armed"; }

unarm() { docker exec "$HELPER" sh -c "rm -f /b/$1.armed /b/$1.reached /b/$1.release"; }

release_point() { docker exec "$HELPER" sh -c ": > /b/$1.release"; }

marker_of() { docker exec "$HELPER" sh -c "cat /b/$1.reached 2>/dev/null" 2>/dev/null | tr -d '\r'; }

# wait_reached <point> <seconds> — bounded; false means the scenario did not fire.
wait_reached() {
  local point="$1" deadline=$((SECONDS + $2))
  while [ "$SECONDS" -lt "$deadline" ]; do
    docker exec "$HELPER" test -f "/b/${point}.reached" 2>/dev/null && return 0
    sleep 0.2
  done
  return 1
}

# wait_sql <sql> <want> <seconds> — bounded poll of a single scalar.
wait_sql() {
  local sql="$1" want="$2" deadline=$((SECONDS + $3)) got
  while [ "$SECONDS" -lt "$deadline" ]; do
    got="$(sql_scalar "$sql")"
    [ "$got" = "$want" ] && return 0
    sleep 1
  done
  SQLLAST="$got"
  return 1
}

sql_scalar() { docker exec "$PG_CONTAINER" psql -U postgres -d streamgate -tAc "$1" 2>/dev/null | tr -d '\r'; }
sql_exec() { docker exec "$PG_CONTAINER" psql -U postgres -d streamgate -v ON_ERROR_STOP=1 -c "$1" >/dev/null 2>&1; }

# stable_for <sql> <want> <seconds> — passes only when the predicate held <want>
# for the WHOLE window; a divergence returns immediately so a re-executed task
# is caught rather than waited out.
stable_for() {
  local sql="$1" want="$2" window="$3" got
  # A second statement on purpose: a local declared in the same `local` command
  # is not visible to a sibling assignment, so `deadline=$((SECONDS + window))`
  # on that line read an unbound variable under `set -u`.
  local deadline=$((SECONDS + window))
  while [ "$SECONDS" -lt "$deadline" ]; do
    got="$(sql_scalar "$sql")"
    if [ "$got" != "$want" ]; then STABLELAST="$got"; return 1; fi
    sleep 1
  done
  return 0
}

# ---------------------------------------------------------------------------
# Chain observation, through the REAL Anvil the candidate writes to. The
# controller is attached to the project network, so the service name resolves;
# every value below is read from the node, never inferred from the candidate.
# ---------------------------------------------------------------------------
hexq() { printf '0x%x' "$1"; }
# hexdec feeds a JSON-RPC hex quantity to printf. `cast rpc` prints its result as
# a QUOTED JSON string ("0x30"), and bash's `printf '%d' '"0x30"'` does not fail —
# it silently yields the character code 48, so every chain read looked like the
# number 48 and every "value equal to the recorded one" assertion was decided by
# a constant. Strip the quotes before converting.
hexdec() {
  local v="${1//\"/}"
  v="${v//[$'\r\n']/}"
  [ -n "$v" ] || return 1
  printf '%d' "$v"
}
head_number() { hexdec "$(cast rpc --rpc-url "$ANVIL_URL" eth_blockNumber 2>/dev/null)"; }
chain_nonce() {
  hexdec "$(cast rpc --rpc-url "$ANVIL_URL" eth_getTransactionCount "$WALLET_A" latest 2>/dev/null)"
}
block_hash() {
  cast rpc --rpc-url "$ANVIL_URL" eth_getBlockByNumber "$(hexq "$1")" false 2>/dev/null \
    | python3 -c "import sys,json;print(json.load(sys.stdin).get('hash',''))" 2>/dev/null
}
# receipt_field <tx> <field> — reads one field of a receipt, and prints NOTHING
# unless the node really returned a receipt. Foundry v1.8.5 answers a failing
# `cast receipt --json` with a structured ENVELOPE
# ({"schema_version":1,"success":false,"data":null,...}) instead of an empty
# stdout, so a naive `d.get('blockHash')` has to be told the two envelopes apart
# before it can be trusted: a false "the tx is mined" would turn the FR01
# assertion (the tx must NOT be on chain while parked) upside down.
receipt_field() {
  cast receipt --rpc-url "$ANVIL_URL" "$1" --json 2>/dev/null \
    | python3 -c "
import sys, json
try:
    d = json.load(sys.stdin)
except Exception:
    print(''); raise SystemExit
if isinstance(d, dict) and 'schema_version' in d:
    if not d.get('success'):
        print(''); raise SystemExit
    d = d.get('data') or {}
    if isinstance(d, dict) and isinstance(d.get('receipt'), dict):
        d = d['receipt']
value = d.get('$2') if isinstance(d, dict) else None
print('' if value is None else value)
" 2>/dev/null
}
tx_mined() { [ -n "$(receipt_field "$1" blockHash)" ]; }
# tx_known <tx> — whether the NODE has ever seen this transaction, read straight
# off eth_getTransactionByHash (null means "never seen"). This is the primary
# evidence for "the signed transaction was persisted but not handed to a node",
# and unlike a receipt it does not depend on whether the tx has been mined yet.
tx_known() {
  local out
  out="$(cast rpc --rpc-url "$ANVIL_URL" eth_getTransactionByHash "$1" 2>/dev/null | tr -d ' \t\r\n')"
  # `cast rpc` prints string results QUOTED, so the quotes go before the compare.
  out="${out//\"/}"
  [ -n "$out" ] && [ "$out" != "null" ]
}
# anvil_rollback un-mines the newest N blocks: the honest way to produce a real
# reorg on a chain that keeps producing blocks. Bounded by anvil itself; a
# refusal is a FAILURE, never a skip.
anvil_rollback() {
  cast rpc --rpc-url "$ANVIL_URL" anvil_rollback "$1" >/dev/null 2>&1
}

# ---------------------------------------------------------------------------
# Registry intent / signed-attempt state (SG-13 A). Every assertion below reads
# the candidate's OWN durable row, so "the tx was persisted before the send" is
# a fact about the database, not about a log line.
# ---------------------------------------------------------------------------
reg_field() { sql_scalar "SELECT ${2} FROM content_registry_intents WHERE content_id='${1}'"; }
reg_raw() { sql_scalar "SELECT raw_tx FROM tx_signed_attempts WHERE tx_hash='${1}'"; }
reg_attempts() {
  sql_scalar "SELECT count(*) FROM tx_signed_attempts WHERE lower(signer)=lower('${WALLET_A}') AND nonce=${2}"
}
reg_inflight() {
  sql_scalar "SELECT count(*) FROM content_registry_intents WHERE status IN ('pending','submitted','broadcast_unknown')"
}
# wait_reg_reorged <content> <old-block-hash> <seconds> — a stale 'confirmed' row
# would satisfy a plain status poll without the chain ever being re-checked, so
# the reorg has to be OBSERVED first: the row must leave 'confirmed' or stop
# naming the orphaned block before "confirmed again" means anything.
wait_reg_reorged() {
  local content="$1" oldhash="$2" deadline=$((SECONDS + $3)) st bh
  while [ "$SECONDS" -lt "$deadline" ]; do
    st="$(reg_field "$content" status)"
    bh="$(reg_field "$content" block_hash)"
    if [ "$st" != "confirmed" ] || [ "$bh" != "$oldhash" ]; then
      REG_LAST="$st/${bh:-empty}"
      return 0
    fi
    sleep 1
  done
  REG_LAST="$st/${bh:-empty}"
  return 1
}
diag_registry() {
  printf -- '   -- registry intents (newest first)\n'
  sql_scalar "SELECT substr(content_id,1,8)||' | '||status||' | tx='||substr(tx_hash,1,12)||' | nonce='||COALESCE(nonce::text,'-')||' | block='||COALESCE(block_number::text,'-')||' | attempt='||attempt FROM content_registry_intents ORDER BY updated_at DESC LIMIT 5" | sed 's/^/      /'
  printf -- '   -- signed attempts\n'
  sql_scalar "SELECT substr(tx_hash,1,12)||' | nonce='||nonce||' | signer='||substr(signer,1,10) FROM tx_signed_attempts ORDER BY created_at DESC LIMIT 5" | sed 's/^/      /'
  printf -- '   -- monolith log (registry/barrier/broadcast)\n'
  docker logs --tail 400 "$MONO_CONTAINER" 2>&1 | grep -iE "registry|barrier|broadcast|reorg" | tail -10 | sed 's/^/      /'
}

# ---------------------------------------------------------------------------
# Container control.
# ---------------------------------------------------------------------------
container_kill() { docker kill "$1" >/dev/null 2>&1 || : ; }
container_start() { docker start "$1" >/dev/null 2>&1 || : ; }

# wait_worker_up <container> <seconds> — the worker answers its health endpoint.
wait_worker_up() {
  local container="$1" deadline=$((SECONDS + $2))
  while [ "$SECONDS" -lt "$deadline" ]; do
    if docker exec "$container" wget -q -O /dev/null http://localhost:8080/health 2>/dev/null; then
      return 0
    fi
    sleep 1
  done
  return 1
}

# ---------------------------------------------------------------------------
# Real media + real authenticated seeding through the candidate's own API.
# ---------------------------------------------------------------------------
WORKDIR="$(mktemp -d)"
trap 'rm -rf "$WORKDIR"' EXIT
MEDIA="${WORKDIR}/fault-src.mp4"
ffmpeg -hide_banner -loglevel error -y \
  -f lavfi -i "testsrc=size=320x180:rate=15:duration=4" \
  -f lavfi -i "sine=frequency=440:sample_rate=44100:duration=4" \
  -c:v libx264 -preset veryfast -pix_fmt yuv420p -c:a aac -shortest "$MEDIA" \
  || { printf 'FAIL: cannot generate the media fixture (ffmpeg)\n' >&2; exit 1; }
MEDIA_SHA="$(python3 -c "import hashlib,sys;print(hashlib.sha256(open(sys.argv[1],'rb').read()).hexdigest())" "$MEDIA")"

KEY_A="0xac0974bec39a17e36ba4a6b4d238ff944bacb478cbed5efcae784d7bf4f2ff80"
WALLET_A="$(cast wallet address --private-key "$KEY_A")"
CHAIN_ID=31337

login() {
  local key="$1" wallet="$2" challenge cid msg sig resp token
  challenge="$(api_post /api/v1/auth/challenge "" "{\"wallet\":\"$wallet\",\"chain_id\":$CHAIN_ID}")" || return 1
  cid="$(json_get "$challenge" challenge_id)"
  msg="$(json_get "$challenge" message)"
  [ -n "$cid" ] && [ -n "$msg" ] || { seed_note "challenge response lacks challenge_id/message"; return 1; }
  sig="$(cast wallet sign --private-key "$key" "$msg")" || { seed_note "cast wallet sign failed"; return 1; }
  resp="$(api_post /api/v1/auth/login "" \
    "{\"wallet\":\"$wallet\",\"chain_id\":$CHAIN_ID,\"challenge_id\":\"$cid\",\"signature\":\"$sig\"}")" || return 1
  token="$(json_get "$resp" token)"
  [ -n "$token" ] || { seed_note "login response lacks a token: $resp"; return 1; }
  printf '%s' "$token"
}

# seed_content <tag> — real login, real upload, real complete-upload. The
# completion transaction is what creates the task row and its enqueue intent, so
# everything downstream of it is the production path.
seed_content() {
  local tag="$1" token upload_resp upload_id complete_resp code out
  SEED_TAG="$tag"
  if [ -z "$TOKEN_A" ]; then
    local attempt
    for attempt in 1 2 3; do
      TOKEN_A="$(login "$KEY_A" "$WALLET_A")" && break
      TOKEN_A=""
      # The auth limiter is 10/min; a fresh login after a restart can still
      # collide with the window and must wait it out rather than report a
      # missing barrier point.
      sleep 7
    done
    [ -n "$TOKEN_A" ] || { seed_note "could not log in the harness wallet after 3 attempts"; return 1; }
  fi
  token="$TOKEN_A"
  out="${WORKDIR}/upload-${tag}.json"
  code="$(curl -s -o "$out" -w '%{http_code}' -X POST "$BASE_URL/api/v1/upload" \
    -H "Authorization: Bearer $token" -F "file=@${MEDIA};filename=fault-${tag}.mp4")" \
    || { seed_note "POST /api/v1/upload: transport failure"; return 1; }
  case "$code" in
    2*) ;;
    *) seed_note "POST /api/v1/upload -> HTTP $code: $(head -c 400 "$out" | tr '\n' ' ')"; return 1 ;;
  esac
  upload_id="$(json_get "$(cat "$out")" upload_id)"
  [ -n "$upload_id" ] || { seed_note "upload response lacks upload_id: $(cat "$out")"; return 1; }
  complete_resp="$(api_post "/api/v1/upload/${upload_id}/complete-upload" "$token" "")" || return 1
  CONTENT_ID="$(json_get "$complete_resp" content_id)"
  [ -n "$CONTENT_ID" ] || { seed_note "complete-upload response lacks content_id: $complete_resp"; return 1; }
  SEED_TAG=""
  return 0
}

task_row() { sql_scalar "SELECT id FROM transcoding_tasks WHERE content_id='${CONTENT_ID}' ORDER BY created_at DESC LIMIT 1"; }
field() { sql_scalar "SELECT ${2} FROM transcoding_tasks WHERE id='${1}'"; }
published_generation() { sql_scalar "SELECT COALESCE(published_generation,0) FROM contents WHERE id='${CONTENT_ID}'"; }
task_count() { sql_scalar "SELECT count(*) FROM transcoding_tasks WHERE content_id='${CONTENT_ID}'"; }

# object_path_from_url <url> — "<bucket>/<key>" of an object URL, so the source
# of a task can be removed from the object store itself. The worker fetches the
# URL carried by the MESSAGE, so editing the task row would change nothing while
# deleting the object is a fault the candidate really observes.
object_path_from_url() {
  python3 - "$1" <<'PY'
import sys, urllib.parse
path = urllib.parse.urlparse(sys.argv[1]).path.lstrip('/')
parts = path.split('/', 1)
print(f"{parts[0]}/{parts[1]}" if len(parts) > 1 and parts[1] else '')
PY
}

# mc with an alias that actually carries credentials. The server image ships
# `mc` with only its BUILT-IN aliases, and the built-in `local` alias points at
# http://localhost:9000 with EMPTY credentials — anonymous against a private
# bucket, so every operation fails with AccessDenied and a scenario that meant to
# delete an object quietly does nothing. The alias is configured from the
# container's own root credentials so it cannot drift from the stack.
MC_DIR=/tmp/mc-acceptance
minio_alias_ready() {
  local u p
  u="$(docker exec "$MINIO_CONTAINER" printenv MINIO_ROOT_USER 2>/dev/null)"
  p="$(docker exec "$MINIO_CONTAINER" printenv MINIO_ROOT_PASSWORD 2>/dev/null)"
  [ -n "$u" ] && [ -n "$p" ] || return 1
  docker exec "$MINIO_CONTAINER" mc "--config-dir=${MC_DIR}" alias set acceptance \
    "http://127.0.0.1:9000" "$u" "$p" >/dev/null 2>&1
}
mc_at() { docker exec "$MINIO_CONTAINER" mc "--config-dir=${MC_DIR}" "$@"; }

# diag_outbox — the candidate's own view when a fault point did not fire: which
# outbox rows exist and what each process logged about them. Without this a
# missing point is indistinguishable from a failed seed or a silent publish.
diag_outbox() {
  printf -- '   -- outbox rows (newest first)\n'
  sql_scalar "SELECT kind||' | '||status||' | '||id FROM outbox ORDER BY created_at DESC LIMIT 6" | sed 's/^/      /'
  printf -- '   -- newest task rows\n'
  sql_scalar "SELECT id||' | '||status||' | attempt='||attempt FROM transcoding_tasks ORDER BY created_at DESC LIMIT 3" | sed 's/^/      /'
  printf -- '   -- monolith log (outbox/barrier/publish/transcode)\n'
  docker logs --tail 400 "$MONO_CONTAINER" 2>&1 | grep -iE "outbox|barrier|publish|transcod|hook" | tail -8 | sed 's/^/      /'
  printf -- '   -- transcoder log (outbox/barrier/publish)\n'
  docker logs --tail 400 "$TC_CONTAINER" 2>&1 | grep -iE "outbox|barrier|publish|transcod" | tail -8 | sed 's/^/      /'
}

# ---------------------------------------------------------------------------
# 0. Preconditions: the durable profile really is in force, and the barrier
#    switch really is refused outside it.
# ---------------------------------------------------------------------------
log 'preconditions'
printf 'candidate=%s media_sha256=%s\n' "$CANDIDATE_SHA" "$MEDIA_SHA" | tee "${ARTIFACT_DIR}/fault-context.txt"

# The barrier is a rendezvous on a shared volume. This process needs the same
# volume mounted where it can read and write the marker files, and it has no
# access to the app containers' mounts — hence the helper. Without it every
# `arm`/`wait_reached` would silently do nothing and the stack would run every
# scenario to completion, which is exactly the failure mode the barrier exists
# to prevent. So a helper that cannot start is a hard failure.
if barrier_boot; then
  p "barrier helper container is live and sees the shared barrier volume"
  record F00-helper preconditions PASS "helper=${HELPER}"
else
  f "could not start the barrier helper container (the barrier cannot be driven)"
  record F00-helper preconditions FAIL "helper=${HELPER} unavailable"
fi

# Prove the rendezvous in BOTH directions before trusting any scenario, and in
# particular prove it as the CANDIDATE's own user: a directory the harness can
# write is no evidence that a non-root app container can. A probe written by the
# worker user must be visible here, and this side must be able to consume it.
PROBE=".probe-$$"
if docker exec --user "$(docker exec "$TC_CONTAINER" id -u 2>/dev/null)" "$TC_CONTAINER" \
     sh -c ": > /var/lib/streamgate/barrier/${PROBE}" 2>/dev/null \
   && docker exec "$HELPER" sh -c "test -f /b/${PROBE} && rm -f /b/${PROBE}" 2>/dev/null; then
  p "the worker's own user can write the rendezvous and this side can consume it"
  record F00-barrierwrite preconditions PASS "wrote and consumed /b/${PROBE} as the worker user"
else
  f "the worker user cannot write the rendezvous (every scenario would be a silent no-op)"
  record F00-barrierwrite preconditions FAIL "probe as the worker user failed"
fi

if curl -sf "$BASE_URL/health" >/dev/null 2>&1; then
  p "monolith answers /health at ${BASE_URL}"
  record F00 preconditions PASS "monolith healthy"
else
  f "monolith does not answer /health at ${BASE_URL}"
  record F00 preconditions FAIL "monolith unreachable"
fi

# The object-store client the scenarios use must carry credentials; the shipped
# built-in alias is anonymous and would turn every delete into a silent no-op.
if minio_alias_ready && mc_at ls "acceptance/streamgate" >/dev/null 2>&1; then
  p "object-store client authenticates against the candidate's bucket"
  record F00-minio preconditions PASS "mc alias configured from the container credentials"
else
  f "the object-store client cannot authenticate (object faults would be silent no-ops)"
  mc_at ls "acceptance/streamgate" | sed 's/^/   /'
  record F00-minio preconditions FAIL "mc alias unusable"
fi

# The barrier is armed through a file, so a stack that never installed the
# barrier would silently run every scenario to completion. Prove the rendezvous
# works before trusting any scenario: arm a point the worker really reaches and
# require the worker to park there.
barrier_reset
arm worker.claimed
SEEDED=0
if seed_content f00; then
  SEEDED=1
  p "seeded a real content through the candidate API (content_id=${CONTENT_ID})"
  record F00-pre seed_content PASS "content_id=${CONTENT_ID}"
else
  f "could not seed a real content through the candidate API"
  record F00-pre seed_content FAIL "upload/complete-upload failed"
fi
if [ "$SEEDED" = 1 ] && wait_reached worker.claimed 60; then
  p "the worker announced worker.claimed and is waiting for a release (barrier live)"
  record F00-barrier worker.claimed PASS "reached marker=$(marker_of worker.claimed)"
  release_point worker.claimed
  unarm worker.claimed
else
  f "the barrier never fired: the worker did not park at worker.claimed"
  record F00-barrier worker.claimed FAIL "not reached within 60s"
fi
if [ "$SEEDED" = 1 ]; then
  wait_sql "SELECT status FROM transcoding_tasks WHERE content_id='${CONTENT_ID}'" completed 120 \
    && p "the seeded task completed on the real worker" \
    || { f "the seeded task did not complete (${SQLLAST:-no row})"; }
fi
barrier_reset

# ---------------------------------------------------------------------------
# Scenario FW01 — kill between claim and execution.
# ---------------------------------------------------------------------------
log 'FW01 kill at worker.claimed'
# Arm the point BEFORE seeding: the claim happens the instant the pump publishes
# the message, so arming after the seed would race the worker to the marker.
arm worker.claimed
if seed_content fw01 && wait_reached worker.claimed 60; then
  TASK="$(task_row)"
  AT_KILL_ATTEMPT="$(field "$TASK" attempt)"
  KILL_AT="$SECONDS"
  container_kill "$TC_CONTAINER"
  unarm worker.claimed
  barrier_reset
  container_start "$TC_CONTAINER"
  if wait_worker_up "$TC_CONTAINER" 120; then
    p "FW01: worker restarted after a kill at the claim"
    if wait_sql "SELECT status FROM transcoding_tasks WHERE id='${TASK}'" completed "$WORKER_RECLAIM_BOUND"; then
      RECLAIM=$((SECONDS - KILL_AT))
      p "FW01: the killed claim was re-taken and the task completed after ${RECLAIM}s"
      record FW01 worker.claimed PASS "re-claim+complete in ${RECLAIM}s (bound ${WORKER_RECLAIM_BOUND}s), attempt ${AT_KILL_ATTEMPT}->$(field "$TASK" attempt)"
    else
      f "FW01: the task did not complete within ${WORKER_RECLAIM_BOUND}s of the kill (status=${SQLLAST:-?})"
      record FW01 worker.claimed FAIL "status=${SQLLAST:-?} after ${WORKER_RECLAIM_BOUND}s"
    fi
    GEN="$(field "$TASK" output_generation)"
    if [ "$(published_generation)" = "$GEN" ] && [ "$(task_count)" = "1" ]; then
      p "FW01: exactly one generation published ($GEN) and one task row"
      record FW01-publish worker.claimed PASS "published_generation=${GEN}, tasks=1"
    else
      f "FW01: published_generation=$(published_generation) but the task generation is ${GEN} (tasks=$(task_count))"
      record FW01-publish worker.claimed FAIL "published=$(published_generation) task_gen=${GEN}"
    fi
  else
    f "FW01: the worker did not come back healthy"
    record FW01 worker.claimed FAIL "worker unhealthy after restart"
  fi
else
  f "FW01: the claim point never fired"
  record FW01 worker.claimed FAIL "not reached within 60s"
fi

# ---------------------------------------------------------------------------
# Scenario FW02 — kill with a half-finished output (renditions staged, task row
# not yet committed).
# ---------------------------------------------------------------------------
log 'FW02 kill at worker.partial_output'
barrier_reset
arm worker.partial_output
if seed_content fw02 && wait_reached worker.partial_output 90; then
  TASK="$(task_row)"
  while_parked_status="$(field "$TASK" status)"
  while_parked_gen="$(published_generation)"
  container_kill "$TC_CONTAINER"
  unarm worker.partial_output
  barrier_reset
  container_start "$TC_CONTAINER"
  if [ "$while_parked_gen" = "0" ]; then
    p "FW02: no generation was published while the worker was parked before the commit"
    record FW02-nopublish worker.partial_output PASS "published_generation=0 while parked"
  else
    f "FW02: a generation was already published before the completion transaction committed (published_generation=${while_parked_gen})"
    record FW02-nopublish worker.partial_output FAIL "published_generation=${while_parked_gen}"
  fi
  if wait_worker_up "$TC_CONTAINER" 120 \
    && wait_sql "SELECT status FROM transcoding_tasks WHERE id='${TASK}'" completed 180; then
    GEN="$(field "$TASK" output_generation)"
    if [ "$(published_generation)" = "$GEN" ] && [ "$(task_count)" = "1" ]; then
      p "FW02: the retried task completed with exactly one published generation (${GEN})"
      record FW02 worker.partial_output PASS "recovered to completed, published=${GEN}, staged_status=${while_parked_status}"
    else
      f "FW02: published=$(published_generation) task_gen=${GEN}"
      record FW02 worker.partial_output FAIL "published=$(published_generation) task_gen=${GEN}"
    fi
  else
    f "FW02: the task did not complete after the restart (status=${SQLLAST:-?})"
    record FW02 worker.partial_output FAIL "status=${SQLLAST:-?}"
  fi
else
  f "FW02: the partial-output point never fired"
  record FW02 worker.partial_output FAIL "not reached within 90s"
fi

# ---------------------------------------------------------------------------
# Scenario FW03 — kill AFTER the result is committed, before the Ack. The task
# row and the published generation are already durable, so the restart must NOT
# produce a second generation and must not re-execute the media work.
# ---------------------------------------------------------------------------
log 'FW03 kill at worker.after_publish'
barrier_reset
arm worker.after_publish
if seed_content fw03 && wait_reached worker.after_publish 120; then
  TASK="$(task_row)"
  parked_status="$(field "$TASK" status)"
  parked_gen="$(published_generation)"
  parked_attempt="$(field "$TASK" attempt)"
  container_kill "$TC_CONTAINER"
  unarm worker.after_publish
  barrier_reset
  container_start "$TC_CONTAINER"
  if wait_worker_up "$TC_CONTAINER" 120; then
    # The redelivery must settle the task without re-running it: the row is
    # already terminal, so the worker Acks it with zero side effects. Watch the
    # whole redelivery window (AckWait + margin) for any divergence.
    ATTEMPT_STABLE=1
    stable_for "SELECT attempt FROM transcoding_tasks WHERE id='${TASK}'" "$parked_attempt" "$SETTLE_WINDOW" \
      || ATTEMPT_STABLE=0
    after_attempt="$(field "$TASK" attempt)"
    if [ "$parked_status" = "completed" ] && [ "$ATTEMPT_STABLE" = 1 ] \
      && [ "$(published_generation)" = "$parked_gen" ] && [ "$(task_count)" = "1" ]; then
      p "FW03: the committed result survived the redelivery; no re-execution, one published generation (${parked_gen})"
      record FW03 worker.after_publish PASS "status=completed before the kill, attempt ${parked_attempt} stable over ${SETTLE_WINDOW}s, published=${parked_gen}"
    else
      f "FW03: parked(status=${parked_status},gen=${parked_gen},attempt=${parked_attempt}) after(attempt=${after_attempt},gen=$(published_generation)) stable=${ATTEMPT_STABLE}"
      record FW03 worker.after_publish FAIL "attempt ${parked_attempt}->${after_attempt}, published=$(published_generation)"
    fi
  else
    f "FW03: the worker did not come back healthy"
    record FW03 worker.after_publish FAIL "worker unhealthy after restart"
  fi
else
  f "FW03: the after-publish point never fired"
  record FW03 worker.after_publish FAIL "not reached within 120s"
fi

# ---------------------------------------------------------------------------
# Scenario FW04 — kill before the Ack, after everything else is done. Same
# invariant as FW03, reached through a different window.
# ---------------------------------------------------------------------------
log 'FW04 kill at worker.before_ack'
barrier_reset
arm worker.before_ack
if seed_content fw04 && wait_reached worker.before_ack 120; then
  TASK="$(task_row)"
  parked_attempt="$(field "$TASK" attempt)"
  parked_gen="$(published_generation)"
  container_kill "$TC_CONTAINER"
  unarm worker.before_ack
  barrier_reset
  container_start "$TC_CONTAINER"
  if wait_worker_up "$TC_CONTAINER" 120; then
    ATTEMPT_STABLE=1
    stable_for "SELECT attempt FROM transcoding_tasks WHERE id='${TASK}'" "$parked_attempt" "$SETTLE_WINDOW" \
      || ATTEMPT_STABLE=0
    if [ "$(field "$TASK" status)" = "completed" ] && [ "$ATTEMPT_STABLE" = 1 ] \
      && [ "$(published_generation)" = "$parked_gen" ] && [ "$(task_count)" = "1" ]; then
      p "FW04: the un-acked redelivery settled without re-executing the task"
      record FW04 worker.before_ack PASS "attempt ${parked_attempt} stable over ${SETTLE_WINDOW}s, published=${parked_gen}"
    else
      f "FW04: attempt ${parked_attempt}->$(field "$TASK" attempt), published=$(published_generation), status=$(field "$TASK" status)"
      record FW04 worker.before_ack FAIL "state moved after the restart"
    fi
  else
    f "FW04: the worker did not come back healthy"
    record FW04 worker.before_ack FAIL "worker unhealthy after restart"
  fi
else
  f "FW04: the before-Ack point never fired"
  record FW04 worker.before_ack FAIL "not reached within 120s"
fi

# ---------------------------------------------------------------------------
# Scenario FW05 — the attempt cap, the terminal failure, and the crash window
# between publishing the DLQ intent and Acking the original message.
#
# The input is destroyed AFTER the first claim (the worker is parked at
# worker.claimed, so nothing has been fetched yet): every attempt then fails to
# download its input, which is a retryable failure, so the task walks the whole
# 3-attempt budget into the terminal DLQ path.
# ---------------------------------------------------------------------------
log 'FW05 attempt cap + DLQ publish/confirm window'
barrier_reset
arm worker.claimed
if seed_content fw05 && wait_reached worker.claimed 60; then
  TASK="$(task_row)"
  OBJ="$(object_path_from_url "$(field "$TASK" input_url)")"
  if [ -n "$OBJ" ] \
    && mc_at rm --force "acceptance/${OBJ}" >/dev/null 2>&1 \
    && ! mc_at stat "acceptance/${OBJ}" >/dev/null 2>&1; then
    p "FW05: deleted the source object ${OBJ} while the task was parked before its first fetch"
  else
    f "FW05: could not delete the source object (${OBJ:-cannot derive a key from the task input URL})"
    mc_at stat "acceptance/${OBJ}" | sed 's/^/   /'
  fi
  # Let the parked claim go and arm the terminal window now. Attempts 2 and 3
  # follow on the worker's own retry delays (2s then 4s); the third exhausts the
  # cap, so that execution goes terminal and parks at the DLQ publish/confirm
  # point — armed here, so it cannot slip past before we observe it. The claim
  # point is disarmed first: it is still selected, and a point that stays armed
  # would re-park each retry for a full barrier timeout instead of walking
  # through (Arrive consumes the release, so the marker would not persist).
  arm worker.dlq_before_ack
  unarm worker.claimed
  release_point worker.claimed
  if wait_reached worker.dlq_before_ack 120; then
    p "FW05: the task reached the DLQ publish/confirm window after $(field "$TASK" attempt) attempts"
    DLQ_STATUS="$(sql_scalar "SELECT status FROM outbox WHERE kind='dlq' AND id='${TASK}'")"
    ROW_STATUS="$(field "$TASK" status)"
    if [ "$ROW_STATUS" = "failed" ] && [ "$DLQ_STATUS" = "delivered" ]; then
      p "FW05: the DLQ intent was published and marked delivered before the Ack"
      record FW05-dlq worker.dlq_before_ack PASS "row=failed, dlq_intent=${DLQ_STATUS}, attempts=$(field "$TASK" attempt)"
    else
      f "FW05: row=${ROW_STATUS}, dlq_intent=${DLQ_STATUS:-none}"
      record FW05-dlq worker.dlq_before_ack FAIL "row=${ROW_STATUS} dlq=${DLQ_STATUS:-none}"
    fi
    container_kill "$TC_CONTAINER"
    unarm worker.claimed
    unarm worker.dlq_before_ack
    barrier_reset
    container_start "$TC_CONTAINER"
    if wait_worker_up "$TC_CONTAINER" 120; then
      DLQ_STABLE=1
      stable_for "SELECT count(*) FROM outbox WHERE kind='dlq' AND id='${TASK}'" "1" "$SETTLE_WINDOW" || DLQ_STABLE=0
      DLQ_ROWS="$(sql_scalar "SELECT count(*) FROM outbox WHERE kind='dlq' AND id='${TASK}'")"
      if [ "$(field "$TASK" status)" = "failed" ] && [ "$DLQ_ROWS" = "1" ] && [ "$DLQ_STABLE" = 1 ] \
        && [ "$(sql_scalar "SELECT status FROM outbox WHERE kind='dlq' AND id='${TASK}'")" = "delivered" ] \
        && [ "$(published_generation)" = "0" ]; then
        p "FW05: the crash between DLQ publish and Ack settled by redelivery with exactly one intent and nothing published"
        record FW05 worker.dlq_before_ack PASS "failed task, one dlq intent delivered (stable over ${SETTLE_WINDOW}s), published_generation=0"
      else
        f "FW05: status=$(field "$TASK" status), dlq rows=${DLQ_ROWS} stable=${DLQ_STABLE}, published=$(published_generation)"
        record FW05 worker.dlq_before_ack FAIL "dlq rows=${DLQ_ROWS} published=$(published_generation)"
      fi
    else
      f "FW05: the worker did not come back healthy"
      record FW05 worker.dlq_before_ack FAIL "worker unhealthy after restart"
    fi
  else
    f "FW05: the terminal DLQ window was never reached (status=$(field "$TASK" status))"
    record FW05 worker.dlq_before_ack FAIL "not reached within 120s"
  fi
else
  f "FW05: the claim point never fired"
  record FW05 worker.claimed FAIL "not reached within 60s"
fi

# ---------------------------------------------------------------------------
# Scenario FW06 — two workers: the one holding the lease is killed and a second
# replica (already consuming) must take over after the lease expires, without a
# second generation appearing.
# ---------------------------------------------------------------------------
log 'FW06 two workers, one lease'
barrier_reset
if [ -n "$TC_CONTAINER_2" ] \
  && docker compose -p "$PROJECT" -f "$COMPOSE" up -d --no-deps --pull never --scale transcoder=2 transcoder >/dev/null 2>&1; then
  wait_worker_up "$TC_CONTAINER_2" 120 || f "FW06: the second worker replica never became healthy"
  arm worker.claimed
  if seed_content fw06 && wait_reached worker.claimed 90; then
    TASK="$(task_row)"
    HOLDER_MARKER="$(marker_of worker.claimed)"
    HOLDER_HOST="$(printf '%s' "$HOLDER_MARKER" | awk '{print $1}')"
    HOLDER_ID="$(docker inspect -f '{{.Id}}' "$TC_CONTAINER" 2>/dev/null | cut -c1-12)"
    HOLDER_ID2="$(docker inspect -f '{{.Id}}' "$TC_CONTAINER_2" 2>/dev/null | cut -c1-12)"
    if [ "$HOLDER_HOST" = "$HOLDER_ID" ]; then
      container_kill "$TC_CONTAINER"; SURVIVOR="$TC_CONTAINER_2"
    elif [ "$HOLDER_HOST" = "$HOLDER_ID2" ]; then
      container_kill "$TC_CONTAINER_2"; SURVIVOR="$TC_CONTAINER"
    else
      SURVIVOR="$TC_CONTAINER"
      f "FW06: the reached marker names host ${HOLDER_HOST:-none}, which is neither worker container"
    fi
    unarm worker.claimed
    barrier_reset
    if wait_sql "SELECT status FROM transcoding_tasks WHERE id='${TASK}'" completed "$WORKER_RECLAIM_BOUND"; then
      p "FW06: the surviving worker took the dead worker's claim and completed the task"
      GEN="$(field "$TASK" output_generation)"
      if [ "$(published_generation)" = "$GEN" ] && [ "$(task_count)" = "1" ]; then
        SURVIVOR_LOGS="$(docker logs --since "$(( $(date +%s) - 900 ))s" "$SURVIVOR" 2>&1 || true)"
        if printf '%s' "$SURVIVOR_LOGS" | grep -q "$TASK"; then
          p "FW06: the surviving worker's logs name task ${TASK}, so the completion is attributable to it"
          record FW06 worker.claimed PASS "survivor=${SURVIVOR}, published=${GEN}, tasks=1"
        else
          f "FW06: the survivor's logs never mention task ${TASK}"
          record FW06 worker.claimed FAIL "not attributable to the survivor"
        fi
      else
        f "FW06: published=$(published_generation) task_gen=${GEN} tasks=$(task_count)"
        record FW06 worker.claimed FAIL "published=$(published_generation) task_gen=${GEN}"
      fi
    else
      f "FW06: the surviving worker did not complete the task within ${WORKER_RECLAIM_BOUND}s"
      record FW06 worker.claimed FAIL "status=${SQLLAST:-?}"
    fi
  else
    f "FW06: the claim point never fired"
    record FW06 worker.claimed FAIL "not reached within 90s"
  fi
  docker compose -p "$PROJECT" -f "$COMPOSE" up -d --no-deps --pull never --scale transcoder=1 transcoder >/dev/null 2>&1 || :
else
  f "FW06: could not start a second worker replica"
  record FW06 worker.claimed FAIL "second replica unavailable"
fi

# ---------------------------------------------------------------------------
# Scenario FO01 — the submitter dies between "intent committed" and "published
# to the broker". The task must still be enqueued by the restarting pump.
# ---------------------------------------------------------------------------
log 'FO01 kill at outbox.before_publish'
barrier_reset
arm outbox.before_publish
# The publish happens INSIDE the upload-completion request: the completion
# transaction commits the task row with its intent, and the post-upload hook
# publishes immediately. So the seed runs in the background and the marker is
# polled WHILE it runs — polling only after it returned would race a marker the
# barrier withdraws the moment it times out.
( seed_content fo01 ) >"$WORKDIR/seed-fo01.log" 2>&1 &
SEED_PID=$!
if wait_reached outbox.before_publish 60; then
  TASK="$(sql_scalar "SELECT id FROM outbox WHERE kind='task' ORDER BY created_at DESC LIMIT 1")"
  CONTENT_ID="$(sql_scalar "SELECT content_id FROM transcoding_tasks WHERE id='${TASK}'")"
  INTENT_STATUS="$(sql_scalar "SELECT status FROM outbox WHERE kind='task' AND id='${TASK}'")"
  CLAIMED="$(field "$TASK" lease_owner)"
  # Two pumps really run in this stack: the monolith submits with no embedded
  # worker and drives a standalone pump, the isolated transcoder drives the same
  # pump from its worker context — and BOTH park here before publishing. Killing
  # only one would leave the other to time out of the barrier and publish, so the
  # enrichment after the restart would prove nothing. Crash the whole stack: no
  # process survives to publish, and only a restart can recover the intent.
  container_kill "$MONO_CONTAINER"
  container_kill "$TC_CONTAINER"
  unarm outbox.before_publish
  barrier_reset
  container_start "$MONO_CONTAINER"
  container_start "$TC_CONTAINER"
  # The in-flight completion request died with the process; that IS the crash.
  wait "$SEED_PID" >/dev/null 2>&1 || :
  SEED_PID=""
  # A token minted before this restart may belong to a session the new process
  # no longer knows; FO02's seed must perform a real login.
  TOKEN_A=""
  if [ -n "$TASK" ] && [ "$INTENT_STATUS" = "pending" ] && [ -z "$CLAIMED" ]; then
    p "FO01: while the submitter was parked the intent was still pending and no worker had claimed the task"
    record FO01-parked outbox.before_publish PASS "outbox=pending, lease_owner empty"
  else
    f "FO01: task=${TASK:-none}, outbox=${INTENT_STATUS:-none}, lease_owner=${CLAIMED:-empty}"
    record FO01-parked outbox.before_publish FAIL "task=${TASK:-none} outbox=${INTENT_STATUS:-none} lease=${CLAIMED:-empty}"
  fi
  if wait_sql "SELECT status FROM outbox WHERE kind='task' AND id='${TASK}'" delivered 90; then
    p "FO01: the restarted pump published the intent and marked it delivered"
    record FO01 outbox.before_publish PASS "intent delivered after restart, no task lost"
  else
    f "FO01: the intent was not delivered after the restart (${SQLLAST:-none})"
    record FO01 outbox.before_publish FAIL "intent=${SQLLAST:-none}"
  fi
  if wait_sql "SELECT status FROM transcoding_tasks WHERE id='${TASK}'" completed 180; then
    p "FO01: the task enqueued after the restart really executed on the worker"
    GEN="$(field "$TASK" output_generation)"
    [ "$(published_generation)" = "$GEN" ] && p "FO01: one published generation (${GEN})" \
      || f "FO01: published=$(published_generation) task_gen=${GEN}"
  else
    f "FO01: the task never completed after the restart (status=${SQLLAST:-?})"
    record FO01-exec outbox.before_publish FAIL "status=${SQLLAST:-?}"
  fi
else
  SEED_RC=0
  if [ -n "$SEED_PID" ]; then wait "$SEED_PID" >/dev/null 2>&1 || SEED_RC=$?; SEED_PID=""; fi
  f "FO01: the outbox point never fired (seed exit=${SEED_RC})"
  sed 's/^/   /' "$WORKDIR/seed-fo01.log" 2>/dev/null | tail -8
  diag_outbox
  record FO01 outbox.before_publish FAIL "not reached within 60s (seed exit=${SEED_RC})"
fi

# ---------------------------------------------------------------------------
# Scenario FO02 — the submitter dies between the broker's PubAck and marking the
# intent delivered. The message is already in the stream, so the restart
# republishes the SAME id: the broker deduplicates it and the media work must
# still happen exactly once.
# ---------------------------------------------------------------------------
log 'FO02 kill at outbox.after_publish'
barrier_reset
arm outbox.after_publish
( seed_content fo02 ) >"$WORKDIR/seed-fo02.log" 2>&1 &
SEED_PID=$!
if wait_reached outbox.after_publish 60; then
  TASK="$(sql_scalar "SELECT id FROM outbox WHERE kind='task' ORDER BY created_at DESC LIMIT 1")"
  CONTENT_ID="$(sql_scalar "SELECT content_id FROM transcoding_tasks WHERE id='${TASK}'")"
  INTENT_STATUS="$(sql_scalar "SELECT status FROM outbox WHERE kind='task' AND id='${TASK}'")"
  # Paired with FO01: the whole stack dies with the broker already holding the
  # message and the intent still pending, so the only way the row converges is a
  # restart republishing the SAME message id.
  container_kill "$MONO_CONTAINER"
  container_kill "$TC_CONTAINER"
  unarm outbox.after_publish
  barrier_reset
  container_start "$MONO_CONTAINER"
  container_start "$TC_CONTAINER"
  wait "$SEED_PID" >/dev/null 2>&1 || :
  SEED_PID=""
  # Do not present a token minted before the restart: session storage may not
  # survive it, and FO02's seed must exercise a real login against the new process.
  TOKEN_A=""
  if [ -n "$TASK" ] && [ "$INTENT_STATUS" = "pending" ]; then
    p "FO02: the broker had already accepted the publish while the intent was still pending"
    record FO02-parked outbox.after_publish PASS "outbox=pending immediately after PubAck"
  else
    f "FO02: task=${TASK:-none}, outbox=${INTENT_STATUS:-none} where a pending intent was required"
    record FO02-parked outbox.after_publish FAIL "task=${TASK:-none} outbox=${INTENT_STATUS:-none}"
  fi
  if wait_sql "SELECT status FROM outbox WHERE kind='task' AND id='${TASK}'" delivered 90; then
    p "FO02: the restarted pump republished the same id and the intent converged to delivered"
  else
    f "FO02: the intent did not converge (${SQLLAST:-none})"
    record FO02 outbox.after_publish FAIL "intent=${SQLLAST:-none}"
  fi
  if wait_sql "SELECT status FROM transcoding_tasks WHERE id='${TASK}'" completed 180; then
    GEN="$(field "$TASK" output_generation)"
    ATTEMPT="$(field "$TASK" attempt)"
    if [ "$(published_generation)" = "$GEN" ] && [ "$(task_count)" = "1" ]; then
      p "FO02: exactly one published generation (${GEN}) and one task row after the duplicate publish"
      record FO02 outbox.after_publish PASS "published=${GEN}, attempt=${ATTEMPT}, tasks=1"
    else
      f "FO02: published=$(published_generation) task_gen=${GEN} tasks=$(task_count)"
      record FO02 outbox.after_publish FAIL "published=$(published_generation) task_gen=${GEN}"
    fi
  else
    f "FO02: the task never completed (status=${SQLLAST:-?})"
    record FO02-exec outbox.after_publish FAIL "status=${SQLLAST:-?}"
  fi
else
  SEED_RC=0
  if [ -n "$SEED_PID" ]; then wait "$SEED_PID" >/dev/null 2>&1 || SEED_RC=$?; SEED_PID=""; fi
  f "FO02: the outbox point never fired (seed exit=${SEED_RC})"
  sed 's/^/   /' "$WORKDIR/seed-fo02.log" 2>/dev/null | tail -8
  diag_outbox
  record FO02 outbox.after_publish FAIL "not reached within 60s (seed exit=${SEED_RC})"
fi

# ---------------------------------------------------------------------------
# Registry / transaction faults (section 0.2 D). The chain write is NEVER atomic
# with the DB write: the signed raw tx is persisted, then handed to a node, and
# only afterwards is the outcome recorded. Every window in that sequence is
# faulted here against the real Anvil, and the recovery is asserted on the
# candidate's persisted row PLUS the node's own state — never on a log line.
# ---------------------------------------------------------------------------
log 'FR00 registry preconditions'
# `grep -q` closes the pipe as soon as it matches, and with `pipefail` a long
# `docker logs` producer then reports a broken pipe — so the check would read
# "no such line" whenever the line WAS there. Capture the log first.
MONO_LOG="$(docker logs "$MONO_CONTAINER" 2>&1)"
case "$MONO_LOG" in
  *"On-chain content registry enabled"*)
    p "the candidate really started with the durable on-chain registry enabled"
    record FR00 preconditions PASS "registry publisher enabled in the monolith" ;;
  *)
    f "the monolith never logged the registry being enabled (no intent row can exist)"
    record FR00 preconditions FAIL "registry not enabled" ;;
esac
HEAD0="$(head_number)"
if [ -n "$HEAD0" ] && [ "$HEAD0" -gt 0 ]; then
  p "the controller reads the real chain over the project network (head=${HEAD0})"
  record FR00-chain preconditions PASS "anvil head=${HEAD0}"
else
  f "the chain is not reachable at ${ANVIL_URL} from the controller"
  record FR00-chain preconditions FAIL "head unreadable"
fi
# The response-dropping proxy is driven by a file in the shared barrier volume,
# so prove this side can switch it on and off before any scenario relies on it.
if docker exec "$HELPER" sh -c ': > /b/registry.broadcast.drop' 2>/dev/null \
  && docker exec "$HELPER" sh -c 'rm -f /b/registry.broadcast.drop' 2>/dev/null; then
  p "the harness can drive the response-dropping proxy through the barrier volume"
  record FR00-proxy preconditions PASS "registry.broadcast.drop writable and removable"
else
  f "the barrier volume does not accept the proxy switch (the drop fault cannot fire)"
  record FR00-proxy preconditions FAIL "registry.broadcast.drop not writable"
fi
# A receipt reorg has to be produced by the CHAIN, not simulated. Probe that the
# node really un-mines blocks; without it the scenario below could not exist.
if [ -n "$HEAD0" ] && [ "$HEAD0" -gt 6 ] && anvil_rollback 3 && [ "$(head_number)" -lt "$HEAD0" ]; then
  p "anvil_rollback really reorgs the chain (head ${HEAD0} -> $(head_number))"
  record FR00-reorg preconditions PASS "a 3-block rollback reduced the head below ${HEAD0}"
else
  f "anvil_rollback is unavailable, so a real receipt reorg cannot be produced"
  record FR00-reorg preconditions FAIL "the head did not fall after a rollback"
fi

# ---------------------------------------------------------------------------
# Scenario FR01 — kill between "signed raw tx persisted" and "handed to a node".
# The persisted row is the whole point of SG-13 A.4: recovery must reuse those
# exact bytes, never sign a new nonce.
# ---------------------------------------------------------------------------
log 'FR01 kill at publisher.before_broadcast'
barrier_reset
arm publisher.before_broadcast
if seed_content fr01 && wait_reached publisher.before_broadcast 120; then
  FR_TX="$(reg_field "$CONTENT_ID" tx_hash)"
  FR_NONCE="$(reg_field "$CONTENT_ID" nonce)"
  FR_STATUS="$(reg_field "$CONTENT_ID" status)"
  FR_RAW="$(reg_raw "$FR_TX")"
  FR_ATTEMPTS="$(reg_attempts "$CONTENT_ID" "$FR_NONCE")"
  FR_BLOCK="$(reg_field "$CONTENT_ID" block_number)"
  FR_CHAINNONCE="$(chain_nonce)"
  if [ "$FR_STATUS" = "submitted" ] && [ -n "$FR_TX" ] && [ -n "$FR_NONCE" ] \
    && [ -n "$FR_RAW" ] && [ -z "$FR_BLOCK" ]; then
    p "FR01: the signed raw tx was persisted as 'submitted' before any node saw it"
    record FR01-park publisher.before_broadcast PASS "tx=${FR_TX:0:12} nonce=${FR_NONCE} signed_attempts=${FR_ATTEMPTS}"
  else
    f "FR01: parked state status=${FR_STATUS:-none} tx=${FR_TX:-none} nonce=${FR_NONCE:-none} block=${FR_BLOCK:-empty} raw=${FR_RAW:+present}"
    record FR01-park publisher.before_broadcast FAIL "status=${FR_STATUS:-none} raw=${FR_RAW:+present}"
  fi
  # The claim is "the node never saw it", so the node's own transaction lookup is
  # the evidence: eth_getTransactionByHash returns null for a hash it has never
  # received. The chain nonce is deliberately NOT part of the verdict — an
  # earlier scenario un-mined blocks (FR00's rollback), so the chain's nonce can
  # legitimately sit BELOW the reserved nonce and comparing the two would decide
  # the check on a stale artefact instead of on the persisted tx.
  if [ -n "$FR_TX" ] && ! tx_known "$FR_TX"; then
    p "FR01: the node has no transaction for the persisted raw tx (chain nonce ${FR_CHAINNONCE}, reserved ${FR_NONCE})"
    record FR01-unsent publisher.before_broadcast PASS "eth_getTransactionByHash=null for tx=${FR_TX:0:12}; chain nonce=${FR_CHAINNONCE}"
  else
    f "FR01: the node already knows tx ${FR_TX:0:12} while the candidate has only persisted it (chain nonce=${FR_CHAINNONCE:-?}, reserved=${FR_NONCE:-?})"
    printf '   raw eth_getTransactionByHash: %s\n' \
      "$(cast rpc --rpc-url "$ANVIL_URL" eth_getTransactionByHash "$FR_TX" 2>/dev/null | head -c 240)"
    record FR01-unsent publisher.before_broadcast FAIL "the node already has the tx; chain nonce=${FR_CHAINNONCE:-?} reserved=${FR_NONCE:-?}"
  fi
  container_kill "$MONO_CONTAINER"
  unarm publisher.before_broadcast
  barrier_reset
  container_start "$MONO_CONTAINER"
  # A token minted before the restart may belong to a session the new process no
  # longer knows; the next seed must perform a real login.
  TOKEN_A=""
  if wait_worker_up "$MONO_CONTAINER" 120; then
    if wait_sql "SELECT status FROM content_registry_intents WHERE content_id='${CONTENT_ID}'" confirmed 150; then
      R_TX="$(reg_field "$CONTENT_ID" tx_hash)"
      R_NONCE="$(reg_field "$CONTENT_ID" nonce)"
      R_RAW2="$(reg_raw "$R_TX")"
      R_ATTEMPTS="$(reg_attempts "$CONTENT_ID" "$R_NONCE")"
      if [ "$R_TX" = "$FR_TX" ] && [ "$R_NONCE" = "$FR_NONCE" ] && [ "$R_RAW2" = "$FR_RAW" ] \
        && [ "$R_ATTEMPTS" = "$FR_ATTEMPTS" ] && tx_mined "$FR_TX"; then
        p "FR01: the restart re-sent the SAME raw tx/hash/nonce and it confirmed"
        record FR01 publisher.before_broadcast PASS "same tx=${FR_TX:0:12}, nonce=${FR_NONCE}, ${R_ATTEMPTS} signed attempt(s), receipt present"
      else
        f "FR01: recovery changed the transaction (tx ${FR_TX:0:12}->${R_TX:0:12}, nonce ${FR_NONCE}->${R_NONCE}, attempts ${FR_ATTEMPTS}->${R_ATTEMPTS}, raw same=$([ "$R_RAW2" = "$FR_RAW" ] && printf yes || printf no))"
        record FR01 publisher.before_broadcast FAIL "transaction identity drifted"
      fi
    else
      f "FR01: the intent never confirmed after the restart (status=${SQLLAST:-none})"
      record FR01 publisher.before_broadcast FAIL "status=${SQLLAST:-none} after 150s"
      diag_registry
    fi
  else
    f "FR01: the monolith did not come back healthy"
    record FR01 publisher.before_broadcast FAIL "monolith unhealthy after restart"
  fi
else
  f "FR01: the before-broadcast point never fired"
  record FR01 publisher.before_broadcast FAIL "not reached within 120s"
  diag_registry
fi

# ---------------------------------------------------------------------------
# Scenario FR02 — the NODE ACCEPTED the transaction and the response was
# discarded by the proxy. This is the window the publisher can never learn
# about: the chain has the tx, the candidate's last recorded outcome is
# "submitted", and a crash right here must re-read the receipt, not re-sign.
# ---------------------------------------------------------------------------
log 'FR02 dropped response after the node accepted the tx'
# The drop belongs to whoever broadcasts next, so no other intent may be in
# flight: a quiet registry is a precondition, not a hope.
if wait_sql "SELECT count(*) FROM content_registry_intents WHERE status IN ('pending','submitted','broadcast_unknown')" 0 150; then
  p "FR02: no other registry write is in flight, so the drop can only belong to this scenario"
else
  f "FR02: ${SQLLAST:-?} registry intents were still in flight (the drop would be unattributable)"
  diag_registry
fi
barrier_reset
arm publisher.before_broadcast
docker exec "$HELPER" sh -c ': > /b/registry.broadcast.drop' 2>/dev/null || f "FR02: could not switch the proxy drop on"
if seed_content fr02 && wait_reached publisher.before_broadcast 120; then
  FR_TX="$(reg_field "$CONTENT_ID" tx_hash)"
  FR_NONCE="$(reg_field "$CONTENT_ID" nonce)"
  FR_RAW="$(reg_raw "$FR_TX")"
  FR_ATTEMPTS="$(reg_attempts "$CONTENT_ID" "$FR_NONCE")"
  # Hand the publisher over to the proxy: dismount the pre-send rendezvous, then
  # stop the process at the drop point instead.
  unarm publisher.before_broadcast
  arm registry.broadcast
  release_point publisher.before_broadcast
  if wait_reached registry.broadcast 60; then
    FR_DROP_HEAD="$(head_number)"
    if docker exec "$HELPER" test -f /b/registry.broadcast.dropped 2>/dev/null && tx_mined "$FR_TX" \
      && [ "$(chain_nonce)" = "$((FR_NONCE + 1))" ] && [ "$(reg_field "$CONTENT_ID" status)" = "submitted" ]; then
      p "FR02: the node accepted tx ${FR_TX:0:12} while the candidate still only knew 'submitted'"
      record FR02-onchain registry.broadcast PASS "receipt present, chain nonce=$((FR_NONCE + 1)), row still submitted"
    else
      f "FR02: receipt=$([ -n "$(receipt_field "$FR_TX" blockHash)" ] && printf present || printf absent) chain nonce=$(chain_nonce) row=$(reg_field "$CONTENT_ID" status) dropped_marker=$([ "$(docker exec "$HELPER" test -f /b/registry.broadcast.dropped 2>/dev/null; echo $?)" = 0 ] && printf yes || printf no)"
      record FR02-onchain registry.broadcast FAIL "the node never accepted the tx the proxy was told to drop"
    fi
    container_kill "$MONO_CONTAINER"
    unarm registry.broadcast
    docker exec "$HELPER" sh -c 'rm -f /b/registry.broadcast.drop /b/registry.broadcast.dropped' 2>/dev/null || :
    barrier_reset
    container_start "$MONO_CONTAINER"
    TOKEN_A=""
    if wait_worker_up "$MONO_CONTAINER" 120; then
      if wait_sql "SELECT status FROM content_registry_intents WHERE content_id='${CONTENT_ID}'" confirmed 150; then
        R_TX="$(reg_field "$CONTENT_ID" tx_hash)"
        R_NONCE="$(reg_field "$CONTENT_ID" nonce)"
        R_RAW2="$(reg_raw "$R_TX")"
        R_ATTEMPTS="$(reg_attempts "$CONTENT_ID" "$R_NONCE")"
        R_BH="$(reg_field "$CONTENT_ID" block_hash)"
        if [ "$R_TX" = "$FR_TX" ] && [ "$R_NONCE" = "$FR_NONCE" ] && [ "$R_RAW2" = "$FR_RAW" ] \
          && [ "$R_ATTEMPTS" = "$FR_ATTEMPTS" ] && [ "$R_BH" = "$(receipt_field "$FR_TX" blockHash)" ]; then
          p "FR02: the restart recovered by the RECEIPT of the same raw tx/hash/nonce (no re-sign)"
          record FR02 registry.broadcast PASS "same tx=${FR_TX:0:12}, nonce=${FR_NONCE}, block=$(receipt_field "$FR_TX" blockNumber), ${R_ATTEMPTS} signed attempt(s)"
        else
          f "FR02: recovery drifted (tx ${FR_TX:0:12}->${R_TX:0:12}, nonce ${FR_NONCE}->${R_NONCE}, attempts ${FR_ATTEMPTS}->${R_ATTEMPTS}, recorded block hash matches receipt=$([ "$R_BH" = "$(receipt_field "$FR_TX" blockHash)" ] && printf yes || printf no))"
          record FR02 registry.broadcast FAIL "transaction identity drifted (head at drop=${FR_DROP_HEAD})"
        fi
      else
        f "FR02: the intent never confirmed after the restart (status=${SQLLAST:-none})"
        record FR02 registry.broadcast FAIL "status=${SQLLAST:-none} after 150s"
        diag_registry
      fi
    else
      f "FR02: the monolith did not come back healthy"
      record FR02 registry.broadcast FAIL "monolith unhealthy after restart"
    fi
  else
    f "FR02: the proxy drop point never fired (the response was not discarded)"
    record FR02 registry.broadcast FAIL "not reached within 60s"
    diag_registry
  fi
else
  f "FR02: the before-broadcast point never fired"
  record FR02-parked publisher.before_broadcast FAIL "not reached within 120s"
  docker exec "$HELPER" sh -c 'rm -f /b/registry.broadcast.drop' 2>/dev/null || :
  diag_registry
fi

# ---------------------------------------------------------------------------
# Scenario FR03 — a real RECEIPT REORG: the confirmed block is un-mined by the
# chain, so the recorded block hash stops being canonical. 'confirmed' must not
# be a one-way door, and the same raw tx must come back through the new history.
# ---------------------------------------------------------------------------
log 'FR03 confirmed receipt reorg + historical recovery'
barrier_reset
if seed_content fr03; then
  if wait_sql "SELECT status FROM content_registry_intents WHERE content_id='${CONTENT_ID}'" confirmed 150; then
    FR_TX="$(reg_field "$CONTENT_ID" tx_hash)"
    FR_NONCE="$(reg_field "$CONTENT_ID" nonce)"
    FR_RAW="$(reg_raw "$FR_TX")"
    FR_ATTEMPTS="$(reg_attempts "$CONTENT_ID" "$FR_NONCE")"
    FR_BLOCK="$(reg_field "$CONTENT_ID" block_number)"
    FR_BH="$(reg_field "$CONTENT_ID" block_hash)"
    HEAD="$(head_number)"
    # Orphan the confirmed block: un-mine it and one block below, but never the
    # blocks that hold the contract deployments.
    DEPTH=$((HEAD - FR_BLOCK + 2))
    [ "$DEPTH" -lt 3 ] && DEPTH=3
    if [ "$DEPTH" -le 64 ] && [ "$((HEAD - DEPTH))" -gt 4 ] && anvil_rollback "$DEPTH"; then
      p "FR03: reorged the chain by ${DEPTH} blocks, orphaning the confirmed block ${FR_BLOCK}"
    else
      f "FR03: could not reorg ${DEPTH} blocks from head ${HEAD} (confirmed block ${FR_BLOCK})"
      record FR03-reorg receipt-reorg FAIL "rollback depth=${DEPTH} head=${HEAD} block=${FR_BLOCK}"
    fi
    # The chain re-mines; the recorded hash must stop matching the canonical one.
    REORG_SEEN=0
    for _ in $(seq 1 60); do
      if [ "$(block_hash "$FR_BLOCK")" != "$FR_BH" ]; then REORG_SEEN=1; break; fi
      sleep 1
    done
    if [ "$REORG_SEEN" = 1 ]; then
      p "FR03: the canonical block at height ${FR_BLOCK} is now a different block"
      record FR03-reorg receipt-reorg PASS "stored ${FR_BH:0:12} -> canonical $(block_hash "$FR_BLOCK" | cut -c1-12)"
    else
      f "FR03: block ${FR_BLOCK} still has the recorded hash after the rollback"
      record FR03-reorg receipt-reorg FAIL "block hash unchanged"
    fi
    # The publisher must notice and re-verify BEFORE any "confirmed again" claim
    # means anything: until the row stops naming the orphaned block, a status poll
    # would be satisfied by the stale confirmation the reorg just invalidated.
    if wait_reg_reorged "$CONTENT_ID" "$FR_BH" 120; then
      p "FR03: the candidate detected the reorg and took the intent out of 'confirmed'"
      record FR03-detected receipt-reorg PASS "row moved to ${REG_LAST} once its block stopped being canonical"
    else
      f "FR03: the intent stayed at ${REG_LAST} while its block was no longer canonical"
      record FR03-detected receipt-reorg FAIL "the reorg was never re-verified (row ${REG_LAST})"
      diag_registry
    fi
    if wait_sql "SELECT status FROM content_registry_intents WHERE content_id='${CONTENT_ID}'" confirmed 180; then
      R_BH="$(reg_field "$CONTENT_ID" block_hash)"
      R_BLOCK="$(reg_field "$CONTENT_ID" block_number)"
      R_TX="$(reg_field "$CONTENT_ID" tx_hash)"
      R_RAW2="$(reg_raw "$R_TX")"
      R_ATTEMPTS="$(reg_attempts "$CONTENT_ID" "$FR_NONCE")"
      if [ "$R_TX" = "$FR_TX" ] && [ "$R_RAW2" = "$FR_RAW" ] && [ "$R_ATTEMPTS" = "$FR_ATTEMPTS" ] \
        && [ "$R_BH" != "$FR_BH" ] && [ "$R_BH" = "$(block_hash "$R_BLOCK")" ] && tx_mined "$FR_TX"; then
        p "FR03: the same transaction confirmed again under the new chain history"
        record FR03 receipt-reorg PASS "tx=${FR_TX:0:12} unchanged, block ${FR_BLOCK}->${R_BLOCK}, hash ${R_BH:0:12} is canonical, ${R_ATTEMPTS} signed attempt(s)"
      else
        f "FR03: after the reorg tx=${R_TX:0:12} block=${R_BLOCK:-?} hash=${R_BH:0:12} canonical=$(block_hash "${R_BLOCK:-0}" | cut -c1-12) attempts=${R_ATTEMPTS} raw same=$([ "$R_RAW2" = "$FR_RAW" ] && printf yes || printf no)"
        record FR03 receipt-reorg FAIL "recovery did not re-verify under the new history"
        diag_registry
      fi
    else
      f "FR03: the intent did not return to confirmed after the reorg (status=${SQLLAST:-none})"
      record FR03 receipt-reorg FAIL "status=${SQLLAST:-none} after 180s"
      diag_registry
    fi
  else
    f "FR03: the intent never confirmed before the reorg (status=${SQLLAST:-none})"
    record FR03 receipt-reorg FAIL "status=${SQLLAST:-none}"
    diag_registry
  fi
else
  f "FR03: could not seed a content for the reorg scenario"
  record FR03 receipt-reorg FAIL "seed failed"
fi

# ---------------------------------------------------------------------------
# Scenario FR04 — kill AFTER the node accepted the tx and BEFORE the outcome was
# written. The `publisher.after_send` point is exactly that instant: a crash here
# leaves 'submitted' with a live receipt, which only a receipt lookup recovers.
# ---------------------------------------------------------------------------
log 'FR04 kill at publisher.after_send'
barrier_reset
arm publisher.after_send
if seed_content fr04 && wait_reached publisher.after_send 120; then
  FR_TX="$(reg_field "$CONTENT_ID" tx_hash)"
  FR_NONCE="$(reg_field "$CONTENT_ID" nonce)"
  FR_RAW="$(reg_raw "$FR_TX")"
  FR_ATTEMPTS="$(reg_attempts "$CONTENT_ID" "$FR_NONCE")"
  FR_STATUS="$(reg_field "$CONTENT_ID" status)"
  FR_BLOCK="$(reg_field "$CONTENT_ID" block_number)"
  if tx_mined "$FR_TX" && [ "$FR_STATUS" = "submitted" ]; then
    p "FR04: the tx is on chain while the candidate has recorded no receipt yet"
    record FR04-park publisher.after_send PASS "receipt present, row submitted, nonce=${FR_NONCE}"
  else
    f "FR04: receipt=$([ -n "$(receipt_field "$FR_TX" blockHash)" ] && printf present || printf absent) row=${FR_STATUS:-none}"
    record FR04-park publisher.after_send FAIL "row=${FR_STATUS:-none}"
  fi
  container_kill "$MONO_CONTAINER"
  unarm publisher.after_send
  barrier_reset
  container_start "$MONO_CONTAINER"
  TOKEN_A=""
  if wait_worker_up "$MONO_CONTAINER" 120; then
    if wait_sql "SELECT status FROM content_registry_intents WHERE content_id='${CONTENT_ID}'" confirmed 150; then
      R_TX="$(reg_field "$CONTENT_ID" tx_hash)"
      R_NONCE="$(reg_field "$CONTENT_ID" nonce)"
      R_RAW2="$(reg_raw "$R_TX")"
      R_ATTEMPTS="$(reg_attempts "$CONTENT_ID" "$R_NONCE")"
      R_BH="$(reg_field "$CONTENT_ID" block_hash)"
      if [ "$R_TX" = "$FR_TX" ] && [ "$R_NONCE" = "$FR_NONCE" ] && [ "$R_RAW2" = "$FR_RAW" ] \
        && [ "$R_ATTEMPTS" = "$FR_ATTEMPTS" ] && [ "$R_BH" = "$(receipt_field "$FR_TX" blockHash)" ]; then
        p "FR04: the restart found the existing receipt and did not sign anything new"
        record FR04 publisher.after_send PASS "same tx=${FR_TX:0:12}, nonce=${FR_NONCE}, ${R_ATTEMPTS} signed attempt(s), recorded block hash matches the receipt"
      else
        f "FR04: recovery drifted (tx ${FR_TX:0:12}->${R_TX:0:12}, nonce ${FR_NONCE}->${R_NONCE}, attempts ${FR_ATTEMPTS}->${R_ATTEMPTS})"
        record FR04 publisher.after_send FAIL "transaction identity drifted"
      fi
    else
      f "FR04: the intent never confirmed after the restart (status=${SQLLAST:-none})"
      record FR04 publisher.after_send FAIL "status=${SQLLAST:-none} after 150s"
      diag_registry
    fi
  else
    f "FR04: the monolith did not come back healthy"
    record FR04 publisher.after_send FAIL "monolith unhealthy after restart"
  fi
else
  f "FR04: the after-send point never fired"
  record FR04 publisher.after_send FAIL "not reached within 120s"
  diag_registry
fi

# ---------------------------------------------------------------------------
# Scenario FR05 — the receipt is durable BEFORE the confirmation decision. The
# two steps must be separately observable: a crash here must not lose the
# receipt, and the confirmation must still be evaluated afterwards.
# ---------------------------------------------------------------------------
log 'FR05 park at publisher.after_receipt'
barrier_reset
arm publisher.after_receipt
if seed_content fr05 && wait_reached publisher.after_receipt 150; then
  FR_TX="$(reg_field "$CONTENT_ID" tx_hash)"
  FR_BLOCK="$(reg_field "$CONTENT_ID" block_number)"
  FR_BH="$(reg_field "$CONTENT_ID" block_hash)"
  FR_STATUS="$(reg_field "$CONTENT_ID" status)"
  if [ "$FR_STATUS" = "submitted" ] && [ -n "$FR_BLOCK" ] && [ "$FR_BH" = "$(receipt_field "$FR_TX" blockHash)" ]; then
    p "FR05: the receipt is durable while the confirmation has not been evaluated yet"
    record FR05-park publisher.after_receipt PASS "row submitted with block ${FR_BLOCK}, hash matches the receipt"
  else
    f "FR05: row=${FR_STATUS:-none} block=${FR_BLOCK:-empty} stored_hash=${FR_BH:0:12} receipt_hash=$(receipt_field "$FR_TX" blockHash | cut -c1-12)"
    record FR05-park publisher.after_receipt FAIL "row=${FR_STATUS:-none} block=${FR_BLOCK:-empty}"
  fi
  release_point publisher.after_receipt
  unarm publisher.after_receipt
  if wait_sql "SELECT status FROM content_registry_intents WHERE content_id='${CONTENT_ID}'" confirmed 60; then
    if [ "$(reg_field "$CONTENT_ID" block_number)" = "$FR_BLOCK" ] && [ "$(reg_field "$CONTENT_ID" tx_hash)" = "$FR_TX" ]; then
      p "FR05: the confirmation was evaluated after the release, for the same receipt"
      record FR05 publisher.after_receipt PASS "confirmed at block ${FR_BLOCK}, same tx=${FR_TX:0:12}"
    else
      f "FR05: confirmed with block=$(reg_field "$CONTENT_ID" block_number) tx=$(reg_field "$CONTENT_ID" tx_hash)"
      record FR05 publisher.after_receipt FAIL "confirmation moved the receipt"
    fi
  else
    f "FR05: no confirmation after the release (status=${SQLLAST:-none})"
    record FR05 publisher.after_receipt FAIL "status=${SQLLAST:-none}"
  fi
else
  f "FR05: the after-receipt point never fired"
  record FR05 publisher.after_receipt FAIL "not reached within 150s"
  diag_registry
fi


# ---------------------------------------------------------------------------
# Event-indexer faults (section 0.2 D). The log subscription may only ACCELERATE
# invalidation — it must never grant access — and it must lose nothing across a
# restart, a checkpoint crash or a chain reorg. Everything below reads the
# candidate's durable event store and the chain's own logs, so "the subscription
# really consumed the log" is a fact about stored rows and block heights.
# ---------------------------------------------------------------------------
DEMO_NFT="0x5FbDB2315678afecb367f032d93F642f64180aa3"
# Anvil account 1: a second subject, so a transfer is not the same actor.
WALLET_B="0x70997970C51812dc3A010C7d01b50e0d17dc79C8"
# The log topics the stored payload carries, so a durable row can be attributed to
# a real event kind without relying on the (in-memory only) decoded form.
TRANSFER_TOPIC="0xddf252ad1be2c89b69c2b068fc378daa952ba7f163c4a11628f55a4df523b3ef"
TRANSFER_BATCH_TOPIC="0x4a39dc06d4c0dbc64b70af90fd698a233a518aa5d07e595d983b8c0526c8f7fb"

event_cursor() { sql_scalar "SELECT block_number FROM event_checkpoints ORDER BY updated_at DESC LIMIT 1"; }
# tx_hash_of parses a transaction hash out of cast's output, whichever shape the
# installed version prints (receipt JSON or a plain receipt).
tx_hash_of() {
  python3 -c "
import sys,json,re
data=sys.stdin.read()
try:
    d=json.loads(data)
    if isinstance(d,dict):
        for k in ('transactionHash','transaction_hash','txHash'):
            if d.get(k): print(str(d[k]).lower()); raise SystemExit
        r=d.get('receipt')
        if isinstance(r,dict):
            for k in ('transactionHash','transaction_hash'):
                if r.get(k): print(str(r[k]).lower()); raise SystemExit
except Exception:
    pass
m=re.search(r'0x[0-9a-fA-F]{64}', data)
print(m.group(0).lower() if m else '')
"
}
# chain_send <to> <signature> [args...] — one real signed transaction from the
# controller, returning the hash the NODE computed for it.
chain_send() {
  local to="$1" sig="$2"; shift 2
  cast send --json --rpc-url "$ANVIL_URL" --private-key "$KEY_A" "$to" "$sig" "$@" 2>/dev/null | tx_hash_of
}
erc721_mint() { chain_send "$DEMO_NFT" 'mint(address)' "$WALLET_A"; }
erc1155_mint() { chain_send "${ERC1155_ADDR}" 'mint(address,uint256,uint256)' "$1" "$2" "$3"; }
erc1155_batch() {
  chain_send "${ERC1155_ADDR}" 'safeBatchTransferFrom(address,address,uint256[],uint256[],bytes)' \
    "$WALLET_A" "$WALLET_B" "[1,2]" "[1,3]" '0x'
}

# The NFT gate caches EVIDENCE per subject; the event path must be able to delete
# exactly those keys. Planting them with the gate's own key shape is what makes
# the invalidation observable from outside the candidate process — so the shape
# here must match nftCacheKey() byte for byte: chain, LOWER-CASED contract,
# token id, LOWER-CASED standard, LOWER-CASED wallet. A mixed-case plant is a key
# no writer ever produces and no deleter can match.
gate_cache_key() { printf 'nft_cache:31337:%s:%s:%s:%s' "$(lower "$1")" "$2" "$(lower "$3")" "$(lower "$4")"; }
lower() { printf '%s' "$1" | tr 'A-Z' 'a-z'; }
# The app's Redis DB comes from the profile's config file (config.yaml: 0,
# config.dev.yaml: 1), so a plant aimed at the wrong DB is invisible to both the
# gate and the invalidation. Rather than assuming, the DB is DISCOVERED from a key
# the candidate itself writes: every HTTP request leaves a rate-limit counter
# behind, so the harness reads which DB holds one.
APP_REDIS_DB=""
detect_app_redis_db() {
  local db key
  # Force a fresh candidate-written key first: the rate limiter counts every HTTP
  # request, so one health call guarantees a counter exists right now — a scan
  # against keys that had already expired would pick the wrong DB or none at all.
  curl -s -o /dev/null -m 5 "$BASE_URL/health" >/dev/null 2>&1 || :
  for db in $(seq 0 15); do
    key="$(docker exec "$REDIS_CONTAINER" redis-cli -n "$db" --scan --pattern 'streamgate:*' 2>/dev/null | head -1)"
    if [ -n "$key" ]; then APP_REDIS_DB="$db"; return 0; fi
  done
  return 1
}
plant_gate_cache() {
  docker exec "$REDIS_CONTAINER" redis-cli -n "$APP_REDIS_DB" SET "$1" \
    '{"HasNFT":true,"Balance":1,"BlockNumber":1,"BlockHash":"0xfeed","Expires":"2030-01-01T00:00:00Z","ObservedAt":"2026-01-01T00:00:00Z"}' EX 600 >/dev/null 2>&1
}
gate_cache_alive() { docker exec "$REDIS_CONTAINER" redis-cli -n "$APP_REDIS_DB" EXISTS "$1" 2>/dev/null | tr -d '\r'; }
# wait_cache_gone <seconds> — every planted key must be gone within the window.
wait_cache_gone() {
  local deadline=$((SECONDS + $1)) alive key
  while [ "$SECONDS" -lt "$deadline" ]; do
    alive=0
    for key in "${PLANTED_KEYS[@]}"; do
      [ "$(gate_cache_alive "$key")" = "1" ] && alive=$((alive + 1))
    done
    [ "$alive" = 0 ] && return 0
    sleep 1
  done
  CACHE_ALIVE="$alive"
  return 1
}
# wait_chain_after <height> <seconds> — the chain must produce blocks above
# <height>, because a log only becomes indexable once the confirmation lag has
# caught up to it.
wait_chain_after() {
  local deadline=$((SECONDS + $2)) head
  while [ "$SECONDS" -lt "$deadline" ]; do
    head="$(head_number)"
    [ -n "$head" ] && [ "$head" -gt "$1" ] && return 0
    sleep 2
  done
  return 1
}

log 'FI00 event indexer preconditions'
PLANTED_KEYS=()
CACHE_ALIVE=""
if [ -n "$(docker exec "$MONO_CONTAINER" printenv STREAMGATE_ETH_WS_URL 2>/dev/null)" ]; then
  p "the stack configures a real WebSocket subscription for the log indexer"
  record FI00-ws preconditions PASS "STREAMGATE_ETH_WS_URL is set on the monolith"
else
  f "no WS endpoint is configured, so no real log subscription can exist"
  record FI00-ws preconditions FAIL "STREAMGATE_ETH_WS_URL empty"
fi
# The stack ships an ERC-721 demo contract only; an ERC-1155 Batch event needs a
# real ERC-1155 contract, deployed here with the same key the app's minter uses.
ERC1155_ADDR=""
if [ -d contracts ] \
  && ( cd contracts && forge create --rpc-url "$ANVIL_URL" --private-key "$KEY_A" \
        src/DemoERC1155.sol:DemoERC1155 --legacy --broadcast ) >"$WORKDIR/erc1155.log" 2>&1; then
  ERC1155_ADDR="$(python3 -c "
import re
# The 40-hex regex alone is not enough: forge also prints a 64-hex transaction
# hash, and its first 40 hex digits are a *substring* of that hash — so a greedy
# 'last 40-hex match' silently picked the tx hash up and the whitelist below
# pointed at an address with no code. Anchor on the label forge itself prints.
m = re.search(r'Deployed to:\s*(0x[0-9a-fA-F]{40})', open('$WORKDIR/erc1155.log').read())
print(m.group(1).lower() if m else '')
" 2>/dev/null)"
fi
if [ -n "$ERC1155_ADDR" ] && [ "$(cast code --rpc-url "$ANVIL_URL" "$ERC1155_ADDR" 2>/dev/null)" != "0x" ]; then
  p "deployed a real ERC-1155 contract for the batch event (${ERC1155_ADDR})"
  record FI00-erc1155 preconditions PASS "DemoERC1155 has runtime code at ${ERC1155_ADDR}"
else
  f "could not deploy an ERC-1155 contract, so a TransferBatch log cannot be produced"
  tail -5 "$WORKDIR/erc1155.log" 2>/dev/null | sed 's/^/   /'
  record FI00-erc1155 preconditions FAIL "DemoERC1155 deployment failed"
fi
# The indexer only scans contracts some ACTIVE rule gates, and it reads that
# whitelist at startup: both rules must exist before the restart below.
FI_CONTENT_OK=0
if seed_content fi00; then
  FI_CONTENT_OK=1
  api_post "/api/v1/content/${CONTENT_ID}/gating-rules" "$TOKEN_A" \
    "{\"contract_address\":\"${DEMO_NFT}\",\"chain_id\":31337,\"standard\":\"erc721\",\"min_balance\":1}" >/dev/null \
    || f "FI00: could not create the ERC-721 gating rule"
  if [ -n "$ERC1155_ADDR" ]; then
    api_post "/api/v1/content/${CONTENT_ID}/gating-rules" "$TOKEN_A" \
      "{\"contract_address\":\"${ERC1155_ADDR}\",\"chain_id\":31337,\"standard\":\"erc1155\",\"min_balance\":1}" >/dev/null \
      || f "FI00: could not create the ERC-1155 gating rule"
  fi
fi
RULES="$(sql_scalar "SELECT count(*) FROM content_gating_rules WHERE is_active AND chain_id=31337")"
if [ "$FI_CONTENT_OK" = 1 ] && [ "$RULES" = 2 ]; then
  p "two active gating rules on chain 31337 give the indexer a real contract whitelist"
  record FI00-whitelist preconditions PASS "active_rules=${RULES} (${DEMO_NFT}, ${ERC1155_ADDR})"
else
  f "the whitelist is ${RULES:-?} rule(s) where 2 were required (the indexer would not start)"
  record FI00-whitelist preconditions FAIL "active_rules=${RULES:-none}"
fi
# The whitelist is read once, at service start, so the indexer only exists after
# a restart. That restart IS the precondition this scenario needs.
container_kill "$MONO_CONTAINER"
container_start "$MONO_CONTAINER"
TOKEN_A=""
if wait_worker_up "$MONO_CONTAINER" 120; then
  INDEXER_LOG="$(docker logs --tail 300 "$MONO_CONTAINER" 2>&1 | grep -E "EventIndexer|WebSocket log subscriber|whitelist" | tail -3)"
  if printf '%s' "$INDEXER_LOG" | grep -q "EventIndexer + NFTEventHandler started" \
    && printf '%s' "$INDEXER_LOG" | grep -q "WebSocket log subscriber started"; then
    p "the restarted candidate really runs the event indexer on a live log subscription"
    record FI00 preconditions PASS "indexer + WS subscriber started after the whitelist appeared"
  else
    f "the indexer did not start on the real subscription after the restart"
    printf '%s\n' "$INDEXER_LOG" | sed 's/^/   /'
    record FI00 preconditions FAIL "no indexer/WS start line in the log"
  fi
else
  f "FI00: the monolith did not come back healthy after the whitelist restart"
  record FI00 preconditions FAIL "monolith unhealthy"
fi

# ---------------------------------------------------------------------------
# Scenario FI01 — a real ERC-721 Transfer reaches the handler and invalidates the
# gate's cached evidence. The event may only remove evidence, never add it.
# ---------------------------------------------------------------------------
log 'FI01 ERC-721 transfer through the real subscription'
barrier_reset
if detect_app_redis_db; then
  p "FI01: the candidate's Redis DB is ${APP_REDIS_DB}, discovered from its own rate-limit key"
else
  f "FI01: no candidate-written Redis key was found in DBs 0-15, so a planted cache key cannot be attributed"
fi
PLANTED_KEYS=(
  "$(gate_cache_key "$DEMO_NFT" 1 erc721 "$WALLET_A")"
  "$(gate_cache_key "$DEMO_NFT" 2 erc721 "$WALLET_A")"
  "$(gate_cache_key "$DEMO_NFT" '__collection__' erc721 "$WALLET_A")"
)
for key in "${PLANTED_KEYS[@]}"; do plant_gate_cache "$key"; done
if [ "$(gate_cache_alive "${PLANTED_KEYS[0]}")" = "1" ]; then
  p "FI01: the gate's own cache keys are planted in Redis, so invalidation is observable"
  record FI01-plant preconditions PASS "${#PLANTED_KEYS[@]} keys planted with the gate's key shape"
  TXH="$(erc721_mint)"
else
  TXH=""
  f "FI01: the planted gate-cache entry is not in Redis, so invalidation is unobservable"
  record FI01-plant preconditions FAIL "planted key missing from Redis"
fi
if [ -n "$TXH" ]; then
  # The stored row keeps the raw log shape (event_type stays "ContractEvent" —
  # classification to "Transfer" happens on dispatch, in memory), so the durable
  # evidence for "this is an ERC-721 Transfer" is the topic, not the type column.
  if wait_sql "SELECT count(*) FROM event_store_events WHERE tx_hash='${TXH}' AND reorged=FALSE AND payload->'topics'->>0='${TRANSFER_TOPIC}'" 1 40; then
    p "FI01: the real subscription stored the ERC-721 Transfer log"
    record FI01-indexed Transfer PASS "tx=${TXH:0:12} stored with the ERC-721 Transfer topic"
  else
    f "FI01: the ERC-721 Transfer was never indexed (tx=${TXH:0:12})"
    record FI01-indexed Transfer FAIL "no stored Transfer-topic event for the tx"
    docker logs --tail 80 "$MONO_CONTAINER" 2>&1 | grep -iE "indexer|event|transfer|subscription" | tail -8 | sed 's/^/   /'
  fi
  if wait_cache_gone 45; then
    p "FI01: the transfer event invalidated every cached evidence key for that contract"
    record FI01-cache Transfer PASS "all ${#PLANTED_KEYS[@]} planted gate keys deleted by the event path"
  else
    f "FI01: ${CACHE_ALIVE:-?} planted gate-cache key(s) survived the transfer event"
    record FI01-cache Transfer FAIL "${CACHE_ALIVE:-?} of ${#PLANTED_KEYS[@]} keys still present after 45s"
  fi
else
  f "FI01: no ERC-721 transfer could be produced"
  record FI01-indexed Transfer FAIL "mint produced no receipt"
fi

# ---------------------------------------------------------------------------
# Scenario FI02 — a real ERC-1155 TransferBatch: one event, several token ids.
# The batch must be parsed and every id's evidence dropped.
# ---------------------------------------------------------------------------
log 'FI02 ERC-1155 TransferBatch'
barrier_reset
if [ -n "$ERC1155_ADDR" ]; then
  # The two mints emit TransferSingle events, and those events invalidate the
  # same contract prefix. Before the keys under test are planted, a PROBE key for
  # the same prefix is planted and the two mints are run: the probe can only
  # disappear because the mint events really reached the invalidation path, which
  # is what proves the plant below cannot be credited to a late mint flush.
  PROBE_KEY="$(gate_cache_key "$ERC1155_ADDR" '__probe__' erc1155 "$WALLET_A")"
  plant_gate_cache "$PROBE_KEY"
  FI02_MINT1="$(erc1155_mint "$WALLET_A" 1 5)"
  FI02_MINT2="$(erc1155_mint "$WALLET_A" 2 7)"
  FI02_MINTS_OK=1
  for tx in "$FI02_MINT1" "$FI02_MINT2"; do
    if [ -z "$tx" ] || ! wait_sql "SELECT count(*) FROM event_store_events WHERE tx_hash='${tx}'" 1 60; then
      FI02_MINTS_OK=0
    fi
  done
  PLANTED_KEYS=("$PROBE_KEY")
  FI02_PROBE_GONE=1
  wait_cache_gone 60 || FI02_PROBE_GONE=0
  PLANTED_KEYS=(
    "$(gate_cache_key "$ERC1155_ADDR" 1 erc1155 "$WALLET_A")"
    "$(gate_cache_key "$ERC1155_ADDR" 2 erc1155 "$WALLET_A")"
  )
  for key in "${PLANTED_KEYS[@]}"; do plant_gate_cache "$key"; done
  if [ "$FI02_MINTS_OK" = 1 ] && [ "$FI02_PROBE_GONE" = 1 ] && [ "$(gate_cache_alive "${PLANTED_KEYS[0]}")" = "1" ]; then
    p "FI02: the mint events already flushed their invalidation, so only the batch can drop the keys planted now"
    record FI02-plant preconditions PASS "both mints indexed and their invalidation observed via the probe key; both per-id keys alive when the batch was sent"
  else
    f "FI02: mints_ok=${FI02_MINTS_OK} probe_deleted=${FI02_PROBE_GONE}, so the batch could not be credited with the invalidation"
    record FI02-plant preconditions FAIL "mints_ok=${FI02_MINTS_OK} probe_deleted=${FI02_PROBE_GONE} planted_key_alive=$(gate_cache_alive "${PLANTED_KEYS[0]}")"
  fi
  TXH="$(erc1155_batch)"
  if [ -n "$TXH" ]; then
    if wait_sql "SELECT count(*) FROM event_store_events WHERE tx_hash='${TXH}' AND reorged=FALSE AND payload->'topics'->>0='${TRANSFER_BATCH_TOPIC}'" 1 40; then
      # The ids of a batch are ABI-encoded in the log DATA (ids and values are not
      # indexed), so the durable row itself is decoded here: word 0 and word 1 are
      # the offsets of the two arrays, then length + elements each.
      IDS="$(sql_scalar "SELECT payload->>'data' FROM event_store_events WHERE tx_hash='${TXH}' AND reorged=FALSE LIMIT 1" \
        | python3 -c "
import sys
data = sys.stdin.read().strip()
if data.startswith('0x'):
    data = data[2:]
try:
    raw = bytes.fromhex(data)
    words = [raw[i*32:(i+1)*32] for i in range(len(raw)//32)]
    o1 = int.from_bytes(words[0], 'big') // 32
    o2 = int.from_bytes(words[1], 'big') // 32
    ids = [int.from_bytes(words[o1+1+i], 'big') for i in range(int.from_bytes(words[o1], 'big'))]
    vals = [int.from_bytes(words[o2+1+i], 'big') for i in range(int.from_bytes(words[o2], 'big'))]
    print(','.join(str(i) for i in ids) + '|' + ','.join(str(v) for v in vals))
except Exception:
    print('')
" 2>/dev/null)"
      if [ "$IDS" = "1,2|1,3" ]; then
        p "FI02: the batch event was stored and both ids/values decoded out of its log data"
        record FI02-batch TransferBatch PASS "tx=${TXH:0:12} ids=1,2 values=1,3 decoded from the TransferBatch log data"
      else
        f "FI02: the batch event landed but its log data decoded as '${IDS}' where '1,2|1,3' was required"
        record FI02-batch TransferBatch FAIL "decoded ids|values='${IDS}'"
      fi
    else
      f "FI02: the TransferBatch was never indexed (tx=${TXH:0:12})"
      record FI02-batch TransferBatch FAIL "no stored TransferBatch-topic event for the tx"
    fi
    if wait_cache_gone 45; then
      p "FI02: the batch invalidated the cached evidence of every id it carried"
      record FI02-cache TransferBatch PASS "both per-id keys deleted"
    else
      f "FI02: ${CACHE_ALIVE:-?} per-id key(s) survived the batch event"
      record FI02-cache TransferBatch FAIL "${CACHE_ALIVE:-?} of ${#PLANTED_KEYS[@]} keys still present"
    fi
  else
    f "FI02: the batch transaction was not accepted by the node"
    record FI02-batch TransferBatch FAIL "safeBatchTransferFrom produced no receipt"
  fi
else
  f "FI02: no ERC-1155 contract is available"
  record FI02-batch TransferBatch FAIL "DemoERC1155 unavailable"
fi

# ---------------------------------------------------------------------------
# Scenario FI03 — GAP FILL: the subscription is dead while the chain keeps
# producing logs. Nothing may be lost, and the durable checkpoint is what the
# restarted indexer catches up from.
# ---------------------------------------------------------------------------
log 'FI03 gap fill after the subscription was down'
barrier_reset
C_BEFORE="$(event_cursor)"; C_BEFORE="${C_BEFORE:-0}"
container_kill "$MONO_CONTAINER"
TOKEN_A=""
GAP_TX_1="$(erc721_mint)"
# The two missed logs have to land in DIFFERENT blocks, which is an observable
# state: wait for the chain to produce the next block instead of sleeping a guess.
GAP_SEP="$(head_number)"
wait_chain_after "$GAP_SEP" 30 \
  || f "FI03: the chain did not produce a second block to separate the two missed logs"
GAP_TX_2="$(erc721_mint)"
GAP_MAX="$(head_number)"
wait_chain_after "$((GAP_MAX + 13))" 120 \
  || f "FI03: the chain did not advance enough for the missed logs to become indexable"
container_start "$MONO_CONTAINER"
if wait_worker_up "$MONO_CONTAINER" 120; then
  LOST=0
  for tx in "$GAP_TX_1" "$GAP_TX_2"; do
    if wait_sql "SELECT count(*) FROM event_store_events WHERE tx_hash='${tx}' AND reorged=FALSE" 1 90; then
      p "FI03: the missed log ${tx:0:12} was recovered from the durable checkpoint"
    else
      f "FI03: the log ${tx:0:12} emitted while the subscription was down was lost"
      LOST=$((LOST + 1))
    fi
  done
  C_AFTER="$(event_cursor)"; C_AFTER="${C_AFTER:-0}"
  if [ "$LOST" = 0 ]; then
    record FI03 gap-fill PASS "2 logs emitted with the subscriber dead were all indexed after the restart (cursor ${C_BEFORE} -> ${C_AFTER})"
  else
    record FI03 gap-fill FAIL "${LOST} of 2 missed logs were never indexed"
  fi
  # The catch-up must be exhaustive and duplicate-free: the stored rows over the
  # gap have to equal what the chain itself says happened.
  CHAIN_LOGS="$(cast logs --json --rpc-url "$ANVIL_URL" --address "$DEMO_NFT" --from-block "$((C_BEFORE + 1))" --to-block "$C_AFTER" 2>/dev/null \
    | python3 -c "import sys,json;print(len(json.load(sys.stdin)))" 2>/dev/null)"
  STORED_ROWS="$(sql_scalar "SELECT count(*) FROM event_store_events WHERE contract_address ILIKE '${DEMO_NFT}' AND block_number > ${C_BEFORE} AND block_number <= ${C_AFTER} AND reorged=FALSE")"
  if [ -n "$CHAIN_LOGS" ] && [ "$CHAIN_LOGS" = "$STORED_ROWS" ]; then
    p "FI03: the gap produced exactly the chain's own number of logs (${CHAIN_LOGS})"
    record FI03-exact gap-fill PASS "chain logs=${CHAIN_LOGS} stored=${STORED_ROWS} over blocks $((C_BEFORE + 1))..${C_AFTER}"
  else
    f "FI03: chain logs=${CHAIN_LOGS:-?} stored=${STORED_ROWS:-?} over the gap"
    record FI03-exact gap-fill FAIL "stored rows do not match the chain's own logs"
  fi
else
  f "FI03: the monolith did not come back healthy"
  record FI03 gap-fill FAIL "monolith unhealthy after restart"
fi

# ---------------------------------------------------------------------------
# Scenario FI04 — the batch is decoded and about to become the durable cursor
# when the process dies. The cursor must NOT advance, so the same blocks are
# replayed on the next start instead of being skipped.
# ---------------------------------------------------------------------------
log 'FI04 kill before the checkpoint commit'
barrier_reset
arm indexer.checkpoint_before_commit
FI04_TX="$(erc721_mint)"
FI04_MAX="$(head_number)"
wait_chain_after "$((FI04_MAX + 13))" 120 \
  || f "FI04: the chain did not advance enough for the new log to become indexable"
if wait_reached indexer.checkpoint_before_commit 90; then
  C_BEFORE="$(event_cursor)"; C_BEFORE="${C_BEFORE:-0}"
  ROWS_BEFORE="$(sql_scalar "SELECT count(*) FROM event_store_events WHERE tx_hash='${FI04_TX}'")"
  FI04_BLOCK="$(receipt_field "$FI04_TX" blockNumber | tr -d '\r')"
  FI04_BLOCK="$(hexdec "$FI04_BLOCK" 2>/dev/null)" || FI04_BLOCK=""
  container_kill "$MONO_CONTAINER"
  unarm indexer.checkpoint_before_commit
  barrier_reset
  container_start "$MONO_CONTAINER"
  TOKEN_A=""
  if wait_worker_up "$MONO_CONTAINER" 120; then
    # A row existing at the park proves nothing: the live subscription path also
    # writes rows and never moves the batch cursor. The claim under test is that
    # the CRASHED BATCH (the one whose cursor commit was interrupted) is replayed
    # without duplicating and the durable cursor then advances — so the cursor is
    # the thing that has to be OBSERVED moving, bounded, never assumed.
    ROW_WAIT=0
    C_AFTER="${C_BEFORE:-0}"
    if wait_sql "SELECT count(*) FROM event_store_events WHERE tx_hash='${FI04_TX}' AND reorged=FALSE" 1 120; then
      ROW_WAIT=1
      CUR_DEADLINE=$((SECONDS + 90))
      while [ "$SECONDS" -lt "$CUR_DEADLINE" ] && [ "${C_AFTER:-0}" -le "${C_BEFORE:-0}" ]; do
        sleep 1
        C_AFTER="$(event_cursor)"; C_AFTER="${C_AFTER:-0}"
      done
    fi
    ROWS="$(sql_scalar "SELECT count(*) FROM event_store_events WHERE tx_hash='${FI04_TX}'")"
    if [ -z "$FI04_BLOCK" ] || [ "${C_BEFORE:-0}" -ge "${FI04_BLOCK:-0}" ]; then
      f "FI04: the cursor was already at/after the log's block ${FI04_BLOCK:-?} when parked, so the kill landed after the commit and proves nothing"
      record FI04 checkpoint-before-commit FAIL "cursor ${C_BEFORE:-?} not behind block ${FI04_BLOCK:-?} at the park"
    elif [ "$ROW_WAIT" = "1" ] && [ "$ROWS" = "1" ] && [ "${C_AFTER:-0}" -gt "${C_BEFORE:-0}" ]; then
      p "FI04: the crashed batch was replayed without duplicating and the cursor advanced past it"
      record FI04 checkpoint-before-commit PASS "row count=1 (no duplicate; ${ROWS_BEFORE} written by the live subscription before the kill), cursor ${C_BEFORE} -> ${C_AFTER}, log block ${FI04_BLOCK}"
    elif [ "$ROW_WAIT" = "0" ]; then
      f "FI04: the log from the crashed batch was never indexed after the restart"
      record FI04 checkpoint-before-commit FAIL "no row for ${FI04_TX:0:12}"
      docker logs --tail 80 "$MONO_CONTAINER" 2>&1 | grep -iE "indexer|checkpoint|batch" | tail -8 | sed 's/^/   /'
    else
      f "FI04: rows=${ROWS} cursor ${C_BEFORE} -> ${C_AFTER} where exactly one row and a forward cursor were required"
      record FI04 checkpoint-before-commit FAIL "rows=${ROWS} cursor=${C_AFTER} (block ${FI04_BLOCK:-?})"
    fi
  else
    f "FI04: the monolith did not come back healthy"
    record FI04 checkpoint-before-commit FAIL "monolith unhealthy after restart"
  fi
else
  f "FI04: the pre-commit checkpoint point never fired"
  record FI04 checkpoint-before-commit FAIL "not reached within 90s"
fi

# ---------------------------------------------------------------------------
# Scenario FI05 — a REORG inside the journal range. Only the range indexed in the
# SAME pass is ever re-verified, so the chain is un-mined while the pass is
# parked between the cursor commit and the canonical check: the orphaned event
# must be flagged reorged, the journal marked orphaned, and the cursor rolled
# back to the common ancestor — never left pointing at a block off the chain.
# ---------------------------------------------------------------------------
log 'FI05 reorg inside the freshly indexed range'
barrier_reset
C_BEFORE="$(event_cursor)"; C_BEFORE="${C_BEFORE:-0}"
# Take the subscription down and keep writing: the restart below re-indexes a
# multi-block range in ONE pass, so the range under test is wide enough to hold
# an orphanable event and the journal can still reach the common ancestor.
container_kill "$MONO_CONTAINER"
TOKEN_A=""
FI05_SEP=0
for _ in 1 2 3 4; do
  # Every mint needs its own block, hence its own journal entry — an observable
  # state, so the chain is waited on rather than slept on.
  if [ "$FI05_SEP" != 0 ]; then
    wait_chain_after "$FI05_SEP" 30 \
      || f "FI05: the chain did not produce a block between the four mints"
  fi
  erc721_mint >/dev/null
  FI05_SEP="$(head_number)"
done
FI05_MAX="$(head_number)"
wait_chain_after "$((FI05_MAX + 14))" 120 \
  || f "FI05: the chain did not advance enough for the orphaned block to become indexable"
arm indexer.after_index_before_reorg_check
container_start "$MONO_CONTAINER"
if wait_worker_up "$MONO_CONTAINER" 120; then
  if wait_reached indexer.after_index_before_reorg_check 120; then
    C_AFTER="$(event_cursor)"; C_AFTER="${C_AFTER:-0}"
    BOOKED="$(sql_scalar "SELECT max(block_number) FROM event_store_events WHERE reorged=FALSE AND block_number > ${C_BEFORE} AND block_number <= ${C_AFTER}")"
    if [ -n "$BOOKED" ]; then
      BH_BEFORE="$(block_hash "$BOOKED")"
      HEAD="$(head_number)"
      # A reorg REPLACES a height, it does not erase it: anvil's rollback keeps
      # every block at or below the rewind point canonical and mines NEW blocks
      # above it. So the rewind must land BELOW the orphaned height (a few blocks
      # under it) and then the chain has to be allowed to mine replacements past
      # it — only then does height BOOKED exist with a different hash, which is
      # exactly the state a real reorg leaves behind and the only state the
      # detector can prove. Un-mining below BOOKED and checking immediately (the
      # previous shape of this scenario) asks for a header that does not exist yet
      # and turns the check into a silent skip.
      REWIND=$((BOOKED - 3))
      DEPTH=$((HEAD - REWIND))
      if [ "$DEPTH" -gt 64 ] || [ "$REWIND" -le 4 ]; then
        f "FI05: a ${DEPTH}-block rewind to ${REWIND} is outside the journal depth (64)"
        record FI05-reorg indexer.after_index_before_reorg_check FAIL "rewind depth=${DEPTH} unusable"
      elif anvil_rollback "$DEPTH" && wait_chain_after "$BOOKED" 120; then
        BH_AFTER="$(block_hash "$BOOKED")"
        if [ -n "$BH_AFTER" ] && [ "$BH_AFTER" != "$BH_BEFORE" ]; then
          p "FI05: reorged the chain below block ${BOOKED} and re-mined past it, replacing that height with a different block"
          record FI05-reorg indexer.after_index_before_reorg_check PASS "rewound ${DEPTH} blocks from head ${HEAD} to ${REWIND}, re-mined past ${BOOKED}: ${BH_BEFORE:0:14} -> ${BH_AFTER:0:14}"
        else
          f "FI05: block ${BOOKED} still carries hash ${BH_AFTER:0:14} after the rewind, so no reorg was produced"
          record FI05-reorg indexer.after_index_before_reorg_check FAIL "block ${BOOKED} hash unchanged"
        fi
      else
        f "FI05: could not orphan block ${BOOKED} (depth=${DEPTH} head=${HEAD})"
        record FI05-reorg indexer.after_index_before_reorg_check FAIL "rollback depth=${DEPTH} unusable"
      fi
    else
      f "FI05: the freshly indexed range ${C_BEFORE}..${C_AFTER} carried no event to orphan"
      record FI05-reorg indexer.after_index_before_reorg_check FAIL "no event block inside the indexed range"
    fi
    # The check runs after this release, over the SAME range: the orphaned event
    # is what has to trigger the journal rollback. The cursor rewind to the
    # common ancestor is a TRANSIENT state (the next pass re-indexes and moves
    # forward again), so it is sampled at high frequency instead of trusted to
    # still be there when the poll below finishes.
    release_point indexer.after_index_before_reorg_check
    unarm indexer.after_index_before_reorg_check
    REORG_ROWS=0
    MIN_CURSOR=""
    SAMPLED_CURSOR=""
    DEADLINE=$((SECONDS + 60))
    while [ "$SECONDS" -lt "$DEADLINE" ]; do
      REORG_ROWS="$(sql_scalar "SELECT count(*) FROM event_store_events WHERE block_hash='${BH_BEFORE:-}' AND reorged=TRUE")"
      REORG_ROWS="${REORG_ROWS:-0}"
      SAMPLED_CURSOR="$(event_cursor)"; SAMPLED_CURSOR="${SAMPLED_CURSOR:-0}"
      if [ -z "$MIN_CURSOR" ] || [ "$SAMPLED_CURSOR" -lt "$MIN_CURSOR" ]; then MIN_CURSOR="$SAMPLED_CURSOR"; fi
      if [ "$REORG_ROWS" -gt 0 ] && [ -n "$MIN_CURSOR" ] && [ "$MIN_CURSOR" -lt "${BOOKED:-0}" ]; then break; fi
      sleep 0.5
    done
    ORPHANS="$(sql_scalar "SELECT count(*) FROM event_block_journal WHERE orphaned=TRUE")"; ORPHANS="${ORPHANS:-0}"
    C_FINAL="$(event_cursor)"; C_FINAL="${C_FINAL:-0}"
    if [ "$REORG_ROWS" -gt 0 ] && [ "$ORPHANS" -gt 0 ] && [ -n "$MIN_CURSOR" ] && [ "$MIN_CURSOR" -lt "${BOOKED:-0}" ]; then
      p "FI05: the orphaned event was flagged, the journal marked orphaned, and the cursor rewound to the common ancestor"
      record FI05 indexer.after_index_before_reorg_check PASS "reorged rows=${REORG_ROWS}, journal orphaned=${ORPHANS}, cursor dipped ${C_AFTER} -> ${MIN_CURSOR} (orphaned block ${BOOKED}), now ${C_FINAL}"
    else
      f "FI05: reorged=${REORG_ROWS} journal_orphaned=${ORPHANS} min_cursor=${MIN_CURSOR:-?} (expected reorged>0, min cursor < ${BOOKED:-?})"
      record FI05 indexer.after_index_before_reorg_check FAIL "reorged=${REORG_ROWS} orphaned=${ORPHANS} min_cursor=${MIN_CURSOR:-?}"
      docker logs --tail 150 "$MONO_CONTAINER" 2>&1 | grep -iE "reorg|journal|ancestor|indexer" | tail -10 | sed 's/^/   /'
    fi
  else
    f "FI05: the post-index/pre-check point never fired after the restart"
    record FI05 indexer.after_index_before_reorg_check FAIL "not reached within 120s"
  fi
else
  f "FI05: the monolith did not come back healthy"
  record FI05 indexer.after_index_before_reorg_check FAIL "monolith unhealthy after restart"
fi
barrier_reset

barrier_reset
docker rm -f "$HELPER" >/dev/null 2>&1 || :

printf '\n===== durable fault acceptance =====\n'
printf 'scenarios: %d PASS, %d FAIL\n' "$PASS" "$FAIL"
printf 'checks: %s\n' "$CHECKS"

# Fail-closed scenario contract (section 0.2 D exit): a scenario that silently
# recorded nothing would otherwise "pass" by omission. Every id the fault matrix
# requires must appear in checks.tsv as PASS.
EXPECTED_IDS="F00-helper F00-barrierwrite F00 F00-minio F00-pre F00-barrier \
FW01 FW01-publish FW02-nopublish FW02 FW03 FW04 FW05-dlq FW05 FW06 \
FO01-parked FO01 FO02-parked FO02 \
FR00 FR00-chain FR00-proxy FR00-reorg FR01-park FR01-unsent FR01 FR02-onchain FR02 \
FR03-reorg FR03-detected FR03 FR04-park FR04 FR05-park FR05 \
FI00-ws FI00-erc1155 FI00-whitelist FI00 FI01-plant FI01-indexed FI01-cache \
FI02-plant FI02-batch FI02-cache FI03 FI03-exact FI04 FI05-reorg FI05"
MISSING=0
for id in $EXPECTED_IDS; do
  if ! awk -F'\t' -v id="$id" '$1==id && $3=="PASS"{found=1} END{exit !found}' "$CHECKS"; then
    printf 'FAIL: required scenario %s has no PASS record\n' "$id"
    MISSING=$((MISSING + 1))
  fi
done
if [ "$MISSING" = 0 ]; then
  printf 'scenario contract: every required scenario id has a PASS record (%s ids)\n' "$(printf '%s\n' $EXPECTED_IDS | wc -l | tr -d ' ')"
else
  printf 'scenario contract: %d required scenario id(s) recorded nothing or failed\n' "$MISSING"
  FAIL=$((FAIL + MISSING))
fi

[ "$FAIL" = 0 ] || exit 1
