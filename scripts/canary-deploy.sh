#!/bin/bash

# Canary Deployment Script for StreamGate
# This script manages canary deployments with gradual traffic shifting

set -e

NAMESPACE="streamgate"
STABLE_DEPLOYMENT="streamgate-stable"
CANARY_DEPLOYMENT="streamgate-canary"
IMAGE="${1:-streamgate:latest}"
TIMEOUT="${2:-300}"
TRAFFIC_STEPS=(5 10 25 50 100)
STEP_DURATION="${3:-60}"

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

log_info() {
    echo -e "${GREEN}[INFO]${NC} $1"
}

log_warn() {
    echo -e "${YELLOW}[WARN]${NC} $1"
}

log_error() {
    echo -e "${RED}[ERROR]${NC} $1"
}

# Check if kubectl is available
if ! command -v kubectl &> /dev/null; then
    log_error "kubectl is not installed"
    exit 1
fi

# Wait for deployment to be ready
wait_for_deployment() {
    local deployment=$1
    local timeout=$2
    local elapsed=0
    local interval=5

    log_info "Waiting for deployment $deployment to be ready (timeout: ${timeout}s)..."

    while [ $elapsed -lt $timeout ]; do
        local ready=$(kubectl get deployment $deployment -n $NAMESPACE -o jsonpath='{.status.conditions[?(@.type=="Available")].status}' 2>/dev/null || echo "False")
        
        if [ "$ready" = "True" ]; then
            log_info "Deployment $deployment is ready"
            return 0
        fi

        sleep $interval
        elapsed=$((elapsed + interval))
        echo -n "."
    done

    log_error "Deployment $deployment did not become ready within ${timeout}s"
    return 1
}

# Check deployment health
check_deployment_health() {
    local deployment=$1
    local replicas=$(kubectl get deployment $deployment -n $NAMESPACE -o jsonpath='{.spec.replicas}')
    local ready=$(kubectl get deployment $deployment -n $NAMESPACE -o jsonpath='{.status.readyReplicas}')

    if [ "$ready" -eq "$replicas" ]; then
        log_info "Deployment $deployment health check passed ($ready/$replicas replicas ready)"
        return 0
    else
        log_error "Deployment $deployment health check failed ($ready/$replicas replicas ready)"
        return 1
    fi
}

# Get error rate from metrics
get_error_rate() {
    local deployment=$1
    # This would query Prometheus in a real implementation
    # For now, return 0 (no errors)
    echo "0"
}

# Get latency from metrics
get_latency() {
    local deployment=$1
    # This would query Prometheus in a real implementation
    # For now, return 0 (no latency issues)
    echo "0"
}

# Check canary metrics
check_canary_metrics() {
    local error_rate=$(get_error_rate $CANARY_DEPLOYMENT)
    local latency=$(get_latency $CANARY_DEPLOYMENT)

    log_info "Canary metrics - Error rate: ${error_rate}%, Latency: ${latency}ms"

    # Fail if error rate > 5% or latency > 500ms
    if (( $(echo "$error_rate > 5" | bc -l) )); then
        log_error "Canary error rate too high: ${error_rate}%"
        return 1
    fi

    if (( $(echo "$latency > 500" | bc -l) )); then
        log_error "Canary latency too high: ${latency}ms"
        return 1
    fi

    return 0
}

# Deploy canary
deploy_canary() {
    log_info "Deploying canary version..."

    # Scale up canary deployment
    kubectl scale deployment $CANARY_DEPLOYMENT -n $NAMESPACE --replicas=1

    # Update canary image
    kubectl set image deployment/$CANARY_DEPLOYMENT streamgate=$IMAGE -n $NAMESPACE --record

    # Wait for canary to be ready
    if ! wait_for_deployment $CANARY_DEPLOYMENT $TIMEOUT; then
        log_error "Failed to deploy canary"
        return 1
    fi

    # Check canary health
    if ! check_deployment_health $CANARY_DEPLOYMENT; then
        log_error "Canary health check failed"
        return 1
    fi

    return 0
}

# Shift traffic to canary
shift_traffic() {
    local traffic_percent=$1
    log_info "Shifting $traffic_percent% traffic to canary..."

    # Calculate stable and canary replicas
    local total_replicas=$(kubectl get deployment $STABLE_DEPLOYMENT -n $NAMESPACE -o jsonpath='{.spec.replicas}')
    local canary_replicas=$((total_replicas * traffic_percent / 100))
    
    if [ $canary_replicas -lt 1 ] && [ $traffic_percent -gt 0 ]; then
        canary_replicas=1
    fi

    # Scale canary deployment
    kubectl scale deployment $CANARY_DEPLOYMENT -n $NAMESPACE --replicas=$canary_replicas

    log_info "Canary replicas: $canary_replicas, Stable replicas: $total_replicas"
}

# Promote canary to stable
promote_canary() {
    log_info "Promoting canary to stable..."

    # Get canary image
    local canary_image=$(kubectl get deployment $CANARY_DEPLOYMENT -n $NAMESPACE -o jsonpath='{.spec.template.spec.containers[0].image}')

    # Update stable deployment
    kubectl set image deployment/$STABLE_DEPLOYMENT streamgate=$canary_image -n $NAMESPACE --record

    # Wait for stable to be ready
    if ! wait_for_deployment $STABLE_DEPLOYMENT $TIMEOUT; then
        log_error "Failed to promote canary to stable"
        return 1
    fi

    # Scale down canary
    kubectl scale deployment $CANARY_DEPLOYMENT -n $NAMESPACE --replicas=0

    log_info "Canary promoted to stable"
    return 0
}

# Rollback canary
rollback_canary() {
    log_info "Rolling back canary deployment..."

    # Scale down canary
    kubectl scale deployment $CANARY_DEPLOYMENT -n $NAMESPACE --replicas=0

    log_info "Canary rolled back"
}

# Main canary deployment flow
main() {
    log_info "Starting canary deployment"
    log_info "Image: $IMAGE"
    log_info "Timeout: ${TIMEOUT}s"
    log_info "Step duration: ${STEP_DURATION}s"

    # Deploy canary
    if ! deploy_canary; then
        log_error "Canary deployment failed"
        exit 1
    fi

    # Gradually shift traffic
    for traffic_percent in "${TRAFFIC_STEPS[@]}"; do
        log_info "Traffic shift step: $traffic_percent%"

        # Shift traffic
        shift_traffic $traffic_percent

        # Wait for step duration
        log_info "Monitoring for ${STEP_DURATION}s..."
        sleep $STEP_DURATION

        # Check metrics
        if ! check_canary_metrics; then
            log_error "Canary metrics check failed, rolling back..."
            rollback_canary
            exit 1
        fi

        log_info "Traffic shift to $traffic_percent% completed successfully"
    done

    # Promote canary to stable
    if ! promote_canary; then
        log_error "Failed to promote canary to stable"
        exit 1
    fi

    log_info "Canary deployment completed successfully"
}

# Run main function
main
