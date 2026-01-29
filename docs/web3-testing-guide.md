# Web3 Integration Testing Guide

## 🎯 Testing Strategy

### Test Pyramid

```
        /\
       /  \  E2E Tests (5%)
      /────\  
     /      \ Integration Tests (25%)
    /────────\
   /          \ Unit Tests (70%)
  /────────────\
```

## 🧪 Unit Tests (Mock On-Chain Calls)

### 1. Mock RPC Client

```go
// internal/blockchain/mock_client.go
package blockchain

import (
    "context"
    "math/big"
    "github.com/ethereum/go-ethereum/common"
    "github.com/stretchr/testify/mock"
)

type MockEthClient struct {
    mock.Mock
}

func (m *MockEthClient) BlockNumber(ctx context.Context) (uint64, error) {
    args := m.Called(ctx)
    return args.Get(0).(uint64), args.Error(1)
}

func (m *MockEthClient) BalanceAt(ctx context.Context, account common.Address, blockNumber *big.Int) (*big.Int, error) {
    args := m.Called(ctx, account, blockNumber)
    if args.Get(0) == nil {
        return nil, args.Error(1)
    }
    return args.Get(0).(*big.Int), args.Error(1)
}

// Test cases
func TestEVMProvider_GetBalance(t *testing.T) {
    tests := []struct {
        name          string
        owner         string
        contract      string
        mockBalance   *big.Int
        mockError     error
        expectedBalance *big.Int
        expectedError bool
    }{
        {
            name:          "user has NFT",
            owner:         "0x1234567890123456789012345678901234567890",
            contract:      "0xabcdefabcdefabcdefabcdefabcdefabcdefabcd",
            mockBalance:   big.NewInt(1),
            mockError:     nil,
            expectedBalance: big.NewInt(1),
            expectedError: false,
        },
        {
            name:          "user has no NFT",
            owner:         "0x1234567890123456789012345678901234567890",
            contract:      "0xabcdefabcdefabcdefabcdefabcdefabcdefabcd",
            mockBalance:   big.NewInt(0),
            mockError:     nil,
            expectedBalance: big.NewInt(0),
            expectedError: false,
        },
        {
            name:          "RPC error",
            owner:         "0x1234567890123456789012345678901234567890",
            contract:      "0xabcdefabcdefabcdefabcdefabcdefabcdefabcd",
            mockBalance:   nil,
            mockError:     errors.New("connection refused"),
            expectedBalance: nil,
            expectedError: true,
        },
    }

    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            // Create mock client
            mockClient := new(MockEthClient)
            mockClient.On("BalanceAt", mock.Anything, mock.Anything, mock.Anything).
                Return(tt.mockBalance, tt.mockError)

            // Create provider
            provider := &EVMProvider{
                client: mockClient,
            }

            // Execute test
            balance, err := provider.GetBalance(context.Background(), tt.owner, tt.contract)

            // Verify results
            if tt.expectedError {
                assert.Error(t, err)
            } else {
                assert.NoError(t, err)
                assert.Equal(t, tt.expectedBalance, balance)
            }

            mockClient.AssertExpectations(t)
        })
    }
}
```

### 2. Mock Signature Verification

```go
func TestSignatureVerifier_VerifySignature(t *testing.T) {
    // Generate test key pair
    privateKey, _ := crypto.GenerateKey()
    address := crypto.PubkeyToAddress(privateKey.PublicKey)

    // Create test message
    nonce := "test-nonce-123"
    timestamp := time.Now().Unix()
    message := fmt.Sprintf("Sign this to login:\nNonce: %s\nTimestamp: %d", nonce, timestamp)

    // Sign
    signature, _ := signMessage(message, privateKey)

    // Mock Redis
    mockRedis := new(MockRedisClient)
    mockRedis.On("Exists", mock.Anything, "nonce:"+nonce).Return(int64(0), nil)
    mockRedis.On("Set", mock.Anything, "nonce:"+nonce, "1", mock.Anything).Return(nil)

    // Create verifier
    verifier := &SignatureVerifier{
        redis: mockRedis,
    }

    // Test verification
    err := verifier.VerifySignature(VerifyRequest{
        Address:   address.Hex(),
        Signature: signature,
        Nonce:     nonce,
        Timestamp: timestamp,
        ChainType: "evm",
    })

    assert.NoError(t, err)
    mockRedis.AssertExpectations(t)
}
```

