#!/usr/bin/env bash
# StreamGate Full-Chain Acceptance Test
#
# Prerequisites: the docker-compose.fullchain.yml stack is running, plus `cast`
# (Foundry) and `ffmpeg` on the host.
# Usage: ./scripts/fullchain-acceptance.sh [BASE_URL]
#
# Design rules (see TRANSITION_PLAN.md SG-05):
#   * No self-signed JWTs. Every token comes from the service's own
#     challenge -> sign -> login flow, signed with a real private key.
#   * No placeholder media. The fixture is a real, decodable video generated
#     with fixed FFmpeg parameters, and its hash/probe/encoder version are
#     recorded so a run is reproducible.
#   * A missing tool is a FAILURE, never a skip.
set -euo pipefail

BASE_URL="${1:-http://localhost:18080}"
DEMO_URL="${BASE_URL}/demo/"
ANVIL_URL="${ANVIL_URL:-http://localhost:18545}"
CHAIN_ID=31337

# Anvil's deterministic accounts. A owns the NFT; B is a real logged-in wallet
# that owns nothing — the negative case must be a real wallet, not a fake token.
KEY_A="0xac0974bec39a17e36ba4a6b4d238ff944bacb478cbed5efcae784d7bf4f2ff80"
KEY_B="0x59c6995e998f97a5a0044966f0945389dc9e86dae88c7a8412f4603b6b78690d"

FAIL=0
PASS=0
p() { PASS=$((PASS + 1)); printf 'PASS: %s\n' "$*"; }
f() { FAIL=$((FAIL + 1)); printf 'FAIL: %s\n' "$*"; }

# json_get <json> <key> — reads a top-level key without assuming jq is present.
json_get() { printf '%s' "$1" | python3 -c "import sys,json;print(json.load(sys.stdin).get('$2',''))" 2>/dev/null || true; }

require_tool() {
    command -v "$1" >/dev/null 2>&1 || { printf 'FAIL: required tool %s not found in PATH (no skip by design)\n' "$1"; exit 1; }
}

require_tool curl
require_tool python3
require_tool cast
require_tool ffmpeg
require_tool ffprobe

WALLET_A="$(cast wallet address --private-key "$KEY_A")"
WALLET_B="$(cast wallet address --private-key "$KEY_B")"

printf '============================================\n'
printf 'StreamGate Full-Chain Acceptance Test\n'
printf 'Backend:  %s\n' "$BASE_URL"
printf 'Wallet A: %s (owner)\n' "$WALLET_A"
printf 'Wallet B: %s (no NFT)\n' "$WALLET_B"
printf '============================================\n\n'

# ---------------------------------------------------------------------------
# Real wallet login: challenge -> personal_sign the returned message -> login.
# The token is whatever the service issues; nothing here constructs a JWT.
# ---------------------------------------------------------------------------
login() {
    local key="$1" wallet="$2"
    local challenge cid msg sig resp token
    challenge="$(curl -sf -X POST "$BASE_URL/api/v1/auth/challenge" \
        -H 'Content-Type: application/json' \
        -d "{\"wallet\":\"$wallet\",\"chain_id\":$CHAIN_ID}")" || return 1
    cid="$(json_get "$challenge" challenge_id)"
    msg="$(json_get "$challenge" message)"
    [ -n "$cid" ] && [ -n "$msg" ] || return 1

    # EIP-191 personal_sign over the exact message the service returned.
    sig="$(cast wallet sign --private-key "$key" "$msg")" || return 1

    resp="$(curl -sf -X POST "$BASE_URL/api/v1/auth/login" \
        -H 'Content-Type: application/json' \
        -d "{\"wallet\":\"$wallet\",\"chain_id\":$CHAIN_ID,\"challenge_id\":\"$cid\",\"signature\":\"$sig\"}")" || return 1
    token="$(json_get "$resp" token)"
    [ -n "$token" ] || return 1
    printf '%s' "$token"
}

# ---------------------------------------------------------------------------
# 1. Infrastructure
# ---------------------------------------------------------------------------
printf '=== Infrastructure ===\n'

HEALTH="$(curl -sf "$BASE_URL/health" 2>/dev/null)" || { f '/health unreachable'; exit 1; }
STATUS="$(json_get "$HEALTH" status)"
[ "$STATUS" = "healthy" ] && p "Status=$STATUS" || f "Status=$STATUS"

docker exec sg-fc-postgres pg_isready -U postgres >/dev/null 2>&1 && p "PostgreSQL ready" || f "PostgreSQL not ready"
docker exec sg-fc-redis redis-cli ping >/dev/null 2>&1 && p "Redis ready" || f "Redis not ready"
curl -sf "http://localhost:19000/minio/health/live" >/dev/null 2>&1 && p "MinIO healthy" || f "MinIO unreachable"

