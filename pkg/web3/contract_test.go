package web3

import (
	"context"
	"errors"
	"fmt"
	"math/big"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

// simpleABI is a minimal ERC-20 balanceOf ABI for testing CallContractFunction.
const simpleABI = `[{"constant":true,"inputs":[{"name":"account","type":"address"}],"name":"balanceOf","outputs":[{"name":"","type":"uint256"}],"type":"function"}]`

func TestContractInteractor_CallContractFunction_Success(t *testing.T) {
	mock := &mockEthCaller{
		callContractFn: func(ctx context.Context, call ethereum.CallMsg, blockNumber *big.Int) ([]byte, error) {
			return make([]byte, 32), nil // uint256 zero
		},
	}

	ci := NewContractInteractor(mock, zap.NewNop())
	result, err := ci.CallContractFunction(context.Background(), "0x1234567890123456789012345678901234567890", simpleABI, "balanceOf", "", common.HexToAddress("0xabcdefabcdefabcdefabcdefabcdefabcdefabcd"))
	require.NoError(t, err)
	assert.NotNil(t, result)
}

func TestContractInteractor_CallContractFunction_ProxyRetry(t *testing.T) {
	proxyAddr := "0x1111111111111111111111111111111111111111"
	implAddr := "0x2222222222222222222222222222222222222222"

	// Prepare the ERC-1967 slot result
	implSlotResult := make([]byte, 32)
	copy(implSlotResult[12:32], common.HexToAddress(implAddr).Bytes())

	counter := &sequentialCallCounter{}
	mock := &mockEthCaller{
		callContractFn: func(ctx context.Context, call ethereum.CallMsg, blockNumber *big.Int) ([]byte, error) {
			addr := call.To.Hex()
			idx := counter.next(addr)

			// Proxy address: 1st call fails, 2nd call returns impl slot
			if addr == proxyAddr {
				switch idx {
				case 0:
					return nil, fmt.Errorf("execution reverted")
				case 1:
					return implSlotResult, nil
				}
			}

			// Implementation address: succeeds
			if addr == implAddr {
				return make([]byte, 32), nil
			}

			return nil, fmt.Errorf("unexpected call to %s", addr)
		},
	}

	ci := NewContractInteractor(mock, zap.NewNop())
	result, err := ci.CallContractFunction(context.Background(), proxyAddr, simpleABI, "balanceOf", "", common.HexToAddress("0xabcdefabcdefabcdefabcdefabcdefabcdefabcd"))
	require.NoError(t, err)
	assert.NotNil(t, result)
	assert.Equal(t, 1, counter.getCount(implAddr))
}

func TestContractInteractor_CallContractFunction_DualError(t *testing.T) {
	proxyAddr := "0x1111111111111111111111111111111111111111"
	implAddr := "0x2222222222222222222222222222222222222222"

	implSlotResult := make([]byte, 32)
	copy(implSlotResult[12:32], common.HexToAddress(implAddr).Bytes())

	counter := &sequentialCallCounter{}
	mock := &mockEthCaller{
		callContractFn: func(ctx context.Context, call ethereum.CallMsg, blockNumber *big.Int) ([]byte, error) {
			addr := call.To.Hex()
			idx := counter.next(addr)

			if addr == proxyAddr {
				switch idx {
				case 0:
					return nil, fmt.Errorf("proxy call failed")
				case 1:
					return implSlotResult, nil
				}
			}

			if addr == implAddr {
				return nil, fmt.Errorf("implementation call failed")
			}

			return nil, fmt.Errorf("unexpected call to %s", addr)
		},
	}

	ci := NewContractInteractor(mock, zap.NewNop())
	_, err := ci.CallContractFunction(context.Background(), proxyAddr, simpleABI, "balanceOf", "", common.HexToAddress("0xabcdefabcdefabcdefabcdefabcdefabcdefabcd"))
	require.Error(t, err)

	var dualErr *DualError
	assert.True(t, errors.As(err, &dualErr), "expected DualError, got %T: %v", err, err)
	if dualErr != nil {
		assert.Contains(t, dualErr.Primary.Error(), "proxy call failed")
		assert.Contains(t, dualErr.Secondary.Error(), "implementation call failed")
	}
}

func TestContractInteractor_ResolveImplementation_Proxy(t *testing.T) {
	proxyAddr := "0x1111111111111111111111111111111111111111"
	implAddr := "0x2222222222222222222222222222222222222222"

	slotResult := make([]byte, 32)
	copy(slotResult[12:32], common.HexToAddress(implAddr).Bytes())

	callCount := 0
	mock := &mockEthCaller{
		callContractFn: func(ctx context.Context, call ethereum.CallMsg, blockNumber *big.Int) ([]byte, error) {
			callCount++
			return slotResult, nil
		},
	}

	ci := NewContractInteractor(mock, zap.NewNop())
	resolved, err := ci.ResolveImplementation(context.Background(), proxyAddr)
	require.NoError(t, err)
	assert.Equal(t, implAddr, resolved)

	// Second call should hit cache
	resolved2, err := ci.ResolveImplementation(context.Background(), proxyAddr)
	require.NoError(t, err)
	assert.Equal(t, implAddr, resolved2)
	assert.Equal(t, 1, callCount) // only one RPC call
}

func TestContractInteractor_ResolveImplementation_NotProxy(t *testing.T) {
	regularAddr := "0x3333333333333333333333333333333333333333"

	mock := &mockEthCaller{
		callContractFn: func(ctx context.Context, call ethereum.CallMsg, blockNumber *big.Int) ([]byte, error) {
			return make([]byte, 32), nil // all zeros — not a proxy
		},
	}

	ci := NewContractInteractor(mock, zap.NewNop())
	resolved, err := ci.ResolveImplementation(context.Background(), regularAddr)
	require.NoError(t, err)
	assert.Equal(t, regularAddr, resolved)
}

func TestContractInteractor_ResolveImplementation_TTLExpiry(t *testing.T) {
	proxyAddr := "0x1111111111111111111111111111111111111111"
	implAddr := "0x2222222222222222222222222222222222222222"

	slotResult := make([]byte, 32)
	copy(slotResult[12:32], common.HexToAddress(implAddr).Bytes())

	callCount := 0
	mock := &mockEthCaller{
		callContractFn: func(ctx context.Context, call ethereum.CallMsg, blockNumber *big.Int) ([]byte, error) {
			callCount++
			return slotResult, nil
		},
	}

	ci := NewContractInteractor(mock, zap.NewNop())

	// First call resolves via RPC
	resolved, err := ci.ResolveImplementation(context.Background(), proxyAddr)
	require.NoError(t, err)
	assert.Equal(t, implAddr, resolved)
	assert.Equal(t, 1, callCount)

	// Expire the cache entry manually
	ci.proxyMu.Lock()
	entry := ci.proxyCache[common.HexToAddress(proxyAddr)]
	entry.expiry = time.Now().Add(-time.Second)
	ci.proxyCache[common.HexToAddress(proxyAddr)] = entry
	ci.proxyMu.Unlock()

	// Next call should re-resolve via RPC
	resolved, err = ci.ResolveImplementation(context.Background(), proxyAddr)
	require.NoError(t, err)
	assert.Equal(t, implAddr, resolved)
	assert.Equal(t, 2, callCount)
}

func TestContractInteractor_InvalidateProxyCache(t *testing.T) {
	proxyAddr := "0x1111111111111111111111111111111111111111"
	implAddr := "0x2222222222222222222222222222222222222222"

	slotResult := make([]byte, 32)
	copy(slotResult[12:32], common.HexToAddress(implAddr).Bytes())

	callCount := 0
	mock := &mockEthCaller{
		callContractFn: func(ctx context.Context, call ethereum.CallMsg, blockNumber *big.Int) ([]byte, error) {
			callCount++
			return slotResult, nil
		},
	}

	ci := NewContractInteractor(mock, zap.NewNop())

	// Resolve to populate cache
	resolved, err := ci.ResolveImplementation(context.Background(), proxyAddr)
	require.NoError(t, err)
	assert.Equal(t, implAddr, resolved)
	assert.Equal(t, 1, callCount)

	// Invalidate cache
	ci.InvalidateProxyCache(proxyAddr)

	// Next call should re-resolve via RPC
	resolved, err = ci.ResolveImplementation(context.Background(), proxyAddr)
	require.NoError(t, err)
	assert.Equal(t, implAddr, resolved)
	assert.Equal(t, 2, callCount)
}

func TestContractInteractor_IsContractAddress(t *testing.T) {
	contractAddr := common.HexToAddress("0x1111111111111111111111111111111111111111")
	emptyAddr := common.HexToAddress("0x2222222222222222222222222222222222222222")

	mock := &mockEthCaller{
		codeAtFn: func(ctx context.Context, contract common.Address, blockNumber *big.Int) ([]byte, error) {
			if contract == contractAddr {
				return []byte{0x60, 0x80, 0x60, 0x40}, nil
			}
			return nil, nil // no code
		},
	}

	ci := NewContractInteractor(mock, zap.NewNop())

	isContract, err := ci.IsContractAddress(context.Background(), contractAddr.Hex())
	require.NoError(t, err)
	assert.True(t, isContract)

	isEmpty, err := ci.IsContractAddress(context.Background(), emptyAddr.Hex())
	require.NoError(t, err)
	assert.False(t, isEmpty)
}

// sequentialCallCounter tracks per-address call counts for sequential mock responses.
type sequentialCallCounter struct {
	counts map[string]int
}

func (s *sequentialCallCounter) next(addr string) int {
	if s.counts == nil {
		s.counts = make(map[string]int)
	}
	idx := s.counts[addr]
	s.counts[addr]++
	return idx
}

func (s *sequentialCallCounter) getCount(addr string) int {
	return s.counts[addr]
}
