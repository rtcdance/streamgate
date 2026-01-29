# Web3 Enterprise Integration Guide

## Overview

This document provides enterprise-grade Web3 and blockchain integration patterns for the StreamGate project, addressing real-world production scenarios beyond basic NFT verification.

## 1. Multi-Chain Architecture

### 1.1 Supported Chains

#### EVM Chains (Ethereum-Compatible)
- **Ethereum Mainnet** (Chain ID: 1)
  - Primary chain for high-value NFTs
  - Highest security and liquidity
  - Highest gas costs
  
- **Polygon (Matic)** (Chain ID: 137)
  - Layer 2 scaling solution
  - Lower gas costs (~100x cheaper than Ethereum)
  - Growing NFT ecosystem
  - Popular for gaming and metaverse
  
- **Binance Smart Chain (BSC)** (Chain ID: 56)
  - High throughput
  - Lower fees
  - Large user base
  - Gaming and DeFi focus
  
- **Arbitrum** (Chain ID: 42161)
  - Optimistic rollup L2
  - EVM-compatible
  - Growing adoption
  
- **Optimism** (Chain ID: 10)
  - Optimistic rollup L2
  - EVM-compatible
  - Strong developer community

#### Non-EVM Chains
- **Solana** (Cluster: mainnet-beta)
  - High throughput (~65k TPS)
  - Low latency
  - Different account model
  - Growing NFT marketplace (Magic Eden)

### 1.2 Chain Configuration

```yaml
# config.yaml
blockchain:
  chains:
    ethereum:
      enabled: true
      chain_id: 1
      rpc_nodes:
        - url: "https://eth-mainnet.g.alchemy.com/v2/YOUR_KEY"
          priority: 1
          timeout: 10s
        - url: "https://eth-mainnet.infura.io/v3/YOUR_KEY"
          priority: 2
          timeout: 10s
      fallback_nodes:
        - url: "https://rpc.ankr.com/eth"
          priority: 3
          timeout: 15s
      
    polygon:
      enabled: true
      chain_id: 137
      rpc_nodes:
        - url: "https://polygon-mainnet.g.alchemy.com/v2/YOUR_KEY"
          priority: 1
          timeout: 10s
      
    bsc:
      enabled: true
      chain_id: 56
      rpc_nodes:
        - url: "https://bsc-dataseed1.binance.org"
          priority: 1
          timeout: 10s
    
    solana:
      enabled: true
      cluster: mainnet-beta
      rpc_nodes:
        - url: "https://api.mainnet-beta.solana.com"
          priority: 1
          timeout: 10s
        - url: "https://solana-api.projectserum.com"
          priority: 2
          timeout: 10s
  
  # Global settings
  verification_cache_ttl: 5m
  max_retries: 3
  retry_backoff: 1s
  request_timeout: 30s
```

## 2. NFT Verification Patterns

### 2.1 ERC-721 (Non-Fungible Token Standard)

**Use Cases**:
- Digital art and collectibles
- Gaming items
- Domain names (ENS)
- Virtual real estate

**Verification Logic**:
```go
// Check if address owns specific NFT
func VerifyERC721Ownership(
    client *ethclient.Client,
    contractAddress string,
    tokenID *big.Int,
    ownerAddress string,
) (bool, error) {
    // Call ownerOf(tokenID) on contract
    // Compare returned address with ownerAddress
    // Return true if match
}
```

**Enterprise Considerations**:
- Single owner per token
- Immutable ownership
- Suitable for exclusive content access
- Gas cost: ~21k gas per verification

### 2.2 ERC-1155 (Multi-Token Standard)

**Use Cases**:
- Batch NFTs (multiple copies)
- Semi-fungible tokens
- Gaming items with quantities
- Membership tokens

**Verification Logic**:
```go
// Check if address holds minimum balance of token
func VerifyERC1155Balance(
    client *ethclient.Client,
    contractAddress string,
    tokenID *big.Int,
    ownerAddress string,
    minBalance *big.Int,
) (bool, error) {
    // Call balanceOf(ownerAddress, tokenID) on contract
    // Compare returned balance with minBalance
    // Return true if balance >= minBalance
}
```

