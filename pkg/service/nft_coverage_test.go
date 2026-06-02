package service

import (
	"context"
	"errors"
	"math/big"
	"sync"
	"testing"
	"time"

	"github.com/rtcdance/streamgate/pkg/cachetypes"
	"github.com/rtcdance/streamgate/pkg/web3"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

type nftCovEthCaller struct {
	mu             sync.RWMutex
	callContractFn func(ctx context.Context, call ethereum.CallMsg, blockNumber *big.Int) ([]byte, error)
	codeAtFn       func(ctx context.Context, contract common.Address, blockNumber *big.Int) ([]byte, error)
}

func (m *nftCovEthCaller) CallContract(ctx context.Context, call ethereum.CallMsg, blockNumber *big.Int) ([]byte, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if m.callContractFn != nil {
		return m.callContractFn(ctx, call, blockNumber)
	}
	return nil, errors.New("CallContract not configured")
}

func (m *nftCovEthCaller) CodeAt(ctx context.Context, contract common.Address, blockNumber *big.Int) ([]byte, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if m.codeAtFn != nil {
		return m.codeAtFn(ctx, contract, blockNumber)
	}
	return nil, errors.New("CodeAt not configured")
}

type nftCovCache struct {
	mu    sync.RWMutex
	store map[string]interface{}
}

func newNftCovCache() *nftCovCache {
	return &nftCovCache{store: make(map[string]interface{})}
}

func (c *nftCovCache) Get(key string) (interface{}, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	v, ok := c.store[key]
	if !ok {
		return nil, errors.New("not found")
	}
	return v, nil
}

func (c *nftCovCache) Set(key string, value interface{}) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.store[key] = value
	return nil
}

func (c *nftCovCache) SetWithExpiration(key string, value interface{}, _ time.Duration) error {
	return c.Set(key, value)
}

func (c *nftCovCache) Delete(key string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.store, key)
	return nil
}

func TestNFTCov_NewNFTServiceWithCaller_NilCaller(t *testing.T) {
	svc, err := NewNFTServiceWithCaller(nil, "http://rpc", nil)
	require.NoError(t, err)
	assert.NotNil(t, svc)
	assert.False(t, svc.cacheEnabled)
}

func TestNFTCov_NewNFTServiceWithCaller_WithCache(t *testing.T) {
	caller := &nftCovEthCaller{}
	cache := newNftCovCache()
	svc, err := NewNFTServiceWithCaller(caller, "http://rpc", cache)
	require.NoError(t, err)
	assert.True(t, svc.cacheEnabled)
}

func TestNFTCov_NewNFTServiceWithCaller_Closer(t *testing.T) {
	caller := &nftCovEthCaller{}
	svc, err := NewNFTServiceWithCaller(caller, "http://rpc", nil)
	require.NoError(t, err)
	assert.Nil(t, svc.closer)
	svc.Close()
}

func TestNFTCov_VerifyNFT_InvalidAddress(t *testing.T) {
	caller := &nftCovEthCaller{}
	svc, err := NewNFTServiceWithCaller(caller, "http://rpc", nil)
	require.NoError(t, err)
	_, err = svc.VerifyNFT(context.Background(), "not-an-address", "0x742d35Cc6634C0532925a3b844Bc9e7595f2bD18", "1")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "invalid address")
}

func TestNFTCov_VerifyNFT_InvalidContractAddress(t *testing.T) {
	caller := &nftCovEthCaller{}
	svc, err := NewNFTServiceWithCaller(caller, "http://rpc", nil)
	require.NoError(t, err)
	_, err = svc.VerifyNFT(context.Background(), "0x742d35Cc6634C0532925a3b844Bc9e7595f2bD18", "not-a-contract", "1")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "invalid contract address")
}

func TestNFTCov_VerifyNFT_InvalidTokenID(t *testing.T) {
	caller := &nftCovEthCaller{}
	cache := newNftCovCache()
	svc, err := NewNFTServiceWithCaller(caller, "http://rpc", cache)
	require.NoError(t, err)
	_, err = svc.VerifyNFT(context.Background(), "0x742d35Cc6634C0532925a3b844Bc9e7595f2bD18", "0x742d35Cc6634C0532925a3b844Bc9e7595f2bD18", "not-a-number")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "invalid token ID")
}

