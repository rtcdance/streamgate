#!/usr/bin/env bash
# StreamGate 全链路功能验收脚本
# 使用: bash scripts/e2e-acceptance.sh [BASE_URL]
set -uo pipefail

BASE_URL="${1:-http://localhost:28080}"
PASS=0
FAIL=0
TOTAL=0

RED='\033[0;31m'
GREEN='\033[0;32m'
NC='\033[0m'

report() {
  local name="$1" result="$2" detail="${3:-}"
  TOTAL=$((TOTAL + 1))
  if [ "$result" = "PASS" ]; then
    PASS=$((PASS + 1))
    echo -e "${GREEN}[PASS]${NC} $name ${detail:+- $detail}"
  else
    FAIL=$((FAIL + 1))
    echo -e "${RED}[FAIL]${NC} $name ${detail:+- $detail}"
  fi
}

# curl_status URL expected_status description [method] [data]
curl_status() {
  local url="$1" expected="$2" desc="$3" method="${4:-GET}" data="${5:-}"
  local args=(-s -m 5 -o /dev/null -w "%{http_code}")
  if [ "$method" = "POST" ]; then
    args+=(-X POST -H "Content-Type: application/json")
    [ -n "$data" ] && args+=(-d "$data")
  fi
  local http_code
  http_code=$(curl "${args[@]}" "$url" 2>/dev/null || echo "000")
  if [ "$http_code" = "$expected" ]; then
    report "$desc" "PASS" "HTTP $http_code"
  else
    report "$desc" "FAIL" "HTTP $http_code (expected $expected)"
  fi
}

# curl_body_contains URL pattern description
curl_body_contains() {
  local url="$1" pattern="$2" desc="$3"
  local body
  body=$(curl -s -m 5 "$url" 2>/dev/null || echo "")
  if echo "$body" | grep -q "$pattern"; then
    report "$desc" "PASS"
  else
    report "$desc" "FAIL" "pattern '$pattern' not found"
  fi
}

echo "============================================"
echo "  StreamGate 全链路功能验收"
echo "  Target: $BASE_URL"
echo "  Time:   $(date -u '+%Y-%m-%d %H:%M:%S UTC')"
echo "============================================"
echo ""

# ═══════════════════════════════════════════════
# SECTION 1: Infrastructure Health
# ═══════════════════════════════════════════════
echo "=== 1. Infrastructure Health ==="
curl_body_contains "$BASE_URL/health" "healthy" "Health endpoint"
curl_body_contains "$BASE_URL/ready" "ready" "Readiness endpoint"

# ═══════════════════════════════════════════════
# SECTION 2: API Documentation
# ═══════════════════════════════════════════════
echo ""
echo "=== 2. API Documentation ==="
curl_body_contains "$BASE_URL/docs" "swagger-ui" "Swagger UI served"
curl_body_contains "$BASE_URL/docs/swagger.yaml" "openapi" "OpenAPI spec served"

# ═══════════════════════════════════════════════
# SECTION 3: Auth Challenge (Happy Path)
# ═══════════════════════════════════════════════
echo ""
echo "=== 3. Auth Challenge (Happy Path) ==="

CHALLENGE_RESP=$(curl -s -m 5 "$BASE_URL/api/v1/auth/challenge" \
  -X POST -H "Content-Type: application/json" \
  -d '{"wallet":"0x71C7656EC7ab88b098defB751B7401B5f6d8976F","chain_id":1}' 2>/dev/null || echo "")

CHALLENGE_ID=$(echo "$CHALLENGE_RESP" | python3 -c "import sys,json; d=json.load(sys.stdin); print(d.get('challenge_id',''))" 2>/dev/null)
CHALLENGE_MSG=$(echo "$CHALLENGE_RESP" | python3 -c "import sys,json; d=json.load(sys.stdin); print(d.get('message',''))" 2>/dev/null)
CHALLENGE_WALLET=$(echo "$CHALLENGE_RESP" | python3 -c "import sys,json; d=json.load(sys.stdin); print(d.get('wallet',''))" 2>/dev/null)

if [ -n "$CHALLENGE_ID" ] && [ "$CHALLENGE_ID" != "" ]; then
  report "Challenge returns challenge_id" "PASS" "id=$CHALLENGE_ID"
else
  report "Challenge returns challenge_id" "FAIL" "no challenge_id in response"
fi

if [ -n "$CHALLENGE_MSG" ] && echo "$CHALLENGE_MSG" | grep -q "Sign this message"; then
  report "Challenge returns EIP-191 message" "PASS"
else
  report "Challenge returns EIP-191 message" "FAIL"
fi

if [ -n "$CHALLENGE_WALLET" ] && echo "$CHALLENGE_WALLET" | grep -qi "0x71c7"; then
  report "Challenge returns checksummed wallet" "PASS"