**Enterprise Considerations**:
- Multiple owners can hold same token
- Supports quantity thresholds
- Suitable for membership/subscription models
- Gas cost: ~21k gas per verification

### 2.3 Solana NFT Verification

**Metaplex Standard**:
- Most common Solana NFT standard
- Uses Program Derived Addresses (PDAs)
- Metadata stored on-chain

**Verification Logic**:
```go
// Check if address owns Metaplex NFT
func VerifySolanaNFTOwnership(
    client *solana.Client,
    mintAddress string,
    ownerAddress string,
) (bool, error) {
    // Get token account for mint
    // Check owner of token account
    // Verify balance > 0
    // Return true if owner matches
}
```

**Enterprise Considerations**:
- No gas fees (rent-based model)
- Faster verification (~400ms)
- Different account model
- Suitable for high-volume verification

## 3. RPC Node Management

### 3.1 Node Selection Strategy

**Priority-Based Selection**:
```
1. Primary node (lowest latency, highest priority)
2. Secondary node (backup, higher latency tolerance)
3. Fallback node (public RPC, highest latency)
```

**Health Monitoring**:
```go
type NodeHealth struct {
    URL              string
    LastChecked      time.Time
    IsHealthy        bool
    AverageLatency   time.Duration
    FailureCount     int
    SuccessCount     int
    ErrorRate        float64
}

// Health check every 30 seconds
func MonitorNodeHealth(ctx context.Context) {
    ticker := time.NewTicker(30 * time.Second)
    for range ticker.C {
        for _, node := range nodes {
            if err := node.HealthCheck(); err != nil {
                node.MarkUnhealthy()
            } else {
                node.MarkHealthy()
            }
        }
    }
}
```

### 3.2 Failover Strategy

**Automatic Failover**:
1. Try primary node
2. If timeout/error, try secondary node
3. If secondary fails, try fallback node
4. If all fail, return cached result (if available)
5. If no cache, return error

**Circuit Breaker Pattern**:
```go
type CircuitBreaker struct {
    State           State
    FailureCount    int
    SuccessCount    int
    LastFailTime    time.Time
    Threshold       int
    ResetTimeout    time.Duration
}

// States: Closed (normal) -> Open (failing) -> Half-Open (testing)
func (cb *CircuitBreaker) Call(fn func() error) error {
    switch cb.State {
    case StateClosed:
        if err := fn(); err != nil {
            cb.FailureCount++
            if cb.FailureCount >= cb.Threshold {
                cb.State = StateOpen
            }
            return err
        }
        cb.FailureCount = 0
        return nil
    
    case StateOpen:
        if time.Since(cb.LastFailTime) > cb.ResetTimeout {
            cb.State = StateHalfOpen
            cb.SuccessCount = 0
        } else {
            return ErrCircuitOpen
        }
        fallthrough
    
    case StateHalfOpen:
        if err := fn(); err != nil {
            cb.State = StateOpen
            cb.LastFailTime = time.Now()
            return err
        }
        cb.SuccessCount++
        if cb.SuccessCount >= 3 {
            cb.State = StateClosed
            cb.FailureCount = 0
        }
        return nil
    }
}
```

### 3.3 RPC Provider Services

**Recommended Providers**:

| Provider | Chains | Features | Cost |
|----------|--------|----------|------|
| Alchemy | EVM, Solana | Enhanced API, webhooks, analytics | $0-500/month |
| Infura | EVM | Reliable, large infrastructure | $0-500/month |
| QuickNode | EVM, Solana | High performance, low latency | $0-500/month |
| Ankr | EVM, Solana | Decentralized, affordable | $0-100/month |
| Chainstack | EVM, Solana | Managed nodes, dedicated | $0-500/month |

**Configuration Example**:
```yaml
rpc_providers:
  primary:
    provider: alchemy
    api_key: ${ALCHEMY_API_KEY}
    tier: growth  # free, growth, scale
  
  secondary:
    provider: infura
    api_key: ${INFURA_API_KEY}
  
  fallback:
    provider: ankr
    api_key: ${ANKR_API_KEY}
```

## 4. Signature Verification

### 4.1 EVM Signature Verification (EIP-191)

