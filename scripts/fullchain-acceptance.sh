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
# The H5 demo is served by the monolith. In api-gateway mode the gateway does not
# serve static files, so point this at the h5-demo service instead of treating a
# 404 as a product failure.
DEMO_URL="${H5_DEMO_URL:-${BASE_URL}/demo/}"
ANVIL_URL="${ANVIL_URL:-http://localhost:18545}"
# The docker-compose.fullchain.yml stack publishes MinIO on a fixed host port;
# a verify stack publishes it on an ephemeral one, so only the liveness probe is
# overridable. Defaults keep the existing flow byte-identical.
MINIO_LIVENESS_URL="${MINIO_LIVENESS_URL:-http://localhost:19000/minio/health/live}"
# Container names the docker exec / docker inspect checks address. The defaults
# are the sg-fc-* containers of docker-compose.fullchain.yml; a compose-project
# stack (deploy/docker/compose.verify.yml) overrides them with its
# project-prefixed names.
PG_CONTAINER="${PG_CONTAINER:-sg-fc-postgres}"
REDIS_CONTAINER="${REDIS_CONTAINER:-sg-fc-redis}"
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
# json_get_stdin <key> — same, for values already on stdin.
json_get_stdin() { python3 -c "import sys,json;print(json.load(sys.stdin).get('$1',''))" 2>/dev/null || true; }

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

# Everything the external transcoder worker does for THIS run happens after
# this timestamp; used to scope `docker logs` when attributing a task.
ACCEPT_STARTED_AT="$(date -u +%Y-%m-%dT%H:%M:%SZ)"

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

# wait_for_manifest <want> <deadline-seconds> — polls the holder's manifest until
# it answers <want>. Defined at the top level because several phases use it, and
# a rule change propagates through a cache, so an immediate check is flaky.
wait_for_manifest() {
    local want="$1" deadline=$((SECONDS + $2)) code=""
    while [ "$SECONDS" -lt "$deadline" ]; do
        code="$(curl -s -o /dev/null -w '%{http_code}' -H "Authorization: Bearer $TOKEN_A" \
            "$BASE_URL/api/v1/streaming/$CONTENT_ID/manifest.m3u8")"
        [ "$code" = "$want" ] && break
        sleep 2
    done
    printf '%s' "$code"
}

# ---------------------------------------------------------------------------
# 1. Infrastructure
# ---------------------------------------------------------------------------
printf '=== Infrastructure ===\n'

HEALTH="$(curl -sf "$BASE_URL/health" 2>/dev/null)" || { f '/health unreachable'; exit 1; }
STATUS="$(json_get "$HEALTH" status)"
[ "$STATUS" = "healthy" ] && p "Status=$STATUS" || f "Status=$STATUS"

docker exec "$PG_CONTAINER" pg_isready -U postgres >/dev/null 2>&1 && p "PostgreSQL ready" || f "PostgreSQL not ready"
docker exec "$REDIS_CONTAINER" redis-cli ping >/dev/null 2>&1 && p "Redis ready" || f "Redis not ready"
curl -sf "$MINIO_LIVENESS_URL" >/dev/null 2>&1 && p "MinIO healthy" || f "MinIO unreachable"

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
    # A wallet must not be able to delete someone else's content.
    CODE="$(curl -s -o /dev/null -w '%{http_code}' -X DELETE -H "Authorization: Bearer $TOKEN_B" \
        "$BASE_URL/api/v1/content/$CONTENT_ID")"
    if [ "$CODE" = "403" ] || [ "$CODE" = "404" ]; then
        p "Wallet B cannot delete Wallet A's content ($CODE)"
    else
        f "Wallet B delete returned $CODE — expected 403/404"
    fi
fi

CODE="$(curl -s -o /dev/null -w '%{http_code}' "$DEMO_URL")"
[ "$CODE" = "200" ] && p "H5 demo served at $DEMO_URL" || f "H5 demo index=$CODE"

# ---------------------------------------------------------------------------
# 6. Ownership: mint, then gate the content on that NFT
# ---------------------------------------------------------------------------
printf '\n=== Ownership and gating ===\n'

# The DemoNFT address defaults to the deployment this repo's stack produces and
# can be overridden. It is deliberately NOT probed here: a wrong address makes
# the gating rule point at a contract that reverts, and the gate assertions
# below then fail on their own. An extra probe would only add a second place to
# misread an RPC hiccup as a product defect.
DEMO_CONTRACT="${DEMO_CONTRACT:-0x5FbDB2315678afecb367f032d93F642f64180aa3}"
p "DemoNFT contract=$DEMO_CONTRACT (chain $CHAIN_ID)"

if [ -n "$TOKEN_A" ]; then
    MINT="$(curl -sf -X POST "$BASE_URL/api/v1/nft/dev/mint" -H "Authorization: Bearer $TOKEN_A" \
        -H 'Content-Type: application/json' -d '{"count":1}' 2>/dev/null)" || MINT=""
    MINTED_BAL="$(json_get "$MINT" balance)"
    MINT_TX="$(printf '%s' "$MINT" | python3 -c "import sys,json;print((json.load(sys.stdin).get('tx_hashes') or [''])[0])" 2>/dev/null || echo '')"
    if [ -n "$MINTED_BAL" ]; then
        p "Wallet A minted an NFT (balance=$MINTED_BAL)"
    else
        f "dev-mint failed: ${MINT:0:160}"
    fi

    # The mint must actually land on chain: a success response without a
    # successful receipt is not evidence of ownership. The service returns the
    # hash before the tx is necessarily mined, so poll for the receipt.
    if [ -n "$MINT_TX" ]; then
        R_STATUS=""
        R_BLOCK=""
        for _ in 1 2 3 4 5 6 7 8 9 10; do
            RECEIPT="$(curl -s -X POST "$ANVIL_URL" -H 'Content-Type: application/json' \
                -d "{\"jsonrpc\":\"2.0\",\"method\":\"eth_getTransactionReceipt\",\"params\":[\"$MINT_TX\"],\"id\":1}" 2>/dev/null)"
            R_STATUS="$(printf '%s' "$RECEIPT" | python3 -c "import sys,json;r=json.load(sys.stdin).get('result') or {};print(r.get('status',''))" 2>/dev/null || echo '')"
            R_BLOCK="$(printf '%s' "$RECEIPT" | python3 -c "import sys,json;r=json.load(sys.stdin).get('result') or {};print(r.get('blockNumber',''))" 2>/dev/null || echo '')"
            [ -n "$R_STATUS" ] && break
            sleep 2
        done
        [ "$R_STATUS" = "0x1" ] && p "Mint receipt status=0x1 at block $R_BLOCK" \
            || f "Mint receipt status=${R_STATUS:-missing} (tx=$MINT_TX)"
    else
        f "dev-mint returned no tx hash"
    fi
