#!/bin/bash

set -e

echo "🗄️  Running database migrations..."

PROJECT_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$PROJECT_ROOT"

ACTION=${1:-up}

case $ACTION in
  up)
    echo "📈 Running migrations up..."
    go run cmd/migrate/main.go up
    ;;
  down)
    echo "📉 Running migrations down..."
    go run cmd/migrate/main.go down
    ;;
  create)
    echo "📝 Creating migration..."
    if [ -z "$2" ]; then
      echo "❌ Usage: $0 create <migration_name>"
      exit 1
    fi
    TIMESTAMP=$(date +%Y%m%d%H%M%S)
    MIGRATION_FILE="migrations/${TIMESTAMP}_${2}.sql"
    touch "$MIGRATION_FILE"
    echo "✅ Created migration: $MIGRATION_FILE"
    ;;
  status)
    echo "📊 Checking migration status..."
    go run cmd/migrate/main.go status
    ;;
  *)
    echo "❌ Unknown action: $ACTION"
    echo "Usage: $0 {up|down|create|status} [migration_name]"
    exit 1
    ;;
esac