ANVIL_ID="$(json_get "$(curl -sf -X POST "$ANVIL_URL" -H 'Content-Type: application/json' \
    -d '{"jsonrpc":"2.0","method":"eth_chainId","id":1}' 2>/dev/null)" result)"
[ "$ANVIL_ID" = "0x7a69" ] && p "Anvil chainId=31337" || f "Anvil chainId=${ANVIL_ID:-unreachable}"

# ---------------------------------------------------------------------------
# 2. Real signature authentication
# ---------------------------------------------------------------------------
printf '\n=== Authentication (real signatures) ===\n'

if TOKEN_A="$(login "$KEY_A" "$WALLET_A")"; then
    p "Wallet A challenge -> personal_sign -> login issued a token"
else
    f "Wallet A could not complete the signature login"; TOKEN_A=""
fi

if TOKEN_B="$(login "$KEY_B" "$WALLET_B")"; then
    p "Wallet B challenge -> personal_sign -> login issued a token"
else
    f "Wallet B could not complete the signature login"; TOKEN_B=""
fi

# The token must be the service's, not one we made up: it has to authenticate.
if [ -n "$TOKEN_A" ]; then
    CODE="$(curl -s -o /dev/null -w '%{http_code}' -H "Authorization: Bearer $TOKEN_A" "$BASE_URL/api/v1/auth/profile")"
    [ "$CODE" = "200" ] && p "Wallet A token authenticates (/auth/profile=200)" || f "Wallet A token rejected (/auth/profile=$CODE)"
else
    f "Skipped: no Wallet A token"
fi

# A forged token signed with a made-up secret must be rejected.
FORGED="$(python3 - <<'PY'
import base64, hmac, hashlib, json, time
def b64(d): return base64.urlsafe_b64encode(d).rstrip(b'=').decode()
now = int(time.time())
h = b64(json.dumps({'alg': 'HS256', 'typ': 'JWT'}).encode())
pl = b64(json.dumps({'sub': '0xdead', 'wallet_address': '0xdead', 'iss': 'streamgate',
                     'aud': 'streamgate-api', 'iat': now, 'exp': now + 3600}).encode())
sig = b64(hmac.new(b'not-the-real-secret', f'{h}.{pl}'.encode(), hashlib.sha256).digest())
print(f'{h}.{pl}.{sig}')
PY
)"
CODE="$(curl -s -o /dev/null -w '%{http_code}' -H "Authorization: Bearer $FORGED" "$BASE_URL/api/v1/auth/profile")"
[ "$CODE" = "401" ] && p "Forged HS256 token rejected (401)" || f "Forged token accepted (=$CODE)"

# ---------------------------------------------------------------------------
# 3. Real media fixture
# ---------------------------------------------------------------------------
printf '\n=== Media fixture (deterministic) ===\n'

WORKDIR="$(mktemp -d)"
trap 'rm -rf "$WORKDIR"' EXIT
MEDIA="$WORKDIR/sg-acceptance-src.mp4"

# Fixed parameters so the fixture is a real, decodable file rather than a
# placeholder: 10s of testsrc video (640x360@30) plus a 440 Hz sine track.
# The encoder is chosen from what THIS build actually has and is recorded, so a
# run states exactly how the input was produced.
FF_ENCODERS="$(ffmpeg -hide_banner -encoders 2>/dev/null)"
if grep -q 'h264_videotoolbox' <<<"$FF_ENCODERS"; then
    VENC="h264_videotoolbox"; VENC_ARGS=(-b:v 500k)
elif grep -q ' libx264' <<<"$FF_ENCODERS"; then
    VENC="libx264"; VENC_ARGS=(-preset veryfast -b:v 500k)
else
    VENC=""
fi

if [ -n "$VENC" ]; then
    ffmpeg -hide_banner -loglevel error -y \
        -f lavfi -i "testsrc=size=640x360:rate=30:duration=10" \
        -f lavfi -i "sine=frequency=440:sample_rate=44100:duration=10" \
        -c:v "$VENC" "${VENC_ARGS[@]}" -pix_fmt yuv420p \
        -c:a aac -b:a 96k -shortest \
        "$MEDIA"
else
    f "no H.264 encoder in this ffmpeg build (need h264_videotoolbox or libx264)"
fi