## 🔗 Integration Tests (Real Testnet)

### 1. Testnet Configuration

```go
// test/integration/config.go
package integration

import (
    "os"
)

type TestConfig struct {
    // Ethereum Sepolia
    EthereumRPC     string
    EthereumChainID int64
    TestNFTContract string
    TestWallet      string

    // Solana Devnet
    SolanaRPC       string
    SolanaWallet    string
    SolanaTestMint  string
}

func LoadTestConfig() *TestConfig {
    return &TestConfig{
        EthereumRPC:     getEnv("TEST_ETH_RPC", "https://sepolia.infura.io/v3/YOUR_KEY"),
        EthereumChainID: 11155111,
        TestNFTContract: getEnv("TEST_NFT_CONTRACT", "0x..."),
        TestWallet:      getEnv("TEST_WALLET", "0x..."),

        SolanaRPC:      getEnv("TEST_SOLANA_RPC", "https://api.devnet.solana.com"),
        SolanaWallet:   getEnv("TEST_SOLANA_WALLET", "..."),
        SolanaTestMint: getEnv("TEST_SOLANA_MINT", "..."),
    }
}

func getEnv(key, defaultValue string) string {
    if value := os.Getenv(key); value != "" {
        return value
    }
    return defaultValue
}
```

### 2. Ethereum Integration Tests

```go
// test/integration/ethereum_test.go
package integration

import (
    "context"
    "testing"
    "time"
    "github.com/stretchr/testify/assert"
    "github.com/stretchr/testify/require"
)

func TestEthereumNFTVerification(t *testing.T) {
    if testing.Short() {
        t.Skip("skipping integration test")
    }

    config := LoadTestConfig()

    // Create provider
    provider, err := NewEVMProvider(ChainEthereum, Config{
        RPC:     config.EthereumRPC,
        ChainID: config.EthereumChainID,
    })
    require.NoError(t, err)

    t.Run("check NFT balance", func(t *testing.T) {
        ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
        defer cancel()

        balance, err := provider.GetBalance(ctx, config.TestWallet, config.TestNFTContract)
        require.NoError(t, err)

        // Verify balance (assuming test wallet has at least 1 NFT)
        assert.True(t, balance.Cmp(big.NewInt(0)) > 0, "test wallet should have at least 1 NFT")

        t.Logf("NFT balance: %s", balance.String())
    })

    t.Run("check non-existent wallet", func(t *testing.T) {
        ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
        defer cancel()

        // Random address (does not hold NFT)
        randomWallet := "0x0000000000000000000000000000000000000001"

        balance, err := provider.GetBalance(ctx, randomWallet, config.TestNFTContract)
        require.NoError(t, err)

        // Should return 0
        assert.Equal(t, big.NewInt(0), balance)
    })

    t.Run("check invalid contract", func(t *testing.T) {
        ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
        defer cancel()

        // Invalid contract address
        invalidContract := "0x0000000000000000000000000000000000000000"

        _, err := provider.GetBalance(ctx, config.TestWallet, invalidContract)
        assert.Error(t, err)
    })
}

func TestEthereumRPCFailover(t *testing.T) {
    if testing.Short() {
        t.Skip("skipping integration test")
    }

    // Configure multiple RPC nodes (including one intentionally wrong)
    nodes := []string{
        "https://invalid-rpc.example.com",  // Will fail
        "https://sepolia.infura.io/v3/YOUR_KEY",  // Will succeed
    }

    pool := NewRPCPool(nodes)

    t.Run("failover to healthy node", func(t *testing.T) {
        ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
        defer cancel()

        // Should automatically switch to healthy node
        client, err := pool.GetClient(ctx)
        require.NoError(t, err)

        // Verify normal calls work
        blockNumber, err := client.BlockNumber(ctx)
        require.NoError(t, err)
        assert.Greater(t, blockNumber, uint64(0))

        t.Logf("Current block number: %d", blockNumber)
    })
}
```

### 3. Solana Integration Tests