**Message Format**:
```
\x19Ethereum Signed Message:\n{length}{message}
```

**Verification Process**:
```go
func VerifyEVMSignature(message, signature, address string) (bool, error) {
    // 1. Construct signed message
    signedMessage := fmt.Sprintf("\x19Ethereum Signed Message:\n%d%s", 
        len(message), message)
    
    // 2. Hash the message
    hash := crypto.Keccak256Hash([]byte(signedMessage))
    
    // 3. Recover public key from signature
    pubKey, err := crypto.SigToPub(hash.Bytes(), hexToBytes(signature))
    if err != nil {
        return false, err
    }
    
    // 4. Derive address from public key
    recoveredAddress := crypto.PubkeyToAddress(*pubKey)
    
    // 5. Compare with provided address
    return strings.EqualFold(recoveredAddress.Hex(), address), nil
}
```

**Enterprise Considerations**:
- Nonce required to prevent replay attacks
- Timestamp validation (prevent old signatures)
- Message format standardization
- Signature expiration (e.g., 5 minutes)

### 4.2 Solana Signature Verification

**Message Format**:
```
Solana off-chain message:\n{message}
```

**Verification Process**:
```go
func VerifySolanaSignature(message, signature, publicKey string) (bool, error) {
    // 1. Construct signed message
    signedMessage := fmt.Sprintf("Solana off-chain message:\n%s", message)
    
    // 2. Decode signature and public key
    sig, err := base58.Decode(signature)
    pubKey, err := base58.Decode(publicKey)
    
    // 3. Verify signature
    return ed25519.Verify(pubKey, []byte(signedMessage), sig), nil
}
```

**Enterprise Considerations**:
- No gas fees for signature verification
- Faster verification than EVM
- Different key format (base58 encoding)

## 5. Caching Strategy

### 5.1 Multi-Level Cache

**Cache Hierarchy**:
```
L1: In-Memory Cache (LRU)
    ├─ TTL: 1 minute
    ├─ Size: 10,000 entries
    └─ Use: Hot data, frequent access

L2: Redis Cache
    ├─ TTL: 5 minutes
    ├─ Size: 1,000,000 entries
    └─ Use: Distributed cache, cross-instance

L3: Database
    ├─ TTL: Permanent
    ├─ Size: Unlimited
    └─ Use: Audit trail, historical data
```

**Cache Key Format**:
```
verification:{chain}:{contract}:{token_id}:{address}
```

**Example**:
```
verification:ethereum:0x1234...5678:1:0xabcd...ef01
verification:polygon:0x9abc...def0:42:0x5678...9012
verification:solana:TokenkegQfeZyiNwAJsyFbPVwwQQfg5bgvro:0xabcd...ef01
```

### 5.2 Cache Invalidation

**Strategies**:
1. **Time-Based**: Automatic expiration after TTL
2. **Event-Based**: Invalidate on transfer/burn events
3. **Manual**: Admin-triggered invalidation
4. **Webhook-Based**: Invalidate on blockchain events

**Webhook Integration**:
```go
// Listen for NFT transfer events
func HandleTransferEvent(event TransferEvent) {
    // Invalidate cache for affected addresses
    cacheKey := fmt.Sprintf("verification:%s:%s:%s:%s",
        event.Chain, event.Contract, event.TokenID, event.From)
    cache.Delete(cacheKey)
    
    cacheKey = fmt.Sprintf("verification:%s:%s:%s:%s",
        event.Chain, event.Contract, event.TokenID, event.To)
    cache.Delete(cacheKey)
}
```

## 6. Batch Verification

### 6.1 Batch Processing

**Use Case**: Verify multiple NFTs for single user

```go
type BatchVerifyRequest struct {
    Address string
    NFTs    []NFTReference
}

type NFTReference struct {
    Chain    string
    Contract string
    TokenID  string
}

func BatchVerifyNFTs(ctx context.Context, req BatchVerifyRequest) ([]bool, error) {
    results := make([]bool, len(req.NFTs))
    
    // Process in parallel with concurrency limit
    sem := make(chan struct{}, 10) // Max 10 concurrent verifications
    
    for i, nft := range req.NFTs {
        sem <- struct{}{}
        go func(idx int, n NFTReference) {
            defer func() { <-sem }()
            results[idx], _ = VerifyNFTOwnership(ctx, n, req.Address)
        }(i, nft)
    }
    
    return results, nil
}
```