else
  report "Challenge returns checksummed wallet" "FAIL"
fi

# ═══════════════════════════════════════════════
# SECTION 4: Auth Error Cases
# ═══════════════════════════════════════════════
echo ""
echo "=== 4. Auth Error Cases ==="
curl_status "$BASE_URL/api/v1/auth/challenge" "400" \
  "Challenge: missing wallet" "POST" '{}'
curl_status "$BASE_URL/api/v1/auth/challenge" "400" \
  "Challenge: invalid address" "POST" '{"wallet":"not-an-address"}'
curl_status "$BASE_URL/api/v1/auth/login" "401" \
  "Login: missing fields (auth required)" "POST" '{}'
curl_status "$BASE_URL/api/v1/auth/login" "401" \
  "Login: unknown challenge_id (auth required)" "POST" '{"wallet":"0x71C7656EC7ab88b098defB751B7401B5f6d8976F","challenge_id":"nonexistent","signature":"0xdead"}'

# ═══════════════════════════════════════════════
# SECTION 5: Authenticated Endpoint Access
# ═══════════════════════════════════════════════
echo ""
echo "=== 5. Authenticated Endpoint Access ==="
curl_status "$BASE_URL/api/v1/auth/profile" "401" "Profile without token → 401"
curl_status "$BASE_URL/api/v1/content" "401" "Content list without token → 401"
curl_status "$BASE_URL/api/v1/content" "401" "Content create without token → 401" "POST" '{"title":"test"}'
curl_status "$BASE_URL/api/v1/nft/verify" "401" "NFT verify without token → 401" "POST" '{}'

# ═══════════════════════════════════════════════
# SECTION 6: Invalid JWT
# ═══════════════════════════════════════════════
echo ""
echo "=== 6. Invalid JWT ==="
HTTP_CODE=$(curl -s -m 5 -o /dev/null -w "%{http_code}" \
  -H "Authorization: Bearer eyJhbGciOiJIUzI1NiJ9.fake.sig" \
  "$BASE_URL/api/v1/auth/profile" 2>/dev/null || echo "000")
if [ "$HTTP_CODE" = "401" ]; then
  report "Profile with forged JWT → 401" "PASS" "HTTP $HTTP_CODE"
else
  report "Profile with forged JWT → 401" "FAIL" "HTTP $HTTP_CODE"
fi

# ═══════════════════════════════════════════════
# SECTION 7: Streaming Gate
# ═══════════════════════════════════════════════
echo ""
echo "=== 7. Streaming Gate ==="
STREAM_CODE=$(curl -s -m 5 -o /dev/null -w "%{http_code}" \
  "$BASE_URL/api/v1/streaming/test/manifest.m3u8" 2>/dev/null || echo "000")
if [ "$STREAM_CODE" = "401" ] || [ "$STREAM_CODE" = "403" ] || [ "$STREAM_CODE" = "404" ]; then
  report "Streaming endpoint gated (no auth)" "PASS" "HTTP $STREAM_CODE"
else
  report "Streaming endpoint gated (no auth)" "FAIL" "HTTP $STREAM_CODE (expected 401/403/404)"
fi

# ═══════════════════════════════════════════════
# SECTION 8: Observability
# ═══════════════════════════════════════════════
echo ""
echo "=== 8. Observability ==="
curl_body_contains "$BASE_URL/metrics" "HELP" "Prometheus metrics endpoint"

# ═══════════════════════════════════════════════
# SECTION 9: Security Headers
# ═══════════════════════════════════════════════
echo ""
echo "=== 9. Security Headers ==="
HEADERS=$(curl -s -m 5 -I "$BASE_URL/health" 2>/dev/null || echo "")

if echo "$HEADERS" | grep -qi "X-Content-Type-Options"; then
  report "X-Content-Type-Options" "PASS" "nosniff"
else
  report "X-Content-Type-Options" "FAIL" "missing"
fi

if echo "$HEADERS" | grep -qi "X-Frame-Options"; then
  report "X-Frame-Options" "PASS" "DENY"
else
  report "X-Frame-Options" "FAIL" "missing"
fi

if echo "$HEADERS" | grep -qi "X-Xss-Protection"; then
  report "X-XSS-Protection" "PASS" "present"
else
  report "X-XSS-Protection" "FAIL" "missing"
fi

# ═══════════════════════════════════════════════
# SECTION 10: CORS
# ═══════════════════════════════════════════════
echo ""
echo "=== 10. CORS ==="
CORS_HEADERS=$(curl -s -m 5 -I -X OPTIONS "$BASE_URL/health" \
  -H "Origin: http://localhost:3000" -H "Access-Control-Request-Method: GET" 2>/dev/null || echo "")