```go
// test/integration/solana_test.go
package integration

func TestSolanaNFTVerification(t *testing.T) {
    if testing.Short() {
        t.Skip("skipping integration test")
    }

    config := LoadTestConfig()

    provider, err := NewSolanaProvider(Config{
        RPC: config.SolanaRPC,
    })
    require.NoError(t, err)

    t.Run("check NFT balance", func(t *testing.T) {
        ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
        defer cancel()

        balance, err := provider.GetBalance(ctx, config.SolanaWallet, config.SolanaTestMint)
        require.NoError(t, err)

        assert.True(t, balance.Cmp(big.NewInt(0)) > 0)
        t.Logf("Solana NFT balance: %s", balance.String())
    })
}
```

## 🚀 End-to-End Tests

### 1. Complete Flow Tests

```go
// test/e2e/content_access_test.go
package e2e

func TestContentAccessWithNFT(t *testing.T) {
    if testing.Short() {
        t.Skip("skipping e2e test")
    }

    // 1. Start test server
    server := startTestServer(t)
    defer server.Close()

    config := LoadTestConfig()

    // 2. Get nonce
    nonceResp := getNonce(t, server.URL, config.TestWallet)
    require.NotEmpty(t, nonceResp.Message)

    // 3. Sign (simulate frontend)
    signature := signMessage(t, nonceResp.Message, config.TestPrivateKey)

    // 4. Verify signature and login
    loginResp := verifySignature(t, server.URL, VerifyRequest{
        Address:   config.TestWallet,
        Signature: signature,
        ChainType: "evm",
    })
    require.NotEmpty(t, loginResp.Token)

    // 5. Access protected content
    content := getProtectedContent(t, server.URL, loginResp.Token, "test-content-id")
    require.NotNil(t, content)
    assert.Equal(t, "test-content-id", content.ID)

    // 6. Verify playback is possible
    streamURL := getStreamURL(t, server.URL, loginResp.Token, "test-content-id")
    require.NotEmpty(t, streamURL)

    // 7. Verify HLS playlist is accessible
    playlist := fetchPlaylist(t, streamURL)
    require.Contains(t, playlist, "#EXTM3U")
}

func TestContentAccessWithoutNFT(t *testing.T) {
    if testing.Short() {
        t.Skip("skipping e2e test")
    }

    server := startTestServer(t)
    defer server.Close()

    // Use wallet without NFT
    walletWithoutNFT := "0x0000000000000000000000000000000000000001"

    // 1. Get nonce
    nonceResp := getNonce(t, server.URL, walletWithoutNFT)

    // 2. Sign
    signature := signMessage(t, nonceResp.Message, testPrivateKey)

    // 3. Login (should succeed)
    loginResp := verifySignature(t, server.URL, VerifyRequest{
        Address:   walletWithoutNFT,
        Signature: signature,
        ChainType: "evm",
    })
    require.NotEmpty(t, loginResp.Token)

    // 4. Try to access protected content (should fail)
    resp, err := http.Get(fmt.Sprintf("%s/api/v1/content/test-content-id", server.URL))
    require.NoError(t, err)
    defer resp.Body.Close()

    // Should return 403 Forbidden
    assert.Equal(t, http.StatusForbidden, resp.StatusCode)
}
```

### 2. Performance Tests

```go
// test/performance/nft_verification_test.go
package performance

func BenchmarkNFTVerification(b *testing.B) {
    config := LoadTestConfig()
    provider, _ := NewEVMProvider(ChainEthereum, Config{
        RPC: config.EthereumRPC,
    })

    b.Run("without cache", func(b *testing.B) {
        b.ResetTimer()
        for i := 0; i < b.N; i++ {
            provider.GetBalance(context.Background(), config.TestWallet, config.TestNFTContract)
        }
    })

    b.Run("with cache", func(b *testing.B) {
        cache := NewNFTCache()
        cachedProvider := &CachedProvider{
            provider: provider,
            cache:    cache,
        }

        b.ResetTimer()
        for i := 0; i < b.N; i++ {
            cachedProvider.GetBalance(context.Background(), config.TestWallet, config.TestNFTContract)
        }
    })
}

// Example run results:
// BenchmarkNFTVerification/without_cache-8    2    500000000 ns/op
// BenchmarkNFTVerification/with_cache-8    10000    100000 ns/op
// Cache improvement: 5000x!
```

## 📊 Test Coverage

