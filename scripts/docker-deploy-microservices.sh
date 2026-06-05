#!/bin/bash
# StreamGate Docker Microservices Deployment
# Usage: ./scripts/docker-deploy-microservices.sh [--build] [--clean]
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
echo "StreamGate Docker Microservices Deployment"
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
    echo "[2/4] Cleaning existing containers..."
    docker compose -f "$COMPOSE_FILE" down -v 2>/dev/null || true
fi

# ---- 3. Build & Start ----
echo "[3/4] Starting microservices + infrastructure..."
BUILD_ARGS=""
[ -n "$BUILD_FLAG" ] || [ -n "$CLEAN_FLAG" ] && BUILD_ARGS="--build"
docker compose -f "$COMPOSE_FILE" up -d $BUILD_ARGS \
    postgres redis minio nats anvil consul \
    h5-demo \
    auth cache metadata monitor streaming transcoder upload worker api-gateway 2>&1
echo "PASS: Microservices started"

# ---- 4. Wait for healthy ----
echo "[4/4] Waiting for services to be healthy..."
MAX_WAIT=120; ELAPSED=0
while [ $ELAPSED -lt $MAX_WAIT ]; do
    UNHEALTHY=$(docker ps --filter "name=sg-fc" --format "{{.Names}} {{.Status}}" | grep -v "healthy" | grep -v "no such" || true)
    [ -z "$UNHEALTHY" ] && break
    sleep 3; ELAPSED=$((ELAPSED + 3))
done
echo "PASS: All microservices healthy (${ELAPSED}s)"

# ---- Summary ----
echo ""
echo "============================================"
echo "Microservices deployment complete!"
echo ""
echo "  API Gateway:   http://localhost:28080/health"
echo "  Auth:          http://localhost:28086"
echo "  Upload:        http://localhost:28082"
echo "  Transcoder:    http://localhost:28081"
echo "  Streaming:     http://localhost:28083"
echo "  Metadata:      http://localhost:28084"
echo "  Cache:         http://localhost:28085"
echo "  Worker:        http://localhost:28087"
echo "  Monitor:       http://localhost:28088"
echo "  H5 Demo:       http://localhost:18000/"
echo ""
echo "  Jaeger:        http://localhost:26686"
echo "  Grafana:       http://localhost:23000"
echo "  Prometheus:    http://localhost:29090"
echo "  MinIO Console: http://localhost:29001 (minioadmin/minioadmin)"
echo "  PostgreSQL:    localhost:25432"
echo ""
echo "Stop: docker compose -f $COMPOSE_FILE down"
echo "============================================"