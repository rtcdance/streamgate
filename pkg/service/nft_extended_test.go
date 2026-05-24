package service

import (
	"context"
	"fmt"
	"math/big"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/common"
	"github.com/rtcdance/streamgate/pkg/cachetypes"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"strings"
	"go.uber.org/zap"
)

type testEthCaller struct {
	callContractFn func(ctx context.Context, call ethereum.CallMsg, blockNumber *big.Int) ([]byte, error)
	codeAtFn       func(ctx context.Context, contract common.Address, blockNumber *big.Int) ([]byte, error)
}

func (m *testEthCaller) CallContract(ctx context.Context, call ethereum.CallMsg, blockNumber *big.Int) ([]byte, error) {
	if m.callContractFn != nil {
		return m.callContractFn(ctx, call, blockNumber)
	}
	return nil, nil
}

func (m *testEthCaller) CodeAt(ctx context.Context, contract common.Address, blockNumber *big.Int) ([]byte, error) {
	if m.codeAtFn != nil {
		return m.codeAtFn(ctx, contract, blockNumber)
	}
	return nil, nil
}

type testCacheBackend struct {
	data map[string]interface{}
}

func (m *testCacheBackend) Get(key string) (interface{}, error) {
	if v, ok := m.data[key]; ok {
		return v, nil
	}
	return nil, fmt.Errorf("not found")
}

func (m *testCacheBackend) Set(key string, value interface{}) error {
	m.data[key] = value
	return nil
}

func (m *testCacheBackend) SetWithExpiration(key string, value interface{}, ttl time.Duration) error {
	m.data[key] = value
	return nil
}

func (m *testCacheBackend) Delete(key string) error {
	delete(m.data, key)
	return nil
}

var _ cachetypes.CacheBackend = (*testCacheBackend)(nil)

func TestNFTService_VerifyNFT_InvalidAddress(t *testing.T) {
	svc, err := NewNFTServiceWithCaller(&testEthCaller{}, "", nil)
	require.NoError(t, err)

	_, err = svc.VerifyNFT(context.Background(), "not-an-address", "0x1234567890123456789012345678901234567890", "1")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "invalid address")
}

func TestNFTService_VerifyNFT_InvalidContractAddress(t *testing.T) {
	svc, err := NewNFTServiceWithCaller(&testEthCaller{}, "", nil)
	require.NoError(t, err)

	_, err = svc.VerifyNFT(context.Background(), "0x742d35Cc6634C0532925a3b844Bc9e7595f2bD18", "not-a-contract", "1")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "invalid contract address")
}

func TestNFTService_VerifyNFT_InvalidTokenID(t *testing.T) {
	svc, err := NewNFTServiceWithCaller(&testEthCaller{}, "", nil)
	require.NoError(t, err)

	_, err = svc.VerifyNFT(context.Background(), "0x742d35Cc6634C0532925a3b844Bc9e7595f2bD18", "0x1234567890123456789012345678901234567890", "not-a-number")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "invalid token ID")
}

func TestNFTService_VerifyNFT_ContractCallError(t *testing.T) {
	caller := &testEthCaller{
		callContractFn: func(ctx context.Context, call ethereum.CallMsg, blockNumber *big.Int) ([]byte, error) {
			return nil, fmt.Errorf("RPC error")
		},
	}
	svc, err := NewNFTServiceWithCaller(caller, "", nil)
	require.NoError(t, err)

	_, err = svc.VerifyNFT(context.Background(), "0x742d35Cc6634C0532925a3b844Bc9e7595f2bD18", "0x1234567890123456789012345678901234567890", "1")
	assert.Error(t, err)
}

func TestNFTService_VerifyNFT_InsufficientData(t *testing.T) {
	caller := &testEthCaller{
		callContractFn: func(ctx context.Context, call ethereum.CallMsg, blockNumber *big.Int) ([]byte, error) {
			return []byte{0x01, 0x02}, nil
		},
	}
	svc, err := NewNFTServiceWithCaller(caller, "", nil)
	require.NoError(t, err)

	_, err = svc.VerifyNFT(context.Background(), "0x742d35Cc6634C0532925a3b844Bc9e7595f2bD18", "0x1234567890123456789012345678901234567890", "1")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "insufficient data")
}

func TestNFTService_VerifyNFT_OwnerMatches(t *testing.T) {
	owner := common.HexToAddress("0x742d35Cc6634C0532925a3b844Bc9e7595f2bD18")
	result := make([]byte, 32)
	copy(result[12:32], owner[:])

	caller := &testEthCaller{
		callContractFn: func(ctx context.Context, call ethereum.CallMsg, blockNumber *big.Int) ([]byte, error) {
			return result, nil
		},
	}
	svc, err := NewNFTServiceWithCaller(caller, "", nil)
	require.NoError(t, err)

	verified, err := svc.VerifyNFT(context.Background(), "0x742d35Cc6634C0532925a3b844Bc9e7595f2bD18", "0x1234567890123456789012345678901234567890", "1")
	assert.NoError(t, err)
	assert.True(t, verified)
}