### 1. Generate Coverage Report

```bash
# Run all tests and generate coverage
go test ./... -coverprofile=coverage.out -covermode=atomic

# View coverage
go tool cover -func=coverage.out

# Generate HTML report
go tool cover -html=coverage.out -o coverage.html
```

### 2. Coverage Goals

```
Overall coverage: > 70%
├─ Core business logic: > 90%
│  ├─ NFT verification: 95%
│  ├─ Signature verification: 95%
│  └─ Permission control: 90%
├─ Plugin system: > 80%
└─ Utility functions: > 60%
```

## 🔄 CI/CD Integration

### 1. GitHub Actions Configuration

```yaml
# .github/workflows/test.yml
name: Tests

on: [push, pull_request]

jobs:
  unit-tests:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v3
      
      - name: Set up Go
        uses: actions/setup-go@v4
        with:
          go-version: '1.21'
      
      - name: Run unit tests
        run: go test -short -v ./...
      
      - name: Upload coverage
        uses: codecov/codecov-action@v3

  integration-tests:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v3
      
      - name: Set up Go
        uses: actions/setup-go@v4
        with:
          go-version: '1.21'
      
      - name: Run integration tests
        env:
          TEST_ETH_RPC: ${{ secrets.TEST_ETH_RPC }}
          TEST_NFT_CONTRACT: ${{ secrets.TEST_NFT_CONTRACT }}
          TEST_WALLET: ${{ secrets.TEST_WALLET }}
        run: go test -v ./test/integration/...
```

### 2. Test Environment Variables

```bash
# .env.test
TEST_ETH_RPC=https://sepolia.infura.io/v3/YOUR_KEY
TEST_ETH_CHAIN_ID=11155111
TEST_NFT_CONTRACT=0x...
TEST_WALLET=0x...

TEST_SOLANA_RPC=https://api.devnet.solana.com
TEST_SOLANA_WALLET=...
TEST_SOLANA_MINT=...
```

## 🎯 Testing Best Practices

### 1. Test Naming

```go
// ✅ Good naming
func TestEVMProvider_GetBalance_UserHasNFT(t *testing.T) {}
func TestEVMProvider_GetBalance_UserHasNoNFT(t *testing.T) {}
func TestEVMProvider_GetBalance_RPCError(t *testing.T) {}

// ❌ Bad naming
func TestGetBalance1(t *testing.T) {}
func TestGetBalance2(t *testing.T) {}
```

### 2. Test Isolation

```go
// Each test is independent
func TestNFTVerification(t *testing.T) {
    // Create independent provider
    provider := NewEVMProvider(...)
    
    // Use independent cache
    cache := NewCache()
    
    // Test...
}
```

### 3. Test Data

```go
// Use constants
const (
    TestWallet1 = "0x1234567890123456789012345678901234567890"
    TestWallet2 = "0xabcdefabcdefabcdefabcdefabcdefabcdefabcd"
    TestNFTContract = "0x..."
)

// Use factory functions
func createTestProvider(t *testing.T) *EVMProvider {
    provider, err := NewEVMProvider(...)
    require.NoError(t, err)
    return provider
}
```

### 4. Clean Up Resources

```go
func TestWithCleanup(t *testing.T) {
    // Create resources
    client, err := ethclient.Dial(rpcURL)
    require.NoError(t, err)
    
    // Register cleanup function
    t.Cleanup(func() {
        client.Close()
    })
    
    // Test...
}
```

## 📝 Testing Checklist

When developing new features, ensure:

- [ ] Write unit tests (Mock)
- [ ] Write integration tests (Testnet)
- [ ] Test normal cases
- [ ] Test edge cases
- [ ] Test error cases
- [ ] Test concurrent cases
- [ ] Check test coverage
- [ ] Update documentation

## 🚨 Common Testing Issues

### Q: Testnet transactions are too slow?
A: Use Mock for unit tests, only use real testnet for integration tests.

### Q: How to test RPC failover?
A: Configure an invalid RPC node and verify automatic switching to healthy node.

### Q: How to test caching?
A: First call should query on-chain, second call should hit cache.

### Q: Test coverage is insufficient?
A: Focus on improving coverage for core business logic, utility functions can have lower requirements.

Remember: Good tests are the guarantee of project quality!
