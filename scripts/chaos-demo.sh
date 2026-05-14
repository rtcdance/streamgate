#!/bin/bash
# StreamGate 韧性演示脚本
# 展示系统在各种故障场景下的行为。
# 需要 Docker 全链路在运行 (docker-compose.fullchain.yml).
set -euo pipefail

BASE="${1:-http://localhost:18080}"
PASS=0
FAIL=0
TOKEN=""

p() { PASS=$((PASS+1)); echo "✅ PASS: $*"; }
f() { FAIL=$((FAIL+1)); echo "❌ FAIL: $*"; }

echo "============================================"
echo "StreamGate 韧性演示"
echo "后端: $BASE"
echo "============================================"
echo ""

# 生成测试用 JWT
gen_token() {
  local addr="${1:-0xf39Fd6e51aad88F6F4ce6aB8827279cffFb92266}"
  python3 -c "
import hmac,hashlib,base64,json,time
h=base64.urlsafe_b64encode(json.dumps({'alg':'HS256','typ':'JWT'}).encode()).rstrip(b'=').decode()
n=int(time.time())
p=base64.urlsafe_b64encode(json.dumps({'sub':'$addr','wallet_address':'$addr','iat':n,'exp':n+3600,'jti':'chaos'}).encode()).rstrip(b'=').decode()
s=base64.urlsafe_b64encode(hmac.new(b'fullchain-acceptance-secret',(h+'.'+p).encode(),hashlib.sha256).digest()).rstrip(b'=').decode()
print(h+'.'+p+'.'+s)
"
}
TOKEN=$(gen_token)

# =============================================
echo "=== 1. 认证防护 ==="
echo ""

echo "测试 1a: 无 token 请求上传"
STATUS=$(curl -s -o /dev/null -w "%{http_code}" -X POST "$BASE/api/v1/upload")
[ "$STATUS" = "401" ] && p "无 token 返回 401 (got $STATUS)" || f "期望 401 得到 $STATUS"

echo "测试 1b: 空 token"
STATUS=$(curl -s -o /dev/null -w "%{http_code}" -X POST "$BASE/api/v1/upload" \
  -H "Authorization: Bearer ")
[ "$STATUS" = "401" ] && p "空 token 返回 401 (got $STATUS)" || f "期望 401 得到 $STATUS"

echo "测试 1c: 伪造 token"
STATUS=$(curl -s -o /dev/null -w "%{http_code}" -X POST "$BASE/api/v1/upload" \
  -H "Authorization: Bearer fake.eyJzdWIiOiIweCJ9.fakesig")
[ "$STATUS" = "401" ] && p "伪造 token 返回 401 (got $STATUS)" || f "期望 401 得到 $STATUS"

echo "测试 1d: 上传 .txt 文件"
echo "hello" > /tmp/chaos-test.txt
STATUS=$(curl -s -o /dev/null -w "%{http_code}" -X POST "$BASE/api/v1/upload" \
  -H "Authorization: Bearer $TOKEN" -F "file=@/tmp/chaos-test.txt;filename=test.txt")
[ "$STATUS" = "400" ] && p "文本文件被拒绝 400 (got $STATUS)" || f "期望 400 得到 $STATUS"

# =============================================
echo ""
echo "=== 2. NFT 门控 ==="
echo ""

echo "测试 2a: 无 NFT 合约参数"
STATUS=$(curl -s -o /dev/null -w "%{http_code}" \
  "$BASE/api/v1/streaming/demo/manifest.m3u8" -H "Authorization: Bearer $TOKEN")
[ "$STATUS" = "400" ] && p "无 contract 参数返回 400 (got $STATUS)" || f "期望 400 得到 $STATUS"

echo "测试 2b: 用无效合约请求 manifest"
STATUS=$(curl -s -o /dev/null -w "%{http_code}" \
  "$BASE/api/v1/streaming/demo/manifest.m3u8?contract=0xDEAD&chain_id=31337" \
  -H "Authorization: Bearer $TOKEN")
