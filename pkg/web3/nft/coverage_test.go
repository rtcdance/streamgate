package nft

import (
	"context"
	"fmt"
	"math/big"
	"sync"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

type mockBlockTagCaller struct {
	mockEthCaller
	callAtBlockFn func(ctx context.Context, msg ethereum.CallMsg, blockTag BlockTag) ([]byte, error)
}

func (m *mockBlockTagCaller) CallContractAtBlock(ctx context.Context, msg ethereum.CallMsg, blockTag BlockTag) ([]byte, error) {
	if m.callAtBlockFn != nil {
		return m.callAtBlockFn(ctx, msg, blockTag)
	}
	return nil, fmt.Errorf("not configured")
}

func TestNFTVerifier_VerifyNFTOwnershipAutoDetect_ERC721(t *testing.T) {
	ownerAddr := common.HexToAddress("0x742d35Cc6634C0532925a3b844Bc9e7595f2bD18")
	contractAddr := common.HexToAddress("0x1234567890123456789012345678901234567890")

	mock := &mockEthCaller{
		callContractFn: func(ctx context.Context, call ethereum.CallMsg, blockNumber *big.Int) ([]byte, error) {
			selector := common.Bytes2Hex(call.Data[:4])
			switch selector {
			case "01ffc9a7":
				interfaceID := common.Bytes2Hex(call.Data[4:8])
				switch interfaceID {
				case "01ffc9a7": // ERC-165
					return encodeBool(true), nil
				case "80ac58cd": // ERC-721
					return encodeBool(true), nil
				default: // ERC-1155 etc
					return encodeBool(false), nil
				}
			case "80ac58cd":
				return encodeBool(true), nil
			case "6352211e":
				return common.LeftPadBytes(ownerAddr.Bytes(), 32), nil
			default:
				return nil, fmt.Errorf("unsupported")
			}
		},
	}

	verifier := NewNFTVerifier(mock, zap.NewNop())
	isOwner, err := verifier.VerifyNFTOwnershipAutoDetect(context.Background(), contractAddr.Hex(), "1", ownerAddr.Hex())
	require.NoError(t, err)
	assert.True(t, isOwner)
}

func TestNFTVerifier_VerifyNFTOwnershipAutoDetect_ERC1155(t *testing.T) {
	ownerAddr := common.HexToAddress("0x742d35Cc6634C0532925a3b844Bc9e7595f2bD18")
	contractAddr := common.HexToAddress("0x1234567890123456789012345678901234567890")

	mock := &mockEthCaller{
		callContractFn: func(ctx context.Context, call ethereum.CallMsg, blockNumber *big.Int) ([]byte, error) {
			selector := common.Bytes2Hex(call.Data[:4])
			switch selector {
			case "01ffc9a7":
				return encodeBool(true), nil
			case "d9b67a26":
				return encodeBool(true), nil
			case "00fdd58e":
				return common.LeftPadBytes(big.NewInt(5).Bytes(), 32), nil
			default:
				return nil, fmt.Errorf("unsupported")
			}
		},
	}

	verifier := NewNFTVerifier(mock, zap.NewNop())
	owned, err := verifier.VerifyNFTOwnershipAutoDetect(context.Background(), contractAddr.Hex(), "1", ownerAddr.Hex())
	require.NoError(t, err)
	assert.True(t, owned)
}

func TestNFTVerifier_GetNFTBalanceAutoDetect_ERC721(t *testing.T) {
	contractAddr := common.HexToAddress("0x1234567890123456789012345678901234567890")
	ownerAddr := common.HexToAddress("0x742d35Cc6634C0532925a3b844Bc9e7595f2bD18")

	mock := &mockEthCaller{
		callContractFn: func(ctx context.Context, call ethereum.CallMsg, blockNumber *big.Int) ([]byte, error) {
			selector := common.Bytes2Hex(call.Data[:4])
			switch selector {
			case "01ffc9a7":
				interfaceID := common.Bytes2Hex(call.Data[4:8])
				switch interfaceID {
				case "01ffc9a7": // ERC-165
					return encodeBool(true), nil
				case "80ac58cd": // ERC-721
					return encodeBool(true), nil
				default: // ERC-1155 etc
					return encodeBool(false), nil
				}
			case "80ac58cd":
				return encodeBool(true), nil
			case "70a08231":
				return common.LeftPadBytes(big.NewInt(3).Bytes(), 32), nil
			default:
				return nil, fmt.Errorf("unsupported")
			}
		},
	}

	verifier := NewNFTVerifier(mock, zap.NewNop())
	balance, err := verifier.GetNFTBalanceAutoDetect(context.Background(), contractAddr.Hex(), ownerAddr.Hex())
	require.NoError(t, err)
	assert.Equal(t, 0, big.NewInt(3).Cmp(balance))
}

func TestNFTVerifier_GetNFTBalanceAutoDetect_ERC1155_WithTokenID(t *testing.T) {
	contractAddr := common.HexToAddress("0x1234567890123456789012345678901234567890")
	ownerAddr := common.HexToAddress("0x742d35Cc6634C0532925a3b844Bc9e7595f2bD18")

	mock := &mockEthCaller{
		callContractFn: func(ctx context.Context, call ethereum.CallMsg, blockNumber *big.Int) ([]byte, error) {
			selector := common.Bytes2Hex(call.Data[:4])
			switch selector {
			case "01ffc9a7":
				return encodeBool(true), nil
			case "d9b67a26":
				return encodeBool(true), nil
			case "00fdd58e":
				return common.LeftPadBytes(big.NewInt(10).Bytes(), 32), nil
			default:
				return nil, fmt.Errorf("unsupported")
			}
		},
	}

	verifier := NewNFTVerifier(mock, zap.NewNop())
	balance, err := verifier.GetNFTBalanceAutoDetect(context.Background(), contractAddr.Hex(), ownerAddr.Hex(), "1")
	require.NoError(t, err)
	assert.Equal(t, 0, big.NewInt(10).Cmp(balance))
}

func TestNFTVerifier_GetNFTBalanceAutoDetect_ERC1155_EmptyTokenID(t *testing.T) {
	contractAddr := common.HexToAddress("0x1234567890123456789012345678901234567890")
	ownerAddr := common.HexToAddress("0x742d35Cc6634C0532925a3b844Bc9e7595f2bD18")

	mock := &mockEthCaller{
		callContractFn: func(ctx context.Context, call ethereum.CallMsg, blockNumber *big.Int) ([]byte, error) {
			selector := common.Bytes2Hex(call.Data[:4])
			switch selector {
			case "01ffc9a7":
				return encodeBool(true), nil
			case "d9b67a26":
				return encodeBool(true), nil
			default:
				return nil, fmt.Errorf("unsupported")
			}
		},
	}

	verifier := NewNFTVerifier(mock, zap.NewNop())
	_, err := verifier.GetNFTBalanceAutoDetect(context.Background(), contractAddr.Hex(), ownerAddr.Hex(), "")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "requires a tokenID parameter")
}

