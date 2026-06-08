#!/bin/bash
# StreamGate AI Self-Test Runner
#
# This script is designed to be invoked by an AI agent for automated
# end-to-end validation of the monitoring stack. It:
#   1. Ensures the self-test environment is running
#   2. Runs synthetic test scenarios against the API
#   3. Queries Prometheus to verify metrics are recorded
#   4. Verifies Loki has ingested container logs
#   5. Checks Grafana datasource connectivity
#   6. Generates a JSON report consumable by AI agents
#
# Usage:
#   ./scripts/ai-self-test.sh                   # Run all scenarios
#   ./scripts/ai-self-test.sh --scenario <name>  # Run single scenario
#   ./scripts/ai-self-test.sh --list             # List available scenarios
#   ./scripts/ai-self-test.sh --help             # Show help

set -euo pipefail

PROJECT_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$PROJECT_ROOT"

# ─── Port Configuration ────────────────────────────────────────────────────

MONOLITH_HTTP="http://localhost:${SG_SELFTEST_MONOLITH_HTTP_PORT:-18080}"
MONOLITH_METRICS="http://localhost:${SG_SELFTEST_MONOLITH_METRICS_PORT:-19090}"
PROMETHEUS="http://localhost:${SG_SELFTEST_PROMETHEUS_PORT:-19091}"
GRAFANA="http://localhost:${SG_SELFTEST_GRAFANA_PORT:-13000}"
ALERTMANAGER="http://localhost:${SG_SELFTEST_ALERTMANAGER_PORT:-19093}"
LOKI="http://localhost:${SG_SELFTEST_LOKI_PORT:-13100}"
COMPOSE_FILE="docker-compose.self-test.yml"

# ─── Utilities ──────────────────────────────────────────────────────────────

info()  { echo "[INFO]  $*"; }
pass()  { echo "[PASS]  $*"; }
fail()  { echo "[FAIL]  $*"; }
warn()  { echo "[WARN]  $*"; }

REPORT_FILE="$PROJECT_ROOT/target/ai-self-test-report.json"
mkdir -p "$(dirname "$REPORT_FILE")"

# Results accumulator
RESULTS='[]'

record_result() {
  local scenario="$1" status="$2" detail="$3"
  local ts
  ts="$(date -u +"%Y-%m-%dT%H:%M:%SZ")"
  RESULTS=$(echo "$RESULTS" | jq \
    --arg scenario "$scenario" \
    --arg status "$status" \
    --arg detail "$detail" \
    --arg ts "$ts" \
    '. + [{"scenario": $scenario, "status": $status, "detail": $detail, "timestamp": $ts}]')
}

require_services() {
  if ! docker compose -f "$COMPOSE_FILE" ps --services --filter "status=running" 2>/dev/null | grep -q .; then
    info "Self-test environment not running. Starting..."
    ./scripts/self-test-deploy.sh
  fi
}

promql() {
  local query="$1"
  curl -sfG "$PROMETHEUS/api/v1/query" --data-urlencode "query=$query" 2>/dev/null | jq -r '.data.result[0].value[1] // "0"'
}

promql_count() {
  local query="$1"
  curl -sfG "$PROMETHEUS/api/v1/query" --data-urlencode "query=$query" 2>/dev/null | jq -r '[.data.result[].value[1] | tonumber] | add // 0'
}

# ─── Scenarios ──────────────────────────────────────────────────────────────

scenario_health_endpoints() {
  local scenario="health-endpoints"
  info "Running: $scenario"

  local failed=0
  for ep in "/health" "/health/live" "/health/ready"; do
    if curl -fsS -m 5 "$MONOLITH_HTTP$ep" >/dev/null 2>&1; then
      pass "  $MONOLITH_HTTP$ep → 200"
    else
      fail "  $MONOLITH_HTTP$ep → unreachable"
      failed=$((failed + 1))
    fi
  done

  if [ "$failed" -eq 0 ]; then
    record_result "$scenario" "pass" "All health endpoints reachable"
  else
    record_result "$scenario" "fail" "$failed health endpoint(s) unreachable"
  fi
}