func TestNFTCov_VerifyNFT_CacheHit(t *testing.T) {
	cache := newNftCovCache()
	_ = cache.Set("nft:owner:0x742d35Cc6634C0532925a3b844Bc9e7595f2bD18:1", "0x742d35Cc6634C0532925a3b844Bc9e7595f2bD18")
	caller := &nftCovEthCaller{}
	svc, err := NewNFTServiceWithCaller(caller, "http://rpc", cache)
	require.NoError(t, err)
	result, err := svc.VerifyNFT(context.Background(), "0x742d35Cc6634C0532925a3b844Bc9e7595f2bD18", "0x742d35Cc6634C0532925a3b844Bc9e7595f2bD18", "1")
	require.NoError(t, err)
	assert.True(t, result)
}

func TestNFTCov_VerifyNFT_CacheHitDifferentOwner(t *testing.T) {
	cache := newNftCovCache()
	_ = cache.Set("nft:owner:0x742d35Cc6634C0532925a3b844Bc9e7595f2bD18:1", "0x0000000000000000000000000000000000000001")
	caller := &nftCovEthCaller{}
	svc, err := NewNFTServiceWithCaller(caller, "http://rpc", cache)
	require.NoError(t, err)
	result, err := svc.VerifyNFT(context.Background(), "0x742d35Cc6634C0532925a3b844Bc9e7595f2bD18", "0x742d35Cc6634C0532925a3b844Bc9e7595f2bD18", "1")
	require.NoError(t, err)
	assert.False(t, result)
}

func TestNFTCov_VerifyNFT_ContractCallFailed(t *testing.T) {
	caller := &nftCovEthCaller{
		callContractFn: func(_ context.Context, _ ethereum.CallMsg, _ *big.Int) ([]byte, error) {
			return nil, errors.New("RPC error")
		},
	}
	svc, err := NewNFTServiceWithCaller(caller, "http://rpc", nil)
	require.NoError(t, err)
	_, err = svc.VerifyNFT(context.Background(), "0x742d35Cc6634C0532925a3b844Bc9e7595f2bD18", "0x742d35Cc6634C0532925a3b844Bc9e7595f2bD18", "1")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to get NFT owner")
}

func TestNFTCov_VerifyNFT_InsufficientData(t *testing.T) {
	caller := &nftCovEthCaller{
		callContractFn: func(_ context.Context, _ ethereum.CallMsg, _ *big.Int) ([]byte, error) {
			return []byte{0x01, 0x02}, nil
		},
	}
	svc, err := NewNFTServiceWithCaller(caller, "http://rpc", nil)
	require.NoError(t, err)
	_, err = svc.VerifyNFT(context.Background(), "0x742d35Cc6634C0532925a3b844Bc9e7595f2bD18", "0x742d35Cc6634C0532925a3b844Bc9e7595f2bD18", "1")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "insufficient data")
}

func TestNFTCov_VerifyNFT_Success(t *testing.T) {
	ownerAddr := common.HexToAddress("0x742d35Cc6634C0532925a3b844Bc9e7595f2bD18")
	padded := common.LeftPadBytes(ownerAddr.Bytes(), 32)
	caller := &nftCovEthCaller{
		callContractFn: func(_ context.Context, _ ethereum.CallMsg, _ *big.Int) ([]byte, error) {
			return padded, nil
		},
	}
	svc, err := NewNFTServiceWithCaller(caller, "http://rpc", nil)
	require.NoError(t, err)
	result, err := svc.VerifyNFT(context.Background(), "0x742d35Cc6634C0532925a3b844Bc9e7595f2bD18", "0x742d35Cc6634C0532925a3b844Bc9e7595f2bD18", "1")
	require.NoError(t, err)
	assert.True(t, result)
}

func TestNFTCov_VerifyNFT_DifferentOwner(t *testing.T) {
	ownerAddr := common.HexToAddress("0x0000000000000000000000000000000000000001")
	padded := common.LeftPadBytes(ownerAddr.Bytes(), 32)
	caller := &nftCovEthCaller{
		callContractFn: func(_ context.Context, _ ethereum.CallMsg, _ *big.Int) ([]byte, error) {
			return padded, nil
		},
	}
	svc, err := NewNFTServiceWithCaller(caller, "http://rpc", nil)
	require.NoError(t, err)
	result, err := svc.VerifyNFT(context.Background(), "0x742d35Cc6634C0532925a3b844Bc9e7595f2bD18", "0x742d35Cc6634C0532925a3b844Bc9e7595f2bD18", "1")
	require.NoError(t, err)
	assert.False(t, result)
}

