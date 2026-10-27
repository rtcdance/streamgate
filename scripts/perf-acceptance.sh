#!/usr/bin/env bash
# Candidate media load runs in a separate 1 vCPU / 512 MiB tool container.
# The controller builds the harness once, prepares scenario faults at the
# post-warmup barrier, and retains failed runs and original process statuses.
set -euo pipefail
ROOT_DIR="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT_DIR"
die() { printf 'perf-acceptance: FATAL: %s\n' "$*" >&2; exit 1; }
[ "${VERIFY_IN_CONTROLLER:-}" = 1 ] || die "run through the Docker verification controller"
for required in PERF_CONTENT_ID PERF_FIXTURE_HASH PERF_MEDIA_HASHES_FILE PERF_DOCKER_PROJECT PERF_DOCKER_NETWORK PERF_COMPOSE_FILE PERF_API_CONTAINER PERF_WORKER_CONTAINER PERF_LOADER_IMAGE PERF_LOADER_BINARY PERF_RUN_LABEL RESULT_DIR; do
  [ -n "${!required:-}" ] || die "$required is required"
done
[[ "$PERF_LOADER_IMAGE" =~ ^sha256:[0-9a-f]{64}$ ]] || die "loader image must be an immutable image ID"
[[ "$PERF_FIXTURE_HASH" =~ ^[0-9a-f]{64}$ ]] || die "fixture SHA256 is required"
[ -s "$PERF_MEDIA_HASHES_FILE" ] || die "independent fixture media hashes are missing"
BASE_URL="${BASE_URL:-http://monolith:8080}"
SCENARIO="${SCENARIO:-warm_cache}"
VIEWERS_LIST="${VIEWERS_LIST:-1 10 50 100}"
PERF_INTERVAL="${PERF_INTERVAL:-2s}"
PERF_WARMUP="${PERF_WARMUP:-10s}"
PERF_DURATION="${PERF_DURATION:-30s}"
PERF_LOGIN_PACE="${PERF_LOGIN_PACE:-1s}"
PERF_GO_TIMEOUT="${PERF_GO_TIMEOUT:-1800s}"
PERF_CHAIN_ID="${PERF_CHAIN_ID:-31337}"
PERF_QUALITY="${PERF_QUALITY:-640x360}"
[[ "$PERF_GO_TIMEOUT" =~ ^[1-9][0-9]*s$ ]] || die "timeout must be a positive number of seconds"
case "$SCENARIO" in warm_cache|cold_cache|rpc_primary_down|rpc_all_down) ;; *) die "unknown scenario" ;; esac
max_viewers=0
for viewers in $VIEWERS_LIST; do
  [[ "$viewers" =~ ^[1-9][0-9]*$ ]] || die "viewer counts must be positive integers"
  [ "$viewers" -le "$max_viewers" ] || max_viewers="$viewers"
done
mkdir -p "$RESULT_DIR"
[ -n "${PERF_WALLET_KEYS_FILE:-}" ] || die "independent viewer wallet keys file is required"
if [ ! -x "$PERF_LOADER_BINARY" ]; then
  CGO_ENABLED=0 go test -tags load -c -o "$PERF_LOADER_BINARY" ./test/load || die "loader compilation failed"
  python3 - "$PERF_LOADER_BINARY" "$PERF_LOADER_IMAGE" "$(git rev-parse HEAD)" <<'PY'
import hashlib, json, pathlib, sys
binary = pathlib.Path(sys.argv[1])
binary.with_suffix('.build.json').write_text(json.dumps({'harnessRevision': sys.argv[3], 'imageID': sys.argv[2], 'binarySHA256': hashlib.sha256(binary.read_bytes()).hexdigest()}, sort_keys=True) + '\n')
PY
fi
python3 - "$PERF_LOADER_BINARY" "$PERF_LOADER_IMAGE" "$(git rev-parse HEAD)" <<'PY'
import hashlib, json, pathlib, sys
binary = pathlib.Path(sys.argv[1])
expected = {'harnessRevision': sys.argv[3], 'imageID': sys.argv[2], 'binarySHA256': hashlib.sha256(binary.read_bytes()).hexdigest()}
if json.loads(binary.with_suffix('.build.json').read_text()) != expected:
    raise SystemExit('cached loader binary differs from source or tool identity')
PY