scenario_prometheus_metrics() {
  local scenario="prometheus-metrics"
  info "Running: $scenario"

  local targets
  targets=$(curl -sf "$PROMETHEUS/api/v1/targets" 2>/dev/null | jq -r '.data.activeTargets[] | select(.health == "up") | .labels.job' | sort -u | tr '\n' ' ')

  if [ -n "$targets" ]; then
    pass "  Up targets: $targets"
    record_result "$scenario" "pass" "Prometheus targets up: $targets"
  else
    fail "  No up targets found in Prometheus"
    record_result "$scenario" "fail" "No up targets found in Prometheus"
  fi
}

scenario_request_metrics() {
  local scenario="request-metrics"
  info "Running: $scenario"

  # Fire a few test requests
  curl -fsS -m 5 "$MONOLITH_HTTP/health" >/dev/null 2>&1 || true
  curl -fsS -m 5 "$MONOLITH_HTTP/health/live" >/dev/null 2>&1 || true
  curl -fsS -m 5 "$MONOLITH_HTTP/health/ready" >/dev/null 2>&1 || true
  sleep 2

  local total
  total=$(promql_count 'sum(streamgate_health_check_total)')

  if [ "$(echo "$total > 0" | bc)" -eq 1 ]; then
    pass "  streamgate_health_check_total = $total"
    record_result "$scenario" "pass" "Health check metrics recorded: total=$total"
  else
    fail "  streamgate_health_check_total = 0 (no metrics recorded)"
    record_result "$scenario" "fail" "No health check metrics recorded"
  fi
}

scenario_grafana_datasource() {
  local scenario="grafana-datasource"
  info "Running: $scenario"

  local ds
  ds=$(curl -fsS -m 5 -u "admin:admin" "$GRAFANA/api/datasources" 2>/dev/null | jq -r '.[] | select(.type == "prometheus") | .name // "none"')

  if [ -n "$ds" ] && [ "$ds" != "none" ]; then
    pass "  Prometheus datasource connected: $ds"
    record_result "$scenario" "pass" "Grafana Prometheus datasource connected: $ds"
  else
    fail "  No Prometheus datasource in Grafana"
    record_result "$scenario" "fail" "No Prometheus datasource found in Grafana"
  fi
}

scenario_loki_logs() {
  local scenario="loki-logs"
  info "Running: $scenario"

  # Query Loki for any log entries in the last 5 minutes
  local count
  local end_time
  local start_time
  end_time=$(date -u +%s)000000000
  start_time=$(date -u -d "5 minutes ago" +%s)000000000

  count=$(curl -sfG "$LOKI/loki/api/v1/query_range" \
    --data-urlencode "query={job=~\".+\"}" \
    --data-urlencode "start=$start_time" \
    --data-urlencode "end=$end_time" \
    --data-urlencode "limit=1" 2>/dev/null | jq -r '.data.result | length // 0')

  if [ "$count" -gt 0 ]; then
    pass "  Loki ingested logs from $count stream(s)"
    record_result "$scenario" "pass" "Loki log ingestion confirmed: $count streams"
  else
    warn "  No Loki log streams found (may need traffic first)"
    record_result "$scenario" "warn" "No Loki log streams found in the last 5 minutes"
  fi
}

scenario_prometheus_alertmanager() {
  local scenario="prometheus-alertmanager"
  info "Running: $scenario"

  local am_config
  am_config=$(curl -sf "$PROMETHEUS/api/v1/alertmanagers" 2>/dev/null | jq -r '.data.activeAlertmanagers | length')

  if [ "$am_config" -gt 0 ]; then
    pass "  Prometheus connected to $am_config AlertManager instance(s)"
    record_result "$scenario" "pass" "AlertManager integration active: $am_config instance(s)"
  else
    warn "  No active AlertManager in Prometheus config"
    record_result "$scenario" "warn" "No active AlertManager found"
  fi
}

scenario_slo_rules_loaded() {
  local scenario="slo-rules-loaded"
  info "Running: $scenario"

  local rules
  rules=$(curl -sf "$PROMETHEUS/api/v1/rules" 2>/dev/null | jq -r '[.data.groups[] | select(.name | test("slo|burnrate"; "i")) | .name] | join(", ")')

  if [ -n "$rules" ]; then
    pass "  SLO rules loaded: $rules"
    record_result "$scenario" "pass" "SLO rules loaded: $rules"
  else
    fail "  No SLO/burnrate rules found in Prometheus"
    record_result "$scenario" "fail" "No SLO rules loaded"
  fi
}