func TestNFTVerifier_VerifyNFTCollectionAutoDetect_ERC721(t *testing.T) {
	contractAddr := common.HexToAddress("0x1234567890123456789012345678901234567890")
	ownerAddr := common.HexToAddress("0x742d35Cc6634C0532925a3b844Bc9e7595f2bD18")

	mock := &mockEthCaller{
		callContractFn: func(ctx context.Context, call ethereum.CallMsg, blockNumber *big.Int) ([]byte, error) {
			selector := common.Bytes2Hex(call.Data[:4])
			switch selector {
			case "01ffc9a7":
				interfaceID := common.Bytes2Hex(call.Data[4:8])
				switch interfaceID {
				case "01ffc9a7": // ERC-165
					return encodeBool(true), nil
				case "80ac58cd": // ERC-721
					return encodeBool(true), nil
				default: // ERC-1155 etc
					return encodeBool(false), nil
				}
			case "80ac58cd":
				return encodeBool(true), nil
			case "70a08231":
				return common.LeftPadBytes(big.NewInt(2).Bytes(), 32), nil
			default:
				return nil, fmt.Errorf("unsupported")
			}
		},
	}

	verifier := NewNFTVerifier(mock, zap.NewNop())
	hasNFT, err := verifier.VerifyNFTCollectionAutoDetect(context.Background(), contractAddr.Hex(), ownerAddr.Hex())
	require.NoError(t, err)
	assert.True(t, hasNFT)
}

func TestNFTVerifier_CheckApprovalAutoDetect_ERC721(t *testing.T) {
	contractAddr := "0x1234567890123456789012345678901234567890"
	ownerAddr := "0x742d35Cc6634C0532925a3b844Bc9e7595f2bD18"

	mock := &mockEthCaller{
		callContractFn: func(ctx context.Context, call ethereum.CallMsg, blockNumber *big.Int) ([]byte, error) {
			selector := common.Bytes2Hex(call.Data[:4])
			switch selector {
			case "01ffc9a7":
				interfaceID := common.Bytes2Hex(call.Data[4:8])
				switch interfaceID {
				case "01ffc9a7": // ERC-165
					return encodeBool(true), nil
				case "80ac58cd": // ERC-721
					return encodeBool(true), nil
				default: // ERC-1155 etc
					return encodeBool(false), nil
				}
			case "80ac58cd":
				return encodeBool(true), nil
			case "081812fc":
				return make([]byte, 32), nil
			case "e985e9c5":
				return encodeBool(false), nil
			default:
				return nil, fmt.Errorf("unsupported")
			}
		},
	}

	verifier := NewNFTVerifier(mock, zap.NewNop())
	info, err := verifier.CheckApprovalAutoDetect(context.Background(), contractAddr, "1", ownerAddr)
	require.NoError(t, err)
	assert.Empty(t, info.ApprovedAddress)
	assert.Empty(t, info.ApprovedOperator)
}

func TestNFTVerifier_CheckApprovalAutoDetect_ERC1155(t *testing.T) {
	contractAddr := "0x1234567890123456789012345678901234567890"
	ownerAddr := "0x742d35Cc6634C0532925a3b844Bc9e7595f2bD18"

	mock := &mockEthCaller{
		callContractFn: func(ctx context.Context, call ethereum.CallMsg, blockNumber *big.Int) ([]byte, error) {
			selector := common.Bytes2Hex(call.Data[:4])
			switch selector {
			case "01ffc9a7":
				interfaceID := common.Bytes2Hex(call.Data[4:8])
				switch interfaceID {
				case "01ffc9a7", "d9b67a26": // ERC-165 or ERC-1155
					return encodeBool(true), nil
				default:
					return encodeBool(false), nil
				}
			case "d9b67a26":
				return encodeBool(true), nil
			case "e985e9c5":
				return encodeBool(false), nil
			default:
				return nil, fmt.Errorf("unsupported")
			}
		},
	}

	verifier := NewNFTVerifier(mock, zap.NewNop())
	info, err := verifier.CheckApprovalAutoDetect(context.Background(), contractAddr, "1", ownerAddr)
	require.NoError(t, err)
	assert.Empty(t, info.ApprovedOperator)
}

