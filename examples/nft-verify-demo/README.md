# nft-verify-demo

Standalone Go program that demonstrates the minimal NFT ownership check used in
StreamGate. Connects to an Ethereum-compatible RPC, reads the current block
height, queries an ETH balance, and finally calls `balanceOf` on an ERC-721
contract to decide if the user holds the gating token.

## What it shows

1. RPC client setup with fallback URL.
2. `BlockNumber` and `BalanceAt` calls (free, read-only).
3. `balanceOf(address)` ABI call against an ERC-721 contract.
4. Decision branch: hold NFT → allow; do not hold → deny.

## Run

```bash
cd examples/nft-verify-demo
go run main.go
```

Optional:

```bash
export ETH_RPC_URL="https://sepolia.infura.io/v3/YOUR_API_KEY"
```

## Required edits before running

`main.go` contains two placeholder addresses. Replace before execution:

| Variable | Replace with |
|----------|-------------|
| `0x你的钱包地址` (line 43) | Your test wallet |
| `0xNFT合约地址` (line 51) | The NFT contract you want to gate on |

## Production path

`main.go` is intentionally a teaching artifact. The real StreamGate
implementation lives in `pkg/web3/nft.go` and uses the production NFT
verification path (ERC-165 auto-detect, multicall batching, RPC failover,
ownership cache with TTL).

## Output

```
✅ 成功连接到以太坊测试网
📦 当前区块高度: 5234567
💰 钱包余额: 0.123456 ETH
🎨 NFT 余额: 1
✅ 用户持有 NFT，允许访问内容
```