**Performance**:
- Sequential: 10 NFTs × 500ms = 5 seconds
- Parallel (10 concurrent): 10 NFTs × 500ms / 10 = 500ms
- Improvement: 10x faster

### 6.2 Batch Caching

```go
// Cache batch results
type BatchVerificationResult struct {
    Address    string
    NFTs       []NFTReference
    Results    []bool
    Timestamp  time.Time
    ExpiresAt  time.Time
}

func CacheBatchResult(req BatchVerifyRequest, results []bool) {
    key := fmt.Sprintf("batch_verification:%s:%s", 
        req.Address, hashNFTList(req.NFTs))
    
    result := BatchVerificationResult{
        Address:   req.Address,
        NFTs:      req.NFTs,
        Results:   results,
        Timestamp: time.Now(),
        ExpiresAt: time.Now().Add(5 * time.Minute),
    }
    
    cache.Set(key, result, 5*time.Minute)
}
```

## 7. Access Control Models

### 7.1 Single NFT Requirement

**Use Case**: Content requires specific NFT

```go
type AccessPolicy struct {
    Type     string // "single_nft"
    Chain    string
    Contract string
    TokenID  string
}

func CheckAccess(user User, policy AccessPolicy) (bool, error) {
    return VerifyNFTOwnership(ctx, AccessNFT{
        Chain:    policy.Chain,
        Contract: policy.Contract,
        TokenID:  policy.TokenID,
    }, user.Address)
}
```

### 7.2 Multiple NFT Options

**Use Case**: Content requires ANY of multiple NFTs

```go
type AccessPolicy struct {
    Type  string // "any_of"
    NFTs  []NFTReference
}

func CheckAccess(user User, policy AccessPolicy) (bool, error) {
    for _, nft := range policy.NFTs {
        if ok, _ := VerifyNFTOwnership(ctx, nft, user.Address); ok {
            return true, nil
        }
    }
    return false, nil
}
```

### 7.3 Minimum Balance Requirement

**Use Case**: Content requires minimum token balance

```go
type AccessPolicy struct {
    Type       string // "min_balance"
    Chain      string
    Contract   string
    MinBalance int64
}

func CheckAccess(user User, policy AccessPolicy) (bool, error) {
    balance, err := GetNFTBalance(ctx, policy.Chain, 
        policy.Contract, user.Address)
    return balance >= policy.MinBalance, err
}
```

### 7.4 Time-Limited Access

**Use Case**: Content access expires after time period

```go
type AccessPolicy struct {
    Type      string // "time_limited"
    NFT       NFTReference
    ExpiresAt time.Time
}

func CheckAccess(user User, policy AccessPolicy) (bool, error) {
    if time.Now().After(policy.ExpiresAt) {
        return false, ErrAccessExpired
    }
    
    return VerifyNFTOwnership(ctx, policy.NFT, user.Address)
}
```

## 8. Error Handling & Resilience

### 8.1 Common Errors

| Error | Cause | Recovery |
|-------|-------|----------|
| RPC Timeout | Network latency | Retry with fallback node |
| Invalid Contract | Wrong address | Return error, log for review |
| Insufficient Balance | User doesn't own NFT | Return false |
| Chain Unreachable | Network issue | Use cached result if available |
| Invalid Signature | Tampered message | Return error |

### 8.2 Retry Strategy

```go
func VerifyWithRetry(ctx context.Context, req VerifyRequest) (bool, error) {
    var lastErr error
    
    for attempt := 0; attempt < 3; attempt++ {
        result, err := VerifyNFTOwnership(ctx, req)
        if err == nil {
            return result, nil
        }
        
        lastErr = err
        
        // Exponential backoff
        backoff := time.Duration(math.Pow(2, float64(attempt))) * time.Second
        select {
        case <-time.After(backoff):
        case <-ctx.Done():
            return false, ctx.Err()
        }
    }
    
    return false, lastErr
}
```

