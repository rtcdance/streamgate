#!/usr/bin/env bash
#
# StreamGate dependency-fault acceptance
#
# Covers the two SG-06 required cases that need a dependency to actually fail:
#
#   1. RPC total failure — a NEW manifest/renewal is refused, while a capability
#      already handed to a player keeps working on its own validity alone. The
#      segment path must not depend on the chain, and must still expire on time.
#   2. Redis / session-epoch failure — every protected resource is refused.
#
# This is DESTRUCTIVE: it stops containers. It is therefore a separate entry
# point rather than part of fullchain-acceptance.sh, which stays non-destructive
# and fast. Every dependency is restarted by a trap, so a failure mid-run cannot
# leave the stack down.
#
#   bash scripts/dependency-fault-acceptance.sh [backend-url]
#
set -euo pipefail

BASE_URL="${1:-http://localhost:18080}"
ANVIL_URL="${ANVIL_URL:-http://localhost:18545}"
CHAIN_ID=31337
DEMO_CONTRACT="${DEMO_CONTRACT:-0x5FbDB2315678afecb367f032d93F642f64180aa3}"

REDIS_CONTAINER="${REDIS_CONTAINER:-sg-fc-redis}"
ANVIL_CONTAINER="${ANVIL_CONTAINER:-sg-fc-anvil}"

KEY_A="${KEY_A:-0xac0974bec39a17e36ba4a6b4d238ff944bacb478cbed5efcae784d7bf4f2ff80}"
KEY_B="${KEY_B:-0x59c6995e998f97a5a0044966f0945389dc9e86dae88c7a8412f4603b6b78690d}"

PASS=0
FAIL=0
p() { printf 'PASS: %s\n' "$1"; PASS=$((PASS + 1)); }
f() { printf 'FAIL: %s\n' "$1"; FAIL=$((FAIL + 1)); }

json_get() { printf '%s' "$1" | python3 -c "import sys,json;print(json.load(sys.stdin).get('$2',''))" 2>/dev/null || true; }

for tool in docker cast curl ffmpeg ffprobe python3; do
    command -v "$tool" >/dev/null 2>&1 || { echo "missing required tool: $tool" >&2; exit 1; }
done

WALLET_A="$(cast wallet address --private-key "$KEY_A")"

# ---------------------------------------------------------------------------
# Always put the stack back. A run that leaves Redis or Anvil stopped would be
# worse than the failure it was trying to demonstrate.
#
# One handler only: two traps on EXIT would mean the second replaces the first,
# and the cleanup command in between would clobber the exit status.
# ---------------------------------------------------------------------------
WORKDIR=""
restore_all() {
    local rc=$?
    printf '\n--- restoring dependencies ---\n'
    [ -n "$WORKDIR" ] && rm -rf "$WORKDIR" 2>/dev/null
    docker start "$REDIS_CONTAINER" >/dev/null 2>&1 || true
    docker start "$ANVIL_CONTAINER" >/dev/null 2>&1 || true
    for _ in $(seq 1 30); do
        if curl -s -o /dev/null "http://localhost:18545" -X POST -H 'Content-Type: application/json' \
            -d '{"jsonrpc":"2.0","method":"eth_blockNumber","params":[],"id":1}' 2>/dev/null; then break; fi
        sleep 1
    done
    docker ps --format '{{.Names}} {{.Status}}' | grep -E "($REDIS_CONTAINER|$ANVIL_CONTAINER)" || true
    exit $rc
}
trap restore_all EXIT

login() {
    local key="$1" wallet="$2" challenge cid msg sig resp token
    challenge="$(curl -sf -X POST "$BASE_URL/api/v1/auth/challenge" \
        -H 'Content-Type: application/json' \
        -d "{\"wallet\":\"$wallet\",\"chain_id\":$CHAIN_ID}")" || return 1
    cid="$(json_get "$challenge" challenge_id)"
    msg="$(json_get "$challenge" message)"
    [ -n "$cid" ] && [ -n "$msg" ] || return 1
    sig="$(cast wallet sign --private-key "$key" "$msg")" || return 1
    resp="$(curl -sf -X POST "$BASE_URL/api/v1/auth/login" \
        -H 'Content-Type: application/json' \
        -d "{\"wallet\":\"$wallet\",\"chain_id\":$CHAIN_ID,\"challenge_id\":\"$cid\",\"signature\":\"$sig\"}")" || return 1
    token="$(json_get "$resp" token)"
    [ -n "$token" ] || return 1
    printf '%s' "$token"
}