func TestNFTVerifier_GetNFTInfo_WithData(t *testing.T) {
	contractAddr := common.HexToAddress("0x1234567890123456789012345678901234567890")
	ownerAddr := common.HexToAddress("0x742d35Cc6634C0532925a3b844Bc9e7595f2bD18")

	mock := &mockEthCaller{
		callContractFn: func(ctx context.Context, call ethereum.CallMsg, blockNumber *big.Int) ([]byte, error) {
			selector := common.Bytes2Hex(call.Data[:4])
			switch selector {
			case "06fdde03":
				return encodeString("TestNFT"), nil
			case "95d89b41":
				return encodeString("TNFT"), nil
			case "c87b56dd":
				return encodeString("https://example.com/token/1.json"), nil
			case "6352211e":
				return common.LeftPadBytes(ownerAddr.Bytes(), 32), nil
			default:
				return nil, fmt.Errorf("unsupported method")
			}
		},
	}

	verifier := NewNFTVerifier(mock, zap.NewNop())
	info, err := verifier.GetNFTInfo(context.Background(), contractAddr.Hex(), "1")
	require.NoError(t, err)
	assert.Equal(t, "TestNFT", info.Name)
	assert.Equal(t, "TNFT", info.Symbol)
	assert.Equal(t, "https://example.com/token/1.json", info.URI)
	assert.Equal(t, ownerAddr.Hex(), info.Owner)
}

func TestNFTVerifier_GetNFTInfo_WithWarnings(t *testing.T) {
	contractAddr := common.HexToAddress("0x1234567890123456789012345678901234567890")

	mock := &mockEthCaller{
		callContractFn: func(ctx context.Context, call ethereum.CallMsg, blockNumber *big.Int) ([]byte, error) {
			return nil, fmt.Errorf("RPC error")
		},
	}

	verifier := NewNFTVerifier(mock, zap.NewNop())
	info, err := verifier.GetNFTInfo(context.Background(), contractAddr.Hex(), "1")
	require.NoError(t, err)
	assert.Empty(t, info.Name)
	assert.Empty(t, info.Symbol)
	assert.NotEmpty(t, info.Warnings)
}

func TestNFTVerifier_GetNFTInfo_InvalidTokenID(t *testing.T) {
	mock := &mockEthCaller{}
	verifier := NewNFTVerifier(mock, zap.NewNop())
	_, err := verifier.GetNFTInfo(context.Background(), "0x1234567890123456789012345678901234567890", "notanumber")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid token ID")
}

func TestNFTVerifier_CallContract_WithBlockTagCaller(t *testing.T) {
	called := false
	mock := &mockBlockTagCaller{
		callAtBlockFn: func(ctx context.Context, msg ethereum.CallMsg, blockTag BlockTag) ([]byte, error) {
			called = true
			assert.Equal(t, BlockTagSafe, blockTag)
			return common.LeftPadBytes(big.NewInt(1).Bytes(), 32), nil
		},
	}

	verifier := NewNFTVerifier(mock, zap.NewNop()).WithBlockTag(BlockTagSafe)
	_, err := verifier.GetNFTBalance(context.Background(), "0x1234567890123456789012345678901234567890", "0x742d35Cc6634C0532925a3b844Bc9e7595f2bD18")
	require.NoError(t, err)
	assert.True(t, called)
}

func TestNFTVerifier_CallContract_BlockTagLatest(t *testing.T) {
	mock := &mockEthCaller{
		callContractFn: func(ctx context.Context, call ethereum.CallMsg, blockNumber *big.Int) ([]byte, error) {
			return common.LeftPadBytes(big.NewInt(1).Bytes(), 32), nil
		},
	}

	verifier := NewNFTVerifier(mock, zap.NewNop()).WithBlockTag(BlockTagLatest)
	_, err := verifier.GetNFTBalance(context.Background(), "0x1234567890123456789012345678901234567890", "0x742d35Cc6634C0532925a3b844Bc9e7595f2bD18")
	require.NoError(t, err)
}

func TestNFTVerifier_CallContract_BlockTagWithoutBlockTagCaller(t *testing.T) {
	mock := &mockEthCaller{
		callContractFn: func(ctx context.Context, call ethereum.CallMsg, blockNumber *big.Int) ([]byte, error) {
			return common.LeftPadBytes(big.NewInt(1).Bytes(), 32), nil
		},
	}

	verifier := NewNFTVerifier(mock, zap.NewNop()).WithBlockTag(BlockTagSafe)
	_, err := verifier.GetNFTBalance(context.Background(), "0x1234567890123456789012345678901234567890", "0x742d35Cc6634C0532925a3b844Bc9e7595f2bD18")
	require.NoError(t, err)
}