### 8.3 Graceful Degradation

```go
func VerifyWithFallback(ctx context.Context, req VerifyRequest) (bool, error) {
    // Try live verification
    result, err := VerifyNFTOwnership(ctx, req)
    if err == nil {
        return result, nil
    }
    
    // Fall back to cached result
    if cached, ok := cache.Get(req); ok {
        return cached, nil
    }
    
    // If cache miss and verification failed, deny access
    return false, err
}
```

## 9. Monitoring & Observability

### 9.1 Key Metrics

```go
var (
    // Verification metrics
    verificationTotal = promauto.NewCounterVec(
        prometheus.CounterOpts{
            Name: "nft_verification_total",
            Help: "Total NFT verifications",
        },
        []string{"chain", "status"},
    )
    
    verificationDuration = promauto.NewHistogramVec(
        prometheus.HistogramOpts{
            Name:    "nft_verification_duration_seconds",
            Help:    "NFT verification duration",
            Buckets: []float64{.1, .5, 1, 2, 5, 10},
        },
        []string{"chain"},
    )
    
    // RPC metrics
    rpcCallTotal = promauto.NewCounterVec(
        prometheus.CounterOpts{
            Name: "rpc_call_total",
            Help: "Total RPC calls",
        },
        []string{"chain", "method", "status"},
    )
    
    rpcLatency = promauto.NewHistogramVec(
        prometheus.HistogramOpts{
            Name:    "rpc_latency_seconds",
            Help:    "RPC call latency",
            Buckets: []float64{.01, .05, .1, .5, 1},
        },
        []string{"chain", "method"},
    )
    
    // Cache metrics
    cacheHitRate = promauto.NewGaugeVec(
        prometheus.GaugeOpts{
            Name: "cache_hit_rate",
            Help: "Cache hit rate",
        },
        []string{"level"},
    )
)
```

### 9.2 Alerting Rules

```yaml
# prometheus-rules.yml
groups:
  - name: web3_alerts
    rules:
      - alert: HighVerificationFailureRate
        expr: rate(nft_verification_total{status="failed"}[5m]) > 0.1
        for: 5m
        annotations:
          summary: "High NFT verification failure rate"
      
      - alert: RpcNodeUnhealthy
        expr: up{job="rpc_node"} == 0
        for: 2m
        annotations:
          summary: "RPC node is down"
      
      - alert: LowCacheHitRate
        expr: cache_hit_rate < 0.5
        for: 10m
        annotations:
          summary: "Cache hit rate below 50%"
```

## 10. Security Considerations

### 10.1 Signature Replay Prevention

**Nonce-Based**:
```go
type AuthRequest struct {
    Address   string
    Message   string
    Signature string
    Nonce     string
    Timestamp int64
}

func ValidateAuthRequest(req AuthRequest) error {
    // Check nonce hasn't been used
    if nonce.Used(req.Nonce) {
        return ErrNonceAlreadyUsed
    }
    
    // Check timestamp is recent (within 5 minutes)
    if time.Now().Unix()-req.Timestamp > 300 {
        return ErrMessageExpired
    }
    
    // Mark nonce as used
    nonce.Mark(req.Nonce)
    
    return nil
}
```

### 10.2 Address Validation

```go
func ValidateAddress(address string, chain string) error {
    switch chain {
    case "ethereum", "polygon", "bsc":
        // EVM address validation
        if !strings.HasPrefix(address, "0x") {
            return ErrInvalidAddress
        }
        if len(address) != 42 {
            return ErrInvalidAddress
        }
        if _, err := hex.DecodeString(address[2:]); err != nil {
            return ErrInvalidAddress
        }
    
    case "solana":
        // Solana address validation
        if _, err := base58.Decode(address); err != nil {
            return ErrInvalidAddress
        }
    }
    
    return nil
}
```

### 10.3 Contract Verification

```go
// Verify contract is legitimate (not honeypot)
func VerifyContractSafety(chain, contractAddress string) error {
    // Check against known scam contracts
    if isKnownScam(contractAddress) {
        return ErrScamContract
    }
    
    // Verify contract implements expected interface
    if !implementsERC721(contractAddress) && 
       !implementsERC1155(contractAddress) {
        return ErrInvalidContract
    }
    
    return nil
}
```

