# Web3 Integration Guide

## Overview

This guide explains how to use the Web3 integration features in StreamGate for wallet authentication, NFT verification, gas monitoring, and IPFS storage.

## Quick Start

### 1. Initialize Web3 Service

```go
import "github.com/yourusername/streamgate/pkg/service"

// Create Web3 service
web3Service, err := service.NewWeb3Service(cfg, logger)
if err != nil {
    log.Fatal("Failed to create Web3 service", err)
}
defer web3Service.Close()
```

### 2. Verify Wallet Signature

```go
// Verify a message signature
valid, err := web3Service.VerifySignature(ctx, 
    "0x742d35Cc6634C0532925a3b844Bc9e7595f42bE",
    "Sign this message to verify your wallet",
    "0x...",
)

if valid {
    // User is authenticated
}
```

### 3. Verify NFT Ownership

```go
// Verify NFT ownership
valid, err := web3Service.VerifyNFTOwnership(ctx,
    80001, // Polygon Mumbai
    "0x...", // NFT contract address
    "1", // Token ID
    "0x742d35Cc6634C0532925a3b844Bc9e7595f42bE", // Owner address
)

if valid {
    // User owns the NFT
}
```

### 4. Upload to IPFS

```go
// Upload file to IPFS
cid, err := web3Service.UploadToIPFS(ctx, "video.mp4", fileData)
if err != nil {
    log.Fatal("Failed to upload to IPFS", err)
}

// Access file via gateway
url := "https://ipfs.io/ipfs/" + cid
```

### 5. Get Gas Price

```go
// Get current gas price
gasPrice, err := web3Service.GetGasPrice(ctx, 80001)
if err != nil {
    log.Fatal("Failed to get gas price", err)
}

// gasPrice is in Wei
```

## API Endpoints

### Verify Signature

**Endpoint**: `POST /api/v1/web3/verify-signature`

**Request**:
```json
{
  "address": "0x742d35Cc6634C0532925a3b844Bc9e7595f42bE",
  "message": "Sign this message",
  "signature": "0x..."
}
```

**Response**:
```json
{
  "valid": true
}
```

### Verify NFT

**Endpoint**: `POST /api/v1/web3/verify-nft`

**Request**:
```json
{
  "chain_id": 80001,
  "contract_address": "0x...",
  "token_id": "1",
  "owner_address": "0x742d35Cc6634C0532925a3b844Bc9e7595f42bE"
}
```

**Response**:
```json
{
  "valid": true
}
```

### Get Gas Price

**Endpoint**: `GET /api/v1/web3/gas-price?chain_id=80001`

**Response**:
```json
{
  "chain_id": 80001,
  "gas_price": "30000000000",
  "gas_price_gwei": 30.0
}
```

### Get Supported Chains

**Endpoint**: `GET /api/v1/web3/supported-chains`

**Response**:
```json
{
  "chains": [
    {
      "id": 1,
      "name": "Ethereum",
      "rpc": "https://eth.llamarpc.com",
      "explorer": "https://etherscan.io",
      "currency": "ETH",
      "is_testnet": false
    },
    {
      "id": 80001,
      "name": "Polygon Mumbai",
      "rpc": "https://rpc-mumbai.maticvigil.com",
      "explorer": "https://mumbai.polygonscan.com",
      "currency": "MATIC",
      "is_testnet": true
    }
  ]
}
```

### Upload to IPFS

**Endpoint**: `POST /api/v1/web3/ipfs/upload`

**Request**:
```json
{
  "filename": "video.mp4",
  "data": "base64_encoded_data"
}
```

**Response**:
```json
{
  "cid": "QmXxxx...",
  "url": "https://ipfs.io/ipfs/QmXxxx..."
}
```

### Download from IPFS

**Endpoint**: `POST /api/v1/web3/ipfs/download`

**Request**:
```json
{
  "cid": "QmXxxx..."
}
```

**Response**:
```json
{
  "data": "base64_encoded_data"
}
```

## Supported Chains

### Mainnet Chains
- **Ethereum** (Chain ID: 1)
- **Polygon** (Chain ID: 137)
- **Binance Smart Chain** (Chain ID: 56)
- **Arbitrum One** (Chain ID: 42161)
- **Optimism** (Chain ID: 10)

### Testnet Chains
- **Ethereum Sepolia** (Chain ID: 11155111)
- **Polygon Mumbai** (Chain ID: 80001)
- **BSC Testnet** (Chain ID: 97)
- **Arbitrum Sepolia** (Chain ID: 421614)
- **Optimism Sepolia** (Chain ID: 11155420)

## Wallet Authentication Flow

### 1. Request Challenge

