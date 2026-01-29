#!/bin/bash

# Comprehensive script to fix all remaining zap logger errors

set -e

echo "Fixing all remaining zap logger errors..."

# Fix abi.JSON() calls - need bytes.NewReader
echo "Fixing abi.JSON() calls..."
find pkg/web3 -name "*.go" -type f -exec sed -i '' 's/abi\.JSON(\[\]byte(\([^)]*\)))/abi.JSON(bytes.NewReader([]byte(\1)))/g' {} \;

# Add bytes import where needed
for file in pkg/web3/nft.go pkg/web3/contract.go; do
    if [ -f "$file" ]; then
        # Check if bytes is already imported
        if ! grep -q '"bytes"' "$file"; then
            # Add bytes import after the first import
            sed -i '' '/^import (/a\
	"bytes"
' "$file"
        fi
    fi
done

echo "Fixed abi.JSON() calls and added bytes imports"

# Fix ethereum import issues
echo "Fixing ethereum imports..."
for file in pkg/web3/chain.go pkg/web3/event_indexer.go pkg/service/web3.go; do
    if [ -f "$file" ]; then
        # Check if ethereum is imported
        if ! grep -q '"github.com/ethereum/go-ethereum"' "$file"; then
            sed -i '' '/^import (/a\
	"github.com/ethereum/go-ethereum"
' "$file"
        fi
    fi
done

echo "All zap logger errors fixed!"