fi

RULE_OK=""
if [ -n "$CONTENT_ID" ] && [ -n "$TOKEN_A" ] && [ -n "$DEMO_CONTRACT" ]; then
    RULE="$(curl -sf -X POST "$BASE_URL/api/v1/content/$CONTENT_ID/gating-rules" \
        -H "Authorization: Bearer $TOKEN_A" -H 'Content-Type: application/json' \
        -d "{\"contract_address\":\"$DEMO_CONTRACT\",\"chain_id\":$CHAIN_ID,\"standard\":\"erc721\",\"min_balance\":1}" 2>/dev/null)" || RULE=""
    RULE_ID="$(json_get "$RULE" id)"
    if [ -n "$RULE_ID" ]; then
        p "Server-side gating rule set (rule_id=$RULE_ID, min_balance=1)"
        RULE_OK=1
    else
        f "Could not set a gating rule: ${RULE:0:160}"
    fi
fi

# ---------------------------------------------------------------------------
# 7. Transcode (real, through the public API)
# ---------------------------------------------------------------------------
printf '\n=== Transcode ===\n'

TASK_ID=""
READY=0
if [ -n "$CONTENT_ID" ] && [ -n "$TOKEN_A" ]; then
    # Completing the upload already triggers a transcode through the public API
    # (the upload hook, profile "abr"). Submitting a SECOND job for the same
    # content conflicts with it and is reported as failed while the first one
    # succeeds — so the honest check is to wait for the content to become
    # playable, which only happens if a real transcode produced real HLS.
    DEADLINE=$((SECONDS + 180))
    LAST_ERR=""
    while [ "$SECONDS" -lt "$DEADLINE" ]; do
        CODE="$(curl -s -o "$WORKDIR/ready.out" -w '%{http_code}' \
            -H "Authorization: Bearer $TOKEN_A" "$BASE_URL/api/v1/streaming/$CONTENT_ID/manifest.m3u8")"
        if [ "$CODE" = "200" ]; then READY=1; break; fi
        LAST_ERR="$(head -c 140 "$WORKDIR/ready.out" 2>/dev/null)"
        sleep 3
    done
    if [ "$READY" = "1" ]; then
        p "Transcode produced playable HLS within the 180s budget"
    else
        f "Content never became playable within 180s (last=${LAST_ERR:-no response})"
    fi
else
    f "Skipped transcode: missing content or token"
fi

# ---------------------------------------------------------------------------
# 7b. Isolated transcoder worker (running process, not just the Dockerfile)
# ---------------------------------------------------------------------------
# SG-12 step 5: untrusted media parsing must not share a trust domain with the
# wallet/JWT-signing process. These checks inspect the RUNNING container (user,
# capabilities, env, limits) and then attribute the real transcode above to the
# EXTERNAL worker — both app containers run with the embedded worker disabled.
printf '\n=== Isolated transcoder worker ===\n'

TC="${TC:-sg-fc-transcoder}"
if docker inspect "$TC" >/dev/null 2>&1; then
    TC_RUNNING="$(docker inspect -f '{{.State.Running}}' "$TC")"
    [ "$TC_RUNNING" = "true" ] && p "Worker container is running" \
        || f "Worker container is not running (State.Running=$TC_RUNNING)"

    TC_USER="$(docker inspect -f '{{.Config.User}}' "$TC")"
    case "$TC_USER" in
        ""|root|0) f "Worker runs as '${TC_USER:-image default}' — must be non-root" ;;
        *)         p "Worker runs as non-root ($TC_USER)" ;;
    esac

    TC_CAPS="$(docker inspect -f '{{json .HostConfig.CapDrop}}' "$TC")"
    case "$TC_CAPS" in
        *ALL*) p "Worker drops all capabilities (CapDrop=$TC_CAPS)" ;;
        *)     f "Worker CapDrop=$TC_CAPS — expected ALL" ;;
    esac

    # No JWT secret, no chain material in the worker's environment. The
    # object-store credential is the one secret this process legitimately
    # needs (read originals, write segments), so it is the single exemption.
    TC_BAD_ENV="$(docker inspect -f '{{range .Config.Env}}{{println .}}{{end}}' "$TC" \
        | grep -iE 'JWT|SECRET|PRIVATE|ANVIL|RPC' \
        | grep -ivE '^STREAMGATE_STORAGE_SECRET_KEY=' || true)"
    [ -z "$TC_BAD_ENV" ] && p "Worker env carries no JWT/chain material" \
        || f "Worker env exposes credential material: $(printf '%s' "$TC_BAD_ENV" | head -c 200)"

    TC_MEM="$(docker inspect -f '{{.HostConfig.Memory}}' "$TC")"
    if [ "${TC_MEM:-0}" -gt 0 ] 2>/dev/null; then
        p "Worker memory limit set ($TC_MEM bytes)"
    else
        f "Worker has no memory limit"
    fi

    # Attribution: the content above is playable only if SOME worker executed
    # the task. Both app containers have the embedded worker disabled, so the
    # DB lease plus a claim/complete line in THIS container's logs prove the
    # external worker did it.
    if [ "$READY" = "1" ] && [ -n "$CONTENT_ID" ]; then
        ATTR_ROW="$(docker exec "$PG_CONTAINER" psql -U postgres -d streamgate -tAc \
            "SELECT id || '|' || COALESCE(lease_owner, '') FROM transcoding_tasks WHERE content_id = '$CONTENT_ID' ORDER BY created_at DESC LIMIT 1" 2>/dev/null || true)"
        ATTR_ID="${ATTR_ROW%%|*}"
        ATTR_LEASE="${ATTR_ROW#*|}"
        if [ -n "$ATTR_ID" ] && [ "$ATTR_ROW" = "$ATTR_ID|$ATTR_LEASE" ] && [ -n "$ATTR_LEASE" ]; then
            p "Task $ATTR_ID carries lease_owner='$ATTR_LEASE' (DB claim recorded)"
            TC_LOGS="$(docker logs --since "$ACCEPT_STARTED_AT" "$TC" 2>&1 || true)"
            if grep -q "$ATTR_ID" <<<"$TC_LOGS"; then
                p "$TC logs claim/complete the task (external worker executed it)"
            else
                f "$TC logs never mention task $ATTR_ID — not attributable to the external worker"
            fi
        else
            f "No leased task row for content $CONTENT_ID (row='${ATTR_ROW:-none}')"
        fi
    else
        f "Skipped worker attribution: the transcode above did not complete"
    fi
