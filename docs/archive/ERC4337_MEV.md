# ERC-4337 & MEV Architecture

## Overview

StreamGate processes on-chain transactions (NFT minting, content registration, permit approvals) that are vulnerable to MEV extraction and UX friction from gas sponsorship. This document describes how ERC-4337 (Account Abstraction) and MEV mitigation integrate into the existing transaction pipeline.

---

## Current Transaction Flow

```
User → API Gateway → Web3Service.SendTransaction()
                         ↓
                    SecurePrivateKey.UseKey()
                         ↓
                    NonceManager.NextNonce()
                         ↓
                    ChainClient.EstimateGas() + gas buffer
                         ↓
                    Sign + Broadcast → TxLifecycleManager tracks confirmation
```

Key components already in place:
- **`pkg/web3/tx_lifecycle.go`**: TxLifecycleManager with auto-bump and replacement
- **`pkg/web3/tx_tracker.go`**: BumpGas / CancelTx for stuck transactions
- **`pkg/web3/nonce.go`**: Concurrent-safe nonce manager with rollback
- **`pkg/service/web3.go`**: `SendTransaction`, `ReplaceStuckTransaction`, `CancelPendingTransaction`, `SubmitPermit`
- **`pkg/web3/secure_key.go`**: XOR-encrypted signing key with zeroize-after-use

---

## ERC-4337 Integration Design

### Why ERC-4337?

1. **Gasless onboarding**: New users without ETH can still interact with StreamGate smart contracts via Paymaster sponsorship
2. **Batch operations**: NFT purchase + content access grant in a single UserOperation
3. **Session keys**: Temporary spending permissions for recurring content access without exposing the main wallet
4. **Social recovery**: Users can recover accounts without seed phrases

### Architecture

```
User Wallet (EOA or Smart Account)
      │
      ▼
Bundler (external or self-hosted)
      │
      ▼
Entry Point Contract (0x5FF137D4...)
      │
      ├──► Paymaster (StreamGateSponsorPaymaster)
      │       - Validates: user holds valid NFT → sponsor gas
      │       - Or: user pays in ERC-20 tokens (USDC/DAI)
      │
      ├──► Account Factory (if new smart account needed)
      │
      └──► Target Contract (StreamGate NFT / Content Registry)
```

### Component Mapping

| ERC-4337 Concept | StreamGate Component | Implementation Notes |
|---|---|---|
| UserOperation | `UserOpBuilder` (new) | Constructs UserOp from intent; handles gas estimation via `eth_estimateUserOperationGas` |
| Paymaster | `StreamGatePaymaster` (new contract) | Sponsor flow: verify NFT ownership → approve; Token flow: ERC-20 token payment |
| Bundler | External (Pimlico, Alchemy) or self-hosted | Relayed via `BundlerClient` in `pkg/web3/` |
| Smart Account | Kernel / Safe compatible | No custom account contract needed |
| Signature Validation | `pkg/web3/signature.go` | Already supports EIP-191 and EIP-712; extend for ERC-4337 `PackedUserOp` hash |

### API Surface

```go
// pkg/web3/bundler.go (new)
type BundlerClient struct {
    rpcURL    string
    entryAddr common.Address
    logger    *zap.Logger
}

func (b *BundlerClient) SendUserOp(ctx context.Context, op *UserOperation) (hash common.Hash, error)
func (b *BundlerClient) EstimateUserOpGas(ctx context.Context, op *UserOperation) (*GasEstimate, error)
func (b *BundlerClient) GetUserOpReceipt(ctx context.Context, hash common.Hash) (*UserOpReceipt, error)
```

### Paymaster Rules

```
StreamGateSponsorPaymaster.validatePaymasterOp():
  1. Decode intent from UserOp.calldata
  2. If intent == "access_content":
     - Check: caller owns required NFT (via on-chain balanceOf)
     - If yes → sponsor gas, return validateOp success
     - If no  → revert with "NFT_OWNERSHIP_REQUIRED"
  3. If intent == "register_content":
     - Only sponsor for whitelisted creator addresses
  4. Otherwise: user pays own gas (no sponsorship)
```

### Integration with Existing TxLifecycleManager

- UserOperations tracked alongside regular transactions in TxLifecycleManager
- `TxLifecycleManager.AddOp()` extended to accept both `types.Transaction` and `UserOperation`
- Receipt polling uses `eth_getUserOperationReceipt` for UserOps
- Gas bump strategy: UserOps use `replacement` with higher `maxFeePerGas` / `maxPriorityFeePerGas`

---

