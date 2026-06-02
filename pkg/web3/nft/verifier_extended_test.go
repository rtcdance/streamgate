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

func TestDetectTokenStandard_TableDriven(t *testing.T) {
	contractAddr := "0x1234567890123456789012345678901234567890"

	tests := []struct {
		name     string
		callFn   func(ctx context.Context, call ethereum.CallMsg, blockNumber *big.Int) ([]byte, error)
		expected TokenStandard
	}{
		{
			"ERC-721 via ERC-165",
			func() func(ctx context.Context, call ethereum.CallMsg, blockNumber *big.Int) ([]byte, error) {
				callCount := 0
				return func(ctx context.Context, call ethereum.CallMsg, blockNumber *big.Int) ([]byte, error) {
					selector := common.Bytes2Hex(call.Data[:4])
					if selector == "01ffc9a7" {
						callCount++
						switch callCount {
						case 1:
							return encodeBool(true), nil
						case 2:
							return encodeBool(false), nil
						case 3:
							return encodeBool(true), nil
						}
					}
					return nil, fmt.Errorf("unsupported method")
				}
			}(),
			TokenStandardERC721,
		},
		{
			"ERC-1155 via ERC-165",
			func() func(ctx context.Context, call ethereum.CallMsg, blockNumber *big.Int) ([]byte, error) {
				callCount := 0
				return func(ctx context.Context, call ethereum.CallMsg, blockNumber *big.Int) ([]byte, error) {
					selector := common.Bytes2Hex(call.Data[:4])
					if selector == "01ffc9a7" {
						callCount++
						switch callCount {
						case 1:
							return encodeBool(true), nil
						case 2:
							return encodeBool(true), nil
						}
					}
					return nil, fmt.Errorf("unsupported method")
				}
			}(),
			TokenStandardERC1155,
		},
		{
			"ERC-165 says no",
			func(ctx context.Context, call ethereum.CallMsg, blockNumber *big.Int) ([]byte, error) {
				selector := common.Bytes2Hex(call.Data[:4])
				switch selector {
				case "01ffc9a7":
					return encodeBool(false), nil
				case "00fdd58e":
					return make([]byte, 32), nil
				default:
					return nil, fmt.Errorf("unsupported")
				}
			},
			TokenStandardERC1155,
		},
		{
			"unknown - all fail",
			func(ctx context.Context, call ethereum.CallMsg, blockNumber *big.Int) ([]byte, error) {
				return nil, fmt.Errorf("RPC error")
			},
			TokenStandardUnknown,
		},
		{
			"ERC-721 via fallback balanceOf",
			func(ctx context.Context, call ethereum.CallMsg, blockNumber *big.Int) ([]byte, error) {
				selector := common.Bytes2Hex(call.Data[:4])
				switch selector {
				case "01ffc9a7":
					return nil, fmt.Errorf("no ERC-165")
				case "00fdd58e":
					return nil, fmt.Errorf("no 1155")
				case "70a08231":
					return make([]byte, 32), nil
				default:
					return nil, fmt.Errorf("unsupported")
				}
			},
			TokenStandardERC721,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			mock := &mockEthCaller{
				callContractFn: tc.callFn,
			}
			result := DetectTokenStandard(context.Background(), mock, contractAddr, zap.NewNop())
			assert.Equal(t, tc.expected, result)
		})
	}
}

func TestDetectTokenStandard_InvalidAddress(t *testing.T) {
	mock := &mockEthCaller{}
	result := DetectTokenStandard(context.Background(), mock, "not-an-address", zap.NewNop())
	assert.Equal(t, TokenStandardUnknown, result)
}

func TestNFTVerifier_VerifyNFTOwnership_InvalidTokenID(t *testing.T) {
	mock := &mockEthCaller{}
	verifier := NewNFTVerifier(mock, zap.NewNop())
	_, err := verifier.VerifyNFTOwnership(context.Background(), "0x1234567890123456789012345678901234567890", "notanumber", "0x742d35Cc6634C0532925a3b844Bc9e7595f2bD18")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid token ID")
}

