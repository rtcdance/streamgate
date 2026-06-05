#!/bin/bash
# StreamGate Docker Monolith Deployment
# Usage: ./scripts/docker-deploy-monolith.sh [--build] [--clean]
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
PROJECT_DIR="$(cd "$SCRIPT_DIR/.." && pwd)"
COMPOSE_FILE="$PROJECT_DIR/docker-compose.fullchain.yml"

BUILD_FLAG=""
CLEAN_FLAG=""

for arg in "$@"; do
    case $arg in
        --build) BUILD_FLAG="--build" ;;
        --clean) CLEAN_FLAG="1" ;;
        *) echo "Unknown option: $arg"; exit 1 ;;
    esac
done

echo "============================================"
echo "StreamGate Docker Monolith Deployment"
echo "============================================"

# ---- 1. Check Docker ----
echo "[1/4] Checking Docker..."
if ! docker info >/dev/null 2>&1; then
    echo "Docker daemon not running. Attempting to start..."
    open -a "Docker Desktop" 2>/dev/null || open -a "Docker" 2>/dev/null || { echo "FAIL: Cannot start Docker."; exit 1; }
    for i in $(seq 1 40); do docker info >/dev/null 2>&1 && break; sleep 3; done
fi
echo "PASS: Docker is running"

# ---- 2. Clean (optional) ----
if [ -n "$CLEAN_FLAG" ]; then
    echo "[2/4] Cleaning existing monolith containers..."
    docker compose -f "$COMPOSE_FILE" down -v 2>/dev/null || true
fi

# ---- 3. Build & Start ----
echo "[3/4] Starting monolith + infrastructure..."
BUILD_ARGS=""
[ -n "$BUILD_FLAG" ] || [ -n "$CLEAN_FLAG" ] && BUILD_ARGS="--build"
docker compose -f "$COMPOSE_FILE" up -d $BUILD_ARGS \
    postgres redis minio nats anvil h5-demo monolith 2>&1
echo "PASS: Monolith started"

# ---- 4. Wait for healthy ----
echo "[4/4] Waiting for services to be healthy..."
MAX_WAIT=90; ELAPSED=0
while [ $ELAPSED -lt $MAX_WAIT ]; do
    UNHEALTHY=$(docker ps --filter "name=sg-fc" --format "{{.Names}} {{.Status}}" | grep -v "healthy" | grep -v "no such" || true)
    [ -z "$UNHEALTHY" ] && break
    sleep 3; ELAPSED=$((ELAPSED + 3))
done
echo "PASS: All monolith services healthy (${ELAPSED}s)"

# ---- Summary ----
echo ""
echo "============================================"
echo "Monolith deployment complete!"
echo ""
echo "  API:          http://localhost:18080/health"
echo "  H5 Demo:      http://localhost:18000/"
echo "  MinIO Console: http://localhost:19001 (minioadmin/minioadmin)"
echo "  PostgreSQL:    localhost:15432 (postgres/postgres/streamgate)"
echo "  Anvil RPC:     http://localhost:18545"
echo ""
echo "Stop: docker compose -f $COMPOSE_FILE down"
echo "============================================"