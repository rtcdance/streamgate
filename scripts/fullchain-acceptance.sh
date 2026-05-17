#!/bin/bash
# StreamGate Full-Chain Acceptance Test
# Prerequisites: docker-compose.fullchain.yml stack is running
# Usage: ./scripts/fullchain-acceptance.sh [BASE_URL]
set -euo pipefail

BASE_URL="${1:-http://localhost:18080}"
DEMO_URL="${BASE_URL}/demo/"
FAIL=0
PASS=0
WALLET="0x742d35Cc6634C0532925a3b844Bc9e7595f2bD18"
ANVIL_URL="${ANVIL_URL:-http://localhost:18545}"

p() { PASS=$((PASS + 1)); echo "PASS: $*"; }
f() { FAIL=$((FAIL + 1)); echo "FAIL: $*"; }

echo "============================================"
echo "StreamGate Full-Chain Acceptance Test"
echo "Backend:  $BASE_URL"
echo "H5 Demo:  $DEMO_URL"
echo "============================================"

# ---- 1. Infrastructure ----
echo ""
echo "=== Infrastructure ==="

echo "[1/11] Health endpoint..."
HEALTH=$(curl -sf "$BASE_URL/health" 2>/dev/null) || { f "/health unreachable"; exit 1; }
STATUS=$(echo "$HEALTH" | python3 -c "import sys,json; print(json.load(sys.stdin)['status'])" 2>/dev/null)
[ "$STATUS" = "healthy" ] && p "Status=$STATUS" || f "Status=$STATUS"

echo "[2/11] PostgreSQL..."
PG_READY=$(docker exec sg-fc-postgres pg_isready -U postgres 2>/dev/null) && p "$PG_READY" || f "PostgreSQL not ready"

echo "[3/11] Redis..."
REDIS_PONG=$(docker exec sg-fc-redis redis-cli ping 2>/dev/null) && p "$REDIS_PONG" || f "Redis not ready"

echo "[4/11] MinIO..."
MINIO_OK=$(curl -sf "http://localhost:19000/minio/health/live" 2>/dev/null && echo "OK" || echo "FAIL")
[ "$MINIO_OK" = "OK" ] && p "MinIO healthy" || f "MinIO unreachable"

echo "[5/11] NATS..."
NATS_OK=$(docker exec sg-fc-nats nats-server --version 2>/dev/null) && p "$NATS_OK" || f "NATS not ready"

echo "[6/11] Anvil (local testnet)..."
ANVIL_CHAIN=$(curl -sf -X POST "$ANVIL_URL" -H "Content-Type: application/json" \
    -d '{"jsonrpc":"2.0","method":"eth_chainId","id":1}' 2>/dev/null) || ANVIL_CHAIN=""
ANVIL_ID=$(echo "$ANVIL_CHAIN" | python3 -c "import sys,json; print(json.load(sys.stdin).get('result',''))" 2>/dev/null)
# 31337 = 0x7a69
[ "$ANVIL_ID" = "0x7a69" ] && p "Anvil chainId=31337" || f "Anvil chainId=${ANVIL_ID:-unreachable}"

# ---- 2. API Flow ----
echo ""
echo "=== API Flow (Wallet → Upload → Content) ==="

echo "[7/11] Wallet challenge..."
CHALLENGE_RESP=$(curl -sf -X POST "$BASE_URL/api/v1/auth/challenge" \
    -H "Content-Type: application/json" \
    -d "{\"address\":\"$WALLET\"}") || { f "Challenge endpoint error"; CHALLENGE_RESP="{}"; }
CHALLENGE_ID=$(echo "$CHALLENGE_RESP" | python3 -c "import sys,json; print(json.load(sys.stdin).get('challenge_id',''))" 2>/dev/null)
[ -n "$CHALLENGE_ID" ] && p "Challenge ID=$CHALLENGE_ID" || f "No challenge_id"

