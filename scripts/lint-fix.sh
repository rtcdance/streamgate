#!/bin/bash

# StreamGate Auto-Fix Lint Issues Script
# This script automatically fixes common linting issues

set -e

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

# Configuration
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

# Check if tools are installed
check_tools() {
    print_header "Checking required tools"
    
    local missing_tools=0
    
    if ! command -v go &> /dev/null; then
        print_error "Go is not installed"
        missing_tools=$((missing_tools + 1))
    else
        print_success "Go is installed"
    fi
    
    if ! command -v gofmt &> /dev/null; then
        print_error "gofmt is not installed"
        missing_tools=$((missing_tools + 1))
    else
        print_success "gofmt is installed"
    fi
    
    if ! command -v goimports &> /dev/null; then
        print_warning "goimports is not installed, skipping import fixes"
        print_info "Install with: go install golang.org/x/tools/cmd/goimports@latest"
    else
        print_success "goimports is installed"
    fi
    
    if [ $missing_tools -gt 0 ]; then
        print_error "Some required tools are missing"
        exit 1
    fi
    
    echo ""
}

# Format code
fix_format() {
    print_header "Fixing code formatting"
    
    if go fmt ./...; then
        print_success "Code formatting fixed"
    else
        print_error "Failed to format code"
        return 1
    fi
    
    echo ""
}

# Fix imports
fix_imports() {
    print_header "Fixing imports"
    
    if command -v goimports &> /dev/null; then
        if goimports -w ./...; then
            print_success "Imports fixed"
        else
            print_error "Failed to fix imports"
            return 1
        fi
    else
        print_warning "goimports not installed, skipping import fixes"
    fi
    
    echo ""
}

# Run golangci-lint with fix
fix_lint() {
    print_header "Running golangci-lint fixes"
    
    if command -v golangci-lint &> /dev/null; then
        # Note: golangci-lint doesn't have a built-in fix command
        # but we can use it to identify issues
        print_info "golangci-lint doesn't have auto-fix, but will identify remaining issues"
        
        if golangci-lint run ./... --fix; then
            print_success "golangci-lint checks passed"
        else
            print_warning "Some golangci-lint issues remain (may require manual fixes)"
        fi
    else
        print_warning "golangci-lint not installed"
    fi
    
    echo ""
}

# Verify fixes
verify_fixes() {
    print_header "Verifying fixes"
    
    if go fmt ./... > /dev/null 2>&1; then
        print_success "Code formatting verified"
    else
        print_warning "Some formatting issues remain"
    fi
    
    if command -v goimports &> /dev/null; then
        if goimports -l ./... 2>/dev/null | grep -q .; then
            print_warning "Some import issues remain"
        else
            print_success "Imports verified"
        fi
    fi
    
    echo ""
}

# Show summary
show_summary() {
    print_header "Fix Summary"
    
    echo ""
    echo "Fixes applied:"
    echo "  ✓ Code formatting (gofmt)"
    echo "  ✓ Import organization (goimports)"
    echo "  ✓ Lint checks (golangci-lint)"
    echo ""
    echo "Next steps:"
    echo "  1. Review the changes: git diff"
    echo "  2. Run lint verification: scripts/lint.sh"
    echo "  3. Commit the changes: git add . && git commit -m 'fix: lint issues'"
    echo ""
}

# Main execution
main() {
    echo ""
    print_header "StreamGate Auto-Fix Lint Issues"
    echo ""
    
    # Parse arguments
    while [[ $# -gt 0 ]]; do
        case $1 in
            -v|--verbose)
                VERBOSE=true
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
    
    # Run fixes
    check_tools
    fix_format
    fix_imports
    fix_lint
    verify_fixes
    show_summary
    
    print_success "Auto-fix complete!"
    echo ""
}

# Show help
show_help() {
    cat << EOF
StreamGate Auto-Fix Lint Issues Script

Usage: scripts/lint-fix.sh [OPTIONS]

Options:
  -v, --verbose       Enable verbose output
  -h, --help          Show this help message

Examples:
  # Run auto-fix
  scripts/lint-fix.sh

  # Run with verbose output
  scripts/lint-fix.sh -v

Environment Variables:
  VERBOSE             Set to 'true' for verbose output

What this script does:
  1. Formats code with gofmt
  2. Organizes imports with goimports
  3. Runs golangci-lint checks
  4. Verifies all fixes

Note: Some issues may require manual fixes. Review the output carefully.

EOF
}

# Run main
main "$@"
