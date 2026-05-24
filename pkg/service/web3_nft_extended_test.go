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
	"go.uber.org/zap"
)

func TestNFTService_VerifyNFT_CacheWrongType(t *testing.T) {
	cache := &testCacheBackend{data: map[string]interface{}{
		"nft:owner:0x1234567890123456789012345678901234567890:1": 12345,
	}}
	owner := common.HexToAddress("0x742d35Cc6634C0532925a3b844Bc9e7595f2bD18")
	result := make([]byte, 32)
	copy(result[12:32], owner[:])
	caller := &testEthCaller{
		callContractFn: func(ctx context.Context, call ethereum.CallMsg, blockNumber *big.Int) ([]byte, error) {
			return result, nil
		},
	}
	svc, err := NewNFTServiceWithCaller(caller, "", cache)
	require.NoError(t, err)

	verified, err := svc.VerifyNFT(context.Background(), "0x742d35Cc6634C0532925a3b844Bc9e7595f2bD18", "0x1234567890123456789012345678901234567890", "1")
	assert.NoError(t, err)
	assert.True(t, verified)
}

func TestNFTService_GetNFTMetadata_CacheWrongType(t *testing.T) {
	cache := &testCacheBackend{data: map[string]interface{}{
		"nft:metadata:0x1234567890123456789012345678901234567890:1": "not-a-metadata",
	}}
	svc, err := NewNFTServiceWithCaller(&testEthCaller{}, "", cache)
	require.NoError(t, err)

	_, err = svc.GetNFTMetadata(context.Background(), "0x1234567890123456789012345678901234567890", "1")
	assert.Error(t, err)
}

func TestNFTService_GetNFTMetadata_TokenURICallError(t *testing.T) {
	caller := &testEthCaller{
		callContractFn: func(ctx context.Context, call ethereum.CallMsg, blockNumber *big.Int) ([]byte, error) {
			return nil, fmt.Errorf("RPC error")
		},
	}
	svc, err := NewNFTServiceWithCaller(caller, "", nil)
	require.NoError(t, err)

	_, err = svc.GetNFTMetadata(context.Background(), "0x1234567890123456789012345678901234567890", "1")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "token URI")
}

func TestNFTService_GetNFTMetadata_InsufficientData(t *testing.T) {
	caller := &testEthCaller{
		callContractFn: func(ctx context.Context, call ethereum.CallMsg, blockNumber *big.Int) ([]byte, error) {
			return []byte{0x01, 0x02}, nil
		},
	}
	svc, err := NewNFTServiceWithCaller(caller, "", nil)
	require.NoError(t, err)

	_, err = svc.GetNFTMetadata(context.Background(), "0x1234567890123456789012345678901234567890", "1")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "insufficient data")
}

func TestNFTService_InvalidateOwnershipCache_NoCacheEnabled(t *testing.T) {
	svc, err := NewNFTServiceWithCaller(&testEthCaller{}, "", nil)
	require.NoError(t, err)

	assert.NotPanics(t, func() {
		svc.InvalidateOwnershipCache(context.Background(), "0x1234567890123456789012345678901234567890", "1")
	})
}

func TestNFTService_Close_NoCloser(t *testing.T) {
	svc, err := NewNFTServiceWithCaller(&testEthCaller{}, "", nil)
	require.NoError(t, err)

	assert.NotPanics(t, func() {
		svc.Close()
	})
}

func TestNFTService_VerifyNFT_CacheSetFails(t *testing.T) {
	owner := common.HexToAddress("0x742d35Cc6634C0532925a3b844Bc9e7595f2bD18")
	result := make([]byte, 32)
	copy(result[12:32], owner[:])

	caller := &testEthCaller{
		callContractFn: func(ctx context.Context, call ethereum.CallMsg, blockNumber *big.Int) ([]byte, error) {
			return result, nil
		},
	}
	cache := &failingSetCache{}
	svc, err := NewNFTServiceWithCaller(caller, "", cache)
	require.NoError(t, err)

	verified, err := svc.VerifyNFT(context.Background(), "0x742d35Cc6634C0532925a3b844Bc9e7595f2bD18", "0x1234567890123456789012345678901234567890", "1")
	assert.NoError(t, err)
	assert.True(t, verified)
}

type failingSetCache struct {
	testCacheBackend
}

func (m *failingSetCache) SetWithExpiration(key string, value interface{}, ttl time.Duration) error {
	return fmt.Errorf("cache write failed")
}

