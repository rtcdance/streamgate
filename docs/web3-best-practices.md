# Web3 Development Best Practices

## 🎯 Core Principles

### 1. Never Trust On-Chain Data Real-Time

**Problem**: Blockchain has confirmation delay (Ethereum ~15 seconds, Solana ~0.4 seconds)

**Best Practice**:
```go
// ❌ Wrong: Verify immediately
func TransferNFT(from, to string, tokenID int) {
    // User just transferred NFT
    transferNFT(from, to, tokenID)
    
    // Verify immediately (might fail! Transaction not confirmed)
    hasNFT := verifyNFT(to, tokenID)  // Returns false!
}

// ✅ Correct: Wait for confirmation or listen to events
func TransferNFT(from, to string, tokenID int) {
    txHash := transferNFT(from, to, tokenID)
    
    // Option 1: Wait for confirmation
    waitForConfirmation(txHash, 3)  // Wait 3 blocks
    
    // Option 2: Listen to events
    subscribeToTransferEvent(tokenID, func(event) {
        updatePermission(to, tokenID)
    })
}
```

### 2. Blockchain Data May Reorg (Reorg)

**Problem**: Blockchain may reorganize, confirmed transactions may be reverted

**Best Practice**:
```go
// Confirmation depth recommendations
const (
    SafeConfirmations = 12  // Ethereum: 12 blocks (~3 minutes)
    FastConfirmations = 3   // Fast confirmation (risky)
)

// Critical operations wait for more confirmations
func GrantAccess(user string, contentID string) error {
    hasNFT, blockNumber := verifyNFTWithBlock(user)
    
    currentBlock := getCurrentBlock()
    confirmations := currentBlock - blockNumber
    
    if confirmations < SafeConfirmations {
        return errors.New("waiting for more confirmations")
    }
    
    // Safe to grant access
    return grantAccess(user, contentID)
}
```

### 3. Contract Address May Be Malicious

**Problem**: User may provide fake NFT contract address

**Best Practice**:
```go
// Whitelist mechanism
var trustedContracts = map[string]bool{
    "0x1234...": true,  // Official NFT contract
    "0x5678...": true,  // Partner project contract
}

func VerifyNFT(user, contract string) (bool, error) {
    // 1. Check if contract in whitelist
    if !trustedContracts[contract] {
        return false, errors.New("untrusted contract")
    }
    
    // 2. Verify contract implements ERC-721 interface
    if !implementsERC721(contract) {
        return false, errors.New("not a valid ERC-721 contract")
    }
    
    // 3. Check if contract is verified (Etherscan)
    if !isVerifiedContract(contract) {
        log.Warn("unverified contract", "address", contract)
    }
    
    // 4. Execute balance query
    return checkBalance(user, contract)
}
```

### 4. RPC Nodes May Return Inconsistent Data

**Problem**: Different RPC nodes may be at different block heights

**Best Practice**:
```go
type ConsensusChecker struct {
    nodes []*RPCNode
}

// Multi-node consensus verification (for critical operations)
func (c *ConsensusChecker) VerifyWithConsensus(user, contract string) (bool, error) {
    results := make([]bool, len(c.nodes))
    
    // Concurrent query multiple nodes
    var wg sync.WaitGroup
    for i, node := range c.nodes {
        wg.Add(1)
        go func(idx int, n *RPCNode) {
            defer wg.Done()
            result, _ := n.CheckBalance(user, contract)
            results[idx] = result
        }(i, node)
    }
    wg.Wait()
    
    // Majority voting
    trueCount := 0
    for _, r := range results {
        if r {
            trueCount++
        }
    }
    
    // At least 2/3 nodes agree
    if trueCount >= len(c.nodes)*2/3 {
        return true, nil
    }
    
    return false, errors.New("no consensus")
}
```

## 🔐 Security Best Practices

### 1. Complete Signature Verification Flow

