#!/bin/bash
# StreamGate Docker Full-Chain Teardown
# Usage: ./scripts/docker-teardown.sh [--volumes]
#   --volumes   Also remove persistent data volumes
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
PROJECT_DIR="$(cd "$SCRIPT_DIR/.." && pwd)"
COMPOSE_FILE="$PROJECT_DIR/docker-compose.fullchain.yml"

VOLUMES_FLAG=""
for arg in "$@"; do
    case $arg in
        --volumes|-v) VOLUMES_FLAG="-v" ;;
        *) echo "Unknown option: $arg"; exit 1 ;;
    esac
done

echo "============================================"
echo "StreamGate Docker Teardown"
echo "============================================"

echo ""
echo "Stopping services..."
docker compose -f "$COMPOSE_FILE" down $VOLUMES_FLAG 2>&1

if [ -n "$VOLUMES_FLAG" ]; then
    echo "Volumes removed."
fi

echo ""
echo "Remaining containers:"
REMAINING=$(docker ps --filter "name=sg-fc" --format "{{.Names}}" 2>/dev/null || true)
if [ -z "$REMAINING" ]; then
    echo "  (none)"
else
    echo "$REMAINING"
fi

echo ""
echo "============================================"
echo "Teardown complete."
echo ""
echo "To redeploy:"
echo "  make fullchain-deploy"
echo "  or: ./scripts/docker-deploy.sh --build"
echo "============================================"
