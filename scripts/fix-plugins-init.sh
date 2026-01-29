#!/bin/bash
# Fix plugin initialization code

set -e

echo "Fixing plugin initialization code..."

# Fix all handler files
for file in pkg/plugins/*/handler.go; do
    if [ -f "$file" ]; then
        echo "Processing $file..."
        
        # Remove rateLimiter initialization lines
        sed -i.bak '/rateLimiter:[[:space:]]*security\.NewRateLimiter/d' "$file"
        
        # Remove auditLogger initialization lines
        sed -i.bak '/auditLogger:[[:space:]]*security\.NewAuditLogger/d' "$file"
        
        # Remove localCache initialization lines
        sed -i.bak '/localCache:[[:space:]]*optimization\.NewLocalCache/d' "$file"
        
        # Remove rateLimiter usage in if statements
        sed -i.bak '/if !h\.rateLimiter\.Allow/,/}/d' "$file"
        sed -i.bak '/if !p\.rateLimiter\.Allow/,/}/d' "$file"
        
        # Remove auditLogger.Log calls
        sed -i.bak '/h\.auditLogger\.Log/d' "$file"
        sed -i.bak '/p\.auditLogger\.Log/d' "$file"
        
        rm -f "$file.bak"
    fi
done

# Fix gateway.go
for file in pkg/plugins/*/gateway.go; do
    if [ -f "$file" ]; then
        echo "Processing $file..."
        
        sed -i.bak '/rateLimiter:[[:space:]]*security\.NewRateLimiter/d' "$file"
        sed -i.bak '/auditLogger:[[:space:]]*security\.NewAuditLogger/d' "$file"
        sed -i.bak '/cache:[[:space:]]*optimization\.NewLocalCache/d' "$file"
        sed -i.bak '/if !p\.rateLimiter\.Allow/,/}/d' "$file"
        sed -i.bak '/p\.auditLogger\.Log/d' "$file"
        
        rm -f "$file.bak"
    fi
done

echo "Done!"