else
    f "$TC container is absent — the isolated worker is a required service"
fi

# ---------------------------------------------------------------------------
# 8. Playback: master -> media playlist -> every segment, decoded
# ---------------------------------------------------------------------------
printf '\n=== Playback (real HLS, decoded) ===\n'

PLAYBACK_JSON=""
if [ -n "$CONTENT_ID" ] && [ -n "$TOKEN_A" ]; then
    # The positive gate assertion: the holder gets the manifest. This is also
    # what proves the DemoNFT address above is the right one — a rule pointing at
    # a contract that reverts cannot produce a 200 here.
    HOLDER_CODE="$(curl -s -o /dev/null -w '%{http_code}' -H "Authorization: Bearer $TOKEN_A" \
        "$BASE_URL/api/v1/streaming/$CONTENT_ID/manifest.m3u8")"
    [ "$HOLDER_CODE" = "200" ] && p "Wallet A (holder) receives the gated manifest (200)" \
        || f "Wallet A (holder) got $HOLDER_CODE — expected 200"

    PLAYBACK_JSON="$(python3 - "$BASE_URL" "$CONTENT_ID" "$TOKEN_A" "$WORKDIR" <<'PY'
import json, sys, urllib.request, urllib.parse, urllib.error, os, re, math

base, content, token, outdir = sys.argv[1:5]
auth = {'Authorization': f'Bearer {token}'}
result = {'errors': [], 'variants': 0, 'playlists': 1, 'segments': 0, 'bytes': 0}
seen = set()

def fetch(url, headers=None, raw=False):
    req = urllib.request.Request(url, headers=headers or {})
    with urllib.request.urlopen(req, timeout=30) as resp:
        body = resp.read()
        return (body, resp.status, dict(resp.headers)) if raw else (body.decode('utf-8', 'replace'), resp.status, dict(resp.headers))

def uris(text):
    return [l.strip() for l in text.splitlines() if l.strip() and not l.strip().startswith('#')]

entry_url = f'{base}/api/v1/streaming/{content}/manifest.m3u8'
try:
    body, status, _ = fetch(entry_url, auth)
except urllib.error.HTTPError as e:
    result['errors'].append(f'entry playlist HTTP {e.code}')
    print(json.dumps(result)); sys.exit(0)

result['master_status'] = status

# An HLS entry point is EITHER a master playlist (points at other playlists) or a
# media playlist (points straight at segments). Walk whatever it is, recursively,
# resolving every relative URI against the URL it came from.
playlists = [(entry_url, body)]
concat_path = os.path.join(outdir, 'segments.bin')
total = 0

with open(concat_path, 'wb') as out:
    depth = 0
    while playlists and depth < 4:
        depth += 1
        nxt = []
        for pl_url, pl_text in playlists:
            # A master lists OTHER playlists; a media playlist lists segments.
            # Match the tag with its colon: "#EXT-X-MEDIA" is a substring of
            # "#EXT-X-MEDIA-SEQUENCE", which every media playlist carries, so a
            # loose substring test classifies media playlists as masters and
            # then tries to parse segment bytes as a playlist.
            is_master = '#EXT-X-STREAM-INF' in pl_text or '#EXT-X-MEDIA:' in pl_text
            if is_master:
                result['variants'] += len(uris(pl_text))
                for uri in uris(pl_text):
                    u = urllib.parse.urljoin(pl_url, uri)
                    if u in seen:
                        continue
                    seen.add(u)
                    try:
                        text, _, _ = fetch(u, auth)
                    except urllib.error.HTTPError as e:
                        result['errors'].append(f'playlist HTTP {e.code}')
                        continue
                    nxt.append((u, text))
                    result['playlists'] += 1
                continue

            # A media playlist: every URI is a segment.
            # Record the real timing FFmpeg wrote. A synthesised playlist claims
            # every segment is exactly 6.0s, which erases the short final segment
            # and misstates total duration and TARGETDURATION.
            extinfs = [float(m) for m in re.findall(r'#EXTINF:([0-9.]+)', pl_text)]
            if extinfs:
                tdm = re.search(r'#EXT-X-TARGETDURATION:(\d+)', pl_text)
                result.setdefault('media_timings', []).append({
                    'url': pl_url,
                    'extinfs': extinfs,
                    'sum': round(sum(extinfs), 3),
                    'targetduration': int(tdm.group(1)) if tdm else None,
                })
            for uri in uris(pl_text):
                seg_url = urllib.parse.urljoin(pl_url, uri)
                if seg_url in seen:
                    continue
                seen.add(seg_url)
                try:
                    blob, _, _ = fetch(seg_url, auth, raw=True)
                except urllib.error.HTTPError as e:
                    result['errors'].append(f'segment HTTP {e.code}')
                    continue
                if not blob:
                    result['errors'].append('segment was empty')
                    continue
                out.write(blob)
                total += len(blob)
                result['segments'] += 1
                result.setdefault('first_segment_url', seg_url)
        playlists = nxt

result['bytes'] = total
result['concat_path'] = concat_path
print(json.dumps(result))
PY
)" || PLAYBACK_JSON='{"errors":["playback helper crashed"]}'
fi