```bash
curl -X POST http://localhost:9090/api/v1/auth/challenge \
  -H "Content-Type: application/json" \
  -d '{"address": "0x742d35Cc6634C0532925a3b844Bc9e7595f42bE"}'
```

Response:
```json
{
  "challenge": "Sign this message to verify your wallet ownership...",
  "expires_at": 1234567890
}
```

### 2. Sign Challenge

User signs the challenge with their wallet (MetaMask, WalletConnect, etc.)

### 3. Verify Signature

```bash
curl -X POST http://localhost:9090/api/v1/web3/verify-signature \
  -H "Content-Type: application/json" \
  -d '{
    "address": "0x742d35Cc6634C0532925a3b844Bc9e7595f42bE",
    "message": "Sign this message...",
    "signature": "0x..."
  }'
```

Response:
```json
{
  "valid": true
}
```

### 4. Issue Token

If signature is valid, issue authentication token

## NFT-Gated Access

### 1. Check NFT Ownership

```go
// In auth middleware
valid, err := web3Service.VerifyNFTOwnership(ctx,
    chainID,
    contractAddress,
    tokenID,
    userAddress,
)

if !valid {
    http.Error(w, "NFT not owned", http.StatusForbidden)
    return
}
```

### 2. Grant Premium Access

```go
// Grant premium features to NFT holders
if valid {
    // Allow access to premium content
    // Update user permissions
    // Log access
}
```

## Gas Monitoring

### 1. Monitor Gas Prices

```go
// Get gas price levels
gasMonitor := web3Service.GetGasMonitor()
levels := gasMonitor.GetGasPriceLevels()

for _, level := range levels {
    fmt.Printf("%s: %f Gwei (%s)\n", 
        level.Level, 
        level.Gwei, 
        level.EstimatedTime)
}
```

### 2. Estimate Transaction Cost

```go
// Estimate cost for a transaction
gasMonitor := web3Service.GetGasMonitor()
gasAmount := uint64(21000) // Standard transfer

cost := gasMonitor.EstimateGasCost(gasAmount)
costInEther := gasMonitor.EstimateGasCostInEther(gasAmount)

fmt.Printf("Cost: %s Wei = %f ETH\n", cost.String(), costInEther)
```

### 3. Queue Transactions

```go
// Queue transaction when gas is high
queue := web3Service.GetTransactionQueue()

tx := &web3.QueuedTransaction{
    ID:       "tx-123",
    From:     "0x...",
    To:       "0x...",
    Value:    big.NewInt(1000000000000000000),
    GasLimit: 21000,
}

err := queue.Enqueue(tx)
if err != nil {
    log.Fatal("Failed to queue transaction", err)
}
```

## IPFS Integration

### 1. Upload File

```go
// Upload file to IPFS
data := []byte("file content")
cid, err := web3Service.UploadToIPFS(ctx, "filename.txt", data)
if err != nil {
    log.Fatal("Failed to upload", err)
}

// Store CID in database
```

### 2. Download File

```go
// Download file from IPFS
data, err := web3Service.DownloadFromIPFS(ctx, cid)
if err != nil {
    log.Fatal("Failed to download", err)
}

// Use file data
```

### 3. Hybrid Storage

```go
// Use hybrid storage (local + IPFS)
// Files < 100MB stored locally
// Files >= 100MB stored on IPFS

if fileSize > 100*1024*1024 {
    // Upload to IPFS
    cid, err := web3Service.UploadToIPFS(ctx, filename, data)
    // Store CID
} else {
    // Store locally
    // Store path
}
```

## Event Indexing

### 1. Create Event Indexer

```go
import "github.com/yourusername/streamgate/pkg/web3"

// Create event indexer
indexer, err := web3.NewEventIndexer(
    client,
    "0x...", // Contract address
    "0x...", // Event signature
    logger,
)

// Start indexing
err = indexer.Start(ctx)
```

### 2. Listen for Events

```go
// Create event listener
listener := web3.NewEventListener(indexer, logger)

// Register handler
listener.On("ContentRegistered", func(ctx context.Context, event *web3.IndexedEvent) error {
    // Handle event
    fmt.Printf("Content registered: %s\n", event.ID)
    return nil
})

// Process all events
listener.ProcessAllEvents(ctx)
```

### 3. Query Events

```go
// Get all events
events := indexer.GetEvents()

// Get events by type
contentEvents := indexer.GetEventsByType("ContentRegistered")

// Get events by block range
rangeEvents := indexer.GetEventsByBlockRange(1000000, 1000100)

// Get event count
count := indexer.GetEventCount()
```

## Smart Contract Interaction

### 1. Deploy Contract