```go
type SignatureVerifier struct {
    redis *redis.Client
}

func (v *SignatureVerifier) VerifySignature(req VerifyRequest) error {
    // 1. Check timestamp (prevent expiration)
    timestamp := req.Timestamp
    if time.Since(time.Unix(timestamp, 0)) > 5*time.Minute {
        return errors.New("signature expired")
    }
    
    // 2. Check nonce (prevent replay)
    nonceKey := "nonce:" + req.Nonce
    exists, _ := v.redis.Exists(context.Background(), nonceKey).Result()
    if exists > 0 {
        return errors.New("nonce already used")
    }
    
    // 3. Verify signature
    message := fmt.Sprintf(
        "Sign this to login:\nNonce: %s\nTimestamp: %d\nChain: %s",
        req.Nonce, timestamp, req.ChainType,
    )
    
    recoveredAddr, err := recoverAddress(message, req.Signature)
    if err != nil {
        return err
    }
    
    if !strings.EqualFold(recoveredAddr.Hex(), req.Address) {
        return errors.New("signature verification failed")
    }
    
    // 4. Mark nonce as used
    v.redis.Set(context.Background(), nonceKey, "1", 10*time.Minute)
    
    // 5. Record audit log
    log.Info("signature verified",
        "address", req.Address,
        "chain", req.ChainType,
        "timestamp", timestamp,
    )
    
    return nil
}
```

### 2. Address Validation and Normalization

```go
// Ethereum address validation
func ValidateEthereumAddress(addr string) (common.Address, error) {
    // 1. Check format
    if !strings.HasPrefix(addr, "0x") {
        return common.Address{}, errors.New("address must start with 0x")
    }
    
    // 2. Check length
    if len(addr) != 42 {  // 0x + 40 hex chars
        return common.Address{}, errors.New("invalid address length")
    }
    
    // 3. Check valid hex
    if !common.IsHexAddress(addr) {
        return common.Address{}, errors.New("invalid hex address")
    }
    
    // 4. Normalize (unified case)
    address := common.HexToAddress(addr)
    
    // 5. Verify checksum (if provided)
    if addr != address.Hex() && addr != strings.ToLower(address.Hex()) {
        // Address has checksum but incorrect
        return common.Address{}, errors.New("invalid address checksum")
    }
    
    return address, nil
}

// Solana address validation
func ValidateSolanaAddress(addr string) (solana.PublicKey, error) {
    pubKey, err := solana.PublicKeyFromBase58(addr)
    if err != nil {
        return solana.PublicKey{}, errors.New("invalid solana address")
    }
    
    // Solana address length fixed at 32 bytes
    if len(pubKey) != 32 {
        return solana.PublicKey{}, errors.New("invalid address length")
    }
    
    return pubKey, nil
}
```

### 3. Prevent Common Contract Call Vulnerabilities

```go
// Prevent integer overflow
func SafeAdd(a, b *big.Int) (*big.Int, error) {
    result := new(big.Int).Add(a, b)
    
    // Check overflow
    if result.Cmp(a) < 0 {
        return nil, errors.New("integer overflow")
    }
    
    return result, nil
}

// Prevent division by zero
func SafeDiv(a, b *big.Int) (*big.Int, error) {
    if b.Cmp(big.NewInt(0)) == 0 {
        return nil, errors.New("division by zero")
    }
    
    return new(big.Int).Div(a, b), nil
}

// Safe contract call
func SafeContractCall(contract *Contract, method string, args ...interface{}) (interface{}, error) {
    // 1. Set timeout
    ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
    defer cancel()
    
    // 2. Use recover to catch panic
    defer func() {
        if r := recover(); r != nil {
            log.Error("contract call panic", "error", r)
        }
    }()
    
    // 3. Execute call
    result, err := contract.Call(&bind.CallOpts{Context: ctx}, method, args...)
    if err != nil {
        // 4. Parse error
        if strings.Contains(err.Error(), "execution reverted") {
            return nil, errors.New("contract execution reverted")
        }
        return nil, err
    }
    
    return result, nil
}
```

## ⚡ Performance Optimization Best Practices

### 1. Smart Caching Strategy