func TestNFTCov_VerifyNFT_CachesResult(t *testing.T) {
	ownerAddr := common.HexToAddress("0x742d35Cc6634C0532925a3b844Bc9e7595f2bD18")
	padded := common.LeftPadBytes(ownerAddr.Bytes(), 32)
	callCount := 0
	caller := &nftCovEthCaller{
		callContractFn: func(_ context.Context, _ ethereum.CallMsg, _ *big.Int) ([]byte, error) {
			callCount++
			return padded, nil
		},
	}
	cache := newNftCovCache()
	svc, err := NewNFTServiceWithCaller(caller, "http://rpc", cache)
	require.NoError(t, err)
	_, err = svc.VerifyNFT(context.Background(), "0x742d35Cc6634C0532925a3b844Bc9e7595f2bD18", "0x742d35Cc6634C0532925a3b844Bc9e7595f2bD18", "1")
	require.NoError(t, err)
	assert.Equal(t, 1, callCount)
	_, err = svc.VerifyNFT(context.Background(), "0x742d35Cc6634C0532925a3b844Bc9e7595f2bD18", "0x742d35Cc6634C0532925a3b844Bc9e7595f2bD18", "1")
	require.NoError(t, err)
	assert.Equal(t, 1, callCount)
}

func TestNFTCov_GetNFTMetadata_InvalidContract(t *testing.T) {
	caller := &nftCovEthCaller{}
	svc, err := NewNFTServiceWithCaller(caller, "http://rpc", nil)
	require.NoError(t, err)
	_, err = svc.GetNFTMetadata(context.Background(), "not-a-contract", "1")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "invalid contract address")
}

func TestNFTCov_GetNFTMetadata_InvalidTokenID(t *testing.T) {
	caller := &nftCovEthCaller{}
	cache := newNftCovCache()
	svc, err := NewNFTServiceWithCaller(caller, "http://rpc", cache)
	require.NoError(t, err)
	_, err = svc.GetNFTMetadata(context.Background(), "0x742d35Cc6634C0532925a3b844Bc9e7595f2bD18", "not-a-number")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "invalid token ID")
}

func TestNFTCov_GetNFTMetadata_CacheHit(t *testing.T) {
	cache := newNftCovCache()
	meta := &NFTMetadata{Name: "Cached NFT"}
	_ = cache.Set("nft:metadata:0x742d35Cc6634C0532925a3b844Bc9e7595f2bD18:1", meta)
	caller := &nftCovEthCaller{}
	svc, err := NewNFTServiceWithCaller(caller, "http://rpc", cache)
	require.NoError(t, err)
	result, err := svc.GetNFTMetadata(context.Background(), "0x742d35Cc6634C0532925a3b844Bc9e7595f2bD18", "1")
	require.NoError(t, err)
	assert.Equal(t, "Cached NFT", result.Name)
}

func TestNFTCov_GetNFTMetadata_ContractCallFailed(t *testing.T) {
	caller := &nftCovEthCaller{
		callContractFn: func(_ context.Context, _ ethereum.CallMsg, _ *big.Int) ([]byte, error) {
			return nil, errors.New("RPC error")
		},
	}
	svc, err := NewNFTServiceWithCaller(caller, "http://rpc", nil)
	require.NoError(t, err)
	_, err = svc.GetNFTMetadata(context.Background(), "0x742d35Cc6634C0532925a3b844Bc9e7595f2bD18", "1")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to get token URI")
}

func TestNFTCov_GetNFTMetadata_InsufficientData(t *testing.T) {
	caller := &nftCovEthCaller{
		callContractFn: func(_ context.Context, _ ethereum.CallMsg, _ *big.Int) ([]byte, error) {
			return []byte{0x01}, nil
		},
	}
	svc, err := NewNFTServiceWithCaller(caller, "http://rpc", nil)
	require.NoError(t, err)
	_, err = svc.GetNFTMetadata(context.Background(), "0x742d35Cc6634C0532925a3b844Bc9e7595f2bD18", "1")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "insufficient data")
}

func TestNFTCov_VerifyNFTBatch(t *testing.T) {
	ownerAddr := common.HexToAddress("0x742d35Cc6634C0532925a3b844Bc9e7595f2bD18")
	padded := common.LeftPadBytes(ownerAddr.Bytes(), 32)
	caller := &nftCovEthCaller{
		callContractFn: func(_ context.Context, _ ethereum.CallMsg, _ *big.Int) ([]byte, error) {
			return padded, nil
		},
	}
	svc, err := NewNFTServiceWithCaller(caller, "http://rpc", nil)
	require.NoError(t, err)
	nfts := []struct {
		ContractAddress string
		TokenID         string
	}{
		{"0x742d35Cc6634C0532925a3b844Bc9e7595f2bD18", "1"},
		{"0x742d35Cc6634C0532925a3b844Bc9e7595f2bD18", "2"},
	}
	results, err := svc.VerifyNFTBatch(context.Background(), "0x742d35Cc6634C0532925a3b844Bc9e7595f2bD18", nfts)
	require.NoError(t, err)
	assert.Len(t, results, 2)
}

