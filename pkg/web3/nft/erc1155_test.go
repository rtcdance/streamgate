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

func packBalanceOf(t *testing.T, account common.Address, tokenID *big.Int) []byte {
	t.Helper()
	parsedABI := newERC1155Verifier(t).parsedERC1155ABI
	data, err := parsedABI.Pack("balanceOf", account, tokenID)
	require.NoError(t, err)
	return data
}

func packBalanceOfBatch(t *testing.T, accounts []common.Address, ids []*big.Int) []byte {
	t.Helper()
	parsedABI := newERC1155Verifier(t).parsedERC1155ABI
	data, err := parsedABI.Pack("balanceOfBatch", accounts, ids)
	require.NoError(t, err)
	return data
}

func packURI(t *testing.T, tokenID *big.Int) []byte {
	t.Helper()
	parsedABI := newERC1155Verifier(t).parsedERC1155ABI
	data, err := parsedABI.Pack("uri", tokenID)
	require.NoError(t, err)
	return data
}

func packIsApprovedForAll(t *testing.T, account, operator common.Address) []byte {
	t.Helper()
	parsedABI := newERC1155Verifier(t).parsedERC1155ABI
	data, err := parsedABI.Pack("isApprovedForAll", account, operator)
	require.NoError(t, err)
	return data
}

func newERC1155Verifier(t *testing.T) *ERC1155Verifier {
	t.Helper()
	return NewERC1155Verifier(nil, zap.NewNop(), nil)
}

func encodeUint256(val *big.Int) []byte {
	result := make([]byte, 32)
	val.FillBytes(result)
	return result
}

func encodeBool(val bool) []byte {
	result := make([]byte, 32)
	if val {
		result[31] = 1
	}
	return result
}

func encodeString(s string) []byte {
	offset := make([]byte, 32)
	offset[31] = 32
	length := make([]byte, 32)
	length[31] = byte(len(s))
	paddedLen := ((len(s) + 31) / 32) * 32
	if paddedLen == 0 {
		paddedLen = 32
	}
	utf8 := make([]byte, paddedLen)
	copy(utf8, s)
	result := make([]byte, 0, 64+paddedLen)
	result = append(result, offset...)
	result = append(result, length...)
	result = append(result, utf8...)
	return result
}

func encodeUint256Array(vals []*big.Int) []byte {
	offset := make([]byte, 32)
	offset[31] = 32
	count := make([]byte, 32)
	count[31] = byte(len(vals))
	result := make([]byte, 0, 64+len(vals)*32)
	result = append(result, offset...)
	result = append(result, count...)
	for _, v := range vals {
		result = append(result, encodeUint256(v)...)
	}
	return result
}

type erc1155MockCaller struct {
	callContractFn func(ctx context.Context, call ethereum.CallMsg, blockNumber *big.Int) ([]byte, error)
	codeAtFn       func(ctx context.Context, contract common.Address, blockNumber *big.Int) ([]byte, error)
}

func (m *erc1155MockCaller) CallContract(ctx context.Context, call ethereum.CallMsg, blockNumber *big.Int) ([]byte, error) {
	if m.callContractFn != nil {
		return m.callContractFn(ctx, call, blockNumber)
	}
	return nil, fmt.Errorf("not configured")
}

func (m *erc1155MockCaller) CodeAt(ctx context.Context, contract common.Address, blockNumber *big.Int) ([]byte, error) {
	if m.codeAtFn != nil {
		return m.codeAtFn(ctx, contract, blockNumber)
	}
	return nil, nil
}

func TestERC1155Verifier_VerifyNFTOwnership_Owned(t *testing.T) {
	mock := &erc1155MockCaller{
		callContractFn: func(ctx context.Context, call ethereum.CallMsg, blockNumber *big.Int) ([]byte, error) {
			return encodeUint256(big.NewInt(5)), nil
		},
	}
	verifier := NewERC1155Verifier(mock, zap.NewNop(), nil)

	owned, err := verifier.VerifyNFTOwnership(
		context.Background(),
		"0x1234567890123456789012345678901234567890",
		"1",
		"0x742d35Cc6634C0532925a3b844Bc9e7595f2bD18",
	)
	require.NoError(t, err)
	assert.True(t, owned)
}

