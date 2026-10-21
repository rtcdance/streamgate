# Web3 Troubleshooting Guide

## 🔍 Diagnostic Tools

### 1. Quick Diagnostic Script

```bash
#!/bin/bash
# scripts/diagnose-web3.sh

echo "=== Web3 Connection Diagnostics ==="

# Check Ethereum RPC
echo -n "Checking Ethereum RPC... "
if curl -s -X POST -H "Content-Type: application/json" \
    --data '{"jsonrpc":"2.0","method":"eth_blockNumber","params":[],"id":1}' \
    $ETH_RPC_URL | grep -q "result"; then
    echo "✅ OK"
else
    echo "❌ Failed"
fi

# Check contract address
echo -n "Checking NFT contract... "
if curl -s "https://sepolia.etherscan.io/api?module=contract&action=getabi&address=$NFT_CONTRACT" \
    | grep -q "result"; then
    echo "✅ Exists"
else
    echo "❌ Not found or unverified"
fi

# Check wallet balance
echo -n "Checking wallet ETH balance... "
BALANCE=$(curl -s -X POST -H "Content-Type: application/json" \
    --data "{\"jsonrpc\":\"2.0\",\"method\":\"eth_getBalance\",\"params\":[\"$WALLET_ADDRESS\",\"latest\"],\"id\":1}" \
    $ETH_RPC_URL | jq -r '.result')
echo "$BALANCE"

echo "=== Diagnostics Complete ==="
```

### 2. Go Diagnostic Tool

```go
// cmd/diagnose/main.go
package main

import (
    "context"
    "fmt"
    "time"
)

func main() {
    fmt.Println("=== Web3 System Diagnostics ===\n")

    // 1. Check RPC connection
    fmt.Println("1. Checking RPC connection...")
    if err := checkRPCConnection(); err != nil {
        fmt.Printf("   ❌ RPC connection failed: %v\n", err)
    } else {
        fmt.Println("   ✅ RPC connection OK")
    }

    // 2. Check contract
    fmt.Println("\n2. Checking NFT contract...")
    if err := checkContract(); err != nil {
        fmt.Printf("   ❌ Contract check failed: %v\n", err)
    } else {
        fmt.Println("   ✅ Contract OK")
    }

    // 3. Check cache
    fmt.Println("\n3. Checking Redis cache...")
    if err := checkRedis(); err != nil {
        fmt.Printf("   ❌ Redis connection failed: %v\n", err)
    } else {
        fmt.Println("   ✅ Redis OK")
    }

    // 4. Performance test
    fmt.Println("\n4. Performance test...")
    testPerformance()

    fmt.Println("\n=== Diagnostics Complete ===")
}

func checkRPCConnection() error {
    ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
    defer cancel()

    client, err := ethclient.Dial(os.Getenv("ETH_RPC_URL"))
    if err != nil {
        return err
    }
    defer client.Close()

    blockNumber, err := client.BlockNumber(ctx)
    if err != nil {
        return err
    }

    fmt.Printf("   Current block: %d\n", blockNumber)
    return nil
}

func testPerformance() {
    // Test NFT verification performance
    start := time.Now()
    _, err := verifyNFT(testWallet, testContract)
    duration := time.Since(start)

    if err != nil {
        fmt.Printf("   ❌ Verification failed: %v\n", err)
    } else {
        fmt.Printf("   ✅ Verification time: %v\n", duration)
        if duration > 500*time.Millisecond {
            fmt.Println("   ⚠️  Performance slow, check cache")
        }
    }
}
```

## 🚨 Common Problems Troubleshooting

### Problem 1: "connection refused"

**Symptom**:
```
Error: dial tcp: connection refused
```

**Possible Causes**:
1. RPC URL incorrect
2. Network problem
3. RPC service down

**Troubleshooting Steps**:
```bash
# 1. Check RPC URL
echo $ETH_RPC_URL

# 2. Test connection
curl -X POST -H "Content-Type: application/json" \
    --data '{"jsonrpc":"2.0","method":"eth_blockNumber","params":[],"id":1}' \
    $ETH_RPC_URL

# 3. Try other RPC nodes
# Infura
curl https://sepolia.infura.io/v3/YOUR_KEY

# Alchemy
curl https://eth-sepolia.g.alchemy.com/v2/YOUR_KEY
```

**Solution**:
```go
// Configure multiple RPC nodes
nodes := []string{
    "https://sepolia.infura.io/v3/KEY1",
    "https://eth-sepolia.g.alchemy.com/v2/KEY2",
    "https://rpc.sepolia.org",
}

pool := NewRPCPool(nodes)
```

### Problem 2: "execution reverted"

**Symptom**:
```
Error: execution reverted
```

