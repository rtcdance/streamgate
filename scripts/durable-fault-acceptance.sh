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
PG_CONTAINER=""
MONO_CONTAINER=""
TC_CONTAINER=""
TC_CONTAINER_2=""
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
    --pg-container) PG_CONTAINER="$2"; shift 2 ;;
    --mono-container) MONO_CONTAINER="$2"; shift 2 ;;
    --transcoder-container) TC_CONTAINER="$2"; shift 2 ;;
    --transcoder-container-2) TC_CONTAINER_2="$2"; shift 2 ;;
    *) printf 'unknown argument: %s\n' "$1" >&2; exit 2 ;;
  esac
done
for required in PROJECT COMPOSE BASE_URL ARTIFACT_DIR HELPER_IMAGE CANDIDATE_SHA PG_CONTAINER MONO_CONTAINER TC_CONTAINER; do
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
    --label "com.streamgate.verify.run=${PROJECT}" \
    --label "com.docker.compose.project=${PROJECT}" \
    -v "${PROJECT}_fault-barrier:/b" \
    --entrypoint sh "$HELPER_IMAGE" -c 'while :; do sleep 3600; done' >/dev/null \
    || return 1
  local deadline=$((SECONDS + 30))
  while [ "$SECONDS" -lt "$deadline" ]; do
    docker exec "$HELPER" sh -c 'test -d /b' 2>/dev/null && return 0
    sleep 0.3
  done
  return 1
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
  local sql="$1" want="$2" window="$3" deadline=$((SECONDS + window)) got
  while [ "$SECONDS" -lt "$deadline" ]; do
    got="$(sql_scalar "$sql")"
    if [ "$got" != "$want" ]; then STABLELAST="$got"; return 1; fi
    sleep 1
  done
  return 0
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
  challenge="$(curl -sf -X POST "$BASE_URL/api/v1/auth/challenge" -H 'Content-Type: application/json' \
    -d "{\"wallet\":\"$wallet\",\"chain_id\":$CHAIN_ID}")" || return 1
  cid="$(json_get "$challenge" challenge_id)"
  msg="$(json_get "$challenge" message)"
  [ -n "$cid" ] && [ -n "$msg" ] || return 1
  sig="$(cast wallet sign --private-key "$key" "$msg")" || return 1
  resp="$(curl -sf -X POST "$BASE_URL/api/v1/auth/login" -H 'Content-Type: application/json' \
    -d "{\"wallet\":\"$wallet\",\"chain_id\":$CHAIN_ID,\"challenge_id\":\"$cid\",\"signature\":\"$sig\"}")" || return 1
  token="$(json_get "$resp" token)"
  [ -n "$token" ] || return 1
  printf '%s' "$token"
}

# seed_content <tag> — real login, real upload, real complete-upload. The
# completion transaction is what creates the task row and its enqueue intent, so
# everything downstream of it is the production path.
seed_content() {
  local tag="$1" token upload_resp upload_id complete_resp
  token="$(login "$KEY_A" "$WALLET_A")" || return 1
  upload_resp="$(curl -sf -X POST "$BASE_URL/api/v1/upload" -H "Authorization: Bearer $token" \
    -F "file=@${MEDIA};filename=fault-${tag}.mp4")" || return 1
  upload_id="$(json_get "$upload_resp" upload_id)"
  [ -n "$upload_id" ] || return 1
  complete_resp="$(curl -sf -X POST "$BASE_URL/api/v1/upload/${upload_id}/complete-upload" \
    -H "Authorization: Bearer $token")" || return 1
  CONTENT_ID="$(json_get "$complete_resp" content_id)"
  [ -n "$CONTENT_ID" ] || return 1
  return 0
}

task_row() { sql_scalar "SELECT id FROM transcoding_tasks WHERE content_id='${CONTENT_ID}' ORDER BY created_at DESC LIMIT 1"; }
field() { sql_scalar "SELECT ${2} FROM transcoding_tasks WHERE id='${1}'"; }
published_generation() { sql_scalar "SELECT COALESCE(published_generation,0) FROM contents WHERE id='${CONTENT_ID}'"; }
task_count() { sql_scalar "SELECT count(*) FROM transcoding_tasks WHERE content_id='${CONTENT_ID}'"; }

