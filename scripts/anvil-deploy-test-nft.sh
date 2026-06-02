#!/bin/bash
# Deploy TestNFT to local Anvil testnet and mint to the default demo wallet.
#
# This script uses the pre-compiled bytecode from contracts/out/TestNFT.sol/TestNFT.json
# (forge-free — no solidity compilation needed).
#
# The default deployer account (anvil #0, 0xf39Fd6...) has two standard Foundry
# deterministic addresses. We deploy at nonce 0 (0x5FbDB2315...) then at nonce 1
# (0xe7f1725E7734CE288F8367e1Bb143E90bb3F0512), which is the address the h5demo
# frontend expects for chain 31337.
#
# Idempotent: safe to re-run if contracts are already deployed.
#
# Prerequisites: cast (Foundry) — https://book.getfoundry.sh
# Usage: ./scripts/anvil-deploy-test-nft.sh [RPC_URL]
set -euo pipefail

RPC_URL="${1:-http://localhost:18545}"
DEPLOYER_KEY="0xac0974bec39a17e36ba4a6b4d238ff944bacb478cbed5efcae784d7bf4f2ff80"
DEPLOYER="0xf39Fd6e51aad88F6F4ce6aB8827279cffFb92266"
EXPECTED="0xe7f1725E7734CE288F8367e1Bb143E90bb3F0512"

if ! command -v cast &>/dev/null; then
    echo "ERROR: 'cast' not found. Install Foundry:"
    echo "  curl -L https://foundry.paradigm.xyz | bash && foundryup"
    exit 1
fi

# --- Idempotency check ---
EXISTING_CODE=$(cast code --rpc-url "$RPC_URL" "$EXPECTED" 2>/dev/null || true)
if [ "${#EXISTING_CODE}" -gt 4 ]; then
    echo "TestNFT already deployed at $EXPECTED"
    BALANCE=$(cast call --rpc-url "$RPC_URL" "$EXPECTED" 'balanceOf(address)(uint256)' "$DEPLOYER" 2>/dev/null || echo "call failed")
    echo "Demo wallet ($DEPLOYER) balance: $BALANCE"
    exit 0
fi

echo "=== Deploying TestNFT to Anvil ==="
echo "RPC:     $RPC_URL"
echo "Deployer: $DEPLOYER"

# Extract bytecode from pre-compiled Foundry output (no solc needed)
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
BYTECODE_PATH="$SCRIPT_DIR/../contracts/out/TestNFT.sol/TestNFT.json"
if [ ! -f "$BYTECODE_PATH" ]; then
    echo "ERROR: Pre-compiled bytecode not found at $BYTECODE_PATH"
    echo "Run forge build first or check the contracts/out directory."
    exit 1
fi
BYTECODE=$(python3 -c "import json,sys; print(json.load(sys.stdin)['bytecode']['object'])" < "$BYTECODE_PATH")
echo "Bytecode length: ${#BYTECODE} chars"

# Deploy twice so the 2nd deploy lands at 0xe7f1725... (nonce 1)
NONCE=$(cast nonce --rpc-url "$RPC_URL" "$DEPLOYER")

echo "Deployer nonce: $NONCE"

if [ "$NONCE" -eq 0 ]; then
    echo "Deploy 1/2 (nonce 0 → 0x5FbDB2315...)..."
    cast send --rpc-url "$RPC_URL" --private-key "$DEPLOYER_KEY" --create "$BYTECODE" > /dev/null 2>&1
    echo "Deploy 2/2 (nonce 1 → $EXPECTED)..."
    cast send --rpc-url "$RPC_URL" --private-key "$DEPLOYER_KEY" --create "$BYTECODE" > /dev/null 2>&1
elif [ "$NONCE" -eq 1 ]; then
    echo "Deploy 1/1 (nonce 1 → $EXPECTED)..."
    cast send --rpc-url "$RPC_URL" --private-key "$DEPLOYER_KEY" --create "$BYTECODE" > /dev/null 2>&1
else
    echo "Nonce $NONCE: unexpected state. Deploying now..."
    cast send --rpc-url "$RPC_URL" --private-key "$DEPLOYER_KEY" --create "$BYTECODE" > /dev/null 2>&1
    ACTUAL=$(cast code --rpc-url "$RPC_URL" "$EXPECTED" 2>/dev/null || true)
    if [ "${#ACTUAL}" -le 4 ]; then
        echo "WARNING: expected $EXPECTED has no code. Use the deployed address below instead."
    fi
fi

# Verify deployment
DEPLOYED_CODE=$(cast code --rpc-url "$RPC_URL" "$EXPECTED" 2>/dev/null || true)
if [ "${#DEPLOYED_CODE}" -le 4 ]; then
    echo "ERROR: Deployment failed — no code at $EXPECTED"
    ADDR=$(cast send --rpc-url "$RPC_URL" --private-key "$DEPLOYER_KEY" --create "$BYTECODE" 2>/dev/null | awk '/contractAddress/{print $2}')
    echo "Fallback: deployed at $ADDR"
    echo "Update h5-demo/index.html contract address to: $ADDR"
    exit 1
fi

# Mint to demo wallet
echo "Minting Token #1 to $DEPLOYER..."
cast send --rpc-url "$RPC_URL" --private-key "$DEPLOYER_KEY" "$EXPECTED" 'mint(address)' "$DEPLOYER" > /dev/null 2>&1

# Verify
BALANCE=$(cast call --rpc-url "$RPC_URL" "$EXPECTED" 'balanceOf(address)(uint256)' "$DEPLOYER")
echo "=== SUCCESS ==="
echo "Contract: $EXPECTED"
echo "Balance:  $BALANCE"
echo ""
echo "To mint more:"
echo "  cast send --rpc-url $RPC_URL --private-key \$KEY $EXPECTED 'mint(address)' \$WALLET"
echo "To check balance:"
echo "  cast call --rpc-url $RPC_URL $EXPECTED 'balanceOf(address)(uint256)' \$WALLET"