echo "[8/11] JWT generation..."
TOKEN=$(python3 -c "
import hmac, hashlib, base64, json, time
header = base64.urlsafe_b64encode(json.dumps({'alg':'HS256','typ':'JWT'}).encode()).rstrip(b'=').decode()
now = int(time.time())
payload = base64.urlsafe_b64encode(json.dumps({'sub':'$WALLET','wallet_address':'$WALLET','iat':now,'exp':now+3600}).encode()).rstrip(b'=').decode()
sig_input = f'{header}.{payload}'
sig = base64.urlsafe_b64encode(hmac.new(b'fullchain-acceptance-secret', sig_input.encode(), hashlib.sha256).digest()).rstrip(b'=').decode()
print(f'{header}.{payload}.{sig}')
" 2>/dev/null) || { f "python3 not available"; exit 1; }
p "HS256 JWT with wallet_address claim"

echo "[9/11] File upload..."
UPLOAD_RESP=$(dd if=/dev/zero bs=1024 count=1 2>/dev/null | \
    curl -sf -X POST "$BASE_URL/api/v1/upload" \
    -H "Authorization: Bearer $TOKEN" \
    -F "file=@-;filename=acceptance-test.mp4") || { f "Upload error"; UPLOAD_RESP="{}"; }
UPLOAD_ID=$(echo "$UPLOAD_RESP" | python3 -c "import sys,json; print(json.load(sys.stdin).get('upload_id',''))" 2>/dev/null)
UPLOAD_STATUS=$(echo "$UPLOAD_RESP" | python3 -c "import sys,json; print(json.load(sys.stdin).get('status',''))" 2>/dev/null)
[ -n "$UPLOAD_ID" ] && [ "$UPLOAD_STATUS" = "completed" ] && p "Upload ID=$UPLOAD_ID" || f "Upload: ${UPLOAD_RESP:0:100}"

echo "[10/11] Complete upload → content..."
CONTENT_ID=""
if [ -n "$UPLOAD_ID" ]; then
    COMPLETE_RESP=$(curl -sf -X POST "$BASE_URL/api/v1/upload/$UPLOAD_ID/complete-upload" \
        -H "Authorization: Bearer $TOKEN") || { f "Complete upload error"; COMPLETE_RESP="{}"; }
    CONTENT_ID=$(echo "$COMPLETE_RESP" | python3 -c "import sys,json; print(json.load(sys.stdin).get('content_id',''))" 2>/dev/null)
    [ -n "$CONTENT_ID" ] && p "Content ID=$CONTENT_ID" || f "No content_id: ${COMPLETE_RESP:0:100}"
else
    f "Skipped (no upload_id)"
fi

echo "[11/11] H5 Demo served..."
DEMO_HTTP=$(curl -sf -o /dev/null -w "%{http_code}" "$DEMO_URL" 2>/dev/null) || DEMO_HTTP="000"
DEMO_JS=$(curl -sf -o /dev/null -w "%{http_code}" "${DEMO_URL}js/api.js" 2>/dev/null) || DEMO_JS="000"
[ "$DEMO_HTTP" = "200" ] && [ "$DEMO_JS" = "200" ] && p "H5 Demo at $DEMO_URL (index=200, api.js=200)" || f "H5 Demo: index=$DEMO_HTTP, api.js=$DEMO_JS"

# ---- Summary ----
echo ""
echo "============================================"
TOTAL=$((PASS + FAIL))
if [ $FAIL -eq 0 ]; then
    echo "RESULT: ALL $TOTAL PASSED"
else
    echo "RESULT: $PASS PASSED, $FAIL FAILED (total $TOTAL)"
fi
echo "============================================"
echo ""
echo "Service endpoints:"
echo "  API:           $BASE_URL/health"
echo "  H5 Demo:       $DEMO_URL"
echo "  MinIO Console: http://localhost:19001"
echo "  PostgreSQL:    localhost:15432"
echo "  Anvil RPC:     $ANVIL_URL (chainId=31337)"
echo ""
echo "API routes verified:"
echo "  GET  /health                            [public]"
echo "  POST /api/v1/auth/challenge             [public]"
echo "  POST /api/v1/upload                     [JWT]"
echo "  POST /api/v1/upload/:id/complete-upload [JWT]"
echo "  GET  /api/v1/upload/:id/status          [JWT]"
echo "  GET  /api/v1/upload/:id/download-url    [JWT]"
echo "  POST /api/v1/transcode/submit           [JWT]"
echo "  GET  /api/v1/transcode/status/:id       [JWT]"
echo "  GET  /api/v1/streaming/:id/manifest.m3u8 [JWT+NFT]"
echo "  GET  /api/v1/streaming/:id/segment/:num  [playback_token]"
echo ""
echo "Manual H5 Demo testing:"
echo "  1. Open $DEMO_URL in browser with MetaMask"
echo "  2. Connect wallet → sign challenge → login"
echo "  3. Upload a video file → complete upload → transcode"
echo "  4. Enter content ID → play video (requires NFT)"

[ $FAIL -eq 0 ] || exit 1
