#!/bin/bash

set -e

echo "🧪 Running StreamGate tests..."

PROJECT_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$PROJECT_ROOT"

TEST_TYPE=${1:-all}
COVERAGE=${COVERAGE:-true}

echo "📦 Test type: $TEST_TYPE"
echo "📊 Coverage: $COVERAGE"

case $TEST_TYPE in
  unit)
    echo "🔬 Running unit tests..."
    if [ "$COVERAGE" = "true" ]; then
      go test -short -race -coverprofile=coverage.out -covermode=atomic ./pkg/...
      go tool cover -html=coverage.out -o coverage.html
      echo "✅ Coverage report generated: coverage.html"
    else
      go test -short -race ./pkg/...
    fi
    ;;
  integration)
    echo "🔗 Running integration tests..."
    if [ "$COVERAGE" = "true" ]; then
      go test -race -coverprofile=coverage.out -covermode=atomic ./test/integration/...
      go tool cover -html=coverage.out -o coverage.html
      echo "✅ Coverage report generated: coverage.html"
    else
      go test -race ./test/integration/...
    fi
    ;;
  e2e)
    echo "🎯 Running end-to-end tests..."
    if [ "$COVERAGE" = "true" ]; then
      go test -race -coverprofile=coverage.out -covermode=atomic ./test/e2e/...
      go tool cover -html=coverage.out -o coverage.html
      echo "✅ Coverage report generated: coverage.html"
    else
      go test -race ./test/e2e/...
    fi
    ;;
  all)
    echo "🔄 Running all tests..."
    if [ "$COVERAGE" = "true" ]; then
      go test -race -coverprofile=coverage.out -covermode=atomic ./...
      go tool cover -html=coverage.out -o coverage.html
      go tool cover -func=coverage.out -o coverage-func.txt
      echo "✅ Coverage report generated: coverage.html"
      echo "✅ Function coverage: coverage-func.txt"
    else
      go test -race ./...
    fi
    ;;
  *)
    echo "❌ Unknown test type: $TEST_TYPE"
    echo "Usage: $0 {unit|integration|e2e|all}"
    exit 1
    ;;
esac

echo "✅ Tests complete!"
