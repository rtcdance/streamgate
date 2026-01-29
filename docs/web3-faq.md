# Web3 Development Frequently Asked Questions

## 🤔 Concept Understanding

### Q1: What is the relationship between blockchain, Ethereum, and NFT?
**A**: 
- **Blockchain**: Underlying technology, distributed ledger
- **Ethereum**: A blockchain that supports smart contracts
- **NFT**: Smart contract running on Ethereum, follows ERC-721 standard

Analogy:
- Blockchain = Internet
- Ethereum = A website (like Google)
- NFT = A feature on the website

### Q2: Why do we need off-chain services? Can't we just use smart contracts?
**A**: Smart contracts have many limitations:
1. **Storage is expensive**: Storing 1MB of data costs thousands of dollars
2. **Computation is slow**: Video transcoding takes minutes, impossible on-chain
3. **Gas fees**: Every operation costs money, poor user experience
4. **Privacy**: On-chain data is public, unsuitable for private content

**Best Practice**:
- Permission control: On-chain (NFT ownership)
- Content storage: Off-chain (MinIO/IPFS)
- Access verification: Off-chain (fast response)

### Q3: What is Gas fee? Does my project need to pay it?
**A**: 
- **Gas fee**: Cost of executing operations on blockchain
- **Read-only operations**: No gas needed (like querying NFT balance)
- **Write operations**: Gas required (like transfers, minting NFT)

**Your project**:
- ✅ Only performs read-only operations (query NFT)
- ✅ No gas payment needed
- ✅ No private key management needed

### Q4: What's the difference between mainnet and testnet?
**A**:
| Feature | Mainnet | Testnet |
|---------|---------|---------|
| Tokens | Real ETH, has value | Test ETH, no value |
| Purpose | Production | Development/Testing |
| Get tokens | Purchase | Free claim |
| Transaction speed | Slower (congested) | Faster |
| Data | Permanent | May reset |

**Recommendation**:
- Development phase: Only use testnet
- After testing: Deploy to mainnet

### Q5: Why do we need RPC nodes?
**A**: 
- Blockchain nodes store complete blockchain data
- Running your own node requires hundreds of GB storage and high bandwidth
- RPC node providers (Infura, Alchemy) run nodes for you
- You call their nodes via API

Analogy:
- Running your own node = Running your own server
- Using RPC service = Using cloud service (AWS)

## 🔧 Technical Questions

### Q6: How to verify user actually owns a wallet address?
**A**: Through signature verification:

```
1. Backend generates random nonce
2. User signs nonce with private key
3. Backend verifies signature with public key
4. Verification passes = User owns that address
```

**Key**: Private key never leaves user's device!

### Q7: How to prevent signature replay attacks?
**A**: Triple protection:

1. **Nonce**: Generate new random number each login
2. **Timestamp**: Include timestamp in signed message
3. **One-time use**: Delete nonce immediately after use

```go
// Signature message format
message := fmt.Sprintf(
    "Sign this to login:\nNonce: %s\nTimestamp: %d",
    nonce, time.Now().Unix()
)

// Verify on verification
if time.Since(timestamp) > 5*time.Minute {
    return errors.New("signature expired")
}
if redis.Exists("nonce:"+nonce) {
    return errors.New("nonce already used")
}
```

### Q8: What's the difference between ERC-721 and ERC-1155?
**A**:

**ERC-721** (Non-Fungible Token):
- Each token is unique
- Token ID cannot be duplicated
- Suitable for: Art, collectibles
- Example: CryptoPunks, Bored Ape

**ERC-1155** (Multi Token):
- Can have multiple identical tokens
- One contract manages multiple token types
- Suitable for: Game items, tickets
- Example: Game weapons (can have 100 identical swords)

**Your project**: Support both!

### Q9: How to handle RPC node failures?
**A**: Implement node pool and failover:

```go
type RPCPool struct {
    nodes []*RPCNode  // Multiple nodes
}

func (p *RPCPool) Call() {
    for _, node := range p.nodes {
        result, err := node.Call()
        if err == nil {
            return result  // Success
        }
        // Failed, try next node
    }
    return errors.New("all nodes failed")
}
```