func TestNFTVerifier_DetectTokenStandardCached_CacheHit(t *testing.T) {
	contractAddr := "0x1234567890123456789012345678901234567890"
	detectCount := 0

	mock := &mockEthCaller{
		callContractFn: func(ctx context.Context, call ethereum.CallMsg, blockNumber *big.Int) ([]byte, error) {
			selector := common.Bytes2Hex(call.Data[:4])
			switch selector {
			case "01ffc9a7":
				detectCount++
				if len(call.Data) >= 36 {
					ifaceID := common.Bytes2Hex(call.Data[4:36])
					switch ifaceID {
					case "01ffc9a700000000000000000000000000000000000000000000000000000000":
						return encodeBool(true), nil
					case "80ac58cd00000000000000000000000000000000000000000000000000000000":
						return encodeBool(true), nil
					default:
						return encodeBool(false), nil
					}
				}
				return encodeBool(true), nil
			case "6352211e":
				return common.LeftPadBytes(common.HexToAddress("0x742d35Cc6634C0532925a3b844Bc9e7595f2bD18").Bytes(), 32), nil
			default:
				return nil, fmt.Errorf("unsupported")
			}
		},
	}

	verifier := NewNFTVerifier(mock, zap.NewNop())

	_, err := verifier.VerifyNFTOwnershipAutoDetect(context.Background(), contractAddr, "1", "0x742d35Cc6634C0532925a3b844Bc9e7595f2bD18")
	require.NoError(t, err)
	firstDetectCount := detectCount

	_, err = verifier.VerifyNFTOwnershipAutoDetect(context.Background(), contractAddr, "1", "0x742d35Cc6634C0532925a3b844Bc9e7595f2bD18")
	require.NoError(t, err)

	assert.Equal(t, firstDetectCount, detectCount, "second call should use cached standard, not re-detect")
}

func TestNFTVerifier_DetectTokenStandardCached_CacheExpiry(t *testing.T) {
	contractAddr := "0x1234567890123456789012345678901234567890"
	callCount := 0

	mock := &mockEthCaller{
		callContractFn: func(ctx context.Context, call ethereum.CallMsg, blockNumber *big.Int) ([]byte, error) {
			callCount++
			selector := common.Bytes2Hex(call.Data[:4])
			switch selector {
			case "01ffc9a7":
				if len(call.Data) >= 36 {
					ifaceID := common.Bytes2Hex(call.Data[4:36])
					switch ifaceID {
					case "01ffc9a700000000000000000000000000000000000000000000000000000000":
						return encodeBool(true), nil
					case "80ac58cd00000000000000000000000000000000000000000000000000000000":
						return encodeBool(true), nil
					default:
						return encodeBool(false), nil
					}
				}
				return encodeBool(true), nil
			case "6352211e":
				return common.LeftPadBytes(common.HexToAddress("0x742d35Cc6634C0532925a3b844Bc9e7595f2bD18").Bytes(), 32), nil
			default:
				return nil, fmt.Errorf("unsupported")
			}
		},
	}

	verifier := NewNFTVerifier(mock, zap.NewNop())

	verifier.standardCache.Store(contractAddr, &tokenStandardEntry{
		standard: TokenStandardERC721,
		cachedAt: time.Now().Add(-2 * time.Hour),
	})

	_, err := verifier.VerifyNFTOwnershipAutoDetect(context.Background(), contractAddr, "1", "0x742d35Cc6634C0532925a3b844Bc9e7595f2bD18")
	require.NoError(t, err)
	assert.Greater(t, callCount, 0, "expired cache should trigger re-detection")
}

func TestNFTVerifier_DetectTokenStandardCached_UnknownNotCached(t *testing.T) {
	contractAddr := "0x1234567890123456789012345678901234567890"

	mock := &mockEthCaller{
		callContractFn: func(ctx context.Context, call ethereum.CallMsg, blockNumber *big.Int) ([]byte, error) {
			return nil, fmt.Errorf("RPC error")
		},
	}

	verifier := NewNFTVerifier(mock, zap.NewNop())

	_, err := verifier.VerifyNFTOwnershipAutoDetect(context.Background(), contractAddr, "1", "0x742d35Cc6634C0532925a3b844Bc9e7595f2bD18")
	require.Error(t, err)

	_, cached := verifier.standardCache.Load(contractAddr)
	assert.False(t, cached, "unknown standard should not be cached")
}

func TestNFTVerifier_CheckApproval_WithApprovedOperator(t *testing.T) {
	contractAddr := "0x1234567890123456789012345678901234567890"
	ownerAddr := "0x742d35Cc6634C0532925a3b844Bc9e7595f2bD18"

	mock := &mockEthCaller{
		callContractFn: func(ctx context.Context, call ethereum.CallMsg, blockNumber *big.Int) ([]byte, error) {
			selector := common.Bytes2Hex(call.Data[:4])
			switch selector {
			case "081812fc":
				return make([]byte, 32), nil
			case "e985e9c5":
				return encodeBool(true), nil
			default:
				return nil, fmt.Errorf("unsupported")
			}
		},
	}

	verifier := NewNFTVerifier(mock, zap.NewNop())
	info, err := verifier.CheckApproval(context.Background(), contractAddr, "1", ownerAddr)
	require.NoError(t, err)
	assert.NotEmpty(t, info.ApprovedOperator)
}