**Possible Causes**:
1. Contract address wrong
2. Function doesn't exist
3. Parameter type wrong
4. Contract logic error

**Troubleshooting Steps**:
```bash
# 1. View contract on Etherscan
https://sepolia.etherscan.io/address/YOUR_CONTRACT

# 2. Check if contract verified
# 3. View contract ABI
# 4. Test call on Etherscan
```

**Solution**:
```go
// Add detailed logging
log.Debug("calling contract",
    "contract", contractAddr,
    "function", "balanceOf",
    "params", []interface{}{walletAddr},
)

result, err := contract.BalanceOf(nil, walletAddr)
if err != nil {
    log.Error("contract call failed",
        "error", err,
        "contract", contractAddr,
        "wallet", walletAddr,
    )
    return nil, err
}
```

### Problem 3: "invalid API key"

**Symptom**:
```
Error: invalid project id
Error: 401 Unauthorized
```

**Possible Causes**:
1. API Key wrong
2. API Key expired
3. Free tier limit exceeded

**Troubleshooting Steps**:
```bash
# 1. Check API Key
echo $INFURA_API_KEY

# 2. Test API Key
curl https://sepolia.infura.io/v3/$INFURA_API_KEY \
    -X POST \
    -H "Content-Type: application/json" \
    -d '{"jsonrpc":"2.0","method":"eth_blockNumber","params":[],"id":1}'

# 3. Check Infura console
# https://infura.io/dashboard
```

**Solution**:
1. Regenerate API Key
2. Upgrade to paid plan
3. Use multiple API Keys in rotation

### Problem 4: "context deadline exceeded"

**Symptom**:
```
Error: context deadline exceeded
```

**Possible Causes**:
1. RPC response slow
2. Network latency high
3. Timeout too short

**Troubleshooting Steps**:
```go
// Test RPC latency
start := time.Now()
_, err := client.BlockNumber(context.Background())
latency := time.Since(start)
fmt.Printf("RPC latency: %v\n", latency)
```

**Solution**:
```go
// Increase timeout
ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
defer cancel()

// Or use retry
func callWithRetry(fn func() error, maxRetries int) error {
    for i := 0; i < maxRetries; i++ {
        err := fn()
        if err == nil {
            return nil
        }
        
        if strings.Contains(err.Error(), "context deadline exceeded") {
            time.Sleep(time.Second * time.Duration(i+1))
            continue
        }
        
        return err
    }
    return errors.New("max retries exceeded")
}
```

### Problem 5: NFT Verification Returns Wrong Result

**Symptom**:
- User has NFT but verification returns false
- User just transferred NFT but verification shows old data

**Possible Causes**:
1. Cache not updated
2. Transaction not confirmed
3. Queried wrong block

**Troubleshooting Steps**:
```go
// 1. Check cache
cacheKey := fmt.Sprintf("nft:%s:%s", wallet, contract)
cached, _ := redis.Get(cacheKey).Result()
fmt.Printf("Cached value: %s\n", cached)

// 2. Query on-chain directly
balance, _ := client.BalanceAt(context.Background(), address, nil)
fmt.Printf("On-chain balance: %s\n", balance)

// 3. Check block confirmations
currentBlock, _ := client.BlockNumber(context.Background())
txBlock := getTransactionBlock(txHash)
confirmations := currentBlock - txBlock
fmt.Printf("Confirmations: %d\n", confirmations)
```

**Solution**:
```go
// 1. Clear cache
func InvalidateCache(wallet, contract string) {
    cacheKey := fmt.Sprintf("nft:%s:%s", wallet, contract)
    redis.Del(cacheKey)
}

// 2. Wait for confirmations
func VerifyWithConfirmations(wallet, contract string, minConfirmations int) (bool, error) {
    balance, blockNumber, err := getBalanceWithBlock(wallet, contract)
    if err != nil {
        return false, err
    }
    
    currentBlock, _ := client.BlockNumber(context.Background())
    confirmations := currentBlock - blockNumber
    
    if confirmations < uint64(minConfirmations) {
        return false, errors.New("waiting for confirmations")
    }
    
    return balance > 0, nil
}

// 3. Listen to Transfer events
func WatchTransferEvents(contract string) {
    query := ethereum.FilterQuery{
        Addresses: []common.Address{common.HexToAddress(contract)},
    }
    
    logs := make(chan types.Log)
    sub, _ := client.SubscribeFilterLogs(context.Background(), query, logs)
    
    for {
        select {
        case log := <-logs:
            // Parse Transfer event
            from, to, tokenID := parseTransferEvent(log)
            
            // Invalidate related cache
            InvalidateCache(from, contract)
            InvalidateCache(to, contract)
            
        case err := <-sub.Err():
            log.Error("subscription error", "error", err)
        }
    }
}
```

