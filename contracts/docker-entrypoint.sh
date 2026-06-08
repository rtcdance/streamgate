#!/bin/sh
set -e

# StreamGate Anvil Auto-Deploy Entrypoint
# Starts Anvil, deploys DemoNFT, mints 3 NFTs to default wallet

ANVIL_RPC="http://localhost:8545"
DEPLOYER_KEY="0xac0974bec39a17e36ba4a6b4d238ff944bacb478cbed5efcae784d7bf4f2ff80"
WALLET="0xf39Fd6e51aad88F6F4ce6aB8827279cffFb92266"
CONTRACT=0x5FbDB2315678afecb367f032d93F642f64180aa3

# Start anvil in background
anvil --host 0.0.0.0 \
      --chain-id 31337 \
      --block-time 2 &
ANVIL_PID=$!

# Wait for anvil to be ready
echo "[entrypoint] Waiting for Anvil..."
for i in $(seq 1 30); do
  if cast rpc --rpc-url "$ANVIL_RPC" eth_chainId > /dev/null 2>&1; then
    echo "[entrypoint] Anvil ready (chain 31337)"
    break
  fi
  sleep 1
done

# Deploy DemoNFT if not present
CODE=$(cast code --rpc-url "$ANVIL_RPC" "$CONTRACT" 2>/dev/null || echo "")
if [ "$CODE" = "0x" ] || [ -z "$CODE" ]; then
  echo "[entrypoint] Deploying DemoNFT..."
  forge create \
    --rpc-url "$ANVIL_RPC" \
    --private-key "$DEPLOYER_KEY" \
    /contracts/src/DemoNFT.sol:DemoNFT \
    --out /tmp/forge-out \
    --cache-path /tmp/forge-cache \
    --legacy --broadcast 2>&1 | grep -E "Deployed to|Error"
fi

# Mint 3 NFTs
BALANCE=$(cast call --rpc-url "$ANVIL_RPC" "$CONTRACT" "balanceOf(address)(uint256)" "$WALLET" 2>/dev/null || echo 0)
if [ "$BALANCE" -lt 1 ] 2>/dev/null; then
  echo "[entrypoint] Minting 3 NFTs to $WALLET..."
  for i in 1 2 3; do
    cast send --rpc-url "$ANVIL_RPC" \
      --private-key "$DEPLOYER_KEY" \
      "$CONTRACT" "mint(address)" "$WALLET" > /dev/null 2>&1
  done
fi

echo "[entrypoint] Done. Wallet has $(cast call --rpc-url "$ANVIL_RPC" "$CONTRACT" 'balanceOf(address)(uint256)' "$WALLET") NFTs"

# Wait for anvil process
wait $ANVIL_PID