# wait_http <url> <want> <deadline-seconds> [auth-header]
wait_http() {
    local url="$1" want="$2" deadline=$((SECONDS + $3)) code="" hdr="${4:-}"
    while [ "$SECONDS" -lt "$deadline" ]; do
        if [ -n "$hdr" ]; then
            code="$(curl -s -o /dev/null -w '%{http_code}' -H "$hdr" "$url" 2>/dev/null || echo 000)"
        else
            code="$(curl -s -o /dev/null -w '%{http_code}' "$url" 2>/dev/null || echo 000)"
        fi
        [ "$code" = "$want" ] && break
        sleep 1
    done
    printf '%s' "$code"
}

echo "============================================"
echo "StreamGate Dependency-Fault Acceptance"
echo "Backend: $BASE_URL"
echo "Redis:   $REDIS_CONTAINER   Anvil: $ANVIL_CONTAINER"
echo "============================================"

# ---------------------------------------------------------------------------
# 1. A gated, playable content with a capability already issued
# ---------------------------------------------------------------------------
printf '\n=== Setup (real media, real gate, real capability) ===\n'

WORKDIR="$(mktemp -d)"

curl -sf "$BASE_URL/health" >/dev/null || { echo "backend unreachable" >&2; exit 1; }
TOKEN_A="$(login "$KEY_A" "$WALLET_A")" || { echo "login failed" >&2; exit 1; }
p "Wallet A authenticated"

MEDIA="$WORKDIR/src.mp4"
ffmpeg -hide_banner -loglevel error -y \
    -f lavfi -i "testsrc=size=320x180:rate=15:duration=4" \
    -f lavfi -i "sine=frequency=440:sample_rate=44100:duration=4" \
    -c:v h264_videotoolbox -b:v 300k -pix_fmt yuv420p -c:a aac -b:a 64k -shortest "$MEDIA"

# Mint so the holder really holds, then gate the content on that holding.
curl -s -X POST "$BASE_URL/api/v1/nft/dev/mint" -H "Authorization: Bearer $TOKEN_A" \
    -H 'Content-Type: application/json' -d '{"count":1}' >/dev/null 2>&1 || true

UP="$(curl -s -X POST "$BASE_URL/api/v1/upload" -H "Authorization: Bearer $TOKEN_A" \
    -F "file=@$MEDIA;filename=dependency-fault.mp4")"
UPID="$(json_get "$UP" upload_id)"
[ -n "$UPID" ] || { echo "upload failed: ${UP:0:200}" >&2; exit 1; }
CONTENT_ID="$(json_get "$(curl -s -X POST "$BASE_URL/api/v1/upload/$UPID/complete-upload" \
    -H "Authorization: Bearer $TOKEN_A")" content_id)"
[ -n "$CONTENT_ID" ] || { echo "complete-upload produced no content" >&2; exit 1; }
p "Content created ($CONTENT_ID)"

curl -s -X POST "$BASE_URL/api/v1/content/$CONTENT_ID/gating-rules" -H "Authorization: Bearer $TOKEN_A" \
    -H 'Content-Type: application/json' \
    -d "{\"contract_address\":\"$DEMO_CONTRACT\",\"chain_id\":$CHAIN_ID,\"standard\":\"erc721\",\"min_balance\":1}" \
    >/dev/null 2>&1 || true

# The upload hook transcodes; wait until the content is actually playable.
MANIFEST_URL="$BASE_URL/api/v1/streaming/$CONTENT_ID/manifest.m3u8"
READY="$(wait_http "$MANIFEST_URL" 200 180 "Authorization: Bearer $TOKEN_A")"
if [ "$READY" = "200" ]; then
    p "Gated manifest served to the holder (200)"
else
    f "Gated manifest never became available (last=$READY)"
fi

