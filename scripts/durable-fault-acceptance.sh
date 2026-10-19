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
    --transcoder-container) TC_CONTAINER="$2"; shift 2 ;;
    --transcoder-container-2) TC_CONTAINER_2="$2"; shift 2 ;;
    --anvil-url) ANVIL_URL="$2"; shift 2 ;;
    *) printf 'unknown argument: %s\n' "$1" >&2; exit 2 ;;
  esac
done
for required in PROJECT COMPOSE BASE_URL ARTIFACT_DIR HELPER_IMAGE CANDIDATE_SHA RUN_LABEL PG_CONTAINER MINIO_CONTAINER MONO_CONTAINER TC_CONTAINER; do
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
# eth_blockNumber returns a hex quantity; bash's printf handles the 0x prefix.
head_number() {
  local h
  h="$(cast rpc --rpc-url "$ANVIL_URL" eth_blockNumber 2>/dev/null)" || return 1
  [ -n "$h" ] || return 1
  printf '%d' "$h"
}
chain_nonce() {
  local n
  n="$(cast rpc --rpc-url "$ANVIL_URL" eth_getTransactionCount "$WALLET_A" latest 2>/dev/null)" || return 1
  [ -n "$n" ] || return 1
  printf '%d' "$n"
}
block_hash() {
  cast rpc --rpc-url "$ANVIL_URL" eth_getBlockByNumber "$(hexq "$1")" false 2>/dev/null \
    | python3 -c "import sys,json;print(json.load(sys.stdin).get('hash',''))" 2>/dev/null
}
receipt_field() {
  cast receipt --rpc-url "$ANVIL_URL" "$1" --json 2>/dev/null \
    | python3 -c "import sys,json;d=json.load(sys.stdin);print(d.get('$2','') or '')" 2>/dev/null
}
tx_mined() { [ -n "$(receipt_field "$1" blockHash)" ]; }
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
if docker logs "$MONO_CONTAINER" 2>&1 | grep -q "On-chain content registry enabled"; then
  p "the candidate really started with the durable on-chain registry enabled"
  record FR00 preconditions PASS "registry publisher enabled in the monolith"
else
  f "the monolith never logged the registry being enabled (no intent row can exist)"
  record FR00 preconditions FAIL "registry not enabled"
fi
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
if anvil_rollback 5 && [ "$(head_number)" -lt "$HEAD0" ]; then
  p "anvil_rollback really reorgs the chain (head ${HEAD0} -> $(head_number))"
  record FR00-reorg preconditions PASS "a 5-block rollback reduced the head below ${HEAD0}"
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
  if [ -n "$FR_NONCE" ] && [ "$FR_CHAINNONCE" = "$FR_NONCE" ] && ! tx_mined "$FR_TX"; then
    p "FR01: the node never saw the tx (chain nonce ${FR_CHAINNONCE} == the reserved nonce, no receipt)"
    record FR01-unsent publisher.before_broadcast PASS "chain nonce=${FR_CHAINNONCE} unused, no receipt"
  else
    f "FR01: chain nonce=${FR_CHAINNONCE:-?} reserved=${FR_NONCE:-?} receipt=$([ -n "$(receipt_field "$FR_TX" blockHash)" ] && printf present || printf absent)"
    record FR01-unsent publisher.before_broadcast FAIL "chain nonce=${FR_CHAINNONCE:-?} reserved=${FR_NONCE:-?}"
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
    # The publisher must notice and re-verify: the intent goes back through
    # 'submitted' and ends confirmed under the NEW history, with the same raw
    # transaction and no extra signature.
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
        && [ "$R_ATTEMPTS" = "$FR_ATTEMPTS" ] && [ "$R_BH" = "$(receipt_field "$FR_TX" blockHash)" ] && [ -n "$FR_BLOCK" ]; then
        p "FR04: the restart found the existing receipt and did not sign anything new"
        record FR04 publisher.after_send PASS "same tx=${FR_TX:0:12}, nonce=${FR_NONCE}, ${R_ATTEMPTS} signed attempt(s)"
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
FR03-reorg FR03 FR04-park FR04 FR05-park FR05"
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