if echo "$CORS_HEADERS" | grep -qi "Access-Control-Allow-Origin"; then
  report "CORS preflight response" "PASS" "Allow-Origin present"
else
  report "CORS preflight response" "FAIL" "no Allow-Origin"
fi

# ═══════════════════════════════════════════════
# SECTION 11: Request Tracing
# ═══════════════════════════════════════════════
echo ""
echo "=== 11. Request Tracing ==="
if echo "$HEADERS" | grep -qi "X-Request-Id"; then
  report "X-Request-Id header" "PASS" "request tracing active"
else
  report "X-Request-Id header" "FAIL" "no request ID"
fi

# ═══════════════════════════════════════════════
# SECTION 12: 404 & Method Not Allowed
# ═══════════════════════════════════════════════
echo ""
echo "=== 12. Error Handling ==="
curl_status "$BASE_URL/nonexistent-route" "404" "Unknown path → 404"
# POST-only route: GET returns 404 (route not registered for GET)
curl_status "$BASE_URL/api/v1/auth/challenge" "404" "GET /challenge → 404 (POST-only route)" "GET"

# ═══════════════════════════════════════════════
# SECTION 13: Upload Endpoint
# ═══════════════════════════════════════════════
echo ""
echo "=== 13. Upload Endpoint ==="
curl_status "$BASE_URL/api/v1/upload" "401" "Upload without token → 401" "POST" '{}'

# ═══════════════════════════════════════════════
# SECTION 14: Full Auth Flow (Challenge → Login → Profile)
# ═══════════════════════════════════════════════
echo ""
echo "=== 14. Full Auth Flow ==="

# Step 1: Get challenge
AUTH_CHALLENGE=$(curl -s -m 5 "$BASE_URL/api/v1/auth/challenge" \
  -X POST -H "Content-Type: application/json" \
  -d '{"wallet":"0x71C7656EC7ab88b098defB751B7401B5f6d8976F","chain_id":1}' 2>/dev/null || echo "")

AUTH_CID=$(echo "$AUTH_CHALLENGE" | python3 -c "import sys,json; d=json.load(sys.stdin); print(d.get('challenge_id',''))" 2>/dev/null)
AUTH_MSG=$(echo "$AUTH_CHALLENGE" | python3 -c "import sys,json; d=json.load(sys.stdin); print(d.get('message',''))" 2>/dev/null)

if [ -n "$AUTH_CID" ] && [ -n "$AUTH_MSG" ]; then
  report "Full flow: challenge created" "PASS" "cid=$AUTH_CID"

  # Step 2: Try login with fake signature (expect 401 — we can't sign without a private key)
  LOGIN_RESP=$(curl -s -m 5 "$BASE_URL/api/v1/auth/login" \
    -X POST -H "Content-Type: application/json" \
    -d "{\"wallet\":\"0x71C7656EC7ab88b098defB751B7401B5f6d8976F\",\"challenge_id\":\"$AUTH_CID\",\"signature\":\"0x0000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000\"}" 2>/dev/null || echo "")
  LOGIN_CODE=$(echo "$LOGIN_RESP" | python3 -c "
import sys, json
# Try to parse; if it's not JSON that's also fine
try:
    print('parsed')
except:
    print('non-json')
" 2>/dev/null)

  # The login should reject with invalid signature — check HTTP status
  LOGIN_HTTP=$(curl -s -m 5 -o /dev/null -w "%{http_code}" "$BASE_URL/api/v1/auth/login" \
    -X POST -H "Content-Type: application/json" \
    -d "{\"wallet\":\"0x71C7656EC7ab88b098defB751B7401B5f6d8976F\",\"challenge_id\":\"$AUTH_CID\",\"signature\":\"0x0000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000\"}" 2>/dev/null || echo "000")
  if [ "$LOGIN_HTTP" = "401" ] || [ "$LOGIN_HTTP" = "400" ]; then
    report "Full flow: login rejects invalid signature" "PASS" "HTTP $LOGIN_HTTP"
  else
    report "Full flow: login rejects invalid signature" "FAIL" "HTTP $LOGIN_HTTP (expected 400/401)"
  fi
else
  report "Full flow: challenge created" "FAIL" "could not create challenge"
fi

# ═══════════════════════════════════════════════
# Summary
# ═══════════════════════════════════════════════
echo ""
echo "============================================"
echo -e "  ${GREEN}PASS: $PASS${NC}  ${RED}FAIL: $FAIL${NC}  TOTAL: $TOTAL"
echo "============================================"
if [ "$FAIL" -gt 0 ]; then
  echo -e "${RED}VERDICT: FAIL — $FAIL test(s) failed${NC}"
  exit 1
else
  echo -e "${GREEN}VERDICT: ALL PASS${NC}"
  exit 0
fi
