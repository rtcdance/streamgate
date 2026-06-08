#!/bin/bash

set -euo pipefail

PROJECT_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$PROJECT_ROOT"

COMPOSE_FILE="docker-compose.self-test.yml"
DOCKER_CONFIG_DIR="/tmp/streamgate-docker-config"

MONOLITH_URL="http://localhost:${SG_SELFTEST_MONOLITH_HTTP_PORT:-18080}"
GATEWAY_URL="http://localhost:${SG_SELFTEST_GATEWAY_GRPC_PORT:-29090}"
MINIO_CONSOLE_URL="http://localhost:${SG_SELFTEST_MINIO_CONSOLE_PORT:-19001}"
PROMETHEUS_URL="http://localhost:${SG_SELFTEST_PROMETHEUS_PORT:-19091}"
GRAFANA_URL="http://localhost:${SG_SELFTEST_GRAFANA_PORT:-13000}"
JAEGER_URL="http://localhost:${SG_SELFTEST_JAEGER_UI_PORT:-16687}"
LOKI_URL="http://localhost:${SG_SELFTEST_LOKI_PORT:-13100}"
CONSUL_URL="http://localhost:${SG_SELFTEST_CONSUL_PORT:-18500}"

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
if [ -d "$HOME/.docker/contexts" ]; then
    cp -R "$HOME/.docker/contexts" "$DOCKER_CONFIG_DIR/contexts" 2>/dev/null || true
fi
if [ -d "$HOME/.docker/cli-plugins" ]; then
    cp -R "$HOME/.docker/cli-plugins" "$DOCKER_CONFIG_DIR/cli-plugins" 2>/dev/null || true
fi
CURRENT_CONTEXT="$(sed -n 's/.*"currentContext"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/p' "$HOME/.docker/config.json" | head -n 1 || true)"
if [ -n "${CURRENT_CONTEXT:-}" ]; then
    cat > "$DOCKER_CONFIG_DIR/config.json" <<EOF
{
  "auths": {},
  "currentContext": "$CURRENT_CONTEXT"
}
EOF
elif [ ! -f "$DOCKER_CONFIG_DIR/config.json" ]; then
    cat > "$DOCKER_CONFIG_DIR/config.json" <<'EOF'
{
  "auths": {}
}
EOF
fi
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
curl -fsS -m 10 "$GATEWAY_URL/health" >/dev/null && echo "  Gateway health: OK ($GATEWAY_URL/health)"

echo
echo "Service endpoints:"
echo "  Monolith:      $MONOLITH_URL"
echo "  Gateway:       $GATEWAY_URL"
echo "  MinIO:         $MINIO_CONSOLE_URL"
echo "  Prometheus:    $PROMETHEUS_URL"
echo "  Grafana:       $GRAFANA_URL"
echo "  Jaeger:        $JAEGER_URL"
echo "  Loki:          $LOKI_URL"
echo "  Consul:        $CONSUL_URL"

echo
echo "Useful commands:"
echo "  Logs:          ${COMPOSE_CMD[*]} logs -f streamgate"
echo "  Down:          ${COMPOSE_CMD[*]} down"
echo "  Acceptance:    scripts/run-docker-acceptance.sh $GATEWAY_URL"
echo "  AI Self-test:  scripts/ai-self-test.sh"