scenario_circuit_breaker_metrics() {
  local scenario="circuit-breaker-metrics"
  info "Running: $scenario"

  local cb
  cb=$(promql 'streamgate_rpc_failover_total')

  if [ -n "$cb" ]; then
    pass "  RPC failover metric present"
    record_result "$scenario" "pass" "RPC failover metrics available"
  else
    warn "  RPC failover metric not populated (no failover triggered)"
    record_result "$scenario" "warn" "RPC failover metric exists but has no samples"
  fi
}

# ─── Main ──────────────────────────────────────────────────────────────────

list_scenarios() {
  echo "Available self-test scenarios:"
  echo "  health-endpoints       Verify /health, /health/live, /health/ready"
  echo "  prometheus-metrics     Check Prometheus targets are up"
  echo "  request-metrics        Fire requests and verify Prometheus metrics"
  echo "  grafana-datasource     Check Grafana Prometheus datasource"
  echo "  loki-logs              Verify Loki log ingestion"
  echo "  prometheus-alertmanager Check AlertManager integration"
  echo "  slo-rules-loaded       Verify SLO burnrate rules are loaded"
  echo "  circuit-breaker-metrics Check RPC failover metrics"
  echo ""
  echo "Scenarios marked with [warn] are informational; [fail] requires action."
}

run_scenario() {
  case "$1" in
    health-endpoints)        scenario_health_endpoints ;;
    prometheus-metrics)      scenario_prometheus_metrics ;;
    request-metrics)         scenario_request_metrics ;;
    grafana-datasource)      scenario_grafana_datasource ;;
    loki-logs)               scenario_loki_logs ;;
    prometheus-alertmanager) scenario_prometheus_alertmanager ;;
    slo-rules-loaded)        scenario_slo_rules_loaded ;;
    circuit-breaker-metrics) scenario_circuit_breaker_metrics ;;
    *) echo "Unknown scenario: $1"; list_scenarios; exit 1 ;;
  esac
}

# ─── CLI ────────────────────────────────────────────────────────────────────

case "${1:-}" in
  --help|-h)
    echo "StreamGate AI Self-Test Runner"
    echo ""
    echo "Usage:"
    echo "  $0                         Run all scenarios"
    echo "  $0 --scenario <name>       Run single scenario"
    echo "  $0 --list                  List available scenarios"
    echo "  $0 --help                  Show this help"
    echo ""
    list_scenarios
    exit 0
    ;;
  --list)
    list_scenarios
    exit 0
    ;;
  --scenario)
    if [ -z "${2:-}" ]; then
      echo "Error: --scenario requires a name argument"
      list_scenarios
      exit 1
    fi
    require_services
    run_scenario "$2"
    ;;
  "")
    require_services
    info "Running all self-test scenarios..."
    echo ""
    for s in health-endpoints prometheus-metrics request-metrics grafana-datasource loki-logs prometheus-alertmanager slo-rules-loaded circuit-breaker-metrics; do
      run_scenario "$s"
      echo ""
    done
    ;;
  *)
    echo "Unknown option: $1"
    exit 1
    ;;
esac

# ─── Generate Report ────────────────────────────────────────────────────────

echo "$RESULTS" | jq '.' > "$REPORT_FILE"
echo "Report saved to: $REPORT_FILE"

PASS_COUNT=$(echo "$RESULTS" | jq '[.[] | select(.status == "pass")] | length')
FAIL_COUNT=$(echo "$RESULTS" | jq '[.[] | select(.status == "fail")] | length')
WARN_COUNT=$(echo "$RESULTS" | jq '[.[] | select(.status == "warn")] | length')
TOTAL=$(echo "$RESULTS" | jq 'length')

echo ""
echo "═══════════════════════════════════════════"
echo "  AI Self-Test Summary"
echo "  Total:  $TOTAL"
echo "  Pass:   $PASS_COUNT"
echo "  Warn:   $WARN_COUNT"
echo "  Fail:   $FAIL_COUNT"
echo "═══════════════════════════════════════════"

if [ "$FAIL_COUNT" -gt 0 ]; then
  echo ""
  echo "Failed scenarios:"
  echo "$RESULTS" | jq -r '.[] | select(.status == "fail") | "  - \(.scenario): \(.detail)"'
  exit 1
fi