func TestNFTVerifier_CheckApproval_EmptyKnownOperators(t *testing.T) {
	contractAddr := "0x1234567890123456789012345678901234567890"
	ownerAddr := "0x742d35Cc6634C0532925a3b844Bc9e7595f2bD18"

	mock := &mockEthCaller{
		callContractFn: func(ctx context.Context, call ethereum.CallMsg, blockNumber *big.Int) ([]byte, error) {
			selector := common.Bytes2Hex(call.Data[:4])
			switch selector {
			case "081812fc":
				return make([]byte, 32), nil
			case "e985e9c5":
				return encodeBool(false), nil
			default:
				return nil, fmt.Errorf("unsupported")
			}
		},
	}

	verifier := NewNFTVerifier(mock, zap.NewNop())
	verifier.KnownOperators = nil

	info, err := verifier.CheckApproval(context.Background(), contractAddr, "1", ownerAddr)
	require.NoError(t, err)
	assert.Empty(t, info.ApprovedOperator)
}

func TestNFTVerifier_CheckApproval_InsufficientResultData(t *testing.T) {
	contractAddr := "0x1234567890123456789012345678901234567890"
	ownerAddr := "0x742d35Cc6634C0532925a3b844Bc9e7595f2bD18"

	mock := &mockEthCaller{
		callContractFn: func(ctx context.Context, call ethereum.CallMsg, blockNumber *big.Int) ([]byte, error) {
			selector := common.Bytes2Hex(call.Data[:4])
			switch selector {
			case "081812fc":
				return []byte{0x01, 0x02}, nil
			case "e985e9c5":
				return []byte{0x01, 0x02}, nil
			default:
				return nil, fmt.Errorf("unsupported")
			}
		},
	}

	verifier := NewNFTVerifier(mock, zap.NewNop())
	info, err := verifier.CheckApproval(context.Background(), contractAddr, "1", ownerAddr)
	require.NoError(t, err)
	assert.Empty(t, info.ApprovedAddress)
	assert.Empty(t, info.ApprovedOperator)
}

func TestNFTVerifier_CheckERC1155Approval_InvalidOperator(t *testing.T) {
	contractAddr := "0x1234567890123456789012345678901234567890"
	ownerAddr := "0x742d35Cc6634C0532925a3b844Bc9e7595f2bD18"

	mock := &mockEthCaller{
		callContractFn: func(ctx context.Context, call ethereum.CallMsg, blockNumber *big.Int) ([]byte, error) {
			return encodeBool(false), nil
		},
	}

	verifier := NewNFTVerifier(mock, zap.NewNop())
	verifier.KnownOperators = []string{"not-a-valid-address"}

	info, err := verifier.CheckERC1155Approval(context.Background(), contractAddr, ownerAddr)
	require.NoError(t, err)
	assert.Empty(t, info.ApprovedOperator)
}

func TestNFTVerifier_CheckERC1155Approval_EmptyKnownOperators(t *testing.T) {
	contractAddr := "0x1234567890123456789012345678901234567890"
	ownerAddr := "0x742d35Cc6634C0532925a3b844Bc9e7595f2bD18"

	mock := &mockEthCaller{
		callContractFn: func(ctx context.Context, call ethereum.CallMsg, blockNumber *big.Int) ([]byte, error) {
			return encodeBool(false), nil
		},
	}

	verifier := NewNFTVerifier(mock, zap.NewNop())
	verifier.KnownOperators = nil

	info, err := verifier.CheckERC1155Approval(context.Background(), contractAddr, ownerAddr)
	require.NoError(t, err)
	assert.Empty(t, info.ApprovedOperator)
}

func TestNFTVerifier_VerifyNFTOwnership_WrongOwner(t *testing.T) {
	otherAddr := common.HexToAddress("0x0000000000000000000000000000000000000001")
	mock := &mockEthCaller{
		callContractFn: func(ctx context.Context, call ethereum.CallMsg, blockNumber *big.Int) ([]byte, error) {
			return common.LeftPadBytes(otherAddr.Bytes(), 32), nil
		},
	}

	verifier := NewNFTVerifier(mock, zap.NewNop())
	owned, err := verifier.VerifyNFTOwnership(context.Background(), "0x1234567890123456789012345678901234567890", "1", "0x742d35Cc6634C0532925a3b844Bc9e7595f2bD18")
	require.NoError(t, err)
	assert.False(t, owned)
}

func TestNFTVerifier_GetNFTBalance_InsufficientData(t *testing.T) {
	mock := &mockEthCaller{
		callContractFn: func(ctx context.Context, call ethereum.CallMsg, blockNumber *big.Int) ([]byte, error) {
			return []byte{0x01}, nil
		},
	}

	verifier := NewNFTVerifier(mock, zap.NewNop())
	_, err := verifier.GetNFTBalance(context.Background(), "0x1234567890123456789012345678901234567890", "0x742d35Cc6634C0532925a3b844Bc9e7595f2bD18")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "insufficient data")
}