```go
type NFTCache struct {
    l1 *LRUCache      // Memory cache
    l2 *redis.Client  // Redis cache
}

// Layered caching strategy
func (c *NFTCache) GetNFTBalance(user, contract string) (int, error) {
    cacheKey := fmt.Sprintf("nft:%s:%s", user, contract)
    
    // L1: Memory cache (fastest, 1ms)
    if val, found := c.l1.Get(cacheKey); found {
        metrics.CacheHit("l1")
        return val.(int), nil
    }
    
    // L2: Redis cache (fast, 10ms)
    if val, err := c.l2.Get(context.Background(), cacheKey).Int(); err == nil {
        metrics.CacheHit("l2")
        c.l1.Set(cacheKey, val)  // Refill L1
        return val, nil
    }
    
    // L3: On-chain query (slow, 500ms)
    metrics.CacheMiss()
    balance, err := queryBlockchain(user, contract)
    if err != nil {
        return 0, err
    }
    
    // Refill cache
    c.l1.Set(cacheKey, balance)
    c.l2.Set(context.Background(), cacheKey, balance, 5*time.Minute)
    
    return balance, nil
}

// Cache invalidation strategy
func (c *NFTCache) InvalidateOnTransfer(from, to, contract string, tokenID int) {
    // Listen to Transfer events, proactively invalidate cache
    keys := []string{
        fmt.Sprintf("nft:%s:%s", from, contract),
        fmt.Sprintf("nft:%s:%s", to, contract),
    }
    
    for _, key := range keys {
        c.l1.Delete(key)
        c.l2.Del(context.Background(), key)
    }
    
    log.Info("cache invalidated", "keys", keys)
}
```

### 2. Batch Query Optimization

```go
// Multicall contract (query multiple data in one call)
type Multicall struct {
    contract *Contract
}

func (m *Multicall) BatchCheckBalance(users []string, contract string) (map[string]int, error) {
    // Construct batch calls
    calls := make([]Call, len(users))
    for i, user := range users {
        calls[i] = Call{
            Target: contract,
            CallData: encodeBalanceOf(user),
        }
    }
    
    // One RPC call gets all results
    results, err := m.contract.Aggregate(calls)
    if err != nil {
        return nil, err
    }
    
    // Parse results
    balances := make(map[string]int)
    for i, result := range results {
        balance := decodeBalance(result)
        balances[users[i]] = balance
    }
    
    return balances, nil
}

// Usage example
func CheckMultipleUsers(users []string, contract string) {
    // ❌ Wrong: Loop queries (N RPC calls)
    for _, user := range users {
        balance := checkBalance(user, contract)  // Each 500ms
    }
    // Total time: N * 500ms
    
    // ✅ Correct: Batch query (1 RPC call)
    balances := multicall.BatchCheckBalance(users, contract)  // One 500ms
    // Total time: 500ms
}
```

### 3. Async Verification

```go
type AsyncVerifier struct {
    queue chan VerifyTask
    cache *NFTCache
}

// Async verification (don't block main flow)
func (v *AsyncVerifier) VerifyAsync(user, contract string) <-chan VerifyResult {
    resultChan := make(chan VerifyResult, 1)
    
    task := VerifyTask{
        User:     user,
        Contract: contract,
        Result:   resultChan,
    }
    
    // Submit to queue
    v.queue <- task
    
    return resultChan
}

// Worker processes verification tasks
func (v *AsyncVerifier) worker() {
    for task := range v.queue {
        // 1. Return cached result first (if exists)
        if cached, found := v.cache.Get(task.User, task.Contract); found {
            task.Result <- VerifyResult{HasNFT: cached, Cached: true}
            continue
        }
        
        // 2. Query on-chain
        hasNFT, err := queryBlockchain(task.User, task.Contract)
        
        // 3. Update cache
        if err == nil {
            v.cache.Set(task.User, task.Contract, hasNFT)
        }
        
        // 4. Return result
        task.Result <- VerifyResult{HasNFT: hasNFT, Error: err}
    }
}

// Usage example
func HandleRequest(user, contentID string) {
    // Return immediately (don't wait for verification)
    verifyResult := verifier.VerifyAsync(user, contract)
    
    // Return response
    w.Write([]byte("Verification in progress..."))
    
    // Wait for verification result in background
    go func() {
        result := <-verifyResult
        if result.HasNFT {
            grantAccess(user, contentID)
            notifyUser(user, "Access granted")
        }
    }()
}
```