func TestNFTService_VerifyNFT_OwnerMismatch(t *testing.T) {
	owner := common.HexToAddress("0x1111111111111111111111111111111111111111")
	result := make([]byte, 32)
	copy(result[12:32], owner[:])

	caller := &testEthCaller{
		callContractFn: func(ctx context.Context, call ethereum.CallMsg, blockNumber *big.Int) ([]byte, error) {
			return result, nil
		},
	}
	svc, err := NewNFTServiceWithCaller(caller, "", nil)
	require.NoError(t, err)

	verified, err := svc.VerifyNFT(context.Background(), "0x742d35Cc6634C0532925a3b844Bc9e7595f2bD18", "0x1234567890123456789012345678901234567890", "1")
	assert.NoError(t, err)
	assert.False(t, verified)
}

func TestNFTService_GetNFTMetadata_InvalidContract(t *testing.T) {
	svc, err := NewNFTServiceWithCaller(&testEthCaller{}, "", nil)
	require.NoError(t, err)

	_, err = svc.GetNFTMetadata(context.Background(), "not-a-contract", "1")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "invalid contract address")
}

func TestNFTService_GetNFTMetadata_InvalidTokenID(t *testing.T) {
	svc, err := NewNFTServiceWithCaller(&testEthCaller{}, "", nil)
	require.NoError(t, err)

	_, err = svc.GetNFTMetadata(context.Background(), "0x1234567890123456789012345678901234567890", "not-a-number")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "invalid token ID")
}

func TestNFTService_VerifyNFTBatch_Multiple(t *testing.T) {
	owner := common.HexToAddress("0x742d35Cc6634C0532925a3b844Bc9e7595f2bD18")
	result := make([]byte, 32)
	copy(result[12:32], owner[:])

	caller := &testEthCaller{
		callContractFn: func(ctx context.Context, call ethereum.CallMsg, blockNumber *big.Int) ([]byte, error) {
			return result, nil
		},
	}
	svc, err := NewNFTServiceWithCaller(caller, "", nil)
	require.NoError(t, err)

	nfts := []struct {
		ContractAddress string
		TokenID         string
	}{
		{ContractAddress: "0x1234567890123456789012345678901234567890", TokenID: "1"},
		{ContractAddress: "0x1234567890123456789012345678901234567890", TokenID: "2"},
	}

	results, err := svc.VerifyNFTBatch(context.Background(), "0x742d35Cc6634C0532925a3b844Bc9e7595f2bD18", nfts)
	assert.NoError(t, err)
	assert.Len(t, results, 2)
}

func TestNFTService_VerifyNFTBatch_Empty(t *testing.T) {
	svc, err := NewNFTServiceWithCaller(&testEthCaller{}, "", nil)
	require.NoError(t, err)

	results, err := svc.VerifyNFTBatch(context.Background(), "0x742d35Cc6634C0532925a3b844Bc9e7595f2bD18", nil)
	assert.NoError(t, err)
	assert.Empty(t, results)
}

func TestNFTService_VerifyNFT_WithCacheHit(t *testing.T) {
	cache := &testCacheBackend{data: map[string]interface{}{
		"nft:owner:0x1234567890123456789012345678901234567890:1": "0x742d35Cc6634C0532925a3b844Bc9e7595f2bD18",
	}}
	svc, err := NewNFTServiceWithCaller(&testEthCaller{}, "", cache)
	require.NoError(t, err)

	verified, err := svc.VerifyNFT(context.Background(), "0x742d35Cc6634C0532925a3b844Bc9e7595f2bD18", "0x1234567890123456789012345678901234567890", "1")
	assert.NoError(t, err)
	assert.True(t, verified)
}

func TestNFTService_VerifyNFT_WithCacheMismatch(t *testing.T) {
	cache := &testCacheBackend{data: map[string]interface{}{
		"nft:owner:0x1234567890123456789012345678901234567890:1": "0x1111111111111111111111111111111111111111",
	}}
	svc, err := NewNFTServiceWithCaller(&testEthCaller{}, "", cache)
	require.NoError(t, err)

	verified, err := svc.VerifyNFT(context.Background(), "0x742d35Cc6634C0532925a3b844Bc9e7595f2bD18", "0x1234567890123456789012345678901234567890", "1")
	assert.NoError(t, err)
	assert.False(t, verified)
}

