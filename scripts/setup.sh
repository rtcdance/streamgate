#!/bin/bash

set -e

echo "🚀 Setting up StreamGate development environment..."

PROJECT_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$PROJECT_ROOT"

echo "📁 Project root: $PROJECT_ROOT"

echo "🔧 Installing Go dependencies..."
go mod download
go mod tidy

echo "🐳 Setting up Docker..."
if ! command -v docker &> /dev/null; then
    echo "❌ Docker is not installed. Please install Docker first."
    exit 1
fi

echo "📦 Pulling container images (postgres, redis, minio, nats, anvil, consul, nginx)..."
docker compose -f docker-compose.fullchain.yml pull postgres redis minio nats anvil consul h5-demo 2>&1 | tail -5 || true

echo "🗄️  Starting infrastructure services..."
docker compose -f docker-compose.fullchain.yml up -d postgres redis minio nats anvil

echo "⏳ Waiting for services to be ready..."
sleep 10

echo "🗄️  Running database migrations (host-side, requires Go)..."
go run ./cmd/migrate up

echo "✅ Setup complete!"
echo ""
echo "To start the application:"
echo "  make deploy-monolith          # Monolith mode (7 containers, 30s)"
echo "  make deploy-microservices     # Microservices mode (15 containers, 2min)"
echo "  make deploy-status            # Check running containers"
echo "  make deploy-teardown          # Stop everything"
echo ""
echo "To run tests:"
echo "  make test                     # All tests with race + coverage"
echo "  make test-anvil               # Web3 integration tests (needs Anvil)"
echo ""
echo "See DEPLOY.md for the full beginner-friendly walkthrough."