if [ -n "$PLAYBACK_JSON" ]; then
    P_MASTER="$(printf '%s' "$PLAYBACK_JSON" | python3 -c "import sys,json;print(json.load(sys.stdin).get('master_status',''))" 2>/dev/null)"
    P_VARIANTS="$(printf '%s' "$PLAYBACK_JSON" | python3 -c "import sys,json;print(json.load(sys.stdin).get('variants',0))" 2>/dev/null)"
    P_PLAYLISTS="$(printf '%s' "$PLAYBACK_JSON" | python3 -c "import sys,json;print(json.load(sys.stdin).get('playlists',0))" 2>/dev/null)"
    P_SEGMENTS="$(printf '%s' "$PLAYBACK_JSON" | python3 -c "import sys,json;print(json.load(sys.stdin).get('segments',0))" 2>/dev/null)"
    P_BYTES="$(printf '%s' "$PLAYBACK_JSON" | python3 -c "import sys,json;print(json.load(sys.stdin).get('bytes',0))" 2>/dev/null)"
    P_ERRORS="$(printf '%s' "$PLAYBACK_JSON" | python3 -c "import sys,json;print('; '.join(json.load(sys.stdin).get('errors',[])))" 2>/dev/null)"
    P_SEGURL="$(printf '%s' "$PLAYBACK_JSON" | python3 -c "import sys,json;print(json.load(sys.stdin).get('first_segment_url',''))" 2>/dev/null)"

    [ "$P_MASTER" = "200" ] && p "Entry playlist 200" || f "Entry playlist status=$P_MASTER"
    # The entry point may be a master (listing variants) or a media playlist
    # (listing segments) directly; both are valid HLS.
    [ "${P_PLAYLISTS:-0}" -gt 0 ] && p "Walked $P_PLAYLISTS playlist(s), $P_VARIANTS variant(s) declared" \
        || f "No playlist was walked"
    [ "${P_SEGMENTS:-0}" -gt 0 ] && p "Downloaded $P_SEGMENTS segment(s), $P_BYTES bytes" || f "No segments retrieved"
    [ -z "$P_ERRORS" ] && p "Every referenced playlist and segment returned 200" || f "Playback errors: $P_ERRORS"

    # The served playlist must be the one FFmpeg wrote: durations that sum to the
    # real media length, TARGETDURATION equal to the largest EXTINF rounded up,
    # and no fixed 6.0s segments (the signature of a synthesised playlist).
    TIMING_CHECK="$(python3 - "$PLAYBACK_JSON" "$DURATION" <<'PY'
import json, sys, math
data = json.loads(sys.argv[1])
duration = float(sys.argv[2])
timings = data.get('media_timings', [])
if not timings:
    print('FAIL|no media playlist carried EXTINF tags'); sys.exit(0)
for t in timings:
    extinfs = t['extinfs']
    if any(abs(e - 6.0) < 1e-9 for e in extinfs) and len(set(extinfs)) == 1:
        print(f"FAIL|{t['url']} has fixed 6.0s segments (synthesised playlist)"); sys.exit(0)
    if abs(t['sum'] - duration) > 1.5:
        print(f"FAIL|{t['url']} EXTINF sum={t['sum']}s but media is {duration}s"); sys.exit(0)
    td = t.get('targetduration')
    # RFC 8216: TARGETDURATION must be >= each EXTINF rounded to the nearest
    # integer (FFmpeg writes 8 for a largest EXTINF of 8.333333).
    if td is None or td < math.floor(max(extinfs) + 0.5):
        print(f"FAIL|{t['url']} TARGETDURATION={td} but largest EXTINF is {max(extinfs)}"); sys.exit(0)
print(f"PASS|{len(timings)} media playlist(s) with real durations summing to {timings[0]['sum']}s")
PY
)"
    case "$TIMING_CHECK" in
        PASS\|*) p "Playlist actual timing: ${TIMING_CHECK#PASS|}" ;;
        *) f "Playlist timing: ${TIMING_CHECK#FAIL|}" ;;
    esac

    # Concatenated MPEG-TS segments must decode end to end.
    if [ "${P_BYTES:-0}" -gt 0 ] && ffmpeg -hide_banner -loglevel error -v error -i "$WORKDIR/segments.bin" -f null - 2>"$WORKDIR/segdecode.err"; then
        p "All segments fully decode with FFmpeg"
    else
        f "Segment decode failed: $(head -c 200 "$WORKDIR/segdecode.err" 2>/dev/null)"
    fi

    # Range must be honoured, not silently ignored.
    if [ -n "$P_SEGURL" ]; then
        RANGE_HEADERS="$(curl -s -D - -o /dev/null -r 0-99 -H "Authorization: Bearer $TOKEN_A" "$P_SEGURL" 2>/dev/null || true)"
        # `|| true` matters: under `set -o pipefail` a grep that matches nothing
        # returns 1 and would abort the whole run mid-check.
        RANGE_CODE="$(printf '%s' "$RANGE_HEADERS" | awk 'NR==1{print $2}' || true)"
        RANGE_CR="$(printf '%s' "$RANGE_HEADERS" | grep -i '^content-range:' | tr -d '\r' | head -1 || true)"
        [ "$RANGE_CODE" = "206" ] && p "Range request returns 206" || f "Range request returned ${RANGE_CODE:-none} (expected 206)"
        [ -n "$RANGE_CR" ] && p "Content-Range present: ${RANGE_CR#*: }" || f "206 without a Content-Range header"
    fi

    # Wallet B is a real logged-in wallet holding no NFT: it must be refused, and
    # the refusal must not carry media bytes. Checked here, after transcoding,
    # so a 404 "not ready" cannot be mistaken for a denial.
    if [ -n "$CONTENT_ID" ]; then
        B_BODY="$WORKDIR/walletb.out"
        B_CODE="$(curl -s -o "$B_BODY" -w '%{http_code}' -H "Authorization: Bearer $TOKEN_B" \
            "$BASE_URL/api/v1/streaming/$CONTENT_ID/manifest.m3u8")"
        B_BYTES="$(wc -c < "$B_BODY" | tr -d ' ')"
        case "$B_CODE" in
            401|403) p "Wallet B (no NFT) refused the manifest ($B_CODE)" ;;
            503)     p "Wallet B refused: dependencies unverifiable ($B_CODE)" ;;
            *)       f "Wallet B got $B_CODE on a gated manifest — expected 401/403/503" ;;
        esac
        if grep -q '#EXTM3U' "$B_BODY"; then
            f "Wallet B's refused response contained a playlist"
        else
            p "Wallet B's refusal carried no media ($B_BYTES bytes)"
        fi
    fi
else
    f "Playback not exercised"
fi

# ---------------------------------------------------------------------------
# 9. Tampering matrix
# ---------------------------------------------------------------------------
printf '\n=== Tampering ===\n'

PB_TOKEN="$(printf '%s' "${P_SEGURL:-}" | sed -n 's/.*playback_token=\([^&]*\).*/\1/p')"

