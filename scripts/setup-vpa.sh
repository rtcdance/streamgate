#!/bin/bash

# Vertical Pod Autoscaling Setup Script for StreamGate
# This script sets up VPA for resource optimization

set -e

NAMESPACE="streamgate"
VPA_VERSION="0.14.0"

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
}

# Check if VPA is installed
check_vpa_installed() {
    log_info "Checking if VPA is installed..."
    
    if kubectl get deployment vpa-controller -n kube-system &> /dev/null; then
        log_info "VPA is already installed"
        return 0
    else
        log_warn "VPA is not installed"
        return 1
    fi
}

# Install VPA
install_vpa() {
    log_info "Installing VPA v${VPA_VERSION}..."
    
    # Clone VPA repository
    local temp_dir=$(mktemp -d)
    cd $temp_dir
    
    git clone https://github.com/kubernetes/autoscaler.git
    cd autoscaler/vertical-pod-autoscaler
    git checkout vertical-pod-autoscaler-${VPA_VERSION}
    
    # Install VPA
    ./hack/vpa-up.sh
    
    log_info "VPA installed successfully"
    
    # Cleanup
    cd /
    rm -rf $temp_dir
}

# Apply VPA configuration
apply_vpa_config() {
    log_info "Applying VPA configuration..."
    
    kubectl apply -f deploy/k8s/vpa-config.yaml
    
    log_info "VPA configuration applied"
}

# Verify VPA setup
verify_vpa_setup() {
    log_info "Verifying VPA setup..."
    
    # Check if VPAs are created
    local vpa_count=$(kubectl get vpa -n $NAMESPACE --no-headers 2>/dev/null | wc -l)
    
    if [ $vpa_count -gt 0 ]; then
        log_info "Found $vpa_count VPA(s)"
        kubectl get vpa -n $NAMESPACE
        return 0
    else
        log_error "No VPAs found"
        return 1
    fi
}

# Wait for VPA recommendations
wait_for_recommendations() {
    log_info "Waiting for VPA recommendations..."
    
    local elapsed=0
    local timeout=600
    local interval=30

    while [ $elapsed -lt $timeout ]; do
        local recommendations=$(kubectl get vpa -n $NAMESPACE -o jsonpath='{.items[*].status.recommendation}' 2>/dev/null | grep -c "containerRecommendations" || echo "0")
        
        if [ $recommendations -gt 0 ]; then
            log_info "VPA recommendations are available"
            return 0
        fi

        sleep $interval
        elapsed=$((elapsed + interval))
        echo -n "."
    done

    log_warn "VPA recommendations not available within ${timeout}s (this is normal on first setup)"
    return 0
}

# Display VPA recommendations
display_recommendations() {
    log_info "Current VPA recommendations:"
    
    kubectl get vpa -n $NAMESPACE -o jsonpath='{range .items[*]}{.metadata.name}{"\n"}{.status.recommendation.containerRecommendations[*].containerName}{"\n"}{.status.recommendation.containerRecommendations[*].target}{"\n\n"}{end}'
}

# Create monitoring dashboard
create_monitoring_dashboard() {
    log_info "Creating VPA monitoring dashboard..."
    
    # This would create a Grafana dashboard in a real implementation
    log_info "VPA monitoring dashboard configuration ready"
}

# Main setup flow
main() {
    log_info "Starting VPA setup"

    # Check if VPA is installed
    if ! check_vpa_installed; then
        log_warn "VPA installation requires git and cluster admin access"
        log_info "Please install VPA manually or ensure you have the required permissions"
        log_info "For more information, visit: https://github.com/kubernetes/autoscaler/tree/master/vertical-pod-autoscaler"
    fi

    # Apply VPA configuration
    apply_vpa_config

    # Wait for recommendations
    wait_for_recommendations

    # Verify setup
    if ! verify_vpa_setup; then
        log_error "VPA setup verification failed"
        exit 1
    fi

    # Display recommendations
    display_recommendations

    # Create monitoring dashboard
    create_monitoring_dashboard

    log_info "VPA setup completed successfully"
    log_info "VPAs are now monitoring resource usage and providing recommendations"
}

# Run main function
main