func TestERC1155Verifier_VerifyNFTOwnership_NotOwned(t *testing.T) {
	mock := &erc1155MockCaller{
		callContractFn: func(ctx context.Context, call ethereum.CallMsg, blockNumber *big.Int) ([]byte, error) {
			return encodeUint256(big.NewInt(0)), nil
		},
	}
	verifier := NewERC1155Verifier(mock, zap.NewNop(), nil)

	owned, err := verifier.VerifyNFTOwnership(
		context.Background(),
		"0x1234567890123456789012345678901234567890",
		"1",
		"0x742d35Cc6634C0532925a3b844Bc9e7595f2bD18",
	)
	require.NoError(t, err)
	assert.False(t, owned)
}

func TestERC1155Verifier_VerifyNFTOwnership_InvalidContractAddress(t *testing.T) {
	verifier := NewERC1155Verifier(nil, zap.NewNop(), nil)
	_, err := verifier.VerifyNFTOwnership(context.Background(), "notanaddress", "1", "0x742d35Cc6634C0532925a3b844Bc9e7595f2bD18")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "invalid contract address")
}

func TestERC1155Verifier_VerifyNFTOwnership_InvalidOwnerAddress(t *testing.T) {
	verifier := NewERC1155Verifier(nil, zap.NewNop(), nil)
	_, err := verifier.VerifyNFTOwnership(context.Background(), "0x1234567890123456789012345678901234567890", "1", "notanaddress")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "invalid owner address")
}

func TestERC1155Verifier_VerifyNFTOwnership_InvalidTokenID(t *testing.T) {
	mock := &erc1155MockCaller{}
	verifier := NewERC1155Verifier(mock, zap.NewNop(), nil)
	_, err := verifier.VerifyNFTOwnership(context.Background(), "0x1234567890123456789012345678901234567890", "notanumber", "0x742d35Cc6634C0532925a3b844Bc9e7595f2bD18")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "invalid token ID")
}

func TestERC1155Verifier_VerifyNFTOwnership_CallError(t *testing.T) {
	mock := &erc1155MockCaller{
		callContractFn: func(ctx context.Context, call ethereum.CallMsg, blockNumber *big.Int) ([]byte, error) {
			return nil, fmt.Errorf("RPC error")
		},
	}
	verifier := NewERC1155Verifier(mock, zap.NewNop(), nil)
	_, err := verifier.VerifyNFTOwnership(context.Background(), "0x1234567890123456789012345678901234567890", "1", "0x742d35Cc6634C0532925a3b844Bc9e7595f2bD18")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to get balance")
}

func TestERC1155Verifier_VerifyNFTOwnership_InsufficientData(t *testing.T) {
	mock := &erc1155MockCaller{
		callContractFn: func(ctx context.Context, call ethereum.CallMsg, blockNumber *big.Int) ([]byte, error) {
			return []byte{0x01, 0x02}, nil
		},
	}
	verifier := NewERC1155Verifier(mock, zap.NewNop(), nil)
	_, err := verifier.VerifyNFTOwnership(context.Background(), "0x1234567890123456789012345678901234567890", "1", "0x742d35Cc6634C0532925a3b844Bc9e7595f2bD18")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "insufficient data")
}

func TestERC1155Verifier_VerifyBatchNFTOwnership(t *testing.T) {
	mock := &erc1155MockCaller{
		callContractFn: func(ctx context.Context, call ethereum.CallMsg, blockNumber *big.Int) ([]byte, error) {
			return encodeUint256Array([]*big.Int{big.NewInt(3), big.NewInt(0), big.NewInt(1)}), nil
		},
	}
	verifier := NewERC1155Verifier(mock, zap.NewNop(), nil)

	results, err := verifier.VerifyBatchNFTOwnership(
		context.Background(),
		"0x1234567890123456789012345678901234567890",
		[]string{"1", "2", "3"},
		"0x742d35Cc6634C0532925a3b844Bc9e7595f2bD18",
	)
	require.NoError(t, err)
	assert.True(t, results["1"])
	assert.False(t, results["2"])
	assert.True(t, results["3"])
}

func TestERC1155Verifier_VerifyBatchNFTOwnership_InvalidContract(t *testing.T) {
	verifier := NewERC1155Verifier(nil, zap.NewNop(), nil)
	_, err := verifier.VerifyBatchNFTOwnership(context.Background(), "bad", []string{"1"}, "0x742d35Cc6634C0532925a3b844Bc9e7595f2bD18")
	assert.Error(t, err)
}

func TestERC1155Verifier_VerifyBatchNFTOwnership_InvalidTokenID(t *testing.T) {
	mock := &erc1155MockCaller{}
	verifier := NewERC1155Verifier(mock, zap.NewNop(), nil)
	_, err := verifier.VerifyBatchNFTOwnership(context.Background(), "0x1234567890123456789012345678901234567890", []string{"abc"}, "0x742d35Cc6634C0532925a3b844Bc9e7595f2bD18")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "invalid token ID")
}

