#!/bin/bash

# Blue-Green Rollback Script for StreamGate
# This script rolls back to the previous version in case of deployment failure

set -e

NAMESPACE="streamgate"
ACTIVE_SERVICE="streamgate-active"

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

# Get previous version
get_previous_version() {
    local active=$(get_active_version)
    if [ "$active" = "blue" ]; then
        echo "green"
    else
        echo "blue"
    fi
}

# Switch traffic to previous version
switch_traffic() {
    local target=$1
    log_info "Switching traffic back to $target deployment..."
    
    kubectl patch service $ACTIVE_SERVICE -n $NAMESPACE -p '{"spec":{"selector":{"version":"'$target'"}}}'
    
    log_info "Traffic switched back to $target deployment"
}

# Main rollback flow
main() {
    log_info "Starting blue-green rollback"

    # Get current state
    local active=$(get_active_version)
    local previous=$(get_previous_version)

    log_info "Current active: $active"
    log_info "Rolling back to: $previous"

    # Switch traffic back
    switch_traffic $previous

    log_info "Blue-green rollback completed successfully"
    log_info "Active version is now: $previous"
}

# Run main function
main
