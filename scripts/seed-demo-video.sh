#!/usr/bin/env bash
# seed-demo-video.sh — Generate a synthetic HLS test stream and upload to MinIO
# Prerequisites: ffmpeg (required), mc or curl (for upload)
set -euo pipefail

MINIO_ENDPOINT="${MINIO_ENDPOINT:-localhost:29000}"
MINIO_ACCESS_KEY="${MINIO_ACCESS_KEY:-minioadmin}"
MINIO_SECRET_KEY="${MINIO_SECRET_KEY:-minioadmin}"
MINIO_BUCKET="${MINIO_BUCKET:-streamgate}"
CONTENT_ID="${CONTENT_ID:-demo001}"
TMPDIR="${TMPDIR:-/tmp}/streamgate-seed"

echo "==> StreamGate Demo Video Seed"
echo "    MinIO: ${MINIO_ENDPOINT}  Bucket: ${MINIO_BUCKET}  ContentID: ${CONTENT_ID}"

# --- 1. Ensure mc (MinIO Client) is available ---
ensure_mc() {
    if command -v mc &>/dev/null; then
        return 0
    fi
    echo "    mc not found, downloading..."
    local arch
    case "$(uname -m)" in
        x86_64|amd64) arch="amd64" ;;
        arm64|aarch64) arch="arm64" ;;
        *) echo "Unsupported arch $(uname -m)"; exit 1 ;;
    esac
    curl -sL "https://dl.min.io/client/mc/release/darwin-${arch}/mc" -o /usr/local/bin/mc 2>/dev/null || {
        # Fallback: download to local dir
        curl -sL "https://dl.min.io/client/mc/release/darwin-${arch}/mc" -o "${TMPDIR}/mc"
        chmod +x "${TMPDIR}/mc"
        MC_BIN="${TMPDIR}/mc"
        return 0
    }
    chmod +x /usr/local/bin/mc
    MC_BIN="mc"
}

MC_BIN="${MC_BIN:-mc}"
ensure_mc

# --- 2. Configure mc alias ---
echo "==> Configuring MinIO alias"
"${MC_BIN}" alias set sglocal "http://${MINIO_ENDPOINT}" "${MINIO_ACCESS_KEY}" "${MINIO_SECRET_KEY}" 2>/dev/null || true

# --- 3. Create bucket if not exists ---
echo "==> Ensuring bucket ${MINIO_BUCKET} exists"
"${MC_BIN}" mb "sglocal/${MINIO_BUCKET}" 2>/dev/null || true

# --- 4. Generate synthetic test video with FFmpeg ---
echo "==> Generating test pattern video (10 seconds, 720p)"
mkdir -p "${TMPDIR}/${CONTENT_ID}"
ffmpeg -y -f lavfi -i "testsrc2=duration=10:size=1280x720:rate=30" \
       -f lavfi -i "sine=frequency=440:duration=10" \
       -c:v libx264 -preset fast -crf 23 \
       -c:a aac -b:a 128k \
       "${TMPDIR}/${CONTENT_ID}/original.mp4" 2>/dev/null

echo "==> Transcoding to HLS segments"
mkdir -p "${TMPDIR}/${CONTENT_ID}/hls"

# 720p HLS
ffmpeg -y -i "${TMPDIR}/${CONTENT_ID}/original.mp4" \
       -c:v libx264 -preset fast -s 1280x720 -b:v 2500k \
       -c:a aac -b:a 128k \
       -f hls -hls_time 4 -hls_list_size 0 \
       -hls_segment_filename "${TMPDIR}/${CONTENT_ID}/hls/720p_%03d.ts" \
       "${TMPDIR}/${CONTENT_ID}/hls/720p.m3u8" 2>/dev/null

# 480p HLS
ffmpeg -y -i "${TMPDIR}/${CONTENT_ID}/original.mp4" \
       -c:v libx264 -preset fast -s 854x480 -b:v 1000k \
       -c:a aac -b:a 128k \
       -f hls -hls_time 4 -hls_list_size 0 \
       -hls_segment_filename "${TMPDIR}/${CONTENT_ID}/hls/480p_%03d.ts" \
       "${TMPDIR}/${CONTENT_ID}/hls/480p.m3u8" 2>/dev/null

# Master manifest (multi-bitrate)
cat > "${TMPDIR}/${CONTENT_ID}/hls/master.m3u8" <<'M3U8'
#EXTM3U
#EXT-X-STREAM-INF:BANDWIDTH=2800000,RESOLUTION=1280x720
720p.m3u8
#EXT-X-STREAM-INF:BANDWIDTH=1200000,RESOLUTION=854x480
480p.m3u8
M3U8

echo "==> Generated files:"
ls -la "${TMPDIR}/${CONTENT_ID}/hls/"

# --- 5. Upload to MinIO ---
echo "==> Uploading segments to MinIO"
"${MC_BIN}" cp --recursive "${TMPDIR}/${CONTENT_ID}/hls/" "sglocal/${MINIO_BUCKET}/${CONTENT_ID}/" 2>/dev/null

# Also upload the original file
"${MC_BIN}" cp "${TMPDIR}/${CONTENT_ID}/original.mp4" "sglocal/${MINIO_BUCKET}/${CONTENT_ID}/original.mp4" 2>/dev/null

echo "==> Upload complete. Verifying..."
"${MC_BIN}" ls "sglocal/${MINIO_BUCKET}/${CONTENT_ID}/" 2>/dev/null

echo ""
echo "==> Demo video seeded successfully!"
echo "    Content ID: ${CONTENT_ID}"
echo "    MinIO path: ${MINIO_BUCKET}/${CONTENT_ID}/"
echo "    Master manifest: ${MINIO_BUCKET}/${CONTENT_ID}/master.m3u8"
echo "    Access via API: GET /api/v1/stream/${CONTENT_ID}/manifest.m3u8"