func TestNFTCov_VerifyNFTBatch_Empty(t *testing.T) {
	caller := &nftCovEthCaller{}
	svc, err := NewNFTServiceWithCaller(caller, "http://rpc", nil)
	require.NoError(t, err)
	results, err := svc.VerifyNFTBatch(context.Background(), "0x742d35Cc6634C0532925a3b844Bc9e7595f2bD18", nil)
	require.NoError(t, err)
	assert.Empty(t, results)
}

func TestNFTCov_InvalidateOwnershipCache_NoCache(t *testing.T) {
	caller := &nftCovEthCaller{}
	svc, err := NewNFTServiceWithCaller(caller, "http://rpc", nil)
	require.NoError(t, err)
	svc.InvalidateOwnershipCache(context.Background(), "0xContract", "1")
}

func TestNFTCov_InvalidateOwnershipCache_WithCache(t *testing.T) {
	cache := newNftCovCache()
	_ = cache.Set("nft:owner:0xcontract:1", "0xowner")
	caller := &nftCovEthCaller{}
	svc, err := NewNFTServiceWithCaller(caller, "http://rpc", cache)
	require.NoError(t, err)
	svc.InvalidateOwnershipCache(context.Background(), "0xcontract", "1")
	_, err = cache.Get("nft:owner:0xcontract:1")
	assert.Error(t, err)
}

func TestNFTCov_RegisterEventHandler(t *testing.T) {
	caller := &nftCovEthCaller{}
	svc, err := NewNFTServiceWithCaller(caller, "http://rpc", nil)
	require.NoError(t, err)
	func() {
		defer func() { _ = recover() }()
		listener := &web3.EventListener{}
		svc.RegisterEventHandler(listener)
	}()
}

func TestNFTCov_RegisterEventHandlerWithCache(t *testing.T) {
	caller := &nftCovEthCaller{}
	svc, err := NewNFTServiceWithCaller(caller, "http://rpc", nil)
	require.NoError(t, err)
	func() {
		defer func() { _ = recover() }()
		listener := &web3.EventListener{}
		svc.RegisterEventHandlerWithCache(listener, nil, 1)
	}()
}

func TestNFTCov_SetLogger(t *testing.T) {
	caller := &nftCovEthCaller{}
	svc, err := NewNFTServiceWithCaller(caller, "http://rpc", nil)
	require.NoError(t, err)
	svc.SetLogger(zap.NewNop())
}

func TestNFTCov_ParseMetadataJSON_Valid(t *testing.T) {
	data := `{"name":"Test NFT","description":"A test","image":"ipfs://test","attributes":[{"trait_type":"color","value":"blue"}]}`
	meta, err := ParseMetadataJSON([]byte(data))
	require.NoError(t, err)
	assert.Equal(t, "Test NFT", meta.Name)
	assert.Equal(t, "A test", meta.Description)
}

func TestNFTCov_ParseMetadataJSON_Invalid(t *testing.T) {
	_, err := ParseMetadataJSON([]byte("not json"))
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to parse metadata")
}

func TestNFTCov_ParseMetadataJSON_Empty(t *testing.T) {
	meta, err := ParseMetadataJSON([]byte("{}"))
	require.NoError(t, err)
	assert.Empty(t, meta.Name)
}

func TestNFTCov_NewNFTService_DialError(t *testing.T) {
	_, err := NewNFTService("", nil)
	assert.Error(t, err)
}

type nftCovErrorCache struct{}

func (c *nftCovErrorCache) Get(_ string) (interface{}, error) { return nil, errors.New("cache error") }
func (c *nftCovErrorCache) Set(_ string, _ interface{}) error { return errors.New("cache error") }
func (c *nftCovErrorCache) SetWithExpiration(_ string, _ interface{}, _ time.Duration) error {
	return errors.New("cache error")
}
func (c *nftCovErrorCache) Delete(_ string) error { return errors.New("cache error") }

func TestNFTCov_VerifyNFT_CacheSetError(t *testing.T) {
	ownerAddr := common.HexToAddress("0x742d35Cc6634C0532925a3b844Bc9e7595f2bD18")
	padded := common.LeftPadBytes(ownerAddr.Bytes(), 32)
	caller := &nftCovEthCaller{
		callContractFn: func(_ context.Context, _ ethereum.CallMsg, _ *big.Int) ([]byte, error) {
			return padded, nil
		},
	}
	cache := &nftCovErrorCache{}
	svc, err := NewNFTServiceWithCaller(caller, "http://rpc", cache)
	require.NoError(t, err)
	result, err := svc.VerifyNFT(context.Background(), "0x742d35Cc6634C0532925a3b844Bc9e7595f2bD18", "0x742d35Cc6634C0532925a3b844Bc9e7595f2bD18", "1")
	require.NoError(t, err)
	assert.True(t, result)
}