```go
import "github.com/yourusername/streamgate/pkg/web3"

// Create deployer
deployer := web3.NewSmartContractDeployer(logger)

// Deploy contract
config := &web3.DeploymentConfig{
    ChainID:    80001,
    RPC:        "https://rpc-mumbai.maticvigil.com",
    PrivateKey: "0x...",
    GasPrice:   "30000000000",
    GasLimit:   3000000,
}

result, err := deployer.DeployContentRegistry(config)
if err != nil {
    log.Fatal("Failed to deploy", err)
}

fmt.Printf("Contract deployed at: %s\n", result.ContractAddress)
```

### 2. Register Contract

```go
// Create registry
registry := web3.NewSmartContractRegistry(logger)

// Register contract
info := &web3.SmartContractInfo{
    Name:       "ContentRegistry",
    Address:    "0x...",
    ChainID:    80001,
    ABI:        web3.ContentRegistryABI,
    DeployedAt: time.Now().Unix(),
    Verified:   false,
}

registry.RegisterContract(info)

// Get contract
contract := registry.GetContract("ContentRegistry")
```

### 3. Call Contract

```go
// Create contract interactor
interactor := web3.NewContractInteractor(client, logger)

// Call contract function
result, err := interactor.CallContractFunction(
    ctx,
    "0x...", // Contract address
    web3.ContentRegistryABI,
    "verifyContent",
    contentHash,
)
```

## Best Practices

### Security
1. **Validate Signatures**: Always verify signatures before granting access
2. **Use Testnet First**: Test on testnet before mainnet deployment
3. **Secure Keys**: Never expose private keys in code
4. **Rate Limiting**: Implement rate limiting on Web3 endpoints
5. **Error Handling**: Handle all error cases gracefully

### Performance
1. **Cache Results**: Cache signature verification results
2. **Batch Queries**: Batch multiple queries when possible
3. **Connection Pooling**: Reuse RPC connections
4. **Event Indexing**: Index events asynchronously
5. **IPFS Caching**: Cache IPFS downloads locally

### Reliability
1. **Multiple RPC Providers**: Use multiple RPC providers for failover
2. **Retry Logic**: Implement exponential backoff for retries
3. **Health Checks**: Monitor RPC provider health
4. **Event Confirmation**: Wait for block confirmations
5. **Error Recovery**: Implement graceful error recovery

## Troubleshooting

### Signature Verification Fails
- Check address format (should be 0x...)
- Verify message matches exactly
- Check signature format (should be 0x...)
- Ensure signature is from correct wallet

### NFT Verification Fails
- Verify contract address is correct
- Check token ID exists
- Verify owner address is correct
- Check chain ID is correct

### IPFS Upload Fails
- Verify IPFS node is running
- Check file size limits
- Verify network connectivity
- Check IPFS storage space

### Gas Price Query Fails
- Verify RPC provider is accessible
- Check chain ID is supported
- Verify network connectivity
- Check RPC rate limits

## Examples

### Complete Authentication Flow

```go
// 1. Generate challenge
challenge := web3Service.GetSignatureVerifier().GenerateChallenge(userAddress)

// 2. User signs challenge (client-side)
// signature = await wallet.signMessage(challenge.Message)

// 3. Verify signature
valid, err := web3Service.VerifySignature(ctx, userAddress, challenge.Message, signature)
if !valid {
    return errors.New("invalid signature")
}

// 4. Issue token
token := generateAuthToken(userAddress)
return token
```

### NFT-Gated Content

```go
// Check NFT ownership
valid, err := web3Service.VerifyNFTOwnership(ctx, chainID, contractAddress, tokenID, userAddress)
if !valid {
    return errors.New("user does not own NFT")
}

// Grant access to premium content
content := getPremiumContent(contentID)
return content
```

### Hybrid Storage Upload

```go
// Upload file with hybrid storage
if len(data) > 100*1024*1024 {
    // Large file - upload to IPFS
    cid, err := web3Service.UploadToIPFS(ctx, filename, data)
    storeInDatabase(contentID, "ipfs", cid)
} else {
    // Small file - store locally
    path := storeLocally(filename, data)
    storeInDatabase(contentID, "local", path)
}
```

## References

- [Ethereum Documentation](https://ethereum.org/en/developers/)
- [Polygon Documentation](https://docs.polygon.technology/)
- [IPFS Documentation](https://docs.ipfs.tech/)
- [go-ethereum](https://geth.ethereum.org/docs/rpc/server)
- [Web3.js](https://web3js.readthedocs.io/)

---

For more information, see:
- `docs/project-planning/implementation/CODE_IMPLEMENTATION_PHASE5.md`
- `PHASE5_COMPLETE.md`
- `pkg/web3/` - Web3 modules
- `pkg/service/web3.go` - Web3 service