func TestNFTService_GetNFTMetadata_WithCacheHit(t *testing.T) {
	cachedMeta := &NFTMetadata{Name: "Cached NFT", Description: "from cache"}
	cache := &testCacheBackend{data: map[string]interface{}{
		"nft:metadata:0x1234567890123456789012345678901234567890:1": cachedMeta,
	}}
	svc, err := NewNFTServiceWithCaller(&testEthCaller{}, "", cache)
	require.NoError(t, err)

	meta, err := svc.GetNFTMetadata(context.Background(), "0x1234567890123456789012345678901234567890", "1")
	assert.NoError(t, err)
	assert.Equal(t, "Cached NFT", meta.Name)
}

func TestNFTService_VerifyNFT_WithCacheAndChainCall(t *testing.T) {
	owner := common.HexToAddress("0x742d35Cc6634C0532925a3b844Bc9e7595f2bD18")
	result := make([]byte, 32)
	copy(result[12:32], owner[:])

	caller := &testEthCaller{
		callContractFn: func(ctx context.Context, call ethereum.CallMsg, blockNumber *big.Int) ([]byte, error) {
			return result, nil
		},
	}
	cache := &testCacheBackend{data: map[string]interface{}{}}
	svc, err := NewNFTServiceWithCaller(caller, "", cache)
	require.NoError(t, err)

	verified, err := svc.VerifyNFT(context.Background(), "0x742d35Cc6634C0532925a3b844Bc9e7595f2bD18", "0x1234567890123456789012345678901234567890", "1")
	assert.NoError(t, err)
	assert.True(t, verified)

	cached, err := cache.Get("nft:owner:0x1234567890123456789012345678901234567890:1")
	assert.NoError(t, err)
	cachedStr, ok := cached.(string)
	require.True(t, ok)
	assert.Equal(t, strings.ToLower("0x742d35Cc6634C0532925a3b844Bc9e7595f2bD18"), strings.ToLower(cachedStr))
}

func TestNFTService_InvalidateOwnershipCache_WithCache(t *testing.T) {
	cache := &testCacheBackend{data: map[string]interface{}{
		"nft:owner:0x1234567890123456789012345678901234567890:1": "0x742d35Cc6634C0532925a3b844Bc9e7595f2bD18",
	}}
	svc, err := NewNFTServiceWithCaller(&testEthCaller{}, "", cache)
	require.NoError(t, err)

	svc.InvalidateOwnershipCache(context.Background(), "0x1234567890123456789012345678901234567890", "1")

	_, err = cache.Get("nft:owner:0x1234567890123456789012345678901234567890:1")
	assert.Error(t, err)
}

func TestNFTService_Close_WithCloser(t *testing.T) {
	closer := &testCloser{}
	caller := &testEthCallerWithCloser{closer: closer}
	svc, err := NewNFTServiceWithCaller(caller, "", nil)
	require.NoError(t, err)

	svc.Close()
	assert.True(t, closer.closed)
}

type testCloser struct {
	closed bool
}

func (c *testCloser) Close() error {
	c.closed = true
	return nil
}

type testEthCallerWithCloser struct {
	testEthCaller
	closer *testCloser
}

func (m *testEthCallerWithCloser) Close() error {
	return m.closer.Close()
}

func TestNFTService_SetLogger_NilPanics(t *testing.T) {
	svc, err := NewNFTServiceWithCaller(&testEthCaller{}, "", nil)
	require.NoError(t, err)

	assert.NotPanics(t, func() {
		svc.SetLogger(zap.NewNop())
	})
}

func TestNFTService_VerifyNFTBatch_WithError(t *testing.T) {
	caller := &testEthCaller{
		callContractFn: func(ctx context.Context, call ethereum.CallMsg, blockNumber *big.Int) ([]byte, error) {
			return nil, fmt.Errorf("RPC error")
		},
	}
	svc, err := NewNFTServiceWithCaller(caller, "", nil)
	require.NoError(t, err)

	nfts := []struct {
		ContractAddress string
		TokenID         string
	}{
		{ContractAddress: "0x1234567890123456789012345678901234567890", TokenID: "1"},
	}

	results, err := svc.VerifyNFTBatch(context.Background(), "0x742d35Cc6634C0532925a3b844Bc9e7595f2bD18", nfts)
	assert.NoError(t, err)
	assert.False(t, results["0x1234567890123456789012345678901234567890:1"])
}

func TestNFTService_GetNFTMetadata_ContractCallError(t *testing.T) {
	caller := &testEthCaller{
		callContractFn: func(ctx context.Context, call ethereum.CallMsg, blockNumber *big.Int) ([]byte, error) {
			return nil, fmt.Errorf("RPC error")
		},
	}
	svc, err := NewNFTServiceWithCaller(caller, "", nil)
	require.NoError(t, err)

	_, err = svc.GetNFTMetadata(context.Background(), "0x1234567890123456789012345678901234567890", "1")
	assert.Error(t, err)
}