**Best Practice**:
- Configure 3-5 RPC nodes
- Regular health checks
- Prioritize fast-responding nodes

### Q10: How to optimize on-chain query performance?
**A**: Multi-layer optimization:

1. **Caching**: Cache query results for 5 minutes
```go
cacheKey := fmt.Sprintf("nft:%s:%s", address, contract)
if cached := redis.Get(cacheKey); cached != nil {
    return cached  // Cache hit
}
```

2. **Batch queries**: Query multiple addresses at once
```go
// Bad: Loop queries
for _, addr := range addresses {
    balance := queryBalance(addr)  // N RPC calls
}

// Good: Batch query
balances := batchQueryBalance(addresses)  // 1 RPC call
```

3. **Async queries**: Don't block main flow
```go
go func() {
    balance := queryBalance(addr)
    cache.Set(addr, balance)
}()
```

## 🔐 Security Questions

### Q11: What's the difference between private key, seed phrase, and Keystore?
**A**:

- **Private key**: 64-character hex string
  - Example: `0x1234...abcd`
  - Directly controls account
  
- **Seed phrase**: 12 or 24 English words
  - Example: `apple banana cherry ...`
  - Can generate multiple private keys
  - Easier to backup
  
- **Keystore**: Encrypted private key file
  - JSON format
  - Requires password to unlock

**Important**: Your project doesn't need to manage private keys!

### Q12: How to safely store API Key?
**A**: 

❌ **Wrong way**:
```go
const INFURA_KEY = "abc123"  // Hardcoded
```

✅ **Right way**:
```go
// 1. Use environment variables
apiKey := os.Getenv("INFURA_API_KEY")

// 2. Use config file (don't commit to Git)
// config.yaml
infura:
  api_key: ${INFURA_API_KEY}

// 3. Use secret management service
// AWS Secrets Manager, HashiCorp Vault
```

**.gitignore**:
```
config.yaml
.env
*.key
```

### Q13: How to prevent API abuse?
**A**: Multi-layer protection:

1. **Authentication**: JWT token
2. **Rate limiting**: 100 req/min per user
3. **Circuit breaker**: Stop service if failure rate > 50%
4. **Monitoring**: Alert on abnormal traffic

```go
// Rate limiting example
limiter := rate.NewLimiter(100, 10)  // 100 req/min
if !limiter.Allow() {
    return errors.New("rate limit exceeded")
}
```

## 🚀 Development Questions

### Q14: How to debug smart contract calls?
**A**: 

1. **Use Etherscan**:
   - View contract source code
   - View ABI
   - Call functions online

2. **Use Remix**:
   - Connect to testnet
   - Call contract directly
   - View return values

3. **Print debug info**:
```go
log.Printf("Calling contract: %s", contractAddr)
log.Printf("Function: balanceOf(%s)", walletAddr)
result, err := contract.BalanceOf(nil, walletAddr)
log.Printf("Result: %v, Error: %v", result, err)
```

### Q15: Testnet transaction always pending?
**A**: 

1. **Wait longer**: Testnet can be slow (5-10 minutes)
2. **Check Gas fee**: Might be set too low
3. **View on Etherscan**: Check transaction status
4. **Resend**: Increase gas fee

**Note**: Testnet instability is normal!

### Q16: How to test NFT verification functionality?
**A**: 

1. **Deploy test contract**:
```solidity
contract TestNFT is ERC721 {
    function mint(address to) public {
        _mint(to, tokenId++);
    }
}
```

2. **Mint test NFT**:
```bash
# Call mint function in Remix
mint(0x your_address)
```

3. **Test verification**:
```go
hasNFT := verifyNFTOwnership(
    "0x your_address",
    "0x contract_address",
)
assert.True(t, hasNFT)
```

### Q17: How to simulate multiple users for testing?
**A**: 

1. **Create multiple test wallets**:
```go
wallet1, _ := crypto.GenerateKey()
wallet2, _ := crypto.GenerateKey()
wallet3, _ := crypto.GenerateKey()
```

