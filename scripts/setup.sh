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

echo "📦 Building Docker images..."
docker-compose build

echo "🗄️  Starting services..."
docker-compose up -d postgres redis minio

echo "⏳ Waiting for services to be ready..."
sleep 10

echo "🗄️  Running database migrations..."
docker-compose run --rm streamgate migrate up

echo "✅ Setup complete!"
echo ""
echo "To start the application:"
echo "  docker-compose up"
echo ""
echo "To run tests:"
echo "  go test ./..."
echo ""
echo "To build the application:"
echo "  go build ./cmd/monolith"