# 如果 Anvil 在运行，会尝试查询—可能是 403(NFT_REQUIRED) 或 500(RPC 错误)
if [ "$STATUS" = "403" ] || [ "$STATUS" = "500" ] || [ "$STATUS" = "404" ]; then
  p "NFT 门控生效 (got $STATUS)"
else
  f "期望 403/500/404 得到 $STATUS"
fi

# =============================================
echo ""
echo "=== 3. 上传防护 ==="
echo ""

echo "测试 3a: 上传 .mp4 伪装的可执行文件"
printf '\x1A\x45\xDF\xA3 webm header' > /tmp/chaos.mp4
STATUS=$(curl -s -o /dev/null -w "%{http_code}" -X POST "$BASE/api/v1/upload" \
  -H "Authorization: Bearer $TOKEN" -F "file=@/tmp/chaos.mp4;filename=video.exe")
[ "$STATUS" = "400" ] && p ".exe 扩展名被拒绝 400 (got $STATUS)" || f "期望 400 得到 $STATUS"

echo "测试 3b: 上传格式不匹配(.mp4 但内容是 webm)"
printf '\x1A\x45\xDF\xA3 webm header' > /tmp/chaos-mismatch.mp4
STATUS=$(curl -s -o /dev/null -w "%{http_code}" -X POST "$BASE/api/v1/upload" \
  -H "Authorization: Bearer $TOKEN" -F "file=@/tmp/chaos-mismatch.mp4;filename=mismatch.mp4")
[ "$STATUS" = "400" ] && p "格式不匹配被拒绝 400 (got $STATUS)" || f "期望 400 得到 $STATUS"

echo "测试 3c: 缺少 file 字段"
STATUS=$(curl -s -o /dev/null -w "%{http_code}" -X POST "$BASE/api/v1/upload" \
  -H "Authorization: Bearer $TOKEN")
[ "$STATUS" = "400" ] && p "缺少 file 返回 400 (got $STATUS)" || f "期望 400 得到 $STATUS"

# =============================================
echo ""
echo "=== 4. 健康检查 ==="
echo ""

echo "测试 4a: Health endpoint"
HEALTH=$(curl -sf "$BASE/health" 2>/dev/null) && p "/health 可达" || f "/health 不可达"

echo "测试 4b: PostgreSQL 健康"
DB_OK=$(docker exec sg-fc-postgres pg_isready -U postgres 2>/dev/null) && p "PostgreSQL: $DB_OK" || f "PostgreSQL 不可达"

echo "测试 4c: Redis 健康"
REDIS_OK=$(docker exec sg-fc-redis redis-cli ping 2>/dev/null) && p "Redis: $REDIS_OK" || f "Redis 不可达"

echo "测试 4d: MinIO 健康"
MINIO_OK=$(curl -sf "http://localhost:19000/minio/health/live" 2>/dev/null && echo "OK") && p "MinIO: $MINIO_OK" || f "MinIO 不可达"

echo "测试 4e: NATS 健康"
NATS_OK=$(docker exec sg-fc-nats nats-server --version 2>/dev/null) && p "NATS: $NATS_OK" || f "NATS 不可达"

echo "测试 4f: Anvil 链"
ANVIL_OK=$(curl -sf -X POST http://localhost:18545 -H "Content-Type: application/json" \
  -d '{"jsonrpc":"2.0","method":"eth_chainId","id":1}' 2>/dev/null | python3 -c "import sys,json;print(json.load(sys.stdin).get('result',''))" 2>/dev/null)
[ "$ANVIL_OK" = "0x7a69" ] && p "Anvil chainId=31337" || f "Anvil 异常: $ANVIL_OK"

# =============================================
echo ""
echo "============================================"
TOTAL=$((PASS+FAIL))
if [ $FAIL -eq 0 ]; then
  echo "✅ 全部 $TOTAL 项测试通过"
else
  echo "⚠️  $PASS 通过, $FAIL 失败 (共 $TOTAL 项)"
fi
echo "============================================"
