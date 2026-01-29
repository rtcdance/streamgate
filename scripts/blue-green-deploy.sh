#!/bin/bash

# Blue-Green Deployment Script for StreamGate
# This script manages blue-green deployments with zero downtime

set -e

NAMESPACE="streamgate"
BLUE_DEPLOYMENT="streamgate-blue"
GREEN_DEPLOYMENT="streamgate-green"
ACTIVE_SERVICE="streamgate-active"
IMAGE="${1:-streamgate:latest}"
TIMEOUT="${2:-300}"

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

# Get current active version
get_active_version() {
    kubectl get service $ACTIVE_SERVICE -n $NAMESPACE -o jsonpath='{.spec.selector.version}' 2>/dev/null || echo "blue"
}

# Get inactive version
get_inactive_version() {
    local active=$(get_active_version)
    if [ "$active" = "blue" ]; then
        echo "green"
    else
        echo "blue"
    fi
}

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

# Switch traffic to deployment
switch_traffic() {
    local target=$1
    log_info "Switching traffic to $target deployment..."
    
    kubectl patch service $ACTIVE_SERVICE -n $NAMESPACE -p '{"spec":{"selector":{"version":"'$target'"}}}'
    
    log_info "Traffic switched to $target deployment"
}

# Deploy to inactive environment
deploy_to_inactive() {
    local inactive=$(get_inactive_version)
    local active=$(get_active_version)

    log_info "Current active version: $active"
    log_info "Deploying to inactive version: $inactive"

    # Update the inactive deployment with new image
    kubectl set image deployment/$inactive-deployment streamgate=$IMAGE -n $NAMESPACE --record

    # Wait for deployment to be ready
    if ! wait_for_deployment "$inactive-deployment" $TIMEOUT; then
        log_error "Failed to deploy to $inactive environment"
        return 1
    fi

    # Check health
    if ! check_deployment_health "$inactive-deployment"; then
        log_error "Health check failed for $inactive environment"
        return 1
    fi

    return 0
}

# Main deployment flow
main() {
    log_info "Starting blue-green deployment"
    log_info "Image: $IMAGE"
    log_info "Timeout: ${TIMEOUT}s"

    # Get current state
    local active=$(get_active_version)
    local inactive=$(get_inactive_version)

    log_info "Active: $active, Inactive: $inactive"

    # Deploy to inactive
    if ! deploy_to_inactive; then
        log_error "Deployment failed"
        exit 1
    fi

    # Switch traffic
    switch_traffic $inactive

    log_info "Blue-green deployment completed successfully"
    log_info "Active version is now: $inactive"
}

# Run main function
main