if [ "${PERF_SETUP:-0}" = 1 ]; then
  mkdir -p "$(dirname "$PERF_WALLET_KEYS_FILE")"
  setup_log="$RESULT_DIR/setup.raw.log"
  setup_rc=0
  PERF_BASE_URL="$BASE_URL" PERF_CHAIN_ID="$PERF_CHAIN_ID" PERF_SETUP=1 \
  PERF_LOGIN_PACE="$PERF_LOGIN_PACE" PERF_WALLETS="${PERF_WALLETS:-$max_viewers}" \
  PERF_WALLET_KEYS_FILE="$PERF_WALLET_KEYS_FILE" \
    "$PERF_LOADER_BINARY" -test.run '^TestPerfSetup_MintViewerWallets$' -test.v=test2json -test.timeout "$PERF_GO_TIMEOUT" >"$setup_log" 2>&1 || setup_rc=$?
  go tool test2json -p github.com/rtcdance/streamgate/test/load -t <"$setup_log" >"$RESULT_DIR/setup.jsonl" || die "setup log conversion failed"
  python3 scripts/cd/assert_required_tests.py "$RESULT_DIR/setup.jsonl" --suite perf-setup --exit-code "$setup_rc" || die "wallet setup failed"
fi
[ -s "$PERF_WALLET_KEYS_FILE" ] || die "viewer wallet pool is missing"
key_count="$(python3 -c 'import pathlib,sys; print(len(pathlib.Path(sys.argv[1]).read_text().split()))' "$PERF_WALLET_KEYS_FILE")"
[ "$key_count" -ge "$max_viewers" ] || die "wallet pool is smaller than the viewer count"

compose() { docker compose -p "$PERF_DOCKER_PROJECT" -f "$PERF_COMPOSE_FILE" "$@"; }
restart_playback_api() {
  compose restart --timeout 10 monolith >/dev/null || die "cannot restart the API to reset its playback/RPC state"
  python3 - "$BASE_URL/ready" <<'PY'
import sys, time, urllib.error, urllib.request
deadline = time.monotonic() + 60
while time.monotonic() < deadline:
    try:
        with urllib.request.urlopen(sys.argv[1], timeout=2) as response:
            if response.status == 200:
                break
    except (OSError, urllib.error.URLError):
        pass
    time.sleep(.2)
else:
    raise SystemExit('restarted API did not become ready')
PY
}
# wait_for_rpc_proxies — compose start returns once the container is running,
# not once socat forwards. The monolith's readiness probe does not exercise the
# chain, so without this barrier the loader can begin sampling against a proxy
# that has not connected upstream yet: the manifest's NFT check fails over to a
# dead endpoint and healthy requests come back 503 (observed in
# ci-all round=3 warm_cache: 18/538 errors, 96.65% success). Probe THROUGH the
# proxy names from the anvil container, exactly like verify-docker.sh does.
wait_for_rpc_proxies() {
  local name deadline
  for name in anvil-proxy anvil-fallback-proxy; do
    deadline=$((SECONDS + 60))
    until compose exec -T anvil cast chain-id --rpc-url "http://$name:8545" 2>/dev/null | grep -q '31337'; do
      [ "$SECONDS" -lt "$deadline" ] || die "$name never forwarded to the chain"
      sleep .2
    done
  done
}
capture_rpc_status() {
  python3 - "$BASE_URL/api/v1/web3/rpc-status" "$1" <<'PY'
import json, pathlib, sys, urllib.request
url, output = sys.argv[1], pathlib.Path(sys.argv[2])
with urllib.request.urlopen(url, timeout=10) as response:
    body = response.read(1024 * 1024 + 1)
data = json.loads(body)
chains = data.get("chains")
if not isinstance(chains, list) or not any(chain.get("chain_id") == 31337 and len(chain.get("rpcs", [])) == 2 for chain in chains):
    raise SystemExit("rpc-status does not expose the two chain-31337 endpoints")
output.write_bytes(body if body.endswith(b"\n") else body + b"\n")
PY
}
capture_proxy_evidence() {
  local phase="$1" name identity
  for name in anvil-proxy anvil-fallback-proxy; do
    identity="$(compose ps -aq "$name")" || die "cannot identify the $name container"
    [ -n "$identity" ] || die "no $name container in the perf project"
    docker inspect --format '{"containerID":"{{.Id}}","imageID":"{{.Image}}","State":{{json .State}}}' \
      "$identity" >"$phase/${name}.state.json" || die "cannot capture $name container state"
    docker logs --tail 500 "$identity" >"$phase/${name}.logs.txt" 2>&1 || die "cannot capture $name container logs"
  done
}
prepare_sampling() {
  local phase="$1" active keys
  active="$(compose exec -T postgres psql -U postgres -d streamgate -tAc "SELECT count(*) FROM transcoding_tasks WHERE status NOT IN ('completed','failed','cancelled')" | tr -d '[:space:]')" || die "cannot verify the worker is idle"
  [ "$active" = 0 ] || die "playback samples must not overlap transcoding"
  if [ "$SCENARIO" = rpc_primary_down ]; then
    # Restart at the barrier BEFORE the pre-fault snapshots and the injection:
    # setup-phase RPC transients (paced wallet logins saturating the node) can
    # migrate the active RPC slot to the fallback before the injected fault, so
    # the sampling window would then observe zero failovers although the primary
    # is down (observed in sg-all-1019-f round-1 @50: 6->6 flat). A process
    # started at the barrier holds fresh RPC state — primary active, zero
    # failover counter — so the injection deterministically produces the
    # failover the gate must observe inside the sampling window.
    restart_playback_api
    capture_rpc_status "$phase/before.rpc-status.json" || die "pre-fault RPC status snapshot failed"
  fi
  case "$SCENARIO" in
    rpc_primary_down) compose stop anvil-proxy >/dev/null || die "primary RPC fault failed" ;;
    rpc_all_down) compose stop anvil-proxy anvil-fallback-proxy >/dev/null || die "total RPC fault failed" ;;
  esac
  if [ "$SCENARIO" = cold_cache ]; then
    # Warmup sessions are Redis-backed. Restarting while the loader is at its
    # barrier clears every in-process cache without replaying any media read.
    restart_playback_api
  fi
  if [ "$SCENARIO" = cold_cache ] || [ "$SCENARIO" = rpc_all_down ]; then
    keys="$(compose exec -T redis redis-cli --scan --pattern 'nft_cache:*')" || die "NFT cache enumeration failed"
    while IFS= read -r key; do
      [ -z "$key" ] || compose exec -T redis redis-cli DEL "$key" >/dev/null || die "NFT cache invalidation failed"
    done <<< "$keys"
    # No viewer traffic is in flight at ready.json. A six-second fence exceeds
    # the verified five-second total cache age, including every in-process L1.
    python3 - "$phase/ready.json" <<'PY'