func TestNFTVerifier_VerifyERC1155Ownership_ZeroBalance(t *testing.T) {
	mock := &mockEthCaller{
		callContractFn: func(ctx context.Context, call ethereum.CallMsg, blockNumber *big.Int) ([]byte, error) {
			return common.LeftPadBytes(big.NewInt(0).Bytes(), 32), nil
		},
	}

	verifier := NewNFTVerifier(mock, zap.NewNop())
	owned, err := verifier.VerifyERC1155Ownership(context.Background(), "0x1234567890123456789012345678901234567890", "1", "0x742d35Cc6634C0532925a3b844Bc9e7595f2bD18")
	require.NoError(t, err)
	assert.False(t, owned)
}

func TestNFTVerifier_GetERC1155Balance_CallError(t *testing.T) {
	mock := &mockEthCaller{
		callContractFn: func(ctx context.Context, call ethereum.CallMsg, blockNumber *big.Int) ([]byte, error) {
			return nil, fmt.Errorf("RPC error")
		},
	}

	verifier := NewNFTVerifier(mock, zap.NewNop())
	_, err := verifier.GetERC1155Balance(context.Background(), "0x1234567890123456789012345678901234567890", "0x742d35Cc6634C0532925a3b844Bc9e7595f2bD18", "1")
	assert.Error(t, err)
}

func TestNFTVerifier_GetERC1155Balance_InsufficientData(t *testing.T) {
	mock := &mockEthCaller{
		callContractFn: func(ctx context.Context, call ethereum.CallMsg, blockNumber *big.Int) ([]byte, error) {
			return []byte{0x01}, nil
		},
	}

	verifier := NewNFTVerifier(mock, zap.NewNop())
	_, err := verifier.GetERC1155Balance(context.Background(), "0x1234567890123456789012345678901234567890", "0x742d35Cc6634C0532925a3b844Bc9e7595f2bD18", "1")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "insufficient data")
}

func TestNFTVerifier_DefaultKnownOperators(t *testing.T) {
	assert.Len(t, DefaultKnownOperators, 5)
	for _, op := range DefaultKnownOperators {
		assert.True(t, common.IsHexAddress(op))
	}
}

func TestNFTVerifier_StandardCache_Concurrent(t *testing.T) {
	mock := &mockEthCaller{
		callContractFn: func(ctx context.Context, call ethereum.CallMsg, blockNumber *big.Int) ([]byte, error) {
			selector := common.Bytes2Hex(call.Data[:4])
			switch selector {
			case "01ffc9a7":
				return encodeBool(true), nil
			case "80ac58cd":
				return encodeBool(true), nil
			default:
				return nil, fmt.Errorf("unsupported")
			}
		},
	}

	verifier := NewNFTVerifier(mock, zap.NewNop())

	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _ = verifier.VerifyNFTOwnershipAutoDetect(
				context.Background(),
				"0x1234567890123456789012345678901234567890",
				"1",
				"0x742d35Cc6634C0532925a3b844Bc9e7595f2bD18",
			)
		}()
	}
	wg.Wait()
}

func TestERC1155Verifier_VerifyNFTOwnership_WithCache(t *testing.T) {
	mock := &erc1155MockCaller{
		callContractFn: func(ctx context.Context, call ethereum.CallMsg, blockNumber *big.Int) ([]byte, error) {
			return encodeUint256(big.NewInt(3)), nil
		},
	}

	cache := &mockCacheBackend{data: make(map[string]interface{})}
	verifier := NewERC1155Verifier(mock, zap.NewNop(), cache)

	owned, err := verifier.VerifyNFTOwnership(
		context.Background(),
		"0x1234567890123456789012345678901234567890",
		"1",
		"0x742d35Cc6634C0532925a3b844Bc9e7595f2bD18",
	)
	require.NoError(t, err)
	assert.True(t, owned)

	_, cached := cache.data["erc1155:balance:0x1234567890123456789012345678901234567890:1:0x742d35Cc6634C0532925a3b844Bc9e7595f2bD18"]
	assert.True(t, cached)
}

func TestERC1155Verifier_VerifyNFTOwnership_CacheHit(t *testing.T) {
	callCount := 0
	mock := &erc1155MockCaller{
		callContractFn: func(ctx context.Context, call ethereum.CallMsg, blockNumber *big.Int) ([]byte, error) {
			callCount++
			return encodeUint256(big.NewInt(5)), nil
		},
	}

	cache := &mockCacheBackend{data: make(map[string]interface{})}
	cacheKey := "erc1155:balance:0x1234567890123456789012345678901234567890:1:0x742d35Cc6634C0532925a3b844Bc9e7595f2bD18"
	cache.data[cacheKey] = big.NewInt(7)

	verifier := NewERC1155Verifier(mock, zap.NewNop(), cache)

	owned, err := verifier.VerifyNFTOwnership(
		context.Background(),
		"0x1234567890123456789012345678901234567890",
		"1",
		"0x742d35Cc6634C0532925a3b844Bc9e7595f2bD18",
	)
	require.NoError(t, err)
	assert.True(t, owned)
	assert.Equal(t, 0, callCount, "should use cache, not call RPC")
}

