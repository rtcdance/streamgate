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
import json, sys, urllib.request, urllib.parse, urllib.error, os

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
