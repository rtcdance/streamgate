package nft

import (
	"context"
	"fmt"
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

type mockEthCaller struct {
	callContractFn func(ctx context.Context, call ethereum.CallMsg, blockNumber *big.Int) ([]byte, error)
	codeAtFn       func(ctx context.Context, contract common.Address, blockNumber *big.Int) ([]byte, error)
}

func (m *mockEthCaller) CallContract(ctx context.Context, call ethereum.CallMsg, blockNumber *big.Int) ([]byte, error) {
	if m.callContractFn != nil {
		return m.callContractFn(ctx, call, blockNumber)
	}
	return nil, fmt.Errorf("mock EthCaller: CallContract not configured")
}

func (m *mockEthCaller) CodeAt(ctx context.Context, contract common.Address, blockNumber *big.Int) ([]byte, error) {
	if m.codeAtFn != nil {
		return m.codeAtFn(ctx, contract, blockNumber)
	}
	return nil, nil
}

func TestNFTVerifier_VerifyNFTOwnership_IsOwner(t *testing.T) {
	ownerAddr := common.HexToAddress("0x742d35Cc6634C0532925a3b844Bc9e7595f2bD18")
	contractAddr := common.HexToAddress("0x1234567890123456789012345678901234567890")

	mock := &mockEthCaller{
		callContractFn: func(ctx context.Context, call ethereum.CallMsg, blockNumber *big.Int) ([]byte, error) {
			result := common.LeftPadBytes(ownerAddr.Bytes(), 32)
			return result, nil
		},
	}

	verifier := NewNFTVerifier(mock, zap.NewNop())
	isOwner, err := verifier.VerifyNFTOwnership(context.Background(), contractAddr.Hex(), "1", ownerAddr.Hex())

	require.NoError(t, err)
	assert.True(t, isOwner)
}

func TestNFTVerifier_VerifyNFTOwnership_NotOwner(t *testing.T) {
	ownerAddr := common.HexToAddress("0x742d35Cc6634C0532925a3b844Bc9e7595f2bD18")
	otherAddr := common.HexToAddress("0x1111111111111111111111111111111111111111")
	contractAddr := common.HexToAddress("0x1234567890123456789012345678901234567890")

	mock := &mockEthCaller{
		callContractFn: func(ctx context.Context, call ethereum.CallMsg, blockNumber *big.Int) ([]byte, error) {
			result := common.LeftPadBytes(otherAddr.Bytes(), 32)
			return result, nil
		},
	}

	verifier := NewNFTVerifier(mock, zap.NewNop())
	isOwner, err := verifier.VerifyNFTOwnership(context.Background(), contractAddr.Hex(), "1", ownerAddr.Hex())

	require.NoError(t, err)
	assert.False(t, isOwner)
}

func TestNFTVerifier_VerifyNFTOwnership_CallError(t *testing.T) {
	mock := &mockEthCaller{
		callContractFn: func(ctx context.Context, call ethereum.CallMsg, blockNumber *big.Int) ([]byte, error) {
			return nil, fmt.Errorf("RPC error: contract not found")
		},
	}

	verifier := NewNFTVerifier(mock, zap.NewNop())
	_, err := verifier.VerifyNFTOwnership(context.Background(), "0x1234567890123456789012345678901234567890", "1", "0x742d35Cc6634C0532925a3b844Bc9e7595f2bD18")

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to call ownerOf")
}

func TestNFTVerifier_GetNFTBalance(t *testing.T) {
	contractAddr := common.HexToAddress("0x1234567890123456789012345678901234567890")
	ownerAddr := common.HexToAddress("0x742d35Cc6634C0532925a3b844Bc9e7595f2bD18")

	t.Run("positive balance", func(t *testing.T) {
		mock := &mockEthCaller{
			callContractFn: func(ctx context.Context, call ethereum.CallMsg, blockNumber *big.Int) ([]byte, error) {
				return common.LeftPadBytes(big.NewInt(3).Bytes(), 32), nil
			},
		}

		verifier := NewNFTVerifier(mock, zap.NewNop())
		balance, err := verifier.GetNFTBalance(context.Background(), contractAddr.Hex(), ownerAddr.Hex())

		require.NoError(t, err)
		assert.Equal(t, big.NewInt(3), balance)
	})

	t.Run("zero balance", func(t *testing.T) {
		mock := &mockEthCaller{
			callContractFn: func(ctx context.Context, call ethereum.CallMsg, blockNumber *big.Int) ([]byte, error) {
				return make([]byte, 32), nil
			},
		}

		verifier := NewNFTVerifier(mock, zap.NewNop())
		balance, err := verifier.GetNFTBalance(context.Background(), contractAddr.Hex(), ownerAddr.Hex())

		require.NoError(t, err)
		assert.Equal(t, 0, balance.Cmp(big.NewInt(0)))
	})

	t.Run("call error", func(t *testing.T) {
		mock := &mockEthCaller{
			callContractFn: func(ctx context.Context, call ethereum.CallMsg, blockNumber *big.Int) ([]byte, error) {
				return nil, fmt.Errorf("network error")
			},
		}

		verifier := NewNFTVerifier(mock, zap.NewNop())
		_, err := verifier.GetNFTBalance(context.Background(), contractAddr.Hex(), ownerAddr.Hex())

		assert.Error(t, err)
		assert.Contains(t, err.Error(), "failed to call balanceOf")
	})
}

func TestNFTVerifier_VerifyNFTCollection(t *testing.T) {
	contractAddr := common.HexToAddress("0x1234567890123456789012345678901234567890")
	ownerAddr := common.HexToAddress("0x742d35Cc6634C0532925a3b844Bc9e7595f2bD18")

	t.Run("owns NFTs from collection", func(t *testing.T) {
		mock := &mockEthCaller{
			callContractFn: func(ctx context.Context, call ethereum.CallMsg, blockNumber *big.Int) ([]byte, error) {
				return common.LeftPadBytes(big.NewInt(2).Bytes(), 32), nil
			},
		}

		verifier := NewNFTVerifier(mock, zap.NewNop())
		hasNFT, err := verifier.VerifyNFTCollection(context.Background(), contractAddr.Hex(), ownerAddr.Hex())

		require.NoError(t, err)
		assert.True(t, hasNFT)
	})

	t.Run("owns no NFTs from collection", func(t *testing.T) {
		mock := &mockEthCaller{
			callContractFn: func(ctx context.Context, call ethereum.CallMsg, blockNumber *big.Int) ([]byte, error) {
				return common.LeftPadBytes([]byte{}, 32), nil
			},
		}

		verifier := NewNFTVerifier(mock, zap.NewNop())
		hasNFT, err := verifier.VerifyNFTCollection(context.Background(), contractAddr.Hex(), ownerAddr.Hex())

		require.NoError(t, err)
		assert.False(t, hasNFT)
	})
}

func TestNFTVerifier_GetNFTInfo(t *testing.T) {
	mock := &mockEthCaller{}
	verifier := NewNFTVerifier(mock, zap.NewNop())

	info, err := verifier.GetNFTInfo(context.Background(), "0x1234", "42")

	require.NoError(t, err)
	assert.Equal(t, "0x1234", info.ContractAddress)
	assert.Equal(t, "42", info.TokenID)
}

func BenchmarkNFTVerifier_VerifyNFTOwnership(b *testing.B) {
	ownerAddr := common.HexToAddress("0x742d35Cc6634C0532925a3b844Bc9e7595f2bD18")
	mock := &mockEthCaller{
		callContractFn: func(ctx context.Context, call ethereum.CallMsg, blockNumber *big.Int) ([]byte, error) {
			return ownerAddr.Bytes(), nil
		},
	}
	verifier := NewNFTVerifier(mock, zap.NewNop())
	contractAddr := "0xCcCCccccCCCCcCCCCCCcCcCccCcCCCcCcccccccC"
	owner := "0x742d35Cc6634C0532925a3b844Bc9e7595f2bD18"
	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_, _ = verifier.VerifyNFTOwnership(context.Background(), contractAddr, "1", owner)
	}
}