func TestERC1155Verifier_VerifyNFTOwnership_CacheZeroBalance(t *testing.T) {
	cache := &mockCacheBackend{data: make(map[string]interface{})}
	cacheKey := "erc1155:balance:0x1234567890123456789012345678901234567890:1:0x742d35Cc6634C0532925a3b844Bc9e7595f2bD18"
	cache.data[cacheKey] = big.NewInt(0)

	verifier := NewERC1155Verifier(nil, zap.NewNop(), cache)

	owned, err := verifier.VerifyNFTOwnership(
		context.Background(),
		"0x1234567890123456789012345678901234567890",
		"1",
		"0x742d35Cc6634C0532925a3b844Bc9e7595f2bD18",
	)
	require.NoError(t, err)
	assert.False(t, owned)
}

func TestERC1155Verifier_VerifyBatchNFTOwnership_InvalidOwner(t *testing.T) {
	verifier := NewERC1155Verifier(nil, zap.NewNop(), nil)
	_, err := verifier.VerifyBatchNFTOwnership(context.Background(), "0x1234567890123456789012345678901234567890", []string{"1"}, "notanaddress")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "invalid owner address")
}

func TestERC1155Verifier_VerifyBatchNFTOwnership_CallError(t *testing.T) {
	mock := &erc1155MockCaller{
		callContractFn: func(ctx context.Context, call ethereum.CallMsg, blockNumber *big.Int) ([]byte, error) {
			return nil, fmt.Errorf("RPC error")
		},
	}
	verifier := NewERC1155Verifier(mock, zap.NewNop(), nil)
	_, err := verifier.VerifyBatchNFTOwnership(
		context.Background(),
		"0x1234567890123456789012345678901234567890",
		[]string{"1"},
		"0x742d35Cc6634C0532925a3b844Bc9e7595f2bD18",
	)
	assert.Error(t, err)
}

func TestERC1155Verifier_VerifyBatchNFTOwnership_InsufficientData(t *testing.T) {
	mock := &erc1155MockCaller{
		callContractFn: func(ctx context.Context, call ethereum.CallMsg, blockNumber *big.Int) ([]byte, error) {
			return []byte{0x01}, nil
		},
	}
	verifier := NewERC1155Verifier(mock, zap.NewNop(), nil)
	_, err := verifier.VerifyBatchNFTOwnership(
		context.Background(),
		"0x1234567890123456789012345678901234567890",
		[]string{"1"},
		"0x742d35Cc6634C0532925a3b844Bc9e7595f2bD18",
	)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "insufficient data")
}

func TestERC1155Verifier_VerifyURI_InvalidTokenID(t *testing.T) {
	verifier := NewERC1155Verifier(nil, zap.NewNop(), nil)
	_, err := verifier.VerifyURI(context.Background(), "0x1234567890123456789012345678901234567890", "abc", "https://example.com")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "invalid token ID")
}

func TestERC1155Verifier_VerifyURI_CallError(t *testing.T) {
	mock := &erc1155MockCaller{
		callContractFn: func(ctx context.Context, call ethereum.CallMsg, blockNumber *big.Int) ([]byte, error) {
			return nil, fmt.Errorf("RPC error")
		},
	}
	verifier := NewERC1155Verifier(mock, zap.NewNop(), nil)
	_, err := verifier.VerifyURI(context.Background(), "0x1234567890123456789012345678901234567890", "1", "https://example.com")
	assert.Error(t, err)
}

func TestERC1155Verifier_VerifyURI_InsufficientData(t *testing.T) {
	mock := &erc1155MockCaller{
		callContractFn: func(ctx context.Context, call ethereum.CallMsg, blockNumber *big.Int) ([]byte, error) {
			return []byte{0x01}, nil
		},
	}
	verifier := NewERC1155Verifier(mock, zap.NewNop(), nil)
	_, err := verifier.VerifyURI(context.Background(), "0x1234567890123456789012345678901234567890", "1", "https://example.com")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "insufficient data")
}

func TestERC1155Verifier_VerifyTotalSupply_InvalidContract(t *testing.T) {
	verifier := NewERC1155Verifier(nil, zap.NewNop(), nil)
	_, err := verifier.VerifyTotalSupply(context.Background(), "bad", "1", big.NewInt(100))
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "invalid contract address")
}

func TestERC1155Verifier_VerifyTotalSupply_InvalidTokenID(t *testing.T) {
	verifier := NewERC1155Verifier(nil, zap.NewNop(), nil)
	_, err := verifier.VerifyTotalSupply(context.Background(), "0x1234567890123456789012345678901234567890", "abc", big.NewInt(100))
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "invalid token ID")
}

func TestERC1155Verifier_VerifyOperatorApproval_InvalidOwner(t *testing.T) {
	verifier := NewERC1155Verifier(nil, zap.NewNop(), nil)
	_, err := verifier.VerifyOperatorApproval(context.Background(), "0x1234567890123456789012345678901234567890", "bad", "0x8ba1f109551bD432803012645Ac136ddd64DBA72")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "invalid owner address")
}

func TestERC1155Verifier_VerifyOperatorApproval_InvalidOperator(t *testing.T) {
	verifier := NewERC1155Verifier(nil, zap.NewNop(), nil)
	_, err := verifier.VerifyOperatorApproval(context.Background(), "0x1234567890123456789012345678901234567890", "0x742d35Cc6634C0532925a3b844Bc9e7595f2bD18", "bad")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "invalid operator address")
}