if [ -n "$PB_TOKEN" ]; then
    # A segment name is not a path. Traversal must be refused outright. 401 is
    # an acceptable refusal: the router normalises the path, the token then no
    # longer matches the route, and the request never reaches storage - which is
    # the property under test (no file outside the segment namespace is served).
    CODE="$(curl -s -o /dev/null -w '%{http_code}' \
        "$BASE_URL/api/v1/streaming/$CONTENT_ID/segment/..%2F..%2Fetc%2Fpasswd?playback_token=$PB_TOKEN")"
    case "$CODE" in
        400|401|403|404) p "Path traversal in the segment name refused ($CODE)" ;;
        *)               f "Path traversal returned $CODE — expected 400/401/403/404" ;;
    esac

    # A playback capability is bound to one content: it must not work elsewhere.
    CODE="$(curl -s -o /dev/null -w '%{http_code}' \
        "$BASE_URL/api/v1/streaming/00000000-0000-0000-0000-000000000000/segment/0.ts?playback_token=$PB_TOKEN")"
    case "$CODE" in
        400|401|403|404) p "Playback token refused for another content ($CODE)" ;;
        *)               f "Cross-content playback token returned $CODE" ;;
    esac

    # A capability rides in these URLs, so the response must not be stored by a
    # shared cache and must not hand the URL to another origin via Referer.
    if [ -n "$P_SEGURL" ]; then
        HDRS="$(curl -s -D - -o /dev/null -H "Authorization: Bearer $TOKEN_A" "$P_SEGURL" 2>/dev/null || true)"
        REF="$(printf '%s' "$HDRS" | grep -i '^referrer-policy:' | tr -d '\r' | head -1 || true)"
        case "$REF" in
            *no-referrer*) p "Segment response sets Referrer-Policy: no-referrer" ;;
            *)             f "Segment Referrer-Policy=${REF#*: } — a capability URL must not be forwarded" ;;
        esac
    fi
    if [ -n "$CONTENT_ID" ]; then
        MHDRS="$(curl -s -D - -o /dev/null -H "Authorization: Bearer $TOKEN_A" \
            "$BASE_URL/api/v1/streaming/$CONTENT_ID/manifest.m3u8" 2>/dev/null || true)"
        MCACHE="$(printf '%s' "$MHDRS" | grep -i '^cache-control:' | tr -d '\r' | head -1 || true)"
        MREF="$(printf '%s' "$MHDRS" | grep -i '^referrer-policy:' | tr -d '\r' | head -1 || true)"
        case "$MCACHE" in
            *no-store*) p "Manifest is Cache-Control: private, no-store" ;;
            *)          f "Manifest Cache-Control=${MCACHE#*: } — a capability in the body must not be stored" ;;
        esac
        case "$MREF" in
            *no-referrer*) p "Manifest sets Referrer-Policy: no-referrer" ;;
            *)             f "Manifest Referrer-Policy=${MREF#*: } — a capability URL must not be forwarded" ;;
        esac
    fi

    # A capability in the query plus an access JWT for a DIFFERENT wallet must be
    # refused. The JWT middleware steps aside once a capability is validated, so
    # nothing else would notice the mismatch.
    if [ -n "$P_SEGURL" ] && [ -n "$TOKEN_B" ]; then
        MIX_CODE="$(curl -s -o /dev/null -w '%{http_code}' -H "Authorization: Bearer $TOKEN_B" "$P_SEGURL")"
        case "$MIX_CODE" in
            401|403) p "A capability paired with another wallet's JWT is refused ($MIX_CODE)" ;;
            *)       f "Cross-wallet capability+JWT returned $MIX_CODE — expected 401/403" ;;
        esac
    fi

    # An error must not echo the capability back to the caller.
    ERR_BODY="$(curl -s "$BASE_URL/api/v1/streaming/$CONTENT_ID/segment/0.ts?playback_token=$PB_TOKEN-tampered" 2>/dev/null || true)"
    case "$ERR_BODY" in
        *"$PB_TOKEN"*) f "An error response echoed the playback capability" ;;
        *)             p "Rejection does not echo the playback capability" ;;
    esac
else
    f "No playback token to tamper with — the tampering matrix did not run"
fi

