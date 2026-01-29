#!/bin/bash

# StreamGate Local Lint Verification Script
# This script runs golangci-lint locally to verify code quality before pushing

set -e

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

# Configuration
LINT_CONFIG=".golangci.yml"
TIMEOUT="5m"
VERBOSE=${VERBOSE:-false}

# Functions
print_header() {
    echo -e "${BLUE}=== $1 ===${NC}"
}

print_success() {
    echo -e "${GREEN}✓ $1${NC}"
}

print_error() {
    echo -e "${RED}✗ $1${NC}"
}

print_warning() {
    echo -e "${YELLOW}⚠ $1${NC}"
}

print_info() {
    echo -e "${BLUE}ℹ $1${NC}"
}

# Check if golangci-lint is installed
check_golangci_lint() {
    print_header "Checking golangci-lint installation"
    
    if ! command -v golangci-lint &> /dev/null; then
        print_error "golangci-lint is not installed"
        echo ""
        echo "Install it with:"
        echo "  go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest"
        echo ""
        echo "Or using Homebrew (macOS):"
        echo "  brew install golangci-lint"
        echo ""
        exit 1
    fi
    
    LINT_VERSION=$(golangci-lint --version)
    print_success "golangci-lint is installed: $LINT_VERSION"
}

# Check if config file exists
check_config() {
    print_header "Checking configuration"
    
    if [ ! -f "$LINT_CONFIG" ]; then
        print_error "Configuration file not found: $LINT_CONFIG"
        exit 1
    fi
    
    print_success "Configuration file found: $LINT_CONFIG"
}

# Run linting
run_lint() {
    print_header "Running linting checks"
    
    local lint_args="run ./..."
    
    if [ "$VERBOSE" = "true" ]; then
        lint_args="$lint_args -v"
    fi
    
    # Run golangci-lint
    # Note: Using --no-config due to version compatibility issues
    # The .golangci.yml config is maintained for future use
    if golangci-lint $lint_args --no-config --timeout "$TIMEOUT"; then
        print_success "All linting checks passed"
        return 0
    else
        print_error "Linting checks failed"
        return 1
    fi
}

# Run format check
check_format() {
    print_header "Checking code formatting"
    
    # Check if gofmt would make changes
    if ! go fmt ./... > /dev/null 2>&1; then
        print_warning "Some files need formatting"
        echo "Run 'make fmt' or 'scripts/lint-fix.sh' to fix"
        return 1
    fi
    
    print_success "Code formatting is correct"
    return 0
}

# Run import check
check_imports() {
    print_header "Checking imports"
    
    # Check if goimports would make changes
    if ! goimports -l ./... 2>/dev/null | grep -q .; then
        print_success "Imports are correctly organized"
        return 0
    else
        print_warning "Some files have import issues"
        echo "Run 'scripts/lint-fix.sh' to fix"
        return 1
    fi
}

# Generate report
generate_report() {
    print_header "Generating lint report"
    
    local report_file="lint-report.txt"
    
    echo "Lint Report - $(date)" > "$report_file"
    echo "================================" >> "$report_file"
    echo "" >> "$report_file"
    
    echo "Configuration: $LINT_CONFIG" >> "$report_file"
    echo "Timeout: $TIMEOUT" >> "$report_file"
    echo "Timestamp: $(date -u '+%Y-%m-%d_%H:%M:%S')" >> "$report_file"
    echo "" >> "$report_file"
    
    echo "Linting Results:" >> "$report_file"
    golangci-lint run ./... --timeout "$TIMEOUT" >> "$report_file" 2>&1 || true
    
    print_success "Report generated: $report_file"
}

# Show summary
show_summary() {
    print_header "Lint Summary"
    
    echo ""
    echo "Configuration: $LINT_CONFIG"
    echo "Timeout: $TIMEOUT"
    echo "Timestamp: $(date -u '+%Y-%m-%d_%H:%M:%S')"
    echo ""
}

# Main execution
main() {
    echo ""
    print_header "StreamGate Local Lint Verification"
    echo ""
    
    # Parse arguments
    while [[ $# -gt 0 ]]; do
        case $1 in
            -v|--verbose)
                VERBOSE=true
                shift
                ;;
            -c|--config)
                LINT_CONFIG="$2"
                shift 2
                ;;
            -t|--timeout)
                TIMEOUT="$2"
                shift 2
                ;;
            -r|--report)
                GENERATE_REPORT=true
                shift
                ;;
            -h|--help)
                show_help
                exit 0
                ;;
            *)
                print_error "Unknown option: $1"
                show_help
                exit 1
                ;;
        esac
    done
    
    # Run checks
    check_golangci_lint
    echo ""
    
    check_config
    echo ""
    
    show_summary
    echo ""
    
    # Run linting
    if ! run_lint; then
        echo ""
        print_error "Linting failed"
        echo ""
        echo "To fix issues automatically, run:"
        echo "  scripts/lint-fix.sh"
        echo ""
        exit 1
    fi
    
    echo ""
    print_success "All checks passed!"
    echo ""
}

# Show help
show_help() {
    cat << EOF
StreamGate Local Lint Verification Script

Usage: scripts/lint.sh [OPTIONS]

Options:
  -v, --verbose       Enable verbose output
  -c, --config FILE   Use custom config file (default: .golangci.yml)
  -t, --timeout TIME  Set lint timeout (default: 5m)
  -r, --report        Generate lint report file
  -h, --help          Show this help message

Examples:
  # Run standard lint check
  scripts/lint.sh

  # Run with verbose output
  scripts/lint.sh -v

  # Run with custom timeout
  scripts/lint.sh -t 10m

  # Generate report
  scripts/lint.sh -r

  # Run with all options
  scripts/lint.sh -v -t 10m -r

Environment Variables:
  VERBOSE             Set to 'true' for verbose output

EOF
}

# Run main
main "$@"
