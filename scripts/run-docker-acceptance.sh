#!/bin/bash

set -euo pipefail

BASE_URL="${1:-http://localhost:18080}"
WALLET="${WALLET:-0x000000000000000000000000000000000000dEaD}"
CONTRACT="${CONTRACT:-0x8667b7bdf8f27e76200fa450bf48aa78bbbcc61f}"
CHAIN_ID="${CHAIN_ID:-11155111}"
GO_BIN="${GO_BIN:-/opt/homebrew/opt/go@1.24/bin/go}"
GOCACHE_DIR="${GOCACHE_DIR:-/tmp/streamgate-go-build-cache}"

section() {
    printf '\n== %s ==\n' "$1"
}

run_json() {
    local label="$1"
    shift
    section "$label"
    "$@"
    printf '\n'
}

run_json "health" curl -fsS -m 10 "$BASE_URL/health"
run_json "auth challenge" curl -fsS -m 15 -X POST "$BASE_URL/api/v1/auth/challenge" \
    -H "Content-Type: application/json" \
    -d "{\"wallet\":\"$WALLET\",\"chain_id\":$CHAIN_ID}"

run_json "nft verify" curl -fsS -m 20 -X POST "$BASE_URL/api/v1/nft/verify" \
    -H "Content-Type: application/json" \
    -d "{\"wallet\":\"$WALLET\",\"contract\":\"$CONTRACT\",\"chain_id\":$CHAIN_ID}"

run_json "rpc status" curl -fsS -m 10 "$BASE_URL/api/v1/web3/rpc-status"

section "streaming manifest without auth (expect 401)"
curl -sS -m 10 -i "$BASE_URL/api/v1/streaming/demo/manifest.m3u8?contract=$CONTRACT&chain_id=$CHAIN_ID" | sed -n '1,14p'
printf '\n'

section "transcode submit"
SUBMIT_RESPONSE="$(curl -fsS -m 15 -X POST "$BASE_URL/api/v1/transcode/submit" \
    -H "Content-Type: application/json" \
    -d '{"content_id":"demo-content","input_url":"https://example.com/input.mp4","profile":"720p","priority":5}')"
echo "$SUBMIT_RESPONSE"
printf '\n'

TASK_ID="$(printf '%s' "$SUBMIT_RESPONSE" | sed -n 's/.*"task_id":"\([^"]*\)".*/\1/p')"

if [ -n "$TASK_ID" ]; then
    run_json "transcode status" curl -fsS -m 10 "$BASE_URL/api/v1/transcode/status/$TASK_ID"
fi

run_json "transcode tasks" curl -fsS -m 10 "$BASE_URL/api/v1/transcode/tasks?content_id=demo-content&limit=20&offset=0"
run_json "transcode profiles" curl -fsS -m 10 "$BASE_URL/api/v1/transcode/profiles"
run_json "metrics" curl -fsS -m 10 "$BASE_URL/metrics"

section "playback token route checks"
unset GOROOT
export GOCACHE="$GOCACHE_DIR"
"$GO_BIN" test ./cmd/microservices/api-gateway \
    -run 'TestRegisterStreamingRoutes_SegmentRequiresPlaybackToken|TestRegisterStreamingRoutes_SegmentAcceptsPlaybackToken|TestRegisterStreamingRoutes_ManifestSuccess' \
    -v

echo
echo "Acceptance checks completed against $BASE_URL"