MASTER="$(curl -s -H "Authorization: Bearer $TOKEN_A" "$MANIFEST_URL" 2>/dev/null || true)"
SEG_URI="$(printf '%s' "$MASTER" | grep -v '^#' | grep -v '^[[:space:]]*$' | head -1)"
SEG_URL=""
[ -n "$SEG_URI" ] && SEG_URL="$BASE_URL$SEG_URI"
if [ -n "$SEG_URL" ]; then
    CODE="$(curl -s -o /dev/null -w '%{http_code}' "$SEG_URL")"
    [ "$CODE" = "200" ] && p "Capability already issued and working (segment 200)" \
        || f "The issued capability did not work before the fault (segment $CODE)"
else
    f "Could not obtain a capability to test against"
fi

# ---------------------------------------------------------------------------
# 2. Redis / session-epoch failure: every protected resource is refused
# ---------------------------------------------------------------------------
printf '\n=== Redis failure ===\n'

docker stop "$REDIS_CONTAINER" >/dev/null 2>&1 || true
sleep 3

# A session that cannot be read POSITIVELY is not a session that is valid.
CODE="$(curl -s -o /dev/null -w '%{http_code}' -H "Authorization: Bearer $TOKEN_A" "$BASE_URL/api/v1/auth/profile")"
case "$CODE" in
    401|403|503) p "With Redis down, a protected profile read is refused ($CODE)" ;;
    *)           f "With Redis down, the profile returned $CODE — expected a refusal" ;;
esac

CODE="$(curl -s -o /dev/null -w '%{http_code}' -H "Authorization: Bearer $TOKEN_A" "$BASE_URL/api/v1/content")"
case "$CODE" in
    401|403|503) p "With Redis down, a protected content list is refused ($CODE)" ;;
    *)           f "With Redis down, the content list returned $CODE — expected a refusal" ;;
esac

CODE="$(curl -s -o /dev/null -w '%{http_code}' -H "Authorization: Bearer $TOKEN_A" "$MANIFEST_URL")"
case "$CODE" in
    401|403|503) p "With Redis down, the manifest is refused ($CODE)" ;;
    *)           f "With Redis down, the manifest returned $CODE — expected a refusal" ;;
esac

# The health endpoint must still answer: refusing protected resources is not the
# same as the process being down.
CODE="$(curl -s -o /dev/null -w '%{http_code}' "$BASE_URL/health")"
[ "$CODE" = "200" ] && p "Health still reports 200 while Redis is down" \
    || f "Health returned $CODE with Redis down — liveness must not depend on Redis"

docker start "$REDIS_CONTAINER" >/dev/null 2>&1 || true
sleep 4
CODE="$(wait_http "$BASE_URL/api/v1/auth/profile" 200 30 "Authorization: Bearer $TOKEN_A")"
[ "$CODE" = "200" ] && p "Protected reads recover once Redis is back (200)" \
    || f "Protected reads did not recover after Redis returned (last=$CODE)"

# ---------------------------------------------------------------------------
# 3. RPC total failure: no new authorization, but an issued capability stands
# ---------------------------------------------------------------------------
printf '\n=== RPC failure ===\n'

# Re-issue a capability right before the fault, so the TTL is measured from here.
MASTER="$(curl -s -H "Authorization: Bearer $TOKEN_A" "$MANIFEST_URL" 2>/dev/null || true)"
SEG_URI="$(printf '%s' "$MASTER" | grep -v '^#' | grep -v '^[[:space:]]*$' | head -1)"
SEG_URL=""
[ -n "$SEG_URI" ] && SEG_URL="$BASE_URL$SEG_URI"
[ -n "$SEG_URL" ] && p "Fresh capability issued immediately before the fault" \
    || f "Could not issue a capability before the fault"

docker stop "$ANVIL_CONTAINER" >/dev/null 2>&1 || true
sleep 3

# A renewal is a manifest request, which must re-read the chain.
CODE="$(curl -s -o /dev/null -w '%{http_code}' -H "Authorization: Bearer $TOKEN_A" "$MANIFEST_URL")"
case "$CODE" in
    503) p "With the RPC down, a new manifest is refused (503)" ;;
    *)   f "With the RPC down, the manifest returned $CODE — expected 503" ;;
esac

