#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
OVERLAY="${STREAMGATE_OVERLAY:-dev}"
NS="streamgate"
TIMEOUT="${TIMEOUT:-300s}"

RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m'

pass() { echo -e "${GREEN}✓${NC} $1"; }
fail() { echo -e "${RED}✗${NC} $1"; exit 1; }
info() { echo -e "${YELLOW}▶${NC} $1"; }

info "StreamGate K8s Acceptance Tests (overlay: ${OVERLAY})"

info "[1/6] Namespace exists"
kubectl get namespace "${NS}" > /dev/null 2>&1 || fail "Namespace ${NS} not found"
pass "Namespace ${NS}"

info "[2/6] All pods are running"
kubectl wait --for=condition=ready pod -l app.kubernetes.io/name=streamgate -n "${NS}" --timeout="${TIMEOUT}" > /dev/null 2>&1 || \
kubectl wait --for=condition=ready pod -l app.kubernetes.io/name=streamgate -n "${NS}" --timeout="${TIMEOUT}" || fail "Pods not ready"
pass "All pods running"

info "[3/6] No pods in CrashLoopBackOff or Error"
BAD=$(kubectl get pods -n "${NS}" -o jsonpath='{.items[].status.containerStatuses[?(@.state.waiting.reason=="CrashLoopBackOff")].name}' 2>/dev/null || true)
[ -z "${BAD}" ] || fail "CrashLoopBackOff pods: ${BAD}"
BAD=$(kubectl get pods -n "${NS}" -o jsonpath='{.items[].status.containerStatuses[?(@.state.terminated.reason=="Error")].name}' 2>/dev/null || true)
[ -z "${BAD}" ] || fail "Error-terminated pods: ${BAD}"
pass "No unhealthy pods"

info "[4/6] All deployments have available replicas"
for dep in $(kubectl get deployments -n "${NS}" -o jsonpath='{.items[*].metadata.name}'); do
  available=$(kubectl get deployment "${dep}" -n "${NS}" -o jsonpath='{.status.availableReplicas}' 2>/dev/null || echo "0")
  desired=$(kubectl get deployment "${dep}" -n "${NS}" -o jsonpath='{.spec.replicas}' 2>/dev/null || echo "0")
  [ "${available}" -ge "${desired}" ] || fail "Deployment ${dep}: ${available}/${desired} available"
done
pass "All deployments healthy"

info "[5/6] Services have endpoints"
for svc in $(kubectl get services -n "${NS}" -o jsonpath='{.items[*].metadata.name}'); do
  endpoints=$(kubectl get endpoints "${svc}" -n "${NS}" -o jsonpath='{.subsets[*].addresses[*].ip}' 2>/dev/null || true)
  [ -n "${endpoints}" ] || fail "Service ${svc} has no endpoints"
done
pass "All services have endpoints"

info "[6/6] ConfigMap and Secrets exist"
kubectl get configmap streamgate-config -n "${NS}" > /dev/null 2>&1 || fail "ConfigMap streamgate-config missing"
kubectl get secret streamgate-secrets -n "${NS}" > /dev/null 2>&1 || fail "Secret streamgate-secrets missing"
pass "Core config resources exist"

echo ""
echo -e "${GREEN}━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━${NC}"
echo -e "${GREEN}  All K8s acceptance tests passed ✓${NC}"
echo -e "${GREEN}━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━${NC}"
echo ""
echo "Next steps:"
echo "  make k8s-status           - View deployment status"
echo "  make k8s-logs             - Stream logs"
echo "  curl http://<ingress>/health  - Health check"