func BenchmarkNFTVerifier_GetNFTBalance(b *testing.B) {
	mock := &mockEthCaller{
		callContractFn: func(ctx context.Context, call ethereum.CallMsg, blockNumber *big.Int) ([]byte, error) {
			return big.NewInt(5).FillBytes(make([]byte, 32)), nil
		},
	}
	verifier := NewNFTVerifier(mock, zap.NewNop())
	contractAddr := "0xCcCCccccCCCCcCCCCCCcCcCccCcCCCcCcccccccC"
	owner := "0x742d35Cc6634C0532925a3b844Bc9e7595f2bD18"
	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_, _ = verifier.GetNFTBalance(context.Background(), contractAddr, owner)
	}
}

func BenchmarkNFTVerifier_DetectTokenStandard(b *testing.B) {
	mock := &mockEthCaller{
		callContractFn: func(ctx context.Context, call ethereum.CallMsg, blockNumber *big.Int) ([]byte, error) {
			return make([]byte, 32), nil
		},
		codeAtFn: func(ctx context.Context, contract common.Address, blockNumber *big.Int) ([]byte, error) {
			return make([]byte, 100), nil
		},
	}
	contractAddr := "0xCcCCccccCCCCcCCCCCCcCcCccCcCCCcCcccccccC"
	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		DetectTokenStandard(context.Background(), mock, contractAddr, zap.NewNop())
	}
}