func TestNFTCov_VerifyNFT_CacheGetError(t *testing.T) {
	ownerAddr := common.HexToAddress("0x742d35Cc6634C0532925a3b844Bc9e7595f2bD18")
	padded := common.LeftPadBytes(ownerAddr.Bytes(), 32)
	caller := &nftCovEthCaller{
		callContractFn: func(_ context.Context, _ ethereum.CallMsg, _ *big.Int) ([]byte, error) {
			return padded, nil
		},
	}
	cache := &nftCovErrorCache{}
	svc, err := NewNFTServiceWithCaller(caller, "http://rpc", cache)
	require.NoError(t, err)
	result, err := svc.VerifyNFT(context.Background(), "0x742d35Cc6634C0532925a3b844Bc9e7595f2bD18", "0x742d35Cc6634C0532925a3b844Bc9e7595f2bD18", "1")
	require.NoError(t, err)
	assert.True(t, result)
}

func TestNFTCov_VerifyNFT_CacheGetNonString(t *testing.T) {
	cache := newNftCovCache()
	_ = cache.Set("nft:owner:0x742d35Cc6634C0532925a3b844Bc9e7595f2bD18:1", 12345)
	ownerAddr := common.HexToAddress("0x742d35Cc6634C0532925a3b844Bc9e7595f2bD18")
	padded := common.LeftPadBytes(ownerAddr.Bytes(), 32)
	caller := &nftCovEthCaller{
		callContractFn: func(_ context.Context, _ ethereum.CallMsg, _ *big.Int) ([]byte, error) {
			return padded, nil
		},
	}
	svc, err := NewNFTServiceWithCaller(caller, "http://rpc", cache)
	require.NoError(t, err)
	result, err := svc.VerifyNFT(context.Background(), "0x742d35Cc6634C0532925a3b844Bc9e7595f2bD18", "0x742d35Cc6634C0532925a3b844Bc9e7595f2bD18", "1")
	require.NoError(t, err)
	assert.True(t, result)
}

func TestNFTCov_GetNFTMetadata_CacheGetNonMetadata(t *testing.T) {
	cache := newNftCovCache()
	_ = cache.Set("nft:metadata:0x742d35Cc6634C0532925a3b844Bc9e7595f2bD18:1", "not-a-metadata")
	caller := &nftCovEthCaller{
		callContractFn: func(_ context.Context, _ ethereum.CallMsg, _ *big.Int) ([]byte, error) {
			return nil, errors.New("RPC error")
		},
	}
	svc, err := NewNFTServiceWithCaller(caller, "http://rpc", cache)
	require.NoError(t, err)
	_, err = svc.GetNFTMetadata(context.Background(), "0x742d35Cc6634C0532925a3b844Bc9e7595f2bD18", "1")
	assert.Error(t, err)
}

func TestNFTCov_VerifyNFTBatch_WithError(t *testing.T) {
	caller := &nftCovEthCaller{
		callContractFn: func(_ context.Context, _ ethereum.CallMsg, _ *big.Int) ([]byte, error) {
			return nil, errors.New("RPC error")
		},
	}
	svc, err := NewNFTServiceWithCaller(caller, "http://rpc", nil)
	require.NoError(t, err)
	nfts := []struct {
		ContractAddress string
		TokenID         string
	}{
		{"0x742d35Cc6634C0532925a3b844Bc9e7595f2bD18", "1"},
	}
	results, err := svc.VerifyNFTBatch(context.Background(), "0x742d35Cc6634C0532925a3b844Bc9e7595f2bD18", nfts)
	require.NoError(t, err)
	assert.False(t, results["0x742d35Cc6634C0532925a3b844Bc9e7595f2bD18:1"])
}

func TestNFTCov_InvalidateOwnershipCache_DeleteError(t *testing.T) {
	cache := &nftCovErrorCache{}
	caller := &nftCovEthCaller{}
	svc, err := NewNFTServiceWithCaller(caller, "http://rpc", cache)
	require.NoError(t, err)
	svc.InvalidateOwnershipCache(context.Background(), "0xcontract", "1")
}

var _ cachetypes.CacheBackend = (*nftCovCache)(nil)
var _ cachetypes.CacheBackend = (*nftCovErrorCache)(nil)
var _ web3.EthCaller = (*nftCovEthCaller)(nil)