if [ -s "$MEDIA" ]; then
    MEDIA_SHA="$(shasum -a 256 "$MEDIA" | awk '{print $1}')"
    MEDIA_BYTES="$(wc -c < "$MEDIA" | tr -d ' ')"
    DURATION="$(ffprobe -v error -show_entries format=duration -of csv=p=0 "$MEDIA")"
    VCODEC="$(ffprobe -v error -select_streams v:0 -show_entries stream=codec_name -of csv=p=0 "$MEDIA")"
    RES="$(ffprobe -v error -select_streams v:0 -show_entries stream=width,height -of csv=p=0 "$MEDIA")"
    ACODEC="$(ffprobe -v error -select_streams a:0 -show_entries stream=codec_name -of csv=p=0 "$MEDIA")"
    FFMPEG_VERSION="$(ffmpeg -version | head -1)"
    p "Fixture ${MEDIA_BYTES}B sha256=${MEDIA_SHA:0:16}… ${VCODEC}/${ACODEC} ${RES} ${DURATION}s enc=$VENC"
    printf '      %s\n' "$FFMPEG_VERSION"

    # A fixture the encoder cannot decode is not a fixture.
    if ffmpeg -hide_banner -loglevel error -v error -i "$MEDIA" -f null - 2>"$WORKDIR/decode.err"; then
        p "Fixture fully decodes with FFmpeg"
    else
        f "Fixture does not decode: $(head -c 200 "$WORKDIR/decode.err")"
    fi
else
    f "ffmpeg produced no fixture"
fi

# ---------------------------------------------------------------------------
# 4. Upload the real media as Wallet A
# ---------------------------------------------------------------------------
printf '\n=== Upload (real media) ===\n'

UPLOAD_ID=""
CONTENT_ID=""
if [ -n "$TOKEN_A" ] && [ -s "$MEDIA" ]; then
    UPLOAD_RESP="$(curl -sf -X POST "$BASE_URL/api/v1/upload" \
        -H "Authorization: Bearer $TOKEN_A" \
        -F "file=@$MEDIA;filename=sg-acceptance.mp4")" || { f "Upload request failed"; UPLOAD_RESP="{}"; }
    UPLOAD_ID="$(json_get "$UPLOAD_RESP" upload_id)"
    UPLOAD_STATUS="$(json_get "$UPLOAD_RESP" status)"
    if [ -n "$UPLOAD_ID" ] && [ "$UPLOAD_STATUS" = "completed" ]; then
        p "Uploaded real media (upload_id=$UPLOAD_ID)"
    else
        f "Upload: ${UPLOAD_RESP:0:160}"
    fi
else
    f "Skipped: no Wallet A token or no fixture"
fi

if [ -n "$UPLOAD_ID" ]; then
    COMPLETE_RESP="$(curl -sf -X POST "$BASE_URL/api/v1/upload/$UPLOAD_ID/complete-upload" \
        -H "Authorization: Bearer $TOKEN_A")" || { f "complete-upload failed"; COMPLETE_RESP="{}"; }
    CONTENT_ID="$(json_get "$COMPLETE_RESP" content_id)"
    [ -n "$CONTENT_ID" ] && p "Content created (content_id=$CONTENT_ID)" || f "No content_id: ${COMPLETE_RESP:0:160}"
fi

# ---------------------------------------------------------------------------
# 5. Ownership and capability negatives
# ---------------------------------------------------------------------------
printf '\n=== Negatives ===\n'

CODE="$(curl -s -o /dev/null -w '%{http_code}' "$BASE_URL/api/v1/auth/profile")"
[ "$CODE" = "401" ] && p "Unauthenticated request rejected (401)" || f "Unauthenticated request returned $CODE"

if [ -n "$CONTENT_ID" ]; then
    CODE="$(curl -s -o /dev/null -w '%{http_code}' -H "Authorization: Bearer $TOKEN_B" \
        "$BASE_URL/api/v1/streaming/$CONTENT_ID/manifest.m3u8")"
    case "$CODE" in
        401|403) p "Wallet B (no NFT) refused the manifest ($CODE)" ;;
        503)     p "Wallet B refused: dependencies unverifiable ($CODE)" ;;
        *)       f "Wallet B got $CODE on a gated manifest — expected 401/403/503" ;;
    esac
    [ "$CODE" = "200" ] && f "Wallet B must never receive a gated manifest"

    # A wallet must not be able to delete someone else's content.
    CODE="$(curl -s -o /dev/null -w '%{http_code}' -X DELETE -H "Authorization: Bearer $TOKEN_B" \
        "$BASE_URL/api/v1/content/$CONTENT_ID")"
    [ "$CODE" = "403" ] || [ "$CODE" = "404" ] && p "Wallet B cannot delete Wallet A's content ($CODE)" \
        || f "Wallet B delete returned $CODE — expected 403/404"
fi

CODE="$(curl -s -o /dev/null -w '%{http_code}' "$DEMO_URL")"
[ "$CODE" = "200" ] && p "H5 demo served at $DEMO_URL" || f "H5 demo index=$CODE"

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