func TestERC1155Verifier_VerifyTotalSupply(t *testing.T) {
	verifier := NewERC1155Verifier(nil, zap.NewNop(), nil)
	_, err := verifier.VerifyTotalSupply(context.Background(), "0x1234567890123456789012345678901234567890", "1", big.NewInt(100))
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not part of the ERC-1155 standard")
}

func TestERC1155Verifier_VerifyURI(t *testing.T) {
	mock := &erc1155MockCaller{
		callContractFn: func(ctx context.Context, call ethereum.CallMsg, blockNumber *big.Int) ([]byte, error) {
			return encodeString("https://example.com/metadata/1.json"), nil
		},
	}
	verifier := NewERC1155Verifier(mock, zap.NewNop(), nil)

	valid, err := verifier.VerifyURI(context.Background(), "0x1234567890123456789012345678901234567890", "1", "https://example.com/metadata/1.json")
	require.NoError(t, err)
	assert.True(t, valid)
}

func TestERC1155Verifier_VerifyURI_Mismatch(t *testing.T) {
	mock := &erc1155MockCaller{
		callContractFn: func(ctx context.Context, call ethereum.CallMsg, blockNumber *big.Int) ([]byte, error) {
			return encodeString("https://example.com/other.json"), nil
		},
	}
	verifier := NewERC1155Verifier(mock, zap.NewNop(), nil)

	valid, err := verifier.VerifyURI(context.Background(), "0x1234567890123456789012345678901234567890", "1", "https://example.com/metadata/1.json")
	require.NoError(t, err)
	assert.False(t, valid)
}

func TestERC1155Verifier_VerifyURI_InvalidContract(t *testing.T) {
	verifier := NewERC1155Verifier(nil, zap.NewNop(), nil)
	_, err := verifier.VerifyURI(context.Background(), "bad", "1", "https://example.com")
	assert.Error(t, err)
}

func TestERC1155Verifier_IsERC1155Contract(t *testing.T) {
	t.Run("valid_contract", func(t *testing.T) {
		mock := &erc1155MockCaller{
			callContractFn: func(ctx context.Context, call ethereum.CallMsg, blockNumber *big.Int) ([]byte, error) {
				return encodeUint256(big.NewInt(0)), nil
			},
		}
		verifier := NewERC1155Verifier(mock, zap.NewNop(), nil)
		is1155, err := verifier.IsERC1155Contract(context.Background(), "0x1234567890123456789012345678901234567890")
		require.NoError(t, err)
		assert.True(t, is1155)
	})

	t.Run("invalid_contract", func(t *testing.T) {
		mock := &erc1155MockCaller{
			callContractFn: func(ctx context.Context, call ethereum.CallMsg, blockNumber *big.Int) ([]byte, error) {
				return nil, fmt.Errorf("not a contract")
			},
		}
		verifier := NewERC1155Verifier(mock, zap.NewNop(), nil)
		is1155, err := verifier.IsERC1155Contract(context.Background(), "0x1234567890123456789012345678901234567890")
		require.NoError(t, err)
		assert.False(t, is1155)
	})

	t.Run("invalid_address", func(t *testing.T) {
		verifier := NewERC1155Verifier(nil, zap.NewNop(), nil)
		_, err := verifier.IsERC1155Contract(context.Background(), "bad")
		assert.Error(t, err)
	})
}

func TestERC1155Verifier_GetTokenInfo(t *testing.T) {
	mock := &erc1155MockCaller{
		callContractFn: func(ctx context.Context, call ethereum.CallMsg, blockNumber *big.Int) ([]byte, error) {
			return encodeString("https://example.com/token/1.json"), nil
		},
	}
	verifier := NewERC1155Verifier(mock, zap.NewNop(), nil)

	info, err := verifier.GetTokenInfo(context.Background(), "0x1234567890123456789012345678901234567890", "1")
	require.NoError(t, err)
	assert.Equal(t, "0x1234567890123456789012345678901234567890", info.ContractAddress)
	assert.Equal(t, "1", info.TokenID)
	assert.Equal(t, "ERC-1155", info.TokenType)
	assert.Equal(t, "https://example.com/token/1.json", info.URI)
}

func TestERC1155Verifier_GetTokenInfo_InvalidTokenID(t *testing.T) {
	verifier := NewERC1155Verifier(nil, zap.NewNop(), nil)
	_, err := verifier.GetTokenInfo(context.Background(), "0x1234567890123456789012345678901234567890", "abc")
	assert.Error(t, err)
}

