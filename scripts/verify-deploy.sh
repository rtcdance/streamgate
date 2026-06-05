#!/bin/bash
# StreamGate Deployment Health Check
# Runs ~6 curl checks against a running fullchain stack and reports pass/fail.
# Usage: ./scripts/verify-deploy.sh [monolith|microservices]

set -uo pipefail

MODE="${1:-monolith}"
PASS=0
FAIL=0
WARN=0

# Color codes
RED='\033[0;31m'
GRN='\033[0;32m'
YEL='\033[0;33m'
BLU='\033[0;34m'
NC='\033[0m'

ok()   { echo -e "  ${GRN}✓${NC} $1"; PASS=$((PASS+1)); }
bad()  { echo -e "  ${RED}✗${NC} $1"; FAIL=$((FAIL+1)); }
warn() { echo -e "  ${YEL}⚠${NC} $1"; WARN=$((WARN+1)); }
hdr()  { echo -e "\n${BLU}=== $1 ===${NC}"; }

case "$MODE" in
  monolith)      API="http://localhost:18080"; NGINX="http://localhost:18000" ;;
  microservices) API="http://localhost:28080"; NGINX="http://localhost:18001" ;;
  *)
    echo "Usage: $0 [monolith|microservices]"; exit 1 ;;
esac

hdr "0. Docker daemon"
if docker info >/dev/null 2>&1; then
  ok "Docker daemon responding"
else
  bad "Docker daemon not reachable — start Docker Desktop first"
  exit 1
fi

hdr "1. Container health (sg-fc-* = sg fullchain)"
TOTAL=$(docker ps --filter "name=sg-fc" --format "{{.Names}}" | wc -l | tr -d ' ')
HEALTHY=$(docker ps --filter "name=sg-fc" --format "{{.Status}}" | grep -c "healthy" || true)
if [ "$TOTAL" -eq 0 ]; then
  bad "No sg-fc-* containers running — deploy first"
  exit 1
elif [ "$HEALTHY" -eq "$TOTAL" ]; then
  ok "All $TOTAL containers healthy"
else
  warn "$HEALTHY/$TOTAL containers healthy (some still starting — wait 30s and re-run)"
fi

hdr "2. API health endpoint ($API/health)"
CODE=$(curl -s -o /dev/null -w "%{http_code}" --max-time 5 "$API/health" 2>/dev/null || echo "000")
case "$CODE" in
  200) ok "GET $API/health → 200" ;;
  000) bad "GET $API/health → connection refused (is $MODE mode running?)" ;;
  *)   warn "GET $API/health → $CODE (unexpected)" ;;
esac

hdr "3. API version probe ($API/api/v1/health)"
RESP=$(curl -s --max-time 5 -o /dev/null -w "%{http_code}" "$API/api/v1/health" 2>/dev/null || echo "000")
case "$RESP" in
  200|401) ok "GET $API/api/v1/health → $RESP (auth-gated, expected)" ;;
  000)     bad "GET $API/api/v1/health → connection refused" ;;
  *)       warn "GET $API/api/v1/health → $RESP" ;;
esac

hdr "4. Prometheus /metrics endpoint"
RESP=$(curl -s --max-time 5 -o /dev/null -w "%{http_code}" "$API/metrics" 2>/dev/null || echo "000")
if [ "$RESP" = "200" ]; then
  LINES=$(curl -s --max-time 5 "$API/metrics" | wc -l | tr -d ' ')
  ok "GET $API/metrics → 200 ($LINES lines of Prometheus text)"
else
  bad "GET $API/metrics → $RESP (should return Prometheus text, not HTML)"
fi

hdr "5. H5 Demo frontend ($NGINX/)"
RESP=$(curl -s --max-time 5 -o /dev/null -w "%{http_code}" "$NGINX/" 2>/dev/null || echo "000")
CT=$(curl -s --max-time 5 -I "$NGINX/" 2>/dev/null | grep -i "content-type:" | tr -d '\r' | head -1)
if [ "$RESP" = "200" ] && echo "$CT" | grep -qi "text/html"; then
  ok "GET $NGINX/ → 200 (HTML)"
else
  bad "GET $NGINX/ → $RESP ($CT)"
fi

hdr "6. Anvil RPC reachable"
RESP=$(curl -s --max-time 5 -X POST -H "Content-Type: application/json" \
  -d '{"jsonrpc":"2.0","method":"eth_blockNumber","params":[],"id":1}' \
  http://localhost:18545 2>/dev/null | python3 -c "import sys,json; print(json.load(sys.stdin).get('result','?'))" 2>/dev/null || echo "000")
if [ "$RESP" != "000" ] && [ "$RESP" != "?" ]; then
  HEX_BLOCK=$RESP
  DEC_BLOCK=$(python3 -c "print(int('$RESP', 16))" 2>/dev/null || echo "?")
  ok "Anvil :18545 → block $DEC_BLOCK ($RESP)"
else
  bad "Anvil :18545 unreachable (NFT verify and MetaMask will fail)"
fi

hdr "7. Auth challenge roundtrip"
RESP=$(curl -s --max-time 5 -X POST -H "Content-Type: application/json" \
  -d '{"address":"0xf39Fd6e51aad88F6F4ce6aB8827279cffFb92266","chain_id":31337}' \
  "$API/api/v1/auth/challenge" 2>/dev/null || echo "{}")
if echo "$RESP" | python3 -c "import sys,json; d=json.load(sys.stdin); sys.exit(0 if d.get('challenge_id') else 1)" 2>/dev/null; then
  ok "POST $API/api/v1/auth/challenge → challenge_id returned"
else
  bad "POST $API/api/v1/auth/challenge → no challenge_id (auth flow broken)"
fi

hdr "Summary"
echo -e "  ${GRN}Pass: $PASS${NC}  ${YEL}Warn: $WARN${NC}  ${RED}Fail: $FAIL${NC}"
echo ""
if [ "$FAIL" -gt 0 ]; then
  echo -e "${RED}❌ Deployment has $FAIL blocking issue(s) — see above.${NC}"
  exit 1
elif [ "$WARN" -gt 0 ]; then
  echo -e "${YEL}⚠ Deployment looks OK but $WARN warning(s) — review above.${NC}"
  exit 0
else
  echo -e "${GRN}✅ All checks passed — deployment is healthy.${NC}"
  exit 0
fi