# The segment must not depend on the chain: it is authorized by the capability
# alone, so it keeps working on its own validity.
if [ -n "$SEG_URL" ]; then
    CODE="$(curl -s -o /dev/null -w '%{http_code}' "$SEG_URL")"
    case "$CODE" in
        200|206) p "An already-issued capability's segment still works with the RPC down ($CODE)" ;;
        *)       f "The segment returned $CODE with the RPC down — it must not need the chain" ;;
    esac

    # ...and it must still expire on schedule, which is what "only per its own
    # validity" means: the chain being unreachable cannot extend it.
    EXPIRED=0
    DEADLINE=$((SECONDS + 100))
    while [ "$SECONDS" -lt "$DEADLINE" ]; do
        CODE="$(curl -s -o /dev/null -w '%{http_code}' "$SEG_URL")"
        if [ "$CODE" = "401" ] || [ "$CODE" = "403" ]; then EXPIRED=1; break; fi
        sleep 5
    done
    if [ "$EXPIRED" = "1" ]; then
        p "The capability expires on its own schedule while the RPC is down"
    else
        f "The capability was still accepted 100s in with the RPC down — the chain outage extended it"
    fi
fi

docker start "$ANVIL_CONTAINER" >/dev/null 2>&1 || true

# Wait for the RPC to answer at all before asking it anything else.
RPC_BACK=0
for _ in $(seq 1 30); do
    if curl -s -o /dev/null -X POST "$ANVIL_URL" -H 'Content-Type: application/json' \
        -d '{"jsonrpc":"2.0","method":"eth_blockNumber","params":[],"id":1}' 2>/dev/null; then
        RPC_BACK=1
        break
    fi
    sleep 1
done
[ "$RPC_BACK" = "1" ] && p "The RPC answers again after the restart" \
    || f "The RPC never came back"

# Restarting a dev Anvil RESETS the chain, so the entrypoint redeploys the demo
# contract and mints to the holder from scratch. Wait for BOTH to have happened
# BEFORE advancing the chain.
#
# Waiting only for the contract code is not enough: the deploy lands first and
# the mint a moment later, so mining in between pushes the mint past the
# finality view. The gate then reads a block where the holder has nothing, and
# with no further traffic that view never catches up — a permanent 403 that
# looks like a broken gate and is not one.
#
# (On a real node an outage does not rewind history, so this is an artifact of
# restarting a development chain rather than something production needs.)
FUNDED=0
for _ in $(seq 1 60); do
    BAL="$(cast call --rpc-url "$ANVIL_URL" "$DEMO_CONTRACT" 'balanceOf(address)(uint256)' "$WALLET_A" 2>/dev/null | awk '{print $1}' || true)"
    if [ -n "$BAL" ] && [ "$BAL" -gt 0 ] 2>/dev/null; then
        FUNDED=1
        break
    fi
    sleep 1
done
[ "$FUNDED" = "1" ] && p "The demo contract is redeployed and the holder funded again (balance=$BAL)" \
    || f "The demo contract never came back funded after the restart"

# Now advance the finality view past the deployment and the mint.
cast rpc --rpc-url "$ANVIL_URL" anvil_mine 0x40 >/dev/null 2>&1 || true
sleep 1

CODE="$(wait_http "$MANIFEST_URL" 200 60 "Authorization: Bearer $TOKEN_A")"
if [ "$CODE" = "200" ]; then
    p "A new manifest is served once the RPC is back (200)"
else
    f "The manifest did not recover after the RPC returned (last=$CODE, latest=$(cast block-number --rpc-url "$ANVIL_URL" 2>/dev/null || echo ?), safe=$(cast block safe --rpc-url "$ANVIL_URL" --field number 2>/dev/null || echo ?))"
fi

# ---------------------------------------------------------------------------
# Summary
# ---------------------------------------------------------------------------
printf '\n============================================\n'
TOTAL=$((PASS + FAIL))
if [ "$FAIL" -eq 0 ]; then
    printf 'RESULT: ALL %d PASSED\n' "$TOTAL"
else
    printf 'RESULT: %d PASSED, %d FAILED (total %d)\n' "$PASS" "$FAIL" "$TOTAL"
fi
printf '============================================\n'

[ "$FAIL" -eq 0 ] || exit 1