func TestERC1155Verifier_VerifyOperatorApproval_CallError(t *testing.T) {
	mock := &erc1155MockCaller{
		callContractFn: func(ctx context.Context, call ethereum.CallMsg, blockNumber *big.Int) ([]byte, error) {
			return nil, fmt.Errorf("RPC error")
		},
	}
	verifier := NewERC1155Verifier(mock, zap.NewNop(), nil)
	_, err := verifier.VerifyOperatorApproval(
		context.Background(),
		"0x1234567890123456789012345678901234567890",
		"0x742d35Cc6634C0532925a3b844Bc9e7595f2bD18",
		"0x8ba1f109551bD432803012645Ac136ddd64DBA72",
	)
	assert.Error(t, err)
}

func TestERC1155Verifier_VerifyOperatorApproval_InsufficientData(t *testing.T) {
	mock := &erc1155MockCaller{
		callContractFn: func(ctx context.Context, call ethereum.CallMsg, blockNumber *big.Int) ([]byte, error) {
			return []byte{0x01}, nil
		},
	}
	verifier := NewERC1155Verifier(mock, zap.NewNop(), nil)
	_, err := verifier.VerifyOperatorApproval(
		context.Background(),
		"0x1234567890123456789012345678901234567890",
		"0x742d35Cc6634C0532925a3b844Bc9e7595f2bD18",
		"0x8ba1f109551bD432803012645Ac136ddd64DBA72",
	)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "insufficient data")
}

func TestERC1155Verifier_GetTokenInfo_CallError(t *testing.T) {
	mock := &erc1155MockCaller{
		callContractFn: func(ctx context.Context, call ethereum.CallMsg, blockNumber *big.Int) ([]byte, error) {
			return nil, fmt.Errorf("RPC error")
		},
	}
	verifier := NewERC1155Verifier(mock, zap.NewNop(), nil)
	_, err := verifier.GetTokenInfo(context.Background(), "0x1234567890123456789012345678901234567890", "1")
	assert.Error(t, err)
}

func TestERC1155Verifier_GetTokenInfo_InvalidContract(t *testing.T) {
	verifier := NewERC1155Verifier(nil, zap.NewNop(), nil)
	_, err := verifier.GetTokenInfo(context.Background(), "bad", "1")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "invalid contract address")
}

func TestERC1155Verifier_VerifyURI_IDSubstitution(t *testing.T) {
	mock := &erc1155MockCaller{
		callContractFn: func(ctx context.Context, call ethereum.CallMsg, blockNumber *big.Int) ([]byte, error) {
			return encodeString("https://example.com/metadata/{id}.json"), nil
		},
	}
	verifier := NewERC1155Verifier(mock, zap.NewNop(), nil)

	valid, err := verifier.VerifyURI(context.Background(), "0x1234567890123456789012345678901234567890", "1", "https://example.com/metadata/{id}.json")
	require.NoError(t, err)
	assert.False(t, valid)
}

func TestERC20Reader_GetTokenBalance_Success(t *testing.T) {
	balanceSelector := common.Hex2Bytes("70a08231")
	caller := &erc20MockCaller{
		responses: map[string][]byte{
			common.Bytes2Hex(balanceSelector): encodeUint256(big.NewInt(1000)),
		},
	}
	reader := NewERC20Reader(caller, zap.NewNop())

	balance, err := reader.GetTokenBalance(
		context.Background(),
		"0x0000000000000000000000000000000000000001",
		"0x0000000000000000000000000000000000000002",
	)
	require.NoError(t, err)
	assert.Equal(t, 0, big.NewInt(1000).Cmp(balance))
}

func TestERC20Reader_GetTokenInfo_Success(t *testing.T) {
	nameSelector := common.Hex2Bytes("06fdde03")
	symbolSelector := common.Hex2Bytes("95d89b41")
	decimalsSelector := common.Hex2Bytes("313ce567")
	totalSupplySelector := common.Hex2Bytes("18160ddd")

	caller := &erc20MockCaller{
		responses: map[string][]byte{
			common.Bytes2Hex(nameSelector):        encodeString("TestToken"),
			common.Bytes2Hex(symbolSelector):      encodeString("TT"),
			common.Bytes2Hex(decimalsSelector):    encodeUint256(big.NewInt(18)),
			common.Bytes2Hex(totalSupplySelector): encodeUint256(big.NewInt(1000000)),
		},
	}
	reader := NewERC20Reader(caller, zap.NewNop())

	info, err := reader.GetTokenInfo(context.Background(), "0x0000000000000000000000000000000000000001")
	require.NoError(t, err)
	assert.Equal(t, "TestToken", info.Name)
	assert.Equal(t, "TT", info.Symbol)
	assert.Equal(t, uint8(18), info.Decimals)
	assert.Equal(t, 0, big.NewInt(1000000).Cmp(info.TotalSupply))
}

type mockCacheBackend struct {
	mu   sync.RWMutex
	data map[string]interface{}
}

func (m *mockCacheBackend) Get(key string) (interface{}, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	v, ok := m.data[key]
	if !ok {
		return nil, fmt.Errorf("not found")
	}
	return v, nil
}

func (m *mockCacheBackend) Set(key string, value interface{}) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.data[key] = value
	return nil
}

func (m *mockCacheBackend) SetWithExpiration(key string, value interface{}, _ time.Duration) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.data[key] = value
	return nil
}

func (m *mockCacheBackend) Delete(key string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.data, key)
	return nil
}
