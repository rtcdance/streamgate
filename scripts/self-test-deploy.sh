#!/bin/bash

set -euo pipefail

PROJECT_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$PROJECT_ROOT"

COMPOSE_FILE="docker-compose.self-test.yml"
DOCKER_CONFIG_DIR="/tmp/streamgate-docker-config"

MONOLITH_URL="http://localhost:18080"
GATEWAY_URL="http://localhost:29090"
MINIO_CONSOLE_URL="http://localhost:19001"
PROMETHEUS_URL="http://localhost:19091"
GRAFANA_URL="http://localhost:13000"
JAEGER_URL="http://localhost:16687"
CONSUL_URL="http://localhost:18500"

echo "StreamGate self-test deployment"
echo "Project root: $PROJECT_ROOT"

if ! command -v docker >/dev/null 2>&1; then
    echo "Docker is required but not installed."
    exit 1
fi

if command -v docker-compose >/dev/null 2>&1; then
    COMPOSE_CMD=(docker-compose -f "$COMPOSE_FILE")
elif docker compose version >/dev/null 2>&1; then
    COMPOSE_CMD=(docker compose -f "$COMPOSE_FILE")
else
    echo "Docker Compose is required but not installed."
    exit 1
fi

mkdir -p "$DOCKER_CONFIG_DIR"
export DOCKER_CONFIG="$DOCKER_CONFIG_DIR"

if [ ! -f "config/prometheus.yml" ]; then
    mkdir -p config
    cat > config/prometheus.yml <<'EOF'
global:
  scrape_interval: 15s
  evaluation_interval: 15s

scrape_configs:
  - job_name: 'streamgate-monolith'
    static_configs:
      - targets: ['streamgate:8080']
EOF
fi

echo
echo "Building and starting self-test stack..."
"${COMPOSE_CMD[@]}" up -d --build

echo
echo "Waiting for health checks..."
sleep 15

echo
echo "Current service status:"
"${COMPOSE_CMD[@]}" ps

echo
echo "Quick checks:"
curl -fsS -m 10 "$MONOLITH_URL/health" >/dev/null && echo "  Monolith health: OK ($MONOLITH_URL/health)"
curl -fsS -m 10 "$MONOLITH_URL/metrics" >/dev/null && echo "  Monolith metrics: OK ($MONOLITH_URL/metrics)"

echo
echo "Service endpoints:"
echo "  Monolith:      $MONOLITH_URL"
echo "  Gateway:       $GATEWAY_URL"
echo "  MinIO:         $MINIO_CONSOLE_URL"
echo "  Prometheus:    $PROMETHEUS_URL"
echo "  Grafana:       $GRAFANA_URL"
echo "  Jaeger:        $JAEGER_URL"
echo "  Consul:        $CONSUL_URL"

echo
echo "Useful commands:"
echo "  Logs:          ${COMPOSE_CMD[*]} logs -f streamgate"
echo "  Down:          ${COMPOSE_CMD[*]} down"
echo "  Acceptance:    scripts/run-docker-acceptance.sh"