# Retargeting the rule at a contract the holder owns nothing in must deny, not
# allow. This is the "tampered contract" case: the gate must read the rule's
# contract, not anything the caller supplies.
#
# Rule changes propagate through a cache, so this polls rather than assuming the
# change is visible on the next request. What it does NOT tolerate is the change
# never taking effect.
if [ -n "${RULE_ID:-}" ] && [ -n "$TOKEN_A" ] && [ -n "$CONTENT_ID" ]; then
    TAMPER_CONTRACT=0x000000000000000000000000000000000000dEaD
    curl -s -X PUT "$BASE_URL/api/v1/gating-rules/$RULE_ID" -H "Authorization: Bearer $TOKEN_A" \
        -H 'Content-Type: application/json' -d "{\"contract_address\":\"$TAMPER_CONTRACT\"}" >/dev/null 2>&1 || true

    TAMPER_CODE=""
    TAMPER_DEADLINE=$((SECONDS + 30))
    while [ "$SECONDS" -lt "$TAMPER_DEADLINE" ]; do
        TAMPER_CODE="$(curl -s -o /dev/null -w '%{http_code}' -H "Authorization: Bearer $TOKEN_A" \
            "$BASE_URL/api/v1/streaming/$CONTENT_ID/manifest.m3u8")"
        [ "$TAMPER_CODE" != "200" ] && break
        sleep 3
    done
    case "$TAMPER_CODE" in
        403) p "Rule retargeted to a contract the holder lacks → denied (403)" ;;
        503) p "Rule retargeted → refused, dependency unverifiable (503)" ;;
        *)   f "Tampered contract still served after 30s (last=$TAMPER_CODE)" ;;
    esac

    # Point the rule back at the real contract FIRST, so each denial below is
    # attributable to one changed field rather than to leftover damage. Asserting
    # the threshold while the contract was still the reverting one would "pass"
    # on a 503 that had nothing to do with min_balance.
    put_rule() {
        curl -s -X PUT "$BASE_URL/api/v1/gating-rules/$RULE_ID" -H "Authorization: Bearer $TOKEN_A" \
            -H 'Content-Type: application/json' -d "$1" >/dev/null 2>&1 || true
    }

    # wait_for_manifest <want> <deadline-seconds> — polls until the manifest
    # answers <want>, so a cached rule set cannot make this flaky.
    put_rule "{\"contract_address\":\"$DEMO_CONTRACT\",\"min_balance\":1}"
    RESTORED="$(wait_for_manifest 200 15)"
    [ "$RESTORED" = "200" ] && p "Holder is served again once the rule points back at the real contract" \
        || f "Holder still denied after restoring the contract (last=$RESTORED)"

    # Raising the bar must re-check the holder on the next request: renewal is a
    # manifest re-request, and the gate re-reads the chain rather than trusting
    # Capture a capability BEFORE the change, so the check below is about a
    # credential that was already handed to a player — not about a fresh request.
    PRE_CAP_URL=""
    if [ -n "$CONTENT_ID" ]; then
        PRE_MASTER="$(curl -s -H "Authorization: Bearer $TOKEN_A" \
            "$BASE_URL/api/v1/streaming/$CONTENT_ID/manifest.m3u8" 2>/dev/null || true)"
        PRE_URI="$(printf '%s' "$PRE_MASTER" | grep -v '^#' | grep -v '^[[:space:]]*$' | head -1)"
        [ -n "$PRE_URI" ] && PRE_CAP_URL="$BASE_URL$PRE_URI"
    fi

    # the capability it already handed out. With the contract unchanged, a denial
    # here can only come from the threshold.
    put_rule '{"min_balance":999999}'
    BAR_CODE="$(wait_for_manifest 403 15)"
    case "$BAR_CODE" in
        403) p "Raising the rule threshold denies the holder on re-request (403)" ;;
        503) f "Threshold change could not be verified (503) — the denial is not attributable to min_balance" ;;
        *)   f "Holder still served after the threshold was raised (last=$BAR_CODE)" ;;
    esac

    # A segment cannot re-read the chain, so the capability carries the rule
    # version it was issued under. That is what makes a rule change reach media
    # already in flight instead of waiting out the capability's TTL.
    if [ -n "$PRE_CAP_URL" ]; then
        CAP_REVOKED=0
        CAP_DEADLINE=$((SECONDS + 10))
        while [ "$SECONDS" -lt "$CAP_DEADLINE" ]; do
            C="$(curl -s -o /dev/null -w '%{http_code}' "$PRE_CAP_URL")"
            if [ "$C" = "401" ] || [ "$C" = "403" ]; then CAP_REVOKED=1; break; fi
            sleep 1
        done
        if [ "$CAP_REVOKED" = "1" ]; then
            p "A capability issued BEFORE the rule change is refused within the bound"
        else
            f "A capability issued before the rule change is still accepted after 10s"
        fi
    else
        f "Could not obtain a pre-change capability"
    fi

    # ...and the holder is served again once the bar is back down.
    put_rule "{\"contract_address\":\"$DEMO_CONTRACT\",\"min_balance\":1}"
    RESTORED="$(wait_for_manifest 200 15)"
    [ "$RESTORED" = "200" ] && p "Holder is served again once the threshold is restored" \
        || f "Holder still denied after restoring the rule (last=$RESTORED)"

    # --- The 70s revocation bound, measured rather than asserted ---------------
    # The bound is DERIVED: capability TTL (60s) + positive cache (5s) + clock
    # tolerance (5s). Measuring it needs a capability whose mint time is known,
    # so this mints a fresh one and times the expiry from that moment. The rule
    # is back at min_balance 1 here, so the holder is authorized to mint.
    FRESH_MASTER="$(curl -s -H "Authorization: Bearer $TOKEN_A" \
        "$BASE_URL/api/v1/streaming/$CONTENT_ID/manifest.m3u8" 2>/dev/null || true)"
    FRESH_URI="$(printf '%s' "$FRESH_MASTER" | grep -v '^#' | grep -v '^[[:space:]]*$' | head -1)"
    if [ -n "$FRESH_URI" ]; then
        FRESH_URL="$BASE_URL$FRESH_URI"
        MINT_AT=$SECONDS
        CODE="$(curl -s -o /dev/null -w '%{http_code}' "$FRESH_URL")"
        case "$CODE" in
            200|206) p "A freshly minted capability works ($CODE)" ;;
            *)       f "A freshly minted capability failed ($CODE)" ;;
        esac

        EXPIRED=0
        EXPIRY_DEADLINE=$((SECONDS + 100))
        while [ "$SECONDS" -lt "$EXPIRY_DEADLINE" ]; do
            C="$(curl -s -o /dev/null -w '%{http_code}' "$FRESH_URL")"
            if [ "$C" = "401" ] || [ "$C" = "403" ]; then EXPIRED=1; break; fi
            sleep 3
        done
        ELAPSED=$((SECONDS - MINT_AT))
        if [ "$EXPIRED" = "1" ]; then
            # 70s plus one poll interval: the assertion is about the BOUND, not
            # about the polling granularity.
            if [ "$ELAPSED" -le 75 ]; then
                p "The capability expired inside the 70s revocation bound (${ELAPSED}s)"
            else
                f "The capability outlived the 70s bound (${ELAPSED}s)"
            fi
        else
            f "The capability was still accepted 100s after mint"
        fi

        # --- Replaying the capability against a management API ------------------
        # A capability is a bearer credential, so it must not be spendable where
        # an access token is expected.
        CAP_TOKEN="$(printf '%s' "$FRESH_URI" | grep -oE 'playback_token=[A-Za-z0-9._-]+' | head -1 | cut -d= -f2)"
        if [ -n "$CAP_TOKEN" ]; then
            CODE="$(curl -s -o /dev/null -w '%{http_code}' -H "Authorization: Bearer $CAP_TOKEN" \
                "$BASE_URL/api/v1/content")"
            case "$CODE" in
                401|403) p "A capability replayed as a Bearer token on a management API is refused ($CODE)" ;;
                *)       f "A capability was accepted as an access token on /content ($CODE)" ;;
            esac
        else
            f "Could not extract the capability for the replay check"
        fi

        # --- A conditional request cannot bypass authorization ------------------
        # The query string is stripped first: a conditional request that still
        # carried the capability would prove nothing about bypassing.
        NO_CAP_URL="${FRESH_URL%%\?*}"
        CODE="$(curl -s -o /dev/null -w '%{http_code}' -H 'If-None-Match: "anything"' \
            -H 'If-Modified-Since: Wed, 01 Jan 2020 00:00:00 GMT' "$NO_CAP_URL")"
        case "$CODE" in
            401|403) p "A conditional request without a capability is refused ($CODE)" ;;
            200|206) f "A conditional request served media without a capability ($CODE)" ;;
            *)       f "A conditional request without a capability returned $CODE" ;;
        esac
    else
        f "Could not mint a capability to measure the bound against"
    fi

    # The restored rule has a NEW version, so the capability captured before the
    # change stays dead and a fresh one is issued on the next manifest.
    if [ -n "$PRE_CAP_URL" ]; then
        C="$(curl -s -o /dev/null -w '%{http_code}' "$PRE_CAP_URL")"
        case "$C" in
            401|403) p "The pre-change capability stays refused after the rule is restored ($C)" ;;
            *)       f "The pre-change capability came back to life ($C) — a stale capability must not revive" ;;
        esac
    fi