2. **Mint NFT separately**:
```
mint(wallet1.Address)
mint(wallet2.Address)
// wallet3 not minted (test no permission)
```

3. **Test different scenarios**:
```go
// wallet1 has NFT, should pass
assert.True(t, verify(wallet1.Address))

// wallet3 no NFT, should reject
assert.False(t, verify(wallet3.Address))
```

## 📊 Performance Questions

### Q18: RPC calls are very slow?
**A**: 

1. **Check network**: Testnet might be slow
2. **Switch nodes**: Try different RPC providers
3. **Use caching**: Avoid repeated queries
4. **Batch queries**: Reduce RPC call count

**Performance comparison**:
- No cache: 500ms per query
- With cache: < 1ms on hit
- Improvement: 500x!

### Q19: How to improve cache hit rate?
**A**: 

1. **Reasonable TTL**:
   - NFT balance: 5 minutes (doesn't change often)
   - Block height: 10 seconds (changes often)

2. **Warm up popular content**:
```go
// Warm cache on startup
func warmUpCache() {
    popularContent := getPopularContent()
    for _, content := range popularContent {
        cache.Set(content.ID, content)
    }
}
```

3. **Monitor hit rate**:
```go
hitRate := cacheHits / (cacheHits + cacheMisses)
if hitRate < 0.8 {
    log.Warn("cache hit rate too low")
}
```

### Q20: How to handle high concurrency?
**A**: 

1. **Horizontal scaling**: Add more service instances
2. **Load balancing**: Distribute requests
3. **Caching**: Reduce database queries
4. **Async processing**: Queue time-consuming operations

```go
// Sync (slow)
result := heavyOperation()
return result

// Async (fast)
taskID := submitTask(heavyOperation)
return taskID  // Return immediately
```

## 🎯 Job Interview Questions

### Q21: How to answer "Why choose off-chain service?"
**A**: 

"On-chain and off-chain each have advantages. I chose off-chain service based on:

1. **Cost**: Storing 1GB video on-chain costs millions, off-chain costs dollars
2. **Performance**: On-chain transactions need 15 seconds confirmation, off-chain milliseconds
3. **Flexibility**: Off-chain logic can be updated anytime, on-chain contracts hard to modify
4. **User experience**: Users don't pay gas fees, lower barrier to entry

But I retained Web3 core advantages:
- Permission control decentralized (based on NFT)
- Users own data
- Can migrate to other services anytime

This is a hybrid architecture combining Web2 performance and Web3 decentralization."

### Q22: How to prove I really understand Web3?
**A**: 

Show depth of thinking:

1. **Comparative analysis**:
   - "Traditional login uses username/password, Web3 uses signature verification"
   - "Traditional permissions in database, Web3 permissions on blockchain"

2. **Trade-offs**:
   - "I chose off-chain storage because..."
   - "I use caching to balance performance and real-time"

3. **Practical experience**:
   - "I encountered RPC node failures, so implemented failover"
   - "I found on-chain queries slow, so added caching"

4. **Future vision**:
   - "Can integrate IPFS for complete decentralization"
   - "Can support more chains like Arbitrum, Optimism"

### Q23: What's the biggest challenge in your project?
**A**: 

Prepare 2-3 real challenges and solutions:

**Challenge 1: RPC node instability**
- Problem: Infura occasionally times out
- Solution: Implemented RPC node pool and failover
- Result: Availability improved from 95% to 99.9%

**Challenge 2: NFT verification performance**
- Problem: Each verification takes 500ms
- Solution: Implemented 5-minute caching
- Result: P95 latency reduced from 500ms to 10ms

**Challenge 3: Multi-chain support**
- Problem: EVM and Solana interfaces completely different
- Solution: Designed unified Provider interface
- Result: Adding new chain only requires implementing interface

## 💡 Final Advice

1. **Don't fear mistakes**: Testnet is for making mistakes
2. **Read more code**: Many excellent Web3 projects on GitHub
3. **Ask questions**: Community is friendly, no stupid questions
4. **Keep learning**: Web3 evolves fast, continuous learning needed

Remember: Every Web3 developer started as a beginner!