from datetime import datetime
import json, sys, time
ready = datetime.fromisoformat(json.load(open(sys.argv[1]))['timestamp'].replace('Z', '+00:00')).timestamp()
while time.time() < ready + 6:
    time.sleep(min(.1, max(0, ready + 6 - time.time())))
PY
  fi
  python3 scripts/cd/perf_metrics.py --output-prefix "$phase/before" || die "required before-metrics unavailable"
}

run_scenario() (
  local viewers="$1" loader="" monitor_pid="" attach_pid="" rc=0 start_rc=0
  local out="$RESULT_DIR/${SCENARIO}-viewers-${viewers}.json"
  local raw="$RESULT_DIR/${SCENARIO}-viewers-${viewers}.raw.log"
  local stats="$RESULT_DIR/cgroup-${SCENARIO}-${viewers}.jsonl"
  local phase
  phase="$(mktemp -d "$RESULT_DIR/phase-${SCENARIO}-${viewers}-XXXXXX")"
  cleanup_run() {
    local original=$? cleanup_rc=0
    trap - EXIT INT TERM
    : > "$phase/monitor.stop"
    if [ -n "$monitor_pid" ]; then wait "$monitor_pid" || cleanup_rc=1; fi
    if [ -n "$loader" ]; then docker rm -f "$loader" >/dev/null 2>&1 || cleanup_rc=1; fi
    if [ -n "$attach_pid" ]; then wait "$attach_pid" >/dev/null 2>&1 || :; fi
    if [ "$original" = 0 ] && [ "$cleanup_rc" != 0 ]; then original=1; fi
    exit "$original"
  }
  trap cleanup_run EXIT
  trap 'exit 130' INT
  trap 'exit 143' TERM
  compose start anvil-proxy anvil-fallback-proxy >/dev/null || die "cannot restore RPC endpoints before warmup"
  wait_for_rpc_proxies
  restart_playback_api
  loader="$(docker create --init --platform "${VERIFY_PLATFORM:-linux/amd64}" \
    --cpus 1 --memory 512m --network "$PERF_DOCKER_NETWORK" \
    --label "com.docker.compose.project=$PERF_DOCKER_PROJECT" --label "com.streamgate.verify.run=$PERF_RUN_LABEL" \
    --label "org.opencontainers.image.revision=${PERF_HARNESS_SHA:-$(git rev-parse HEAD)}" \
    --label "com.streamgate.perf.target-revision=${PERF_COMMIT:-$(git rev-parse HEAD)}" \
    -v "$PERF_LOADER_BINARY:/loader.test:ro" -v "$PERF_WALLET_KEYS_FILE:$PERF_WALLET_KEYS_FILE:ro" \
    -v "$PERF_MEDIA_HASHES_FILE:$PERF_MEDIA_HASHES_FILE:ro" -v "$RESULT_DIR:$RESULT_DIR" \
    -e GOMAXPROCS=1 -e PERF_BASE_URL="${PERF_LOADER_BASE_URL:-http://monolith:8080}" \
    -e PERF_CHAIN_ID="$PERF_CHAIN_ID" -e PERF_CONTENT_ID="$PERF_CONTENT_ID" -e PERF_QUALITY="$PERF_QUALITY" \
    -e PERF_WALLET_KEYS_FILE="$PERF_WALLET_KEYS_FILE" -e PERF_VIEWERS="$viewers" \
    -e PERF_INTERVAL="$PERF_INTERVAL" -e PERF_WARMUP="$PERF_WARMUP" -e PERF_DURATION="$PERF_DURATION" \
    -e PERF_LOGIN_PACE="$PERF_LOGIN_PACE" -e PERF_SCENARIO="$SCENARIO" -e PERF_RESULT_JSON="$out" \
    -e PERF_FIXTURE_HASH="$PERF_FIXTURE_HASH" -e PERF_MEDIA_HASHES_FILE="$PERF_MEDIA_HASHES_FILE" \
    -e PERF_HARNESS_SHA="${PERF_HARNESS_SHA:-$(git rev-parse HEAD)}" \
    -e PERF_COMMIT="${PERF_COMMIT:-$(git rev-parse HEAD)}" -e PERF_PHASE_DIR="$phase" \
    "$PERF_LOADER_IMAGE" /loader.test -test.run '^TestLoad_AuthorizedMedia$' -test.v=test2json -test.timeout "$PERF_GO_TIMEOUT")" || die "loader creation failed"
  docker start -a "$loader" >"$raw" 2>&1 &
  attach_pid=$!
  local deadline=$((SECONDS + ${PERF_GO_TIMEOUT%s}))
  while [ ! -f "$phase/ready.json" ]; do
    [ "$SECONDS" -lt "$deadline" ] || die "loader did not reach the sampling barrier before its deadline"
    if ! kill -0 "$attach_pid" 2>/dev/null; then break; fi
    sleep .2
  done
  if [ -f "$phase/ready.json" ]; then
    prepare_sampling "$phase"
    python3 scripts/cd/resource_monitor.py monitor --target "api=$PERF_API_CONTAINER" \
      --target "worker=$PERF_WORKER_CONTAINER" --target "loader=$loader" \
      --output "$stats" --stop-file "$phase/monitor.stop" --ready-file "$phase/resources.ready.json" \
      --timeout "${PERF_GO_TIMEOUT%s}" &
    monitor_pid=$!
    local monitor_deadline=$((SECONDS + 15))
    while [ ! -f "$phase/resources.ready.json" ]; do
      kill -0 "$monitor_pid" 2>/dev/null || die "resource collector stopped before becoming ready"
      [ "$SECONDS" -lt "$monitor_deadline" ] || die "resource collector did not observe every role before its deadline"
      sleep .1
    done
    : > "$phase/release"
  fi
  wait "$attach_pid" || start_rc=$?
  attach_pid=""
  rc="$(docker inspect --format '{{.State.ExitCode}}' "$loader")" || die "loader exit status is unavailable"
  if [ "$rc" = 0 ] && [ "$start_rc" != 0 ]; then rc="$start_rc"; fi
  python3 - "$out" "$rc" "$start_rc" <<'PY'
import json, pathlib, sys
pathlib.Path(sys.argv[1]).with_suffix('.execution.json').write_text(json.dumps({'exitCode': int(sys.argv[2]), 'attachExitCode': int(sys.argv[3]), 'testID': 'TestLoad_AuthorizedMedia'}, sort_keys=True) + '\n')
PY
  local metrics_rc=0
  python3 scripts/cd/perf_metrics.py --output-prefix "$phase/after" || metrics_rc=$?
  if [ "$SCENARIO" = rpc_primary_down ]; then
    capture_rpc_status "$phase/after.rpc-status.json" || die "post-run RPC status snapshot failed"
    python3 scripts/cd/perf_metrics.py --failover-delta "$phase" || die "failover counter evidence capture failed"
  fi
  capture_proxy_evidence "$phase"
  : > "$phase/monitor.stop"
  local monitor_rc=0
  if [ -n "$monitor_pid" ]; then wait "$monitor_pid" || monitor_rc=$?; else monitor_rc=1; fi
  monitor_pid=""
  go tool test2json -p github.com/rtcdance/streamgate/test/load -t <"$raw" >"$RESULT_DIR/${SCENARIO}-viewers-${viewers}.jsonl" || die "loader log conversion failed"
  python3 scripts/cd/assert_required_tests.py "$RESULT_DIR/${SCENARIO}-viewers-${viewers}.jsonl" --suite perf-live --exit-code "$rc" || die "live media scenario failed"
  [ "$monitor_rc" = 0 ] || die "resource monitor failed"
  [ "$metrics_rc" = 0 ] || die "required after-metrics unavailable"
  python3 scripts/cd/resource_monitor.py summarize --raw "$stats" --result "$out" \
    --output "$RESULT_DIR/resources-${SCENARIO}-${viewers}.json" || die "resource evidence is incomplete"
)

for viewers in $VIEWERS_LIST; do run_scenario "$viewers"; done
