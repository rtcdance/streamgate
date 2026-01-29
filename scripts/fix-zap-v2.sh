#!/bin/bash

# Comprehensive zap logger fixer v2
# Fixes all incorrect zap logger syntax

set -e

echo "=== Zap Logger Comprehensive Fix ==="
echo ""

# Get all Go files
FILES=$(find pkg cmd -name "*.go" -type f)

for file in $FILES; do
    # Skip if file doesn't contain logger calls
    if ! grep -q 'logger\.\(Error\|Warn\|Info\|Debug\)' "$file" 2>/dev/null; then
        continue
    fi
    
    echo "Processing: $file"
    
    # Create backup
    cp "$file" "$file.bak"
    
    # Fix error fields first (most common)
    sed -i 's/logger\.Error(\([^,]*\), "error", \(err\|error\))/logger.Error(\1, zap.Error(\2))/g' "$file"
    sed -i 's/logger\.Warn(\([^,]*\), "error", \(err\|error\))/logger.Warn(\1, zap.Error(\2))/g' "$file"
    
    # Fix all string fields - comprehensive list
    for key in address chain_id contract cid filename name service_id service_name path method from to key type subject event_type event_id tx_hash block_number function mode url version rpc_url host message_length length expected challenge_id from_chain update_interval gas_price_wei gas_price gas alert_id signal owner token_id asset amount event from_block to_block value gas_limit balance nonce size; do
        sed -i "s/logger\.\(Error\|Warn\|Info\|Debug\)(\([^,]*\), \"$key\", \([a-zA-Z_][a-zA-Z0-9_\.()]*\))/logger.\1(\2, zap.String(\"$key\", \3))/g" "$file"
    done
    
    # Fix int fields
    for key in port count size nonce failures message_length length; do
        sed -i "s/zap\.String(\"$key\", \([^)]*\))/zap.Int(\"$key\", \1)/g" "$file"
    done
    
    # Fix int64 fields
    for key in id user_id content_id block_number from_block to_block; do
        sed -i "s/zap\.String(\"$key\", \([^)]*\))/zap.Int64(\"$key\", \1)/g" "$file"
    done
    
    # Fix duration fields
    for key in duration elapsed update_interval; do
        sed -i "s/zap\.String(\"$key\", \([^)]*\))/zap.Duration(\"$key\", \1)/g" "$file"
    done
    
    # Fix boolean fields
    for key in success is_owner has_nft is_contract; do
        sed -i "s/zap\.String(\"$key\", \([^)]*\))/zap.Bool(\"$key\", \1)/g" "$file"
    done
    
    # Check if modified
    if ! diff -q "$file" "$file.bak" > /dev/null 2>&1; then
        echo "  ✓ Modified"
    fi
    
    rm "$file.bak"
done

echo ""
echo "=== Adding missing imports ==="

for file in $FILES; do
    if grep -q 'zap\.' "$file" 2>/dev/null; then
        if ! grep -q '"go.uber.org/zap"' "$file" 2>/dev/null; then
            echo "Adding import to: $file"
            # This is a simple approach - goimports will clean it up
            sed -i '/^package /a\\nimport "go.uber.org/zap"' "$file"
        fi
    fi
done

echo ""
echo "=== Running goimports ==="
if command -v goimports &> /dev/null; then
    goimports -w pkg cmd
    echo "✓ Imports fixed"
else
    echo "⚠ goimports not found, skipping"
fi

echo ""
echo "=== Running gofmt ==="
gofmt -w pkg cmd
echo "✓ Formatting complete"

echo ""
echo "✓ All fixes complete!"
echo ""
echo "Next steps:"
echo "1. Review: git diff | head -200"
echo "2. Test: go build ./cmd/monolith/streamgate"
echo "3. Lint: golangci-lint run ./... 2>&1 | head -50"