## 🎯 Multi-Chain Support Best Practices

### 1. Unified Abstraction Layer

```go
// Chain type enum
type ChainType string

const (
    ChainEthereum ChainType = "ethereum"
    ChainPolygon  ChainType = "polygon"
    ChainBSC      ChainType = "bsc"
    ChainSolana   ChainType = "solana"
)

// Unified NFT interface
type NFTProvider interface {
    // Basic methods
    GetChainType() ChainType
    GetChainID() int64
    
    // NFT queries
    GetBalance(ctx context.Context, owner, contract string) (*big.Int, error)
    GetOwner(ctx context.Context, contract, tokenID string) (string, error)
    GetTokenURI(ctx context.Context, contract, tokenID string) (string, error)
    
    // Batch queries
    BatchGetBalance(ctx context.Context, owners []string, contract string) (map[string]*big.Int, error)
    
    // Health check
    HealthCheck(ctx context.Context) error
}

// Factory pattern creates Provider
func NewNFTProvider(chainType ChainType, config Config) (NFTProvider, error) {
    switch chainType {
    case ChainEthereum, ChainPolygon, ChainBSC:
        return NewEVMProvider(chainType, config)
    case ChainSolana:
        return NewSolanaProvider(config)
    default:
        return nil, errors.New("unsupported chain")
    }
}
```

### 2. Chain-Specific Optimizations

```go
// EVM chain optimization
type EVMProvider struct {
    client    *ethclient.Client
    multicall *Multicall  // Batch queries
}

// Solana chain optimization
type SolanaProvider struct {
    client *rpc.Client
    // Solana uses getProgramAccounts for batch queries
}

func (p *SolanaProvider) GetBalance(ctx context.Context, owner, mint string) (*big.Int, error) {
    // Solana specific: Query Token Account
    tokenAccount, err := p.getAssociatedTokenAddress(owner, mint)
    if err != nil {
        return big.NewInt(0), err
    }
    
    // Query account balance
    balance, err := p.client.GetTokenAccountBalance(ctx, tokenAccount)
    if err != nil {
        return big.NewInt(0), err
    }
    
    return balance.Value.Amount, nil
}
```

## 📊 Monitoring and Alerting

### 1. Key Metrics

```go
var (
    // RPC call metrics
    rpcCallsTotal = promauto.NewCounterVec(
        prometheus.CounterOpts{
            Name: "rpc_calls_total",
            Help: "Total RPC calls",
        },
        []string{"chain", "method", "status"},
    )
    
    rpcCallDuration = promauto.NewHistogramVec(
        prometheus.HistogramOpts{
            Name: "rpc_call_duration_seconds",
            Help: "RPC call duration",
            Buckets: []float64{0.1, 0.5, 1, 2, 5},
        },
        []string{"chain", "method"},
    )
    
    // NFT verification metrics
    nftVerificationsTotal = promauto.NewCounterVec(
        prometheus.CounterOpts{
            Name: "nft_verifications_total",
            Help: "Total NFT verifications",
        },
        []string{"chain", "result"},
    )
    
    // Cache metrics
    nftCacheHitRate = promauto.NewGaugeVec(
        prometheus.GaugeOpts{
            Name: "nft_cache_hit_rate",
            Help: "NFT cache hit rate",
        },
        []string{"level"},
    )
)

// Record metrics
func (p *EVMProvider) GetBalance(ctx context.Context, owner, contract string) (*big.Int, error) {
    start := time.Now()
    
    balance, err := p.client.BalanceAt(ctx, common.HexToAddress(owner), nil)
    
    // Record duration
    duration := time.Since(start).Seconds()
    rpcCallDuration.WithLabelValues(string(p.chainType), "balanceOf").Observe(duration)
    
    // Record call count
    status := "success"
    if err != nil {
        status = "error"
    }
    rpcCallsTotal.WithLabelValues(string(p.chainType), "balanceOf", status).Inc()
    
    return balance, err
}
```