# ---------------------------------------------------------------------------
# 0. Preconditions: the durable profile really is in force, and the barrier
#    switch really is refused outside it.
# ---------------------------------------------------------------------------
log 'preconditions'
printf 'candidate=%s media_sha256=%s\n' "$CANDIDATE_SHA" "$MEDIA_SHA" | tee "${ARTIFACT_DIR}/fault-context.txt"

if curl -sf "$BASE_URL/health" >/dev/null 2>&1; then
  p "monolith answers /health at ${BASE_URL}"
  record F00 preconditions PASS "monolith healthy"
else
  f "monolith does not answer /health at ${BASE_URL}"
  record F00 preconditions FAIL "monolith unreachable"
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
wait_sql "SELECT status FROM transcoding_tasks WHERE content_id='${CONTENT_ID}'" completed 120 \
  && p "the seeded task completed on the real worker" \
  || { f "the seeded task did not complete (${SQLLAST:-no row})"; }
barrier_reset

# ---------------------------------------------------------------------------
# Scenario FW01 — kill between claim and execution.
# ---------------------------------------------------------------------------
log 'FW01 kill at worker.claimed'
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
  sql_exec "UPDATE transcoding_tasks SET input_url='http://minio:9000/streamgate/fault-absent-input.mp4' WHERE id='${TASK}'" \
    || f "FW05: could not destroy the task input"
  p "FW05: destroyed the input of task ${TASK} while it was parked before the first fetch"
  for attempt in 1 2; do
    release_point worker.claimed
    if wait_reached worker.claimed 60; then
      # still armed: the retry re-claims and parks again
      continue
    fi
    break
  done
  # Third claim: the attempt cap is spent, so this execution goes terminal.
  arm worker.dlq_before_ack
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
if seed_content fo01 && wait_reached outbox.before_publish 60; then
  TASK="$(task_row)"
  INTENT_STATUS="$(sql_scalar "SELECT status FROM outbox WHERE kind='task' AND id='${TASK}'")"
  CLAIMED="$(field "$TASK" lease_owner)"
  container_kill "$MONO_CONTAINER"
  unarm outbox.before_publish
  barrier_reset
  container_start "$MONO_CONTAINER"
  if [ "$INTENT_STATUS" = "pending" ] && [ -z "$CLAIMED" ]; then
    p "FO01: while the submitter was parked the intent was still pending and no worker had claimed the task"
    record FO01-parked outbox.before_publish PASS "outbox=pending, lease_owner empty"
  else
    f "FO01: outbox=${INTENT_STATUS:-none}, lease_owner=${CLAIMED:-empty}"
    record FO01-parked outbox.before_publish FAIL "outbox=${INTENT_STATUS:-none} lease=${CLAIMED:-empty}"
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
  f "FO01: the outbox point never fired"
  record FO01 outbox.before_publish FAIL "not reached within 60s"
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
if seed_content fo02 && wait_reached outbox.after_publish 60; then
  TASK="$(task_row)"
  INTENT_STATUS="$(sql_scalar "SELECT status FROM outbox WHERE kind='task' AND id='${TASK}'")"
  container_kill "$MONO_CONTAINER"
  unarm outbox.after_publish
  barrier_reset
  container_start "$MONO_CONTAINER"
  if [ "$INTENT_STATUS" = "pending" ]; then
    p "FO02: the broker had already accepted the publish while the intent was still pending"
    record FO02-parked outbox.after_publish PASS "outbox=pending immediately after PubAck"
  else
    f "FO02: outbox=${INTENT_STATUS:-none} where a pending intent was required"
    record FO02-parked outbox.after_publish FAIL "outbox=${INTENT_STATUS:-none}"
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
  f "FO02: the outbox point never fired"
  record FO02 outbox.after_publish FAIL "not reached within 60s"
fi

barrier_reset
docker rm -f "$HELPER" >/dev/null 2>&1 || :

printf '\n===== durable fault acceptance =====\n'
printf 'scenarios: %d PASS, %d FAIL\n' "$PASS" "$FAIL"
printf 'checks: %s\n' "$CHECKS"
[ "$FAIL" = 0 ] || exit 1
