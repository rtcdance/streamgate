#!/usr/bin/env bash
# StreamGate Web3 Demo Script
# Runs the test suite with verbose output to showcase Web3 capabilities.
# No external services required — all tests use mocks and httptest servers.
set -euo pipefail

cd "$(dirname "$0")/.."

RED='\033[0;31m'
GREEN='\033[0;32m'
CYAN='\033[0;36m'
BOLD='\033[1m'
NC='\033[0m'

echo -e "${BOLD}${CYAN}========================================${NC}"
echo -e "${BOLD}${CYAN}  StreamGate — Web3 Capabilities Demo  ${NC}"
echo -e "${BOLD}${CYAN}========================================${NC}"
echo ""

# 1. Build check
echo -e "${BOLD}[1/5] Build check${NC}"
if go build ./...; then
    echo -e "  ${GREEN}OK${NC} — all packages compile"
else
    echo -e "  ${RED}FAIL${NC} — build errors"
    exit 1
fi
echo ""

# 2. Signature verification
echo -e "${BOLD}[2/5] EIP-191 Signature Verification${NC}"
go test ./pkg/web3/ -run "TestSignature" -v -count=1 2>&1 | grep -E "^(=== RUN|--- (PASS|FAIL)|ok|FAIL)" | sed 's/^/  /'
echo ""

# 3. Multi-chain manager
echo -e "${BOLD}[3/5] Multi-Chain Manager (EVM + Solana)${NC}"
go test ./pkg/web3/ -run "TestMultiChain|TestChainClient" -v -count=1 2>&1 | grep -E "^(=== RUN|--- (PASS|FAIL)|ok|FAIL)" | sed 's/^/  /'
echo ""

# 4. NFT verification & wallet auth
echo -e "${BOLD}[4/5] NFT Verification & Wallet Auth${NC}"
go test ./pkg/service/ -run "TestWeb3Service|TestAuthService_Wallet|TestRedisChallenge|TestMemoryChallenge" -v -count=1 2>&1 | grep -E "^(=== RUN|--- (PASS|FAIL)|ok|FAIL)" | sed 's/^/  /'
echo ""

# 5. Full test suite with coverage
echo -e "${BOLD}[5/5] Full Suite + Coverage${NC}"
go test ./pkg/... -count=1 -coverprofile=coverage.out 2>&1 | tail -5
echo ""

# Summary
echo -e "${BOLD}${CYAN}========================================${NC}"
echo -e "${BOLD}${CYAN}  Demo Complete                        ${NC}"
echo -e "${BOLD}${CYAN}========================================${NC}"
echo ""
echo -e "Key Web3 capabilities demonstrated:"
echo -e "  - EIP-191 personal_sign signature verification"
echo -e "  - Multi-chain EVM client (Ethereum, Polygon, BSC, Arbitrum, Optimism)"
echo -e "  - Solana Ed25519 signature verification"
echo -e "  - ERC-721 NFT ownership verification"
echo -e "  - Wallet challenge-response auth with Redis TTL"
echo -e "  - TOCTOU-safe challenge consumption (Redis Lua script)"
echo -e "  - RPC failover chain with automatic retry"
echo ""
echo -e "Coverage report: ${CYAN}coverage.out${NC}"
echo -e "View HTML:       ${CYAN}go tool cover -html=coverage.out${NC}"