func TestERC1155Verifier_VerifyOperatorApproval(t *testing.T) {
	t.Run("approved", func(t *testing.T) {
		mock := &erc1155MockCaller{
			callContractFn: func(ctx context.Context, call ethereum.CallMsg, blockNumber *big.Int) ([]byte, error) {
				return encodeBool(true), nil
			},
		}
		verifier := NewERC1155Verifier(mock, zap.NewNop(), nil)
		approved, err := verifier.VerifyOperatorApproval(
			context.Background(),
			"0x1234567890123456789012345678901234567890",
			"0x742d35Cc6634C0532925a3b844Bc9e7595f2bD18",
			"0x8ba1f109551bD432803012645Ac136ddd64DBA72",
		)
		require.NoError(t, err)
		assert.True(t, approved)
	})

	t.Run("not_approved", func(t *testing.T) {
		mock := &erc1155MockCaller{
			callContractFn: func(ctx context.Context, call ethereum.CallMsg, blockNumber *big.Int) ([]byte, error) {
				return encodeBool(false), nil
			},
		}
		verifier := NewERC1155Verifier(mock, zap.NewNop(), nil)
		approved, err := verifier.VerifyOperatorApproval(
			context.Background(),
			"0x1234567890123456789012345678901234567890",
			"0x742d35Cc6634C0532925a3b844Bc9e7595f2bD18",
			"0x8ba1f109551bD432803012645Ac136ddd64DBA72",
		)
		require.NoError(t, err)
		assert.False(t, approved)
	})

	t.Run("invalid_addresses", func(t *testing.T) {
		verifier := NewERC1155Verifier(nil, zap.NewNop(), nil)
		_, err := verifier.VerifyOperatorApproval(context.Background(), "bad", "0x742d35Cc6634C0532925a3b844Bc9e7595f2bD18", "0x8ba1f109551bD432803012645Ac136ddd64DBA72")
		assert.Error(t, err)
	})
}

func TestPackPermitCall(t *testing.T) {
	owner := common.HexToAddress("0x742d35Cc6634C0532925a3b844Bc9e7595f2bD18")
	spender := common.HexToAddress("0x8ba1f109551bD432803012645Ac136ddd64DBA72")
	value := big.NewInt(1000)
	deadline := big.NewInt(1716000000)
	var r, s [32]byte
	copy(r[:], common.Hex2Bytes("0x1234"))
	copy(s[:], common.Hex2Bytes("0x5678"))

	data, err := PackPermitCall(owner, spender, value, deadline, 28, r, s)
	require.NoError(t, err)
	assert.Len(t, data, 4+32*7)
}

func TestERC20Reader_GetPermitNonce(t *testing.T) {
	nonceSelector := common.Hex2Bytes("7ecebe00")

	t.Run("success", func(t *testing.T) {
		caller := &erc20MockCaller{
			responses: map[string][]byte{
				common.Bytes2Hex(nonceSelector): encodeUint256(big.NewInt(5)),
			},
		}
		reader := NewERC20Reader(caller, zap.NewNop())
		nonce, err := reader.GetPermitNonce(context.Background(), "0x1234567890123456789012345678901234567890", "0x742d35Cc6634C0532925a3b844Bc9e7595f2bD18")
		require.NoError(t, err)
		assert.Equal(t, 0, big.NewInt(5).Cmp(nonce))
	})

	t.Run("error", func(t *testing.T) {
		caller := &erc20MockCaller{responses: map[string][]byte{}}
		reader := NewERC20Reader(caller, zap.NewNop())
		_, err := reader.GetPermitNonce(context.Background(), "0x1234567890123456789012345678901234567890", "0x742d35Cc6634C0532925a3b844Bc9e7595f2bD18")
		assert.Error(t, err)
	})
}

func TestERC20Reader_GetTokenAllowance(t *testing.T) {
	allowanceSelector := "dd62ed3e"

	t.Run("success", func(t *testing.T) {
		caller := &erc20MockCaller{
			responses: map[string][]byte{
				allowanceSelector: encodeUint256(big.NewInt(10000)),
			},
		}
		reader := NewERC20Reader(caller, zap.NewNop())
		allowance, err := reader.GetTokenAllowance(
			context.Background(),
			"0x1234567890123456789012345678901234567890",
			"0x742d35Cc6634C0532925a3b844Bc9e7595f2bD18",
			"0x8ba1f109551bD432803012645Ac136ddd64DBA72",
		)
		require.NoError(t, err)
		assert.Equal(t, 0, big.NewInt(10000).Cmp(allowance))
	})
}