## MEV Mitigation Strategies

### Threat Model

| Attack Vector | Impact | Likelihood |
|---|---|---|
| Front-running NFT minting | User pays more / misses mint | High (public mempool) |
| Sandwich attacks on permit approvals | Slippage / token loss | Medium |
| Back-running content registration | Copycat content with earlier timestamp | Low |
| Private transaction leakage | Information extraction before confirmation | Medium |

### Strategy 1: Private Mempool Submission

```
Web3Service.SendTransaction()
       ↓
  Flashbots Protect / MEV Blocker (RPC relay)
       ↓
  Block inclusion without public mempool exposure
```

Implementation:
- Add `flashblocksRPC` to `config.Web3` as alternative submission endpoint
- `ChainClient.SendPrivateTransaction()` method that routes via Flashbots Protect RPC
- Default: use private submission for all contract writes; public RPC only for reads

### Strategy 2: Commit-Reveal for Sensitive Operations

For content registration where timestamp priority matters:

```
Phase 1 (Commit):  user submits hash(content_hash + salt) on-chain
Phase 2 (Reveal):  user submits content_hash + salt in next block
```

The commit transaction reveals nothing about the content; the reveal can only be front-run by someone who already knows the content hash (which they don't).

### Strategy 3: Slippage-Protected Permits

`SubmitPermit` already uses EIP-2612 permits. Add:
- Deadline enforcement: `deadline = block.timestamp + 5 minutes` max
- Amount validation: permit amount matches expected price exactly (no excess approval)
- The `PackPermitCall` in `pkg/web3/erc20.go` already encodes the exact parameters

### Strategy 4: Gas Price Oracle with MEV Awareness

Extend `FeeHistoryEstimator` to:
1. Detect blocks with high MEV reward (using `block.coinbase` balance delta)
2. Recommend lower `maxPriorityFeePerGas` during MEV-heavy blocks (wait for calmer block)
3. Cap `maxFeePerGas` at configured `MaxFeePerGasCapGwei` (already implemented in `web3.go:599`)

### Strategy 5: Transaction Simulation

Already implemented in `Web3Service.SendTransaction()` (line 510-522):
```go
// Pre-send simulation: eth_call to detect reverts before signing.
if len(data) > 0 {
    callMsg := ethereum.CallMsg{...}
    if _, err := ethClient.CallContract(ctx, callMsg, nil); err != nil {
        nm.Rollback(fromAddress.Hex(), nonce)
        return fmt.Errorf("transaction simulation failed: %w", err)
    }
}
```

This prevents wasted gas on transactions that would definitely fail, reducing MEV opportunities from revert-based attacks.

---

## Configuration

```yaml
web3:
  account_abstraction:
    enabled: true
    bundler_url: "https://polygon-mumbai.g.alchemy.com/v2/{key}"
    entry_point: "0x5FF137D4b0F6D4fEDc300A7907cE8D2b937c6cE3"
    paymaster_address: "0x..."  # StreamGateSponsorPaymaster
    sponsor_gas: true            # auto-sponsor for NFT holders

  mev_protection:
    private_mempool: true
    flashblocks_rpc: "https://rpc.flashbots.net"
    commit_reveal: false          # enable for content registration
    max_priority_fee_gwei: 2      # cap tip to reduce MEV incentive
    max_fee_per_gas_cap_gwei: 500 # hard cap (already in config)
```

---

## Implementation Roadmap

1. **BundlerClient**: Add `pkg/web3/bundler.go` with UserOp construction and RPC methods
2. **Paymaster contract**: Deploy `StreamGateSponsorPaymaster.sol` with NFT ownership check
3. **Private submission**: Extend `ChainClient` with `SendPrivateTransaction` using Flashbots RPC
4. **UserOp tracking**: Extend `TxLifecycleManager` to track UserOperations alongside regular txs
5. **Integration tests**: E2E test with local Bundler (using `aa-bundler` or `stackup-bundler`)
6. **Config migration**: Add `account_abstraction` and `mev_protection` sections to config

---

## Security Considerations

- **Paymaster DoS**: Rate-limit sponsorship per address; require NFT ownership verification before sponsoring
- **Bundler trust**: Self-hosted bundler for production; verify UserOp hash matches submitted data
- **Key management**: `SecurePrivateKey` already uses XOR encryption; extend to bundler authentication
- **Replay protection**: UserOperations include `nonce` field managed by EntryPoint contract (separate from EOA nonces)
- **Gas limit**: Paymaster should set `gasLimit` on sponsorship to prevent gas-griefing attacks
