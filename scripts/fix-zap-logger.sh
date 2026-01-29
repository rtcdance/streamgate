#!/bin/bash

# Script to fix zap logger errors throughout the codebase
# This script converts incorrect zap logger calls to use proper field constructors

set -e

echo "Starting zap logger fixes..."

# Find all Go files in pkg and cmd directories
FILES=$(find pkg cmd -name "*.go" -type f)

for file in $FILES; do
    echo "Processing: $file"
    
    # Create a backup
    cp "$file" "$file.bak"
    
    # Fix common patterns using perl for better regex support
    
    # Pattern 1: logger.Error/Warn/Info/Debug("msg", "error", err)
    perl -i -pe 's/(logger\.(Error|Warn))\(([^,]+), "error", (err|error)\)/$1($3, zap.Error($4))/g' "$file"
    
    # Pattern 2: logger.Error/Warn/Info/Debug("msg", "key", value) for string values
    # This is tricky - we'll handle specific known patterns
    
    # Fix "port" fields (usually int)
    perl -i -pe 's/(logger\.(Info|Debug))\(([^,]+), "port", ([a-zA-Z_][a-zA-Z0-9_\.]*)\)/$1($3, zap.Int("port", $4))/g' "$file"
    
    # Fix "count" fields (usually int)
    perl -i -pe 's/(logger\.(Info|Debug))\(([^,]+), "count", ([a-zA-Z_][a-zA-Z0-9_\.]*)\)/$1($3, zap.Int("count", $4))/g' "$file"
    
    # Fix string fields - common patterns
    for key in "address" "chain_id" "contract" "cid" "filename" "name" "service_id" "service_name" "path" "method" "from" "to" "key" "type" "subject" "event_type" "event_id" "tx_hash" "block_number" "function" "mode" "url" "version" "rpc_url" "host" "message_length" "length" "expected" "challenge_id" "from_chain" "update_interval" "gas_price_wei" "gas_price" "gas" "alert_id" "signal"; do
        perl -i -pe "s/(logger\\.(Error|Warn|Info|Debug))\\(([^,]+), \"$key\", ([a-zA-Z_][a-zA-Z0-9_\\.\\(\\)]*?)\\)/\$1(\$3, zap.String(\"$key\", \$4))/g" "$file"
    done
    
    # Fix duration fields
    perl -i -pe 's/(logger\.(Info|Debug))\(([^,]+), "duration", ([a-zA-Z_][a-zA-Z0-9_\.]*)\)/$1($3, zap.Duration("duration", $4))/g' "$file"
    perl -i -pe 's/(logger\.(Info|Debug))\(([^,]+), "elapsed", ([a-zA-Z_][a-zA-Z0-9_\.]*)\)/$1($3, zap.Duration("elapsed", $4))/g' "$file"
    
    # Fix boolean fields
    perl -i -pe 's/(logger\.(Info|Debug))\(([^,]+), "success", ([a-zA-Z_][a-zA-Z0-9_\.]*)\)/$1($3, zap.Bool("success", $4))/g' "$file"
    
    # Fix int64 fields
    for key in "id" "user_id" "content_id"; do
        perl -i -pe "s/(logger\\.(Info|Debug))\\(([^,]+), \"$key\", ([a-zA-Z_][a-zA-Z0-9_\\.]*?)\\)/\$1(\$3, zap.Int64(\"$key\", \$4))/g" "$file"
    done
    
    # Check if file was modified
    if ! diff -q "$file" "$file.bak" > /dev/null 2>&1; then
        echo "  ✓ Modified"
    else
        echo "  - No changes"
    fi
    
    # Remove backup
    rm "$file.bak"
done

echo ""
echo "Phase 1 complete. Running goimports to fix imports..."

# Add missing zap imports and format
for file in $FILES; do
    if grep -q "zap\." "$file" 2>/dev/null; then
        # Check if zap is imported
        if ! grep -q '"go.uber.org/zap"' "$file" 2>/dev/null; then
            echo "Adding zap import to: $file"
            # Add import after package declaration
            perl -i -pe 's/(package \w+)/$1\n\nimport "go.uber.org\/zap"/' "$file"
        fi
    fi
done

echo ""
echo "Running gofmt..."
gofmt -w pkg cmd

echo ""
echo "✓ Zap logger fixes complete!"
echo ""
echo "Next steps:"
echo "1. Review changes: git diff"
echo "2. Run tests: go test ./..."
echo "3. Run linter: golangci-lint run ./..."