## 11. Testing Strategy

### 11.1 Unit Tests

```go
func TestVerifyEVMSignature(t *testing.T) {
    tests := []struct {
        name      string
        message   string
        signature string
        address   string
        expected  bool
    }{
        {
            name:      "valid signature",
            message:   "test message",
            signature: "0x...",
            address:   "0x...",
            expected:  true,
        },
        {
            name:      "invalid signature",
            message:   "test message",
            signature: "0x...",
            address:   "0x...",
            expected:  false,
        },
    }
    
    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            result, _ := VerifyEVMSignature(tt.message, tt.signature, tt.address)
            if result != tt.expected {
                t.Errorf("expected %v, got %v", tt.expected, result)
            }
        })
    }
}
```

### 11.2 Integration Tests

```go
func TestVerifyNFTOnTestnet(t *testing.T) {
    // Use testnet RPC
    client := NewEVMProvider("https://goerli.infura.io/v3/...")
    
    // Use known test NFT
    result, err := client.VerifyNFTOwnership(ctx, VerifyRequest{
        Chain:    "ethereum",
        Contract: "0x...", // Test contract
        TokenID:  "1",
        Address:  "0x...", // Test address
    })
    
    if err != nil {
        t.Fatalf("verification failed: %v", err)
    }
    
    if !result {
        t.Error("expected verification to succeed")
    }
}
```

### 11.3 Load Testing

```bash
# Test verification throughput
k6 run --vus 100 --duration 60s load-test.js

# Expected: 1000+ verifications/second with caching
```

## 12. Deployment Checklist

- [ ] Configure RPC nodes for all supported chains
- [ ] Set up monitoring and alerting
- [ ] Configure cache (Redis)
- [ ] Set up database for audit trail
- [ ] Test signature verification on testnet
- [ ] Test NFT verification on testnet
- [ ] Configure rate limiting
- [ ] Set up backup RPC nodes
- [ ] Test failover scenarios
- [ ] Document supported chains and contracts
- [ ] Set up security scanning for contracts
- [ ] Configure webhook handlers for cache invalidation
- [ ] Test batch verification performance
- [ ] Set up logging and tracing
- [ ] Document access control policies
- [ ] Train team on Web3 concepts

## 13. Common Pitfalls & Solutions

### Pitfall 1: Trusting Unverified RPC Responses
**Solution**: Always verify responses, use multiple RPC nodes, implement circuit breaker

### Pitfall 2: Not Handling Chain Reorgs
**Solution**: Wait for block confirmations (12+ blocks for Ethereum), use event listeners

### Pitfall 3: Ignoring Gas Costs
**Solution**: Use batch calls, cache results, consider L2 solutions

### Pitfall 4: Poor Error Handling
**Solution**: Implement retry logic, graceful degradation, comprehensive logging

### Pitfall 5: Centralized RPC Dependency
**Solution**: Use multiple RPC providers, implement fallback strategy, consider decentralized RPC

## 14. Future Enhancements

- [ ] Support for more chains (Avalanche, Fantom, Polygon zkEVM)
- [ ] Cross-chain verification (verify NFT on one chain, access content on another)
- [ ] Dynamic pricing based on gas costs
- [ ] Decentralized RPC network integration
- [ ] Zero-knowledge proof verification
- [ ] Soulbound token (SBT) support
- [ ] DAO governance integration
- [ ] Royalty distribution on content access

## References

- [EIP-191: Signed Data Standard](https://eips.ethereum.org/EIPS/eip-191)
- [ERC-721: Non-Fungible Token Standard](https://eips.ethereum.org/EIPS/eip-721)
- [ERC-1155: Multi Token Standard](https://eips.ethereum.org/EIPS/eip-1155)
- [Solana Program Library](https://github.com/solana-labs/solana-program-library)
- [Metaplex NFT Standard](https://docs.metaplex.com/)
- [Web3.js Documentation](https://web3js.readthedocs.io/)
- [Ethers.js Documentation](https://docs.ethers.org/)
- [Solana Web3.js Documentation](https://solana-labs.github.io/solana-web3.js/)