func TestNFTVerifier_VerifyNFTOwnership_InsufficientData(t *testing.T) {
	mock := &mockEthCaller{
		callContractFn: func(ctx context.Context, call ethereum.CallMsg, blockNumber *big.Int) ([]byte, error) {
			return []byte{0x01, 0x02}, nil
		},
	}
	verifier := NewNFTVerifier(mock, zap.NewNop())
	_, err := verifier.VerifyNFTOwnership(context.Background(), "0x1234567890123456789012345678901234567890", "1", "0x742d35Cc6634C0532925a3b844Bc9e7595f2bD18")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "insufficient data")
}

func TestNFTVerifier_VerifyERC1155Ownership(t *testing.T) {
	ownerAddr := common.HexToAddress("0x742d35Cc6634C0532925a3b844Bc9e7595f2bD18")
	contractAddr := common.HexToAddress("0x1234567890123456789012345678901234567890")

	t.Run("owns tokens", func(t *testing.T) {
		mock := &mockEthCaller{
			callContractFn: func(ctx context.Context, call ethereum.CallMsg, blockNumber *big.Int) ([]byte, error) {
				return common.LeftPadBytes(big.NewInt(5).Bytes(), 32), nil
			},
		}
		verifier := NewNFTVerifier(mock, zap.NewNop())
		owned, err := verifier.VerifyERC1155Ownership(context.Background(), contractAddr.Hex(), "1", ownerAddr.Hex())
		require.NoError(t, err)
		assert.True(t, owned)
	})

	t.Run("no tokens", func(t *testing.T) {
		mock := &mockEthCaller{
			callContractFn: func(ctx context.Context, call ethereum.CallMsg, blockNumber *big.Int) ([]byte, error) {
				return make([]byte, 32), nil
			},
		}
		verifier := NewNFTVerifier(mock, zap.NewNop())
		owned, err := verifier.VerifyERC1155Ownership(context.Background(), contractAddr.Hex(), "1", ownerAddr.Hex())
		require.NoError(t, err)
		assert.False(t, owned)
	})

	t.Run("invalid token ID", func(t *testing.T) {
		mock := &mockEthCaller{}
		verifier := NewNFTVerifier(mock, zap.NewNop())
		_, err := verifier.VerifyERC1155Ownership(context.Background(), contractAddr.Hex(), "abc", ownerAddr.Hex())
		require.Error(t, err)
		assert.Contains(t, err.Error(), "invalid token ID")
	})

	t.Run("call error", func(t *testing.T) {
		mock := &mockEthCaller{
			callContractFn: func(ctx context.Context, call ethereum.CallMsg, blockNumber *big.Int) ([]byte, error) {
				return nil, fmt.Errorf("RPC error")
			},
		}
		verifier := NewNFTVerifier(mock, zap.NewNop())
		_, err := verifier.VerifyERC1155Ownership(context.Background(), contractAddr.Hex(), "1", ownerAddr.Hex())
		require.Error(t, err)
	})

	t.Run("insufficient data", func(t *testing.T) {
		mock := &mockEthCaller{
			callContractFn: func(ctx context.Context, call ethereum.CallMsg, blockNumber *big.Int) ([]byte, error) {
				return []byte{0x01}, nil
			},
		}
		verifier := NewNFTVerifier(mock, zap.NewNop())
		_, err := verifier.VerifyERC1155Ownership(context.Background(), contractAddr.Hex(), "1", ownerAddr.Hex())
		require.Error(t, err)
		assert.Contains(t, err.Error(), "insufficient data")
	})
}