func TestNFTService_VerifyNFT_OwnerOfReturnsEmpty(t *testing.T) {
	caller := &testEthCaller{
		callContractFn: func(ctx context.Context, call ethereum.CallMsg, blockNumber *big.Int) ([]byte, error) {
			return []byte{}, nil
		},
	}
	svc, err := NewNFTServiceWithCaller(caller, "", nil)
	require.NoError(t, err)

	_, err = svc.VerifyNFT(context.Background(), "0x742d35Cc6634C0532925a3b844Bc9e7595f2bD18", "0x1234567890123456789012345678901234567890", "1")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "insufficient data")
}

func TestNFTService_NewNFTServiceWithCaller_NilCaller(t *testing.T) {
	svc, err := NewNFTServiceWithCaller(nil, "", nil)
	require.NoError(t, err)
	assert.NotNil(t, svc)
}

func TestParseMetadataJSON_EmptyObject(t *testing.T) {
	meta, err := ParseMetadataJSON([]byte(`{}`))
	require.NoError(t, err)
	assert.NotNil(t, meta)
}

func TestParseMetadataJSON_WithAttributes(t *testing.T) {
	meta, err := ParseMetadataJSON([]byte(`{"name":"Test","attributes":[{"trait_type":"Color","value":"Blue"}]}`))
	require.NoError(t, err)
	assert.Len(t, meta.Attributes, 1)
	assert.Equal(t, "Color", meta.Attributes[0].TraitType)
	assert.Equal(t, "Blue", meta.Attributes[0].Value)
}

func TestNFTService_GetNFTMetadata_WithCacheAndChainCall(t *testing.T) {
	uri := "https://example.com/metadata.json"
	uriLen := len(uri)
	paddedLen := ((uriLen + 31) / 32) * 32
	totalLen := 64 + paddedLen
	tokenURIResult := make([]byte, totalLen)
	copy(tokenURIResult[:32], common.LeftPadBytes([]byte{32}, 32))
	copy(tokenURIResult[32:64], common.LeftPadBytes(big.NewInt(int64(uriLen)).Bytes(), 32))
	copy(tokenURIResult[64:], []byte(uri))

	caller := &testEthCaller{
		callContractFn: func(ctx context.Context, call ethereum.CallMsg, blockNumber *big.Int) ([]byte, error) {
			return tokenURIResult, nil
		},
	}
	cache := &testCacheBackend{data: map[string]interface{}{}}
	svc, err := NewNFTServiceWithCaller(caller, "", cache)
	require.NoError(t, err)

	meta, err := svc.GetNFTMetadata(context.Background(), "0x1234567890123456789012345678901234567890", "1")
	require.NoError(t, err)
	assert.NotNil(t, meta)
}

func TestNFTService_VerifyNFTBatch_WithMixedResults(t *testing.T) {
	owner := common.HexToAddress("0x742d35Cc6634C0532925a3b844Bc9e7595f2bD18")
	result := make([]byte, 32)
	copy(result[12:32], owner[:])

	callCount := 0
	caller := &testEthCaller{
		callContractFn: func(ctx context.Context, call ethereum.CallMsg, blockNumber *big.Int) ([]byte, error) {
			callCount++
			if callCount > 1 {
				return nil, fmt.Errorf("RPC error")
			}
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
	assert.True(t, results["0x1234567890123456789012345678901234567890:1"])
	assert.False(t, results["0x1234567890123456789012345678901234567890:2"])
}

func TestNFTService_VerifyNFT_CacheDeleteFails(t *testing.T) {
	cache := &deleteFailCache{data: map[string]interface{}{
		"nft:owner:0x1234567890123456789012345678901234567890:1": "0x742d35Cc6634C0532925a3b844Bc9e7595f2bD18",
	}}
	svc, err := NewNFTServiceWithCaller(&testEthCaller{}, "", cache)
	require.NoError(t, err)

	svc.SetLogger(zap.NewNop())
	svc.InvalidateOwnershipCache(context.Background(), "0x1234567890123456789012345678901234567890", "1")
}

type deleteFailCache struct {
	data map[string]interface{}
}

func (m *deleteFailCache) Get(key string) (interface{}, error) {
	v, ok := m.data[key]
	if !ok {
		return nil, fmt.Errorf("not found")
	}
	return v, nil
}
func (m *deleteFailCache) Set(key string, value interface{}) error {
	m.data[key] = value
	return nil
}
func (m *deleteFailCache) SetWithExpiration(key string, value interface{}, ttl time.Duration) error {
	m.data[key] = value
	return nil
}
func (m *deleteFailCache) Delete(key string) error {
	return fmt.Errorf("delete failed")
}

var _ cachetypes.CacheBackend = (*deleteFailCache)(nil)