### Problem 6: Solana Query Failed

**Symptom**:
```
Error: failed to get account info
```

**Possible Causes**:
1. Token Account doesn't exist
2. RPC node problem
3. Address format wrong

**Troubleshooting Steps**:
```bash
# 1. Check wallet address
solana address

# 2. View Token Accounts
spl-token accounts

# 3. Test RPC
curl https://api.devnet.solana.com -X POST -H "Content-Type: application/json" \
    -d '{"jsonrpc":"2.0","id":1,"method":"getHealth"}'
```

**Solution**:
```go
// Solana needs Associated Token Account first
func GetSolanaNFTBalance(owner, mint string) (int, error) {
    // 1. Calculate Associated Token Account address
    ata, err := getAssociatedTokenAddress(owner, mint)
    if err != nil {
        return 0, err
    }
    
    // 2. Query account info
    accountInfo, err := client.GetAccountInfo(context.Background(), ata)
    if err != nil {
        // Token Account doesn't exist = balance 0
        if strings.Contains(err.Error(), "not found") {
            return 0, nil
        }
        return 0, err
    }
    
    // 3. Parse balance
    balance := parseTokenAccountBalance(accountInfo)
    return balance, nil
}
```

## 📊 Monitoring and Alerting

### 1. Key Metrics Monitoring

```go
// Monitor RPC call failure rate
if rpcErrorRate > 0.1 {  // 10%
    alert("RPC error rate too high")
}

// Monitor NFT verification latency
if nftVerifyLatency > 1*time.Second {
    alert("NFT verification too slow")
}

// Monitor cache hit rate
if cacheHitRate < 0.7 {  // 70%
    alert("Cache hit rate too low")
}
```

### 2. Log Analysis

```bash
# Find RPC errors
grep "RPC error" logs/app.log | tail -20

# Count error types
grep "error" logs/app.log | awk '{print $5}' | sort | uniq -c

# View slow queries
grep "slow query" logs/app.log | awk '{print $NF}' | sort -n
```

## 🔧 Debugging Techniques

### 1. Enable Detailed Logging

```go
// Development environment
log.SetLevel(log.DebugLevel)

// Production environment
log.SetLevel(log.InfoLevel)

// Temporarily enable debugging
if os.Getenv("DEBUG") == "true" {
    log.SetLevel(log.DebugLevel)
}
```

### 2. Use Etherscan API

```go
// Verify contract exists
func VerifyContract(address string) error {
    url := fmt.Sprintf(
        "https://api-sepolia.etherscan.io/api?module=contract&action=getabi&address=%s&apikey=%s",
        address, etherscanAPIKey,
    )
    
    resp, err := http.Get(url)
    if err != nil {
        return err
    }
    defer resp.Body.Close()
    
    var result struct {
        Status  string `json:"status"`
        Message string `json:"message"`
    }
    
    json.NewDecoder(resp.Body).Decode(&result)
    
    if result.Status != "1" {
        return errors.New("contract not found or not verified")
    }
    
    return nil
}
```

### 3. Mock On-Chain Data

```go
// Use Mock data for testing
type MockBlockchain struct {
    balances map[string]int
}

func (m *MockBlockchain) GetBalance(wallet, contract string) (int, error) {
    key := wallet + ":" + contract
    if balance, ok := m.balances[key]; ok {
        return balance, nil
    }
    return 0, nil
}

// Test
func TestNFTVerification(t *testing.T) {
    mock := &MockBlockchain{
        balances: map[string]int{
            "0x123:0xabc": 1,  // Has NFT
            "0x456:0xabc": 0,  // No NFT
        },
    }
    
    // Test...
}
```

## 📝 Troubleshooting Checklist

When encountering problems, check in order:

- [ ] Check RPC connection
- [ ] Check API Key
- [ ] Check contract address
- [ ] Check wallet address format
- [ ] Check network (mainnet vs testnet)
- [ ] Check cache
- [ ] Check logs
- [ ] Check monitoring metrics
- [ ] Verify on Etherscan
- [ ] Use diagnostic tools

## 🆘 Getting Help

If above methods don't solve the problem:

1. **Check Official Documentation**
   - go-ethereum: https://geth.ethereum.org/docs
   - Solana: https://docs.solana.com

2. **Search Known Issues**
   - GitHub Issues
   - Stack Overflow
   - Ethereum Stack Exchange

3. **Ask Questions**
   - Provide complete error message
   - Provide reproduction steps
   - Provide environment info (Go version, OS, etc.)

4. **Community**
   - Ethereum Discord
   - Solana Discord
   - r/ethdev (Reddit)

Remember: Most problems are configuration issues. Careful checking usually solves 80% of problems!