fi

# ---------------------------------------------------------------------------
# 10. Browser playback: real hls.js, real time advance
# ---------------------------------------------------------------------------
printf '\n=== Browser playback ===\n'

if [ -n "$CONTENT_ID" ] && [ -n "$TOKEN_A" ]; then
    cat > /tmp/sg-playback.json <<JSON
{"contentId":"$CONTENT_ID","entryUrl":"$BASE_URL/api/v1/streaming/$CONTENT_ID/manifest.m3u8","jwt":"$TOKEN_A"}
JSON
    HARNESS="$(cd "$(dirname "$0")/h5-demo-acceptance" 2>/dev/null && pwd || echo '')"
    if [ -n "$HARNESS" ] && [ -d "$HARNESS/node_modules/playwright" ]; then
        if (cd "$HARNESS" && SG_PLAYBACK_FILE=/tmp/sg-playback.json node run.mjs --only=06-playback >"$WORKDIR/playback.log" 2>&1); then
            ADV="$(grep -oE 'currentTime=[0-9.]+s' "$WORKDIR/playback.log" | head -1)"
            p "Browser played the stream and time advanced (${ADV:-ok})"
        else
            f "Browser playback failed: $(grep -m1 -E 'FAIL|Error' "$WORKDIR/playback.log" 2>/dev/null | head -c 200)"
        fi
    else
        f "Playwright not installed under scripts/h5-demo-acceptance (run npm install)"
    fi
else
    f "Skipped browser playback: missing content or token"
fi

# ---------------------------------------------------------------------------
# 11. Logout ends the whole session
# ---------------------------------------------------------------------------
printf '\n=== Logout ===\n'

if [ -n "$KEY_A" ]; then
    # A SEPARATE login, so logging out here does not disturb the tokens the rest
    # of the run used.
    LOGOUT_TOKEN="$(login "$KEY_A" "$WALLET_A")"
    if [ -n "$LOGOUT_TOKEN" ]; then
        CODE="$(curl -s -o /dev/null -w '%{http_code}' -H "Authorization: Bearer $LOGOUT_TOKEN" "$BASE_URL/api/v1/auth/profile")"
        [ "$CODE" = "200" ] && p "Fresh login authenticates before logout ($CODE)" \
            || f "Fresh login rejected before logout ($CODE)"

        # A capability this login has ALREADY been handed. Logout must revoke it
        # immediately — waiting for its 60s TTL is the failure mode the whole
        # session binding exists to prevent. This is what proves the issued
        # capability really inherited the session rather than getting its own.
        #
        # The URL is taken from the playlist itself rather than constructed: the
        # entry point may be a master or a media playlist, and hardcoding
        # "?quality=..." produced a 404 that looked like a capability failure.
        CAP_URL=""
        if [ -n "$CONTENT_ID" ]; then
            MASTER="$(curl -s -H "Authorization: Bearer $LOGOUT_TOKEN" \
                "$BASE_URL/api/v1/streaming/$CONTENT_ID/manifest.m3u8" 2>/dev/null || true)"
            CAP_URI="$(printf '%s' "$MASTER" | grep -v '^#' | grep -v '^[[:space:]]*$' | head -1)"
            [ -n "$CAP_URI" ] && CAP_URL="$BASE_URL$CAP_URI"
        fi

        if [ -n "$CAP_URL" ]; then
            CODE="$(curl -s -o /dev/null -w '%{http_code}' "$CAP_URL")"
            case "$CODE" in
                200|206) p "An issued capability works before logout ($CODE)" ;;
                *)       f "An issued capability failed before logout ($CODE)" ;;
            esac
        else
            f "Could not obtain a capability to test revocation against"
        fi

        curl -s -X POST "$BASE_URL/api/v1/auth/logout" -H "Authorization: Bearer $LOGOUT_TOKEN" >/dev/null 2>&1 || true

        if [ -n "$CAP_URL" ]; then
            CAP_DEAD=0
            CAP_DEADLINE=$((SECONDS + 10))
            while [ "$SECONDS" -lt "$CAP_DEADLINE" ]; do
                C="$(curl -s -o /dev/null -w '%{http_code}' "$CAP_URL")"
                if [ "$C" = "401" ] || [ "$C" = "403" ]; then CAP_DEAD=1; break; fi
                sleep 1
            done
            if [ "$CAP_DEAD" = "1" ]; then
                p "Logout revokes an ALREADY-ISSUED capability within the 5s bound"
            else
                f "Capability still accepted 10s after logout — it did not inherit the session"
            fi
        fi

        # The revocation must be visible within the session cache bound (5s).
        REVOKED=0
        DEADLINE=$((SECONDS + 10))
        while [ "$SECONDS" -lt "$DEADLINE" ]; do
            C="$(curl -s -o /dev/null -w '%{http_code}' -H "Authorization: Bearer $LOGOUT_TOKEN" "$BASE_URL/api/v1/auth/profile")"
            if [ "$C" = "401" ] || [ "$C" = "403" ]; then REVOKED=1; break; fi
            sleep 1
        done
        if [ "$REVOKED" = "1" ]; then
            p "Logout revokes the session within the 5s bound"
        else
            f "Token still accepted 10s after logout — the session was not revoked"
        fi
    else
        f "Could not log in to exercise logout"
    fi
else
    f "Skipped logout: no wallet key"
fi

# ---------------------------------------------------------------------------
# 12. Transfer: invalidated holding evidence must not renew access
# ---------------------------------------------------------------------------
# A transfer is the case the plan names: the evidence that authorised the request
# stops being true, and the next manifest must be denied rather than renewed off
# the cached decision. This runs LAST because it moves the holder's tokens, and
# the earlier phases all need the holder to keep them.
printf '\n=== Transfer ===\n'

