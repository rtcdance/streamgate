#!/bin/bash

set -e

echo "🐳 Building Docker images..."

PROJECT_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$PROJECT_ROOT"

ENVIRONMENT=${1:-dev}
VERSION=${2:-latest}

echo "📦 Building for environment: $ENVIRONMENT"
echo "🏷️  Version: $VERSION"

DOCKERFILE_DIR="deploy/docker"

case $ENVIRONMENT in
  dev)
    echo "🔨 Building development images..."
    docker build -f $DOCKERFILE_DIR/Dockerfile.monolith -t streamgate/monolith:$VERSION .
    ;;
  test)
    echo "🧪 Building test images..."
    docker build -f $DOCKERFILE_DIR/Dockerfile.monolith -t streamgate/monolith:$VERSION .
    ;;
  prod)
    echo "🚀 Building production images..."
    docker build -f $DOCKERFILE_DIR/Dockerfile.monolith -t streamgate/monolith:$VERSION .
    docker tag streamgate/monolith:$VERSION streamgate/monolith:latest
    ;;
  all)
    echo "🔄 Building all images..."
    for df in $DOCKERFILE_DIR/Dockerfile.*; do
      name=$(basename "$df" | sed 's/Dockerfile.//')
      docker build -f "$df" -t "streamgate/$name:$VERSION" .
    done
    ;;
  *)
    echo "❌ Unknown environment: $ENVIRONMENT"
    echo "Usage: $0 {dev|test|prod|all} [version]"
    exit 1
    ;;
esac

echo "✅ Build complete!"
echo ""
echo "Available images:"
docker images | grep streamgate
