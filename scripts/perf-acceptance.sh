#!/usr/bin/env bash
# StreamGate SG-07 perf acceptance runner (thin orchestration for test/load).
#
# Prerequisites: the fullchain stack is already running (make fullchain-deploy)
# and reachable at BASE_URL. This script does NOT start, stop or mutate the
# stack — it only drives the `load`-tagged HTTP harness against it and writes
# JSON results OUTSIDE the repo (/tmp by default).
#
# Required environment:
#   PERF_CONTENT_ID   gated content id prepared with real media (from the stack)
#   PERF_PRIVATE_KEY  hex secp256 private key of a REAL logged-in NFT-holding
#                     wallet (Anvil account key is fine on the local chain).
#                     No self-signed JWTs: the harness performs the real
#                     challenge -> sign -> login flow every run.
#
# Optional environment:
#   BASE_URL          stack root                      (default http://localhost:18080)
#   PERF_WALLET       wallet address                  (derived from the key if unset)
#   PERF_CHAIN_ID     login challenge chain id        (default 31337)
#   PERF_QUALITY      variant quality                 (unset = master manifest)
#   PERF_INTERVAL     per-viewer request pace         (default 2s, matches 6s EXTINF ×3)
#   PERF_WARMUP       warmup window per scenario      (default 10s)
#   PERF_DURATION     timed sampling window           (default 30s)
#   PERF_FIXTURE_HASH sha256 of the media fixture     (recorded in results)
#   PERF_SEGMENT_SHA256 hex digest of a segment; if set, every segment body is
#                     sha256-verified (mismatch = error, never success)
#   VIEWERS_LIST      viewer counts to run            (default "1 10 50")
#   CAPACITY          set to 1 to append 100 viewers as a capacity exploration
#   RESULT_DIR        where JSON results go           (default /tmp/streamgate-perf-<ts>)
#   SCENARIO          scenario label                  (default warm_cache)
#
# Scenario matrix (TRANSITION_PLAN.md SG-07): cold_cache / warm_cache /
# rpc_primary_down / rpc_all_down. This script RUNS the labeled scenario; it
# does not inject RPC faults itself. To run a fault scenario, inject the fault
# on the running stack first (e.g. stop the primary RPC container), then invoke
# this script with SCENARIO=rpc_primary_down — the harness records the label,
# and the safety assertions for fault scenarios live with the fault injection,
# not here. For cold cache, restart the stack (or its Redis/cache container)
# before invoking with SCENARIO=cold_cache. Never clean a user's cache in place.
#
# Usage:
#   PERF_CONTENT_ID=... PERF_PRIVATE_KEY=... ./scripts/perf-acceptance.sh
set -euo pipefail

BASE_URL="${BASE_URL:-http://localhost:18080}"
SCENARIO="${SCENARIO:-warm_cache}"
VIEWERS_LIST="${VIEWERS_LIST:-1 10 50}"
if [ "${CAPACITY:-0}" = "1" ]; then
  VIEWERS_LIST="$VIEWERS_LIST 100"
fi
PERF_INTERVAL="${PERF_INTERVAL:-2s}"
PERF_WARMUP="${PERF_WARMUP:-10s}"
PERF_DURATION="${PERF_DURATION:-30s}"
PERF_CHAIN_ID="${PERF_CHAIN_ID:-31337}"
RESULT_DIR="${RESULT_DIR:-/tmp/streamgate-perf-$(date +%Y%m%d-%H%M%S)}"

die() { echo "perf-acceptance: FATAL: $*" >&2; exit 1; }

[ -n "${PERF_CONTENT_ID:-}" ] || die "PERF_CONTENT_ID is required"
[ -n "${PERF_PRIVATE_KEY:-}" ] || die "PERF_PRIVATE_KEY is required (real wallet login; no self-signed tokens)"
command -v go >/dev/null 2>&1 || die "go toolchain not found"