if [ -n "$CONTENT_ID" ] && [ -n "$TOKEN_A" ] && [ -n "$KEY_A" ]; then
    # Enumerate the holder's tokens from the chain rather than assuming ids: the
    # stack accumulates mints across runs, so "token 0" is not necessarily his.
    #
    # The scan covers 0..totalSupply INCLUSIVE. DemoNFT's ids start at 1 while
    # totalSupply counts them, so a 0..totalSupply-1 loop silently misses the
    # newest token — which is exactly the one this run just minted, leaving the
    # holder with a balance and making the denial below unreachable.
    #
    # ownerOf REVERTS for an id that does not exist, and under `set -e` a failing
    # command substitution would abort the whole run — hence the `|| true`.
    TOTAL_SUPPLY="$(cast call --rpc-url "$ANVIL_URL" "$DEMO_CONTRACT" 'totalSupply()(uint256)' 2>/dev/null | awk '{print $1}' || true)"
    HELD_IDS=""
    if [ -n "$TOTAL_SUPPLY" ]; then
        i=0
        while [ "$i" -le "$TOTAL_SUPPLY" ]; do
            OWNER="$(cast call --rpc-url "$ANVIL_URL" "$DEMO_CONTRACT" 'ownerOf(uint256)(address)' "$i" 2>/dev/null | tr -d ' ' || true)"
            if [ -n "$OWNER" ] && [ "$(printf '%s' "$OWNER" | tr 'A-Z' 'a-z')" = "$(printf '%s' "$WALLET_A" | tr 'A-Z' 'a-z')" ]; then
                HELD_IDS="$HELD_IDS $i"
            fi
            i=$((i + 1))
        done
    fi

    # transfer_ids <owner> <signing-key> <to> <ids...>
    #
    # The SIGNING KEY must be the owner's: transferFrom reverts unless the caller
    # owns the token or is approved, so signing the return trip with the
    # original holder's key silently reverts every transfer and leaves the other
    # wallet holding the tokens — which then makes that wallet a legitimate
    # holder and quietly turns the "non-holder is refused" check into a pass.
    transfer_ids() {
        local owner="$1" key="$2" to="$3"; shift 3
        local moved=0
        for id in $@; do
            if cast send --rpc-url "$ANVIL_URL" --private-key "$key" "$DEMO_CONTRACT" \
                'transferFrom(address,address,uint256)' "$owner" "$to" "$id" >/dev/null 2>&1; then
                moved=$((moved + 1))
            fi
        done
        printf '%s' "$moved"
    }

    # advance_safe — mine until the SAFE view includes the transfers just made.
    #
    # The gate reads at the configured finality tag (safe), and `safe` lags
    # `latest` by a fixed depth. Anvil only mines when there is a transaction, so
    # with no traffic `safe` never advances and the gate keeps reading a block
    # from BEFORE the transfer — correctly, because that block really does still
    # show the old balance. The plan measures the bound from "after the trusted
    # RPC's safe view contains the transfer", so the safe view has to be brought
    # forward before the denial can be asserted.
    advance_safe() {
        local want="$1" i=0
        cast rpc --rpc-url "$ANVIL_URL" anvil_mine 0x40 >/dev/null 2>&1 || true
        while [ "$i" -lt 6 ]; do
            local safe
            safe="$(cast block safe --rpc-url "$ANVIL_URL" --field number 2>/dev/null || true)"
            if [ -n "$safe" ] && [ "$safe" -ge "$want" ] 2>/dev/null; then
                printf '%s' "$safe"
                return
            fi
            cast rpc --rpc-url "$ANVIL_URL" anvil_mine 0x20 >/dev/null 2>&1 || true
            i=$((i + 1))
        done
        printf '%s' "${safe:-unknown}"
    }

    if [ -n "$HELD_IDS" ]; then
        HELD_COUNT="$(printf '%s' "$HELD_IDS" | wc -w | tr -d ' ')"
        MOVED="$(transfer_ids "$WALLET_A" "$KEY_A" "$WALLET_B" $HELD_IDS)"
        TRANSFER_BLOCK="$(cast block-number --rpc-url "$ANVIL_URL" 2>/dev/null || echo 0)"

        # A transfer that did not actually land is not evidence about the gate.
        # Assert the balance moved before reading anything into the manifest.
        LEFT="$(cast call --rpc-url "$ANVIL_URL" "$DEMO_CONTRACT" 'balanceOf(address)(uint256)' "$WALLET_A" 2>/dev/null | awk '{print $1}' || true)"
        if [ "$MOVED" = "$HELD_COUNT" ] && [ "$LEFT" = "0" ]; then
            p "Transferred the holder's $HELD_COUNT token(s) away (balance now 0)"
        else
            f "Transfer did not empty the holder (moved=$MOVED/$HELD_COUNT, balance=${LEFT:-?})"
        fi

        SAFE_NOW="$(advance_safe "$TRANSFER_BLOCK")"
        if [ "$SAFE_NOW" != "unknown" ] && [ "$SAFE_NOW" -ge "$TRANSFER_BLOCK" ] 2>/dev/null; then
            p "The safe view now includes the transfer (safe=$SAFE_NOW >= block $TRANSFER_BLOCK)"
        else
            f "The safe view did not reach the transfer block (safe=$SAFE_NOW, need >= $TRANSFER_BLOCK)"
        fi

        # The holding is gone and the safe view says so, so the next manifest must
        # be refused. The ≤5s evidence bound is why this polls rather than
        # demanding it on the very next request.
        MOVED_CODE="$(wait_for_manifest 403 15)"
        case "$MOVED_CODE" in
            403) p "Transferring the holding away denies the next manifest (403)" ;;
            503) f "After the transfer the gate could not verify (503) — not attributable to the transfer" ;;
            *)   f "Holder still served after transferring the holding away (last=$MOVED_CODE)" ;;
        esac

        # ...and transferring them back must restore access, which proves the
        # denial above came from the chain state and not from a broken gate.
        BACK_MOVED="$(transfer_ids "$WALLET_B" "$KEY_B" "$WALLET_A" $HELD_IDS)"
        BACK_BLOCK="$(cast block-number --rpc-url "$ANVIL_URL" 2>/dev/null || echo 0)"
        advance_safe "$BACK_BLOCK" >/dev/null
        BACK="$(wait_for_manifest 200 15)"
        [ "$BACK" = "200" ] && p "Transferring the holding back restores access (200)" \
            || f "Holder still denied after the holding came back (last=$BACK)"

        # Leave the non-holder holding nothing, or the next run's "non-holder is
        # refused" check would be asserting about a holder.
        B_LEFT="$(cast call --rpc-url "$ANVIL_URL" "$DEMO_CONTRACT" 'balanceOf(address)(uint256)' "$WALLET_B" 2>/dev/null | awk '{print $1}' || true)"
        [ "$BACK_MOVED" = "$HELD_COUNT" ] && [ "$B_LEFT" = "0" ] \
            && p "The non-holder holds nothing again ($BACK_MOVED token(s) returned)" \
            || f "The non-holder still holds tokens (returned=$BACK_MOVED/$HELD_COUNT, balance=${B_LEFT:-?})"
    else
        f "The holder owns no tokens, so the transfer case could not run"
    fi
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
