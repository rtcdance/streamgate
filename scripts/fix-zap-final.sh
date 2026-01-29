#!/bin/bash

# Final comprehensive zap logger fixer
# Uses perl for better regex handling

set -e

echo "=== Zap Logger Final Fix ==="
echo ""

FILES=$(find pkg cmd -name "*.go" -type f)
MODIFIED=0

for file in $FILES; do
    if ! grep -q 'logger\.\(Error\|Warn\|Info\|Debug\)' "$file" 2>/dev/null; then
        continue
    fi
    
    echo "Processing: $file"
    cp "$file" "$file.bak"
    
    # Fix: logger.Error("msg", "error", err) -> logger.Error("msg", zap.Error(err))
    perl -i -pe 's/(logger\.(Error|Warn))\(([^,]+), "error", (err|error)\)/$1($3, zap.Error($4))/g' "$file"
    
    # Fix all string-type fields
    perl -i -pe 's/(logger\.(Error|Warn|Info|Debug))\(([^,]+), "address", ([a-zA-Z_][\w.()]*)\)/$1($3, zap.String("address", $4))/g' "$file"
    perl -i -pe 's/(logger\.(Error|Warn|Info|Debug))\(([^,]+), "chain_id", ([a-zA-Z_][\w.()]*)\)/$1($3, zap.String("chain_id", $4))/g' "$file"
    perl -i -pe 's/(logger\.(Error|Warn|Info|Debug))\(([^,]+), "contract", ([a-zA-Z_][\w.()]*)\)/$1($3, zap.String("contract", $4))/g' "$file"
    perl -i -pe 's/(logger\.(Error|Warn|Info|Debug))\(([^,]+), "cid", ([a-zA-Z_][\w.()]*)\)/$1($3, zap.String("cid", $4))/g' "$file"
    perl -i -pe 's/(logger\.(Error|Warn|Info|Debug))\(([^,]+), "filename", ([a-zA-Z_][\w.()]*)\)/$1($3, zap.String("filename", $4))/g' "$file"
    perl -i -pe 's/(logger\.(Error|Warn|Info|Debug))\(([^,]+), "name", ([a-zA-Z_][\w.()]*)\)/$1($3, zap.String("name", $4))/g' "$file"
    perl -i -pe 's/(logger\.(Error|Warn|Info|Debug))\(([^,]+), "service_id", ([a-zA-Z_][\w.()]*)\)/$1($3, zap.String("service_id", $4))/g' "$file"
    perl -i -pe 's/(logger\.(Error|Warn|Info|Debug))\(([^,]+), "service_name", ([a-zA-Z_][\w.()]*)\)/$1($3, zap.String("service_name", $4))/g' "$file"
    perl -i -pe 's/(logger\.(Error|Warn|Info|Debug))\(([^,]+), "path", ([a-zA-Z_][\w.()]*)\)/$1($3, zap.String("path", $4))/g' "$file"
    perl -i -pe 's/(logger\.(Error|Warn|Info|Debug))\(([^,]+), "method", ([a-zA-Z_][\w.()]*)\)/$1($3, zap.String("method", $4))/g' "$file"
    perl -i -pe 's/(logger\.(Error|Warn|Info|Debug))\(([^,]+), "from", ([a-zA-Z_][\w.()]*)\)/$1($3, zap.String("from", $4))/g' "$file"
    perl -i -pe 's/(logger\.(Error|Warn|Info|Debug))\(([^,]+), "to", ([a-zA-Z_][\w.()]*)\)/$1($3, zap.String("to", $4))/g' "$file"
    perl -i -pe 's/(logger\.(Error|Warn|Info|Debug))\(([^,]+), "key", ([a-zA-Z_][\w.()]*)\)/$1($3, zap.String("key", $4))/g' "$file"
    perl -i -pe 's/(logger\.(Error|Warn|Info|Debug))\(([^,]+), "type", ([a-zA-Z_][\w.()]*)\)/$1($3, zap.String("type", $4))/g' "$file"
    perl -i -pe 's/(logger\.(Error|Warn|Info|Debug))\(([^,]+), "subject", ([a-zA-Z_][\w.()]*)\)/$1($3, zap.String("subject", $4))/g' "$file"
    perl -i -pe 's/(logger\.(Error|Warn|Info|Debug))\(([^,]+), "event_type", ([a-zA-Z_][\w.()]*)\)/$1($3, zap.String("event_type", $4))/g' "$file"
    perl -i -pe 's/(logger\.(Error|Warn|Info|Debug))\(([^,]+), "event_id", ([a-zA-Z_][\w.()]*)\)/$1($3, zap.String("event_id", $4))/g' "$file"
    perl -i -pe 's/(logger\.(Error|Warn|Info|Debug))\(([^,]+), "tx_hash", ([a-zA-Z_][\w.()]*)\)/$1($3, zap.String("tx_hash", $4))/g' "$file"
    perl -i -pe 's/(logger\.(Error|Warn|Info|Debug))\(([^,]+), "function", ([a-zA-Z_][\w.()]*)\)/$1($3, zap.String("function", $4))/g' "$file"
    perl -i -pe 's/(logger\.(Error|Warn|Info|Debug))\(([^,]+), "mode", ([a-zA-Z_][\w.()]*)\)/$1($3, zap.String("mode", $4))/g' "$file"
    perl -i -pe 's/(logger\.(Error|Warn|Info|Debug))\(([^,]+), "url", ([a-zA-Z_][\w.()]*)\)/$1($3, zap.String("url", $4))/g' "$file"
    perl -i -pe 's/(logger\.(Error|Warn|Info|Debug))\(([^,]+), "version", ([a-zA-Z_][\w.()]*)\)/$1($3, zap.String("version", $4))/g' "$file"
    perl -i -pe 's/(logger\.(Error|Warn|Info|Debug))\(([^,]+), "rpc_url", ([a-zA-Z_][\w.()]*)\)/$1($3, zap.String("rpc_url", $4))/g' "$file"
    perl -i -pe 's/(logger\.(Error|Warn|Info|Debug))\(([^,]+), "host", ([a-zA-Z_][\w.()]*)\)/$1($3, zap.String("host", $4))/g' "$file"
    perl -i -pe 's/(logger\.(Error|Warn|Info|Debug))\(([^,]+), "challenge_id", ([a-zA-Z_][\w.()]*)\)/$1($3, zap.String("challenge_id", $4))/g' "$file"
    perl -i -pe 's/(logger\.(Error|Warn|Info|Debug))\(([^,]+), "from_chain", ([a-zA-Z_][\w.()]*)\)/$1($3, zap.String("from_chain", $4))/g' "$file"
    perl -i -pe 's/(logger\.(Error|Warn|Info|Debug))\(([^,]+), "alert_id", ([a-zA-Z_][\w.()]*)\)/$1($3, zap.String("alert_id", $4))/g' "$file"
    perl -i -pe 's/(logger\.(Error|Warn|Info|Debug))\(([^,]+), "signal", ([a-zA-Z_][\w.()]*)\)/$1($3, zap.String("signal", $4))/g' "$file"
    perl -i -pe 's/(logger\.(Error|Warn|Info|Debug))\(([^,]+), "owner", ([a-zA-Z_][\w.()]*)\)/$1($3, zap.String("owner", $4))/g' "$file"
    perl -i -pe 's/(logger\.(Error|Warn|Info|Debug))\(([^,]+), "token_id", ([a-zA-Z_][\w.()]*)\)/$1($3, zap.String("token_id", $4))/g' "$file"
    perl -i -pe 's/(logger\.(Error|Warn|Info|Debug))\(([^,]+), "asset", ([a-zA-Z_][\w.()]*)\)/$1($3, zap.String("asset", $4))/g' "$file"
    perl -i -pe 's/(logger\.(Error|Warn|Info|Debug))\(([^,]+), "amount", ([a-zA-Z_][\w.()]*)\)/$1($3, zap.String("amount", $4))/g' "$file"
    perl -i -pe 's/(logger\.(Error|Warn|Info|Debug))\(([^,]+), "event", ([a-zA-Z_][\w.()]*)\)/$1($3, zap.String("event", $4))/g' "$file"
    perl -i -pe 's/(logger\.(Error|Warn|Info|Debug))\(([^,]+), "balance", ([a-zA-Z_][\w.()]*)\)/$1($3, zap.String("balance", $4))/g' "$file"
    perl -i -pe 's/(logger\.(Error|Warn|Info|Debug))\(([^,]+), "gas_price_wei", ([a-zA-Z_][\w.()]*)\)/$1($3, zap.String("gas_price_wei", $4))/g' "$file"
    perl -i -pe 's/(logger\.(Error|Warn|Info|Debug))\(([^,]+), "gas_price", ([a-zA-Z_][\w.()]*)\)/$1($3, zap.String("gas_price", $4))/g' "$file"
    perl -i -pe 's/(logger\.(Error|Warn|Info|Debug))\(([^,]+), "gas", ([a-zA-Z_][\w.()]*)\)/$1($3, zap.String("gas", $4))/g' "$file"
    perl -i -pe 's/(logger\.(Error|Warn|Info|Debug))\(([^,]+), "expected", ([a-zA-Z_][\w.()]*)\)/$1($3, zap.String("expected", $4))/g' "$file"
    perl -i -pe 's/(logger\.(Error|Warn|Info|Debug))\(([^,]+), "value", ([a-zA-Z_][\w.()]*)\)/$1($3, zap.String("value", $4))/g' "$file"
    perl -i -pe 's/(logger\.(Error|Warn|Info|Debug))\(([^,]+), "gas_limit", ([a-zA-Z_][\w.()]*)\)/$1($3, zap.String("gas_limit", $4))/g' "$file"
    
    # Fix int fields
    perl -i -pe 's/(logger\.(Error|Warn|Info|Debug))\(([^,]+), "port", ([a-zA-Z_][\w.()]*)\)/$1($3, zap.Int("port", $4))/g' "$file"
    perl -i -pe 's/(logger\.(Error|Warn|Info|Debug))\(([^,]+), "count", ([a-zA-Z_][\w.()]*)\)/$1($3, zap.Int("count", $4))/g' "$file"
    perl -i -pe 's/(logger\.(Error|Warn|Info|Debug))\(([^,]+), "size", ([a-zA-Z_][\w.()]*)\)/$1($3, zap.Int("size", $4))/g' "$file"
    perl -i -pe 's/(logger\.(Error|Warn|Info|Debug))\(([^,]+), "nonce", ([a-zA-Z_][\w.()]*)\)/$1($3, zap.Int("nonce", $4))/g' "$file"
    perl -i -pe 's/(logger\.(Error|Warn|Info|Debug))\(([^,]+), "failures", ([a-zA-Z_][\w.()]*)\)/$1($3, zap.Int("failures", $4))/g' "$file"
    perl -i -pe 's/(logger\.(Error|Warn|Info|Debug))\(([^,]+), "message_length", ([a-zA-Z_][\w.()]*)\)/$1($3, zap.Int("message_length", $4))/g' "$file"
    perl -i -pe 's/(logger\.(Error|Warn|Info|Debug))\(([^,]+), "length", ([a-zA-Z_][\w.()]*)\)/$1($3, zap.Int("length", $4))/g' "$file"
    
    # Fix int64 fields
    perl -i -pe 's/(logger\.(Error|Warn|Info|Debug))\(([^,]+), "block_number", ([a-zA-Z_][\w.()]*)\)/$1($3, zap.Int64("block_number", $4))/g' "$file"
    perl -i -pe 's/(logger\.(Error|Warn|Info|Debug))\(([^,]+), "from_block", ([a-zA-Z_][\w.()]*)\)/$1($3, zap.Int64("from_block", $4))/g' "$file"
    perl -i -pe 's/(logger\.(Error|Warn|Info|Debug))\(([^,]+), "to_block", ([a-zA-Z_][\w.()]*)\)/$1($3, zap.Int64("to_block", $4))/g' "$file"
    
    # Fix duration fields
    perl -i -pe 's/(logger\.(Error|Warn|Info|Debug))\(([^,]+), "duration", ([a-zA-Z_][\w.()]*)\)/$1($3, zap.Duration("duration", $4))/g' "$file"
    perl -i -pe 's/(logger\.(Error|Warn|Info|Debug))\(([^,]+), "elapsed", ([a-zA-Z_][\w.()]*)\)/$1($3, zap.Duration("elapsed", $4))/g' "$file"
    perl -i -pe 's/(logger\.(Error|Warn|Info|Debug))\(([^,]+), "update_interval", ([a-zA-Z_][\w.()]*)\)/$1($3, zap.Duration("update_interval", $4))/g' "$file"
    
    # Fix boolean fields
    perl -i -pe 's/(logger\.(Error|Warn|Info|Debug))\(([^,]+), "success", ([a-zA-Z_][\w.()]*)\)/$1($3, zap.Bool("success", $4))/g' "$file"
    perl -i -pe 's/(logger\.(Error|Warn|Info|Debug))\(([^,]+), "is_owner", ([a-zA-Z_][\w.()]*)\)/$1($3, zap.Bool("is_owner", $4))/g' "$file"
    perl -i -pe 's/(logger\.(Error|Warn|Info|Debug))\(([^,]+), "has_nft", ([a-zA-Z_][\w.()]*)\)/$1($3, zap.Bool("has_nft", $4))/g' "$file"
    perl -i -pe 's/(logger\.(Error|Warn|Info|Debug))\(([^,]+), "is_contract", ([a-zA-Z_][\w.()]*)\)/$1($3, zap.Bool("is_contract", $4))/g' "$file"
    
    if ! diff -q "$file" "$file.bak" > /dev/null 2>&1; then
        echo "  ✓ Modified"
        ((MODIFIED++))
    fi
    
    rm "$file.bak"
done

echo ""
echo "✓ Modified $MODIFIED files"
echo ""
echo "Running gofmt..."
gofmt -w pkg cmd 2>/dev/null || true

echo "✓ Complete!"