# Fail loudly if the stack is unreachable: a green perf run needs a live target.
HEALTH_CODE=$(curl -s -o /dev/null -w '%{http_code}' --max-time 5 "$BASE_URL/health" || echo 000)
[ "$HEALTH_CODE" = "200" ] || die "$BASE_URL/health returned $HEALTH_CODE — stack not reachable (start it with 'make fullchain-deploy')"

mkdir -p "$RESULT_DIR"
cd "$(dirname "$0")/.."

run_scenario() {
  local viewers="$1"
  local out="$RESULT_DIR/${SCENARIO}-viewers-${viewers}.json"
  echo "perf-acceptance: scenario=$SCENARIO viewers=$viewers pace=$PERF_INTERVAL warmup=$PERF_WARMUP duration=$PERF_DURATION -> $out"
  PERF_BASE_URL="$BASE_URL" \
  PERF_SCENARIO="$SCENARIO" \
  PERF_PRIVATE_KEY="$PERF_PRIVATE_KEY" \
  PERF_WALLET="${PERF_WALLET:-}" \
  PERF_CHAIN_ID="$PERF_CHAIN_ID" \
  PERF_CONTENT_ID="$PERF_CONTENT_ID" \
  PERF_QUALITY="${PERF_QUALITY:-}" \
  PERF_VIEWERS="$viewers" \
  PERF_INTERVAL="$PERF_INTERVAL" \
  PERF_WARMUP="$PERF_WARMUP" \
  PERF_DURATION="$PERF_DURATION" \
  PERF_FIXTURE_HASH="${PERF_FIXTURE_HASH:-}" \
  PERF_SEGMENT_SHA256="${PERF_SEGMENT_SHA256:-}" \
  PERF_RESULT_JSON="$out" \
    go test -tags load -count=1 -run TestLoad_AuthorizedMedia ./test/load/ -v
}

echo "perf-acceptance: warming up the stack (1 viewer, short window)"
PERF_WARMUP=0s PERF_DURATION=5s run_scenario 1 >/dev/null 2>&1 \
  || die "warmup run failed — check stack health and PERF_CONTENT_ID/PERF_PRIVATE_KEY"

for v in $VIEWERS_LIST; do
  run_scenario "$v"
done

echo
echo "perf-acceptance: results in $RESULT_DIR"
echo "perf-acceptance: summary (real HTTP load; NOT the sleep-based scheduler simulation):"
if command -v python3 >/dev/null 2>&1; then
  python3 - "$RESULT_DIR" <<'PY'
import glob, json, os, sys
d = sys.argv[1]
rows = []
for f in sorted(glob.glob(os.path.join(d, "*.json"))):
    try:
        r = json.load(open(f))
    except Exception as e:
        rows.append((os.path.basename(f), "unreadable", "", "", "", "", "", ""))
        continue
    tot = r.get("success_count", 0) + r.get("error_count", 0)
    rate = 100.0 * r.get("success_count", 0) / tot if tot else 0.0
    rows.append((
        r.get("scenario", "?"), r.get("viewers", "?"), f"{rate:.2f}%",
        r.get("success_count", 0), r.get("error_count", 0),
        r.get("total_bytes", 0),
        f"{r.get('ttfb_ms', {}).get('p95', 0):.2f}",
        f"{r.get('latency_ms', {}).get('p95', 0):.2f}",
    ))
hdr = ("scenario", "viewers", "success%", "ok", "err", "bytes", "ttfb_p95_ms", "lat_p95_ms")
w = [max(len(str(x)) for x in col) for col in zip(hdr, *rows)] if rows else [len(h) for h in hdr]
print("  " + "  ".join(h.ljust(w[i]) for i, h in enumerate(hdr)))
for row in rows:
    print("  " + "  ".join(str(x).ljust(w[i]) for i, x in enumerate(row)))
PY
else
  ls -1 "$RESULT_DIR"
  echo "  (python3 not found; inspect the JSON files above manually)"
fi