func TestNFTVerifier_GetERC1155Balance(t *testing.T) {
	contractAddr := common.HexToAddress("0x1234567890123456789012345678901234567890")
	ownerAddr := common.HexToAddress("0x742d35Cc6634C0532925a3b844Bc9e7595f2bD18")

	t.Run("success", func(t *testing.T) {
		mock := &mockEthCaller{
			callContractFn: func(ctx context.Context, call ethereum.CallMsg, blockNumber *big.Int) ([]byte, error) {
				return common.LeftPadBytes(big.NewInt(10).Bytes(), 32), nil
			},
		}
		verifier := NewNFTVerifier(mock, zap.NewNop())
		balance, err := verifier.GetERC1155Balance(context.Background(), contractAddr.Hex(), ownerAddr.Hex(), "1")
		require.NoError(t, err)
		assert.Equal(t, 0, big.NewInt(10).Cmp(balance))
	})

	t.Run("invalid token ID", func(t *testing.T) {
		mock := &mockEthCaller{}
		verifier := NewNFTVerifier(mock, zap.NewNop())
		_, err := verifier.GetERC1155Balance(context.Background(), contractAddr.Hex(), ownerAddr.Hex(), "abc")
		require.Error(t, err)
	})
}

func TestNFTVerifier_WithBlockTag(t *testing.T) {
	mock := &mockEthCaller{}
	verifier := NewNFTVerifier(mock, zap.NewNop())
	result := verifier.WithBlockTag(BlockTagSafe)
	assert.Equal(t, verifier, result)
	assert.Equal(t, BlockTagSafe, verifier.blockTag)
}

