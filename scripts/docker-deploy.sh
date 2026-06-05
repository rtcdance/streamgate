#!/bin/bash
# StreamGate Docker Full-Chain Deployment (monolith + microservices)
# Usage: ./scripts/docker-deploy.sh [--build] [--clean]
#   --build   Force rebuild images (default: use cache)
#   --clean   Remove volumes and rebuild from scratch
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
echo "StreamGate Full-Chain Deployment"
echo "  Monolith (:18000) + Microservices (:18001)"
echo "============================================"

echo ""
echo "[1/5] Checking Docker..."
if ! docker info >/dev/null 2>&1; then
    echo "Docker daemon not running. Starting..."
    open -a "Docker Desktop" 2>/dev/null || open -a "Docker" 2>/dev/null || {
        echo "FAIL: Cannot start Docker."
        exit 1
    }
    for i in $(seq 1 40); do
        docker info >/dev/null 2>&1 && break
        sleep 3
    done
    if ! docker info >/dev/null 2>&1; then
        echo "FAIL: Docker daemon did not start in time."
        exit 1
    fi
fi
echo "PASS: Docker is running"

if [ -n "$CLEAN_FLAG" ]; then
    echo ""
    echo "[2/5] Cleaning existing stack..."
    docker compose -f "$COMPOSE_FILE" down -v 2>/dev/null || true
    echo "PASS: Cleaned"
else
    echo ""
    echo "[2/5] Skipping clean (use --clean to reset)"
fi

echo ""
echo "[3/5] Building and starting services..."
if [ -n "$BUILD_FLAG" ] || [ -n "$CLEAN_FLAG" ]; then
    docker compose -f "$COMPOSE_FILE" up --build -d 2>&1
else
    docker compose -f "$COMPOSE_FILE" up -d 2>&1
fi
echo "PASS: Compose started"

echo ""
echo "[4/5] Waiting for services to be healthy..."
MAX_WAIT=180
ELAPSED=0
while [ $ELAPSED -lt $MAX_WAIT ]; do
    UNHEALTHY=$(docker ps --filter "name=sg-fc" --format "{{.Names}} {{.Status}}" | grep -v "healthy" | grep -v "no such" || true)
    if [ -z "$UNHEALTHY" ]; then
        break
    fi
    sleep 5
    ELAPSED=$((ELAPSED + 5))
done

if [ $ELAPSED -ge $MAX_WAIT ]; then
    echo "WARN: Some services not healthy after ${MAX_WAIT}s"
    docker ps --filter "name=sg-fc" --format "  {{.Names}}: {{.Status}}"
else
    echo "PASS: All services healthy (${ELAPSED}s)"
fi

echo ""
echo "[5/5] Service status:"
echo ""
docker ps --filter "name=sg-fc" --format "table {{.Names}}\t{{.Status}}\t{{.Ports}}" 2>/dev/null
echo ""
echo "============================================"
echo "Deployment complete!"
echo ""
echo "  Monolith demo:      http://localhost:18000/demo/"
echo "  Microservices demo: http://localhost:18001/demo/"
echo ""
echo "  Monolith API:       http://localhost:18080/health"
echo "  API Gateway:        http://localhost:28080/health"
echo ""
echo "  MinIO Console:      http://localhost:19001 (minioadmin/minioadmin)"
echo "  PostgreSQL:         localhost:15432 (postgres/postgres/streamgate)"
echo ""
echo "  Teardown: make fullchain-teardown"
echo "============================================"