### 2. Alert Rules

```yaml
# prometheus-alerts.yml
groups:
  - name: web3
    rules:
      # RPC node down
      - alert: RPCNodeDown
        expr: up{job="rpc-node"} == 0
        for: 1m
        annotations:
          summary: "RPC node is down"
          
      # RPC call error rate high
      - alert: HighRPCErrorRate
        expr: rate(rpc_calls_total{status="error"}[5m]) > 0.1
        for: 5m
        annotations:
          summary: "RPC error rate > 10%"
          
      # NFT verification failure rate high
      - alert: HighNFTVerificationFailure
        expr: rate(nft_verifications_total{result="error"}[5m]) > 0.05
        for: 5m
        annotations:
          summary: "NFT verification failure rate > 5%"
          
      # Cache hit rate low
      - alert: LowCacheHitRate
        expr: nft_cache_hit_rate < 0.7
        for: 10m
        annotations:
          summary: "Cache hit rate < 70%"
```

## 🧪 Testing Best Practices

### 1. Use Testnet

```go
// Test configuration
const (
    TestnetRPC = "https://sepolia.infura.io/v3/YOUR_KEY"
    TestnetChainID = 11155111  // Sepolia
    TestNFTContract = "0x..."   // Your test contract
)

// Integration test
func TestNFTVerification(t *testing.T) {
    // 1. Connect to testnet
    client, err := ethclient.Dial(TestnetRPC)
    require.NoError(t, err)
    
    // 2. Use test account
    testWallet := "0x..."  // Your test wallet
    
    // 3. Verify NFT
    provider := NewEVMProvider(ChainEthereum, Config{
        RPC: TestnetRPC,
    })
    
    balance, err := provider.GetBalance(context.Background(), testWallet, TestNFTContract)
    require.NoError(t, err)
    assert.True(t, balance.Cmp(big.NewInt(0)) > 0)
}
```

### 2. Mock On-Chain Calls

```go
// Mock RPC client
type MockRPCClient struct {
    mock.Mock
}

func (m *MockRPCClient) BalanceAt(ctx context.Context, account common.Address, blockNumber *big.Int) (*big.Int, error) {
    args := m.Called(ctx, account, blockNumber)
    return args.Get(0).(*big.Int), args.Error(1)
}

// Unit test
func TestNFTVerificationWithMock(t *testing.T) {
    mockClient := new(MockRPCClient)
    
    // Set expectations
    mockClient.On("BalanceAt", mock.Anything, mock.Anything, mock.Anything).
        Return(big.NewInt(1), nil)
    
    // Test
    provider := &EVMProvider{client: mockClient}
    balance, err := provider.GetBalance(context.Background(), "0x...", "0x...")
    
    assert.NoError(t, err)
    assert.Equal(t, big.NewInt(1), balance)
    mockClient.AssertExpectations(t)
}
```

## 🚨 Common Errors and Solutions

### 1. "execution reverted"

**Cause**: Contract call failed
**Solution**:
- Check contract address is correct
- Check function parameter types
- Check contract implements the function
- Test call on Etherscan

### 2. "insufficient funds for gas"

**Cause**: Account balance insufficient (only for sending transactions)
**Solution**: Your project only does read-only operations, won't encounter this

### 3. "nonce too low"

**Cause**: Transaction nonce conflict (only for sending transactions)
**Solution**: Your project doesn't send transactions, won't encounter this

### 4. "context deadline exceeded"

**Cause**: RPC call timeout
**Solution**:
```go
ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
defer cancel()
```

## 📝 Summary

Web3 development core principles:
1. **Don't trust**: Verify everything
2. **Async**: Don't block main flow
3. **Cache**: Reduce on-chain queries
4. **Fault-tolerant**: Handle failures gracefully
5. **Monitor**: Detect problems early

Remember: Your project is off-chain service, only does read-only operations, no private key management, no gas payment. This greatly reduces complexity!
