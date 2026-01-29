#!/bin/bash
# Fix plugin handler files one by one

set -e

echo "Fixing plugin handler files..."

# List of files to fix
files=(
    "pkg/plugins/api/gateway.go"
    "pkg/plugins/api/handler.go"
    "pkg/plugins/auth/handler.go"
    "pkg/plugins/cache/handler.go"
    "pkg/plugins/metadata/handler.go"
    "pkg/plugins/metadata/server.go"
    "pkg/plugins/monitor/handler.go"
    "pkg/plugins/streaming/handler.go"
    "pkg/plugins/transcoder/handler.go"
    "pkg/plugins/upload/handler.go"
    "pkg/plugins/upload/server.go"
    "pkg/plugins/worker/handler.go"
    "pkg/plugins/cache/server.go"
)

for file in "${files[@]}"; do
    if [ -f "$file" ]; then
        echo "Processing $file..."
        
        # Remove security import
        sed -i.bak '/^[[:space:]]*"streamgate\/pkg\/security"$/d' "$file"
        
        # Remove time import if not used elsewhere (we'll add it back if needed)
        # sed -i.bak '/^[[:space:]]*"time"$/d' "$file"
        
        # Remove rateLimiter field
        sed -i.bak '/^[[:space:]]*rateLimiter[[:space:]]*\*security\.RateLimiter$/d' "$file"
        
        # Remove auditLogger field  
        sed -i.bak '/^[[:space:]]*auditLogger[[:space:]]*\*security\.AuditLogger$/d' "$file"
        
        # Remove optimization import
        sed -i.bak '/^[[:space:]]*"streamgate\/pkg\/optimization"$/d' "$file"
        
        # Remove localCache field
        sed -i.bak '/^[[:space:]]*localCache[[:space:]]*\*optimization\.LocalCache$/d' "$file"
        
        rm -f "$file.bak"
        echo "  ✓ Removed security/optimization fields and imports"
    fi
done

echo "Done! Now run gofmt to format files."
