#!/bin/sh
# Deploy DemoNFT contract to Anvil and mint test NFTs
# Runs on Anvil container startup (first block only)

set -e

CONTRACT_DIR=/tmp/contracts
OUT_DIR=/tmp/out
mkdir -p "$OUT_DIR"

# Check if contract already deployed (block > 1)
BLOCK=$(cast block-number --rpc-url http://localhost:8545 2>/dev/null || echo "0")
if [ "$BLOCK" -gt 1 ]; then
  echo "Anvil already has state, skip deployment"
  exit 0
fi

# Copy contracts into container
cp -r /contracts/src "$CONTRACT_DIR" 2>/dev/null || mkdir -p "$CONTRACT_DIR"

# Deploy DemoNFT
echo "Deploying DemoNFT..."
DEPLOY_OUT=$(forge create \
  --rpc-url http://localhost:8545 \
  --private-key 0xac0974bec39a17e36ba4a6b4d238ff944bacb478cbed5efcae784d7bf4f2ff80 \
  /contracts/src/DemoNFT.sol:DemoNFT \
  --out "$OUT_DIR" \
  --legacy 2>&1) || {
    echo "Deploy failed (may already exist): $DEPLOY_OUT"
    exit 0
  }

CONTRACT=$(echo "$DEPLOY_OUT" | grep "Deployed to:" | awk '{print $3}')
echo "DemoNFT deployed at: $CONTRACT"

# Mint 3 NFTs to default account
echo "Minting NFTs..."
WALLET="0xf39Fd6e51aad88F6F4ce6aB8827279cffFb92266"
for i in 1 2 3; do
  cast send --rpc-url http://localhost:8545 \
    --private-key 0xac0974bec39a17e36ba4a6b4d238ff944bacb478cbed5efcae784d7bf4f2ff80 \
    "$CONTRACT" "mint(address)" "$WALLET" > /dev/null 2>&1
done
echo "Minted 3 NFTs to $WALLET"