func TestNFTVerifier_VerifyNFTCollectionAutoDetect_ERC1155(t *testing.T) {
	mock := &mockEthCaller{
		callContractFn: func(ctx context.Context, call ethereum.CallMsg, blockNumber *big.Int) ([]byte, error) {
			selector := call.Data[:4]
			switch common.Bytes2Hex(selector) {
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
	_, err := verifier.VerifyNFTCollectionAutoDetect(context.Background(), "0x1234567890123456789012345678901234567890", "0x742d35Cc6634C0532925a3b844Bc9e7595f2bD18")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "ERC-1155 collection verification requires a specific tokenID")
}

func TestNFTVerifier_GetNFTBalanceAutoDetect_ERC1155_NoTokenID(t *testing.T) {
	mock := &mockEthCaller{
		callContractFn: func(ctx context.Context, call ethereum.CallMsg, blockNumber *big.Int) ([]byte, error) {
			selector := call.Data[:4]
			switch common.Bytes2Hex(selector) {
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
	_, err := verifier.GetNFTBalanceAutoDetect(context.Background(), "0x1234567890123456789012345678901234567890", "0x742d35Cc6634C0532925a3b844Bc9e7595f2bD18")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "requires a tokenID parameter")
}

func TestNFTVerifier_CheckApproval(t *testing.T) {
	contractAddr := "0x1234567890123456789012345678901234567890"
	ownerAddr := "0x742d35Cc6634C0532925a3b844Bc9e7595f2bD18"

	t.Run("with approved address", func(t *testing.T) {
		approvedAddr := common.HexToAddress("0x8ba1f109551bD432803012645Ac136ddd64DBA72")
		mock := &mockEthCaller{
			callContractFn: func(ctx context.Context, call ethereum.CallMsg, blockNumber *big.Int) ([]byte, error) {
				selector := call.Data[:4]
				switch common.Bytes2Hex(selector) {
				case "081812fc":
					return common.LeftPadBytes(approvedAddr.Bytes(), 32), nil
				case "e985e9c5":
					return encodeBool(false), nil
				default:
					return nil, fmt.Errorf("unsupported")
				}
			},
		}
		verifier := NewNFTVerifier(mock, zap.NewNop())
		info, err := verifier.CheckApproval(context.Background(), contractAddr, "1", ownerAddr)
		require.NoError(t, err)
		assert.Equal(t, approvedAddr.Hex(), info.ApprovedAddress)
	})

	t.Run("call error returns empty info", func(t *testing.T) {
		mock := &mockEthCaller{
			callContractFn: func(ctx context.Context, call ethereum.CallMsg, blockNumber *big.Int) ([]byte, error) {
				return nil, fmt.Errorf("RPC error")
			},
		}
		verifier := NewNFTVerifier(mock, zap.NewNop())
		info, err := verifier.CheckApproval(context.Background(), contractAddr, "1", ownerAddr)
		require.NoError(t, err)
		assert.Empty(t, info.ApprovedAddress)
		assert.Empty(t, info.ApprovedOperator)
	})

	t.Run("invalid token ID", func(t *testing.T) {
		mock := &mockEthCaller{}
		verifier := NewNFTVerifier(mock, zap.NewNop())
		_, err := verifier.CheckApproval(context.Background(), contractAddr, "abc", ownerAddr)
		require.Error(t, err)
	})
}

func TestNFTVerifier_CheckERC1155Approval(t *testing.T) {
	contractAddr := "0x1234567890123456789012345678901234567890"
	ownerAddr := "0x742d35Cc6634C0532925a3b844Bc9e7595f2bD18"

	t.Run("approved operator", func(t *testing.T) {
		mock := &mockEthCaller{
			callContractFn: func(ctx context.Context, call ethereum.CallMsg, blockNumber *big.Int) ([]byte, error) {
				return encodeBool(true), nil
			},
		}
		verifier := NewNFTVerifier(mock, zap.NewNop())
		info, err := verifier.CheckERC1155Approval(context.Background(), contractAddr, ownerAddr)
		require.NoError(t, err)
		assert.NotEmpty(t, info.ApprovedOperator)
	})

	t.Run("not approved", func(t *testing.T) {
		mock := &mockEthCaller{
			callContractFn: func(ctx context.Context, call ethereum.CallMsg, blockNumber *big.Int) ([]byte, error) {
				return encodeBool(false), nil
			},
		}
		verifier := NewNFTVerifier(mock, zap.NewNop())
		info, err := verifier.CheckERC1155Approval(context.Background(), contractAddr, ownerAddr)
		require.NoError(t, err)
		assert.Empty(t, info.ApprovedOperator)
	})
}

func TestNFTVerifier_CallContract_WithBlockTag(t *testing.T) {
	mock := &mockEthCaller{
		callContractFn: func(ctx context.Context, call ethereum.CallMsg, blockNumber *big.Int) ([]byte, error) {
			return make([]byte, 32), nil
		},
	}
	verifier := NewNFTVerifier(mock, zap.NewNop()).WithBlockTag(BlockTagSafe)

	_, err := verifier.GetNFTBalance(context.Background(), "0x1234567890123456789012345678901234567890", "0x742d35Cc6634C0532925a3b844Bc9e7595f2bD18")
	require.NoError(t, err)
}

func TestBlockTag_Constants(t *testing.T) {
	assert.Equal(t, BlockTag("latest"), BlockTagLatest)
	assert.Equal(t, BlockTag("safe"), BlockTagSafe)
	assert.Equal(t, BlockTag("finalized"), BlockTagFinalized)
}

func TestTokenStandard_Constants(t *testing.T) {
	assert.Equal(t, TokenStandard(0), TokenStandardUnknown)
	assert.Equal(t, TokenStandard(1), TokenStandardERC721)
	assert.Equal(t, TokenStandard(2), TokenStandardERC1155)
}

func TestApprovalInfo_Fields(t *testing.T) {
	info := &ApprovalInfo{
		ApprovedOperator: "0x1234",
		ApprovedAddress:  "0x5678",
	}
	assert.Equal(t, "0x1234", info.ApprovedOperator)
	assert.Equal(t, "0x5678", info.ApprovedAddress)
}

func TestNFTInfo_Fields(t *testing.T) {
	info := &NFTInfo{
		ContractAddress: "0x1",
		TokenID:         "42",
		Owner:           "0x2",
		Name:            "TestNFT",
		Symbol:          "TNFT",
		URI:             "https://example.com/42",
		Warnings:        []string{"name: RPC error"},
	}
	assert.Equal(t, "TestNFT", info.Name)
	assert.Len(t, info.Warnings, 1)
}
