package contract

import (
	"context"
	"errors"
	"fmt"
	"math/big"
	"strings"
	"testing"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func TestExt_TransactionBuilder_BuildTransaction(t *testing.T) {
	tb := NewTransactionBuilder(zap.NewNop())

	tx := tb.BuildTransaction(
		"0x1234567890123456789012345678901234567890",
		big.NewInt(1e18),
		"0xabcdef",
		21000,
		big.NewInt(3e9),
	)

	assert.Equal(t, "0x1234567890123456789012345678901234567890", tx.To)
	assert.Equal(t, big.NewInt(1e18), tx.Value)
	assert.Equal(t, "0xabcdef", tx.Data)
	assert.Equal(t, uint64(21000), tx.GasLimit)
	assert.Equal(t, big.NewInt(3e9), tx.GasPrice)
	assert.Equal(t, uint64(0), tx.Nonce)
}

func TestExt_TransactionBuilder_EstimateTransactionCost(t *testing.T) {
	tb := NewTransactionBuilder(zap.NewNop())

	tx := tb.BuildTransaction("0x1", big.NewInt(0), "0x", 100000, big.NewInt(2e9))
	cost := tb.EstimateTransactionCost(tx)

	expected := new(big.Int).Mul(big.NewInt(100000), big.NewInt(2e9))
	assert.Equal(t, expected, cost)
}

func TestExt_TransactionBuilder_EstimateTransactionCost_ZeroGas(t *testing.T) {
	tb := NewTransactionBuilder(zap.NewNop())

	tx := tb.BuildTransaction("0x1", big.NewInt(0), "0x", 0, big.NewInt(0))
	cost := tb.EstimateTransactionCost(tx)

	assert.Equal(t, big.NewInt(0), cost)
}

func TestExt_Transaction_Fields(t *testing.T) {
	tx := &Transaction{
		To:       "0x1",
		Value:    big.NewInt(100),
		Data:     "0xdead",
		GasLimit: 50000,
		GasPrice: big.NewInt(1e9),
		Nonce:    7,
	}
	assert.Equal(t, "0x1", tx.To)
	assert.Equal(t, big.NewInt(100), tx.Value)
	assert.Equal(t, "0xdead", tx.Data)
	assert.Equal(t, uint64(50000), tx.GasLimit)
	assert.Equal(t, big.NewInt(1e9), tx.GasPrice)
	assert.Equal(t, uint64(7), tx.Nonce)
}

func TestExt_ContentInfo_Fields(t *testing.T) {
	ci := &ContentInfo{
		Hash:      "0xabc",
		Owner:     "0x1234567890123456789012345678901234567890",
		Timestamp: 1700000000,
		Metadata:  "test metadata",
		IsValid:   true,
	}
	assert.Equal(t, "0xabc", ci.Hash)
	assert.Equal(t, "0x1234567890123456789012345678901234567890", ci.Owner)
	assert.Equal(t, int64(1700000000), ci.Timestamp)
	assert.Equal(t, "test metadata", ci.Metadata)
	assert.True(t, ci.IsValid)
}

func TestExt_HexToBytes32_Table(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantErr bool
	}{
		{"valid with 0x prefix", "0x" + strings.Repeat("ab", 32), false},
		{"valid with 0X prefix", "0X" + strings.Repeat("cd", 32), false},
		{"valid without prefix", strings.Repeat("ef", 32), false},
		{"too short", "0xabcdef", true},
		{"too long", "0x" + strings.Repeat("ab", 33), true},
		{"invalid hex chars", "0x" + strings.Repeat("gh", 32), true},
		{"empty string", "", true},
		{"just 0x", "0x", true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := hexToBytes32(tc.input)
			if tc.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestExt_DualError_Error(t *testing.T) {
	de := &DualError{
		Primary:   fmt.Errorf("proxy err"),
		Secondary: fmt.Errorf("impl err"),
	}
	msg := de.Error()
	assert.Contains(t, msg, "proxy err")
	assert.Contains(t, msg, "impl err")
	assert.Contains(t, msg, "proxy")
	assert.Contains(t, msg, "implementation")
}

func TestExt_DualError_Unwrap(t *testing.T) {
	pri := fmt.Errorf("primary")
	sec := fmt.Errorf("secondary")
	de := &DualError{Primary: pri, Secondary: sec}
	unwrapped := de.Unwrap()
	assert.Len(t, unwrapped, 2)
	assert.Equal(t, pri, unwrapped[0])
	assert.Equal(t, sec, unwrapped[1])
}

func TestExt_DualError_IsRetryable_NeitherRetryable(t *testing.T) {
	de := &DualError{
		Primary:   fmt.Errorf("not retryable 1"),
		Secondary: fmt.Errorf("not retryable 2"),
	}
	assert.False(t, de.IsRetryable())
}

func TestExt_DualError_IsRetryable_PrimaryRetryable(t *testing.T) {
	de := &DualError{
		Primary:   &extRetryableErr{retryable: true},
		Secondary: fmt.Errorf("not retryable"),
	}
	assert.True(t, de.IsRetryable())
}

func TestExt_DualError_IsRetryable_SecondaryRetryable(t *testing.T) {
	de := &DualError{
		Primary:   fmt.Errorf("not retryable"),
		Secondary: &extRetryableErr{retryable: true},
	}
	assert.True(t, de.IsRetryable())
}

func TestExt_DualError_IsRetryable_PrimaryNotRetryable(t *testing.T) {
	de := &DualError{
		Primary:   &extRetryableErr{retryable: false},
		Secondary: &extRetryableErr{retryable: true},
	}
	assert.False(t, de.IsRetryable())
}

func TestExt_DualError_IsRetryable_SecondaryNotRetryable(t *testing.T) {
	de := &DualError{
		Primary:   &extRetryableErr{retryable: true},
		Secondary: &extRetryableErr{retryable: false},
	}
	assert.False(t, de.IsRetryable())
}

func TestExt_DualError_IsRetryable_BothRetryable(t *testing.T) {
	de := &DualError{
		Primary:   &extRetryableErr{retryable: true},
		Secondary: &extRetryableErr{retryable: true},
	}
	assert.True(t, de.IsRetryable())
}

type extRetryableErr struct {
	retryable bool
}

func (e *extRetryableErr) Error() string { return fmt.Sprintf("retryable=%v", e.retryable) }

func (e *extRetryableErr) IsRetryable() bool { return e.retryable }

func TestExt_ContractInteractor_GetContractCode_Success(t *testing.T) {
	mock := &mockEthCaller{
		codeAtFn: func(ctx context.Context, contract common.Address, blockNumber *big.Int) ([]byte, error) {
			return []byte{0x60, 0x80, 0x60, 0x40, 0x52}, nil
		},
	}
	ci := NewContractInteractor(mock, zap.NewNop())

	code, err := ci.GetContractCode(context.Background(), "0x1111111111111111111111111111111111111111")
	require.NoError(t, err)
	assert.Contains(t, code, "0x6080604052")
}

func TestExt_ContractInteractor_GetContractCode_Error(t *testing.T) {
	mock := &mockEthCaller{
		codeAtFn: func(ctx context.Context, contract common.Address, blockNumber *big.Int) ([]byte, error) {
			return nil, fmt.Errorf("network error")
		},
	}
	ci := NewContractInteractor(mock, zap.NewNop())

	_, err := ci.GetContractCode(context.Background(), "0x1111111111111111111111111111111111111111")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to get contract code")
}

func TestExt_ContractInteractor_GetContractCode_Empty(t *testing.T) {
	mock := &mockEthCaller{
		codeAtFn: func(ctx context.Context, contract common.Address, blockNumber *big.Int) ([]byte, error) {
			return nil, nil
		},
	}
	ci := NewContractInteractor(mock, zap.NewNop())

	code, err := ci.GetContractCode(context.Background(), "0x1111111111111111111111111111111111111111")
	require.NoError(t, err)
	assert.Equal(t, "0x", code)
}

func TestExt_ContractInteractor_IsContractAddress_Error(t *testing.T) {
	mock := &mockEthCaller{
		codeAtFn: func(ctx context.Context, contract common.Address, blockNumber *big.Int) ([]byte, error) {
			return nil, fmt.Errorf("rpc error")
		},
	}
	ci := NewContractInteractor(mock, zap.NewNop())

	_, err := ci.IsContractAddress(context.Background(), "0x1111111111111111111111111111111111111111")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to check contract address")
}

func TestExt_ContractInteractor_CallContractFunction_InvalidABI(t *testing.T) {
	mock := &mockEthCaller{
		callContractFn: func(ctx context.Context, call ethereum.CallMsg, blockNumber *big.Int) ([]byte, error) {
			return make([]byte, 32), nil
		},
	}
	ci := NewContractInteractor(mock, zap.NewNop())

	_, err := ci.CallContractFunction(context.Background(), "0x1111111111111111111111111111111111111111", "not valid json", "balanceOf", "")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to parse ABI")
}

func TestExt_ContractInteractor_CallContractFunction_InvalidFunction(t *testing.T) {
	mock := &mockEthCaller{
		callContractFn: func(ctx context.Context, call ethereum.CallMsg, blockNumber *big.Int) ([]byte, error) {
			return make([]byte, 32), nil
		},
	}
	ci := NewContractInteractor(mock, zap.NewNop())

	_, err := ci.CallContractFunction(context.Background(), "0x1111111111111111111111111111111111111111", simpleABI, "nonExistentFunction", "")
	assert.Error(t, err)
}

func TestExt_ContractInteractor_CallContractFunction_WithFromAddress(t *testing.T) {
	var capturedFrom common.Address
	mock := &mockEthCaller{
		callContractFn: func(ctx context.Context, call ethereum.CallMsg, blockNumber *big.Int) ([]byte, error) {
			capturedFrom = call.From
			return make([]byte, 32), nil
		},
	}
	ci := NewContractInteractor(mock, zap.NewNop())

	fromAddr := "0xabcdefabcdefabcdefabcdefabcdefabcdefabcd"
	_, err := ci.CallContractFunction(context.Background(), "0x1111111111111111111111111111111111111111", simpleABI, "balanceOf", fromAddr, common.HexToAddress("0x1"))
	require.NoError(t, err)
	assert.Equal(t, common.HexToAddress(fromAddr), capturedFrom)
}

func TestExt_ContractInteractor_CallContractFunction_InvalidFromAddress(t *testing.T) {
	var capturedMsg ethereum.CallMsg
	mock := &mockEthCaller{
		callContractFn: func(ctx context.Context, call ethereum.CallMsg, blockNumber *big.Int) ([]byte, error) {
			capturedMsg = call
			return make([]byte, 32), nil
		},
	}
	ci := NewContractInteractor(mock, zap.NewNop())

	_, err := ci.CallContractFunction(context.Background(), "0x1111111111111111111111111111111111111111", simpleABI, "balanceOf", "not_an_address", common.HexToAddress("0x1"))
	require.NoError(t, err)
	assert.Equal(t, common.Address{}, capturedMsg.From)
}

func TestExt_ContractInteractor_CallContractFunction_EmptyFromAddress(t *testing.T) {
	var capturedMsg ethereum.CallMsg
	mock := &mockEthCaller{
		callContractFn: func(ctx context.Context, call ethereum.CallMsg, blockNumber *big.Int) ([]byte, error) {
			capturedMsg = call
			return make([]byte, 32), nil
		},
	}
	ci := NewContractInteractor(mock, zap.NewNop())

	_, err := ci.CallContractFunction(context.Background(), "0x1111111111111111111111111111111111111111", simpleABI, "balanceOf", "", common.HexToAddress("0x1"))
	require.NoError(t, err)
	assert.Equal(t, common.Address{}, capturedMsg.From)
}

func TestExt_ContractInteractor_CallContractFunction_CallError_NoProxy(t *testing.T) {
	mock := &mockEthCaller{
		callContractFn: func(ctx context.Context, call ethereum.CallMsg, blockNumber *big.Int) ([]byte, error) {
			return nil, fmt.Errorf("call failed")
		},
	}
	ci := NewContractInteractor(mock, zap.NewNop())

	_, err := ci.CallContractFunction(context.Background(), "0x1111111111111111111111111111111111111111", simpleABI, "balanceOf", "", common.HexToAddress("0x1"))
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "call failed")
}

func TestExt_ContractInteractor_ResolveImplementation_SlotReadError(t *testing.T) {
	mock := &mockEthCaller{
		callContractFn: func(ctx context.Context, call ethereum.CallMsg, blockNumber *big.Int) ([]byte, error) {
			return nil, fmt.Errorf("slot read error")
		},
	}
	ci := NewContractInteractor(mock, zap.NewNop())

	resolved, err := ci.ResolveImplementation(context.Background(), "0x1111111111111111111111111111111111111111")
	require.NoError(t, err)
	assert.Equal(t, "0x1111111111111111111111111111111111111111", resolved)
}

func TestExt_ContractInteractor_ResolveImplementation_ShortResult(t *testing.T) {
	mock := &mockEthCaller{
		callContractFn: func(ctx context.Context, call ethereum.CallMsg, blockNumber *big.Int) ([]byte, error) {
			return make([]byte, 16), nil
		},
	}
	ci := NewContractInteractor(mock, zap.NewNop())

	resolved, err := ci.ResolveImplementation(context.Background(), "0x1111111111111111111111111111111111111111")
	require.NoError(t, err)
	assert.Equal(t, "0x1111111111111111111111111111111111111111", resolved)
}

func TestExt_ContractInteractor_ResolveImplementation_ZeroImplementation(t *testing.T) {
	mock := &mockEthCaller{
		callContractFn: func(ctx context.Context, call ethereum.CallMsg, blockNumber *big.Int) ([]byte, error) {
			return make([]byte, 32), nil
		},
	}
	ci := NewContractInteractor(mock, zap.NewNop())

	resolved, err := ci.ResolveImplementation(context.Background(), "0x1111111111111111111111111111111111111111")
	require.NoError(t, err)
	assert.Equal(t, "0x1111111111111111111111111111111111111111", resolved)
}

func TestExt_Multicall3DeployedAddress_AllChains(t *testing.T) {
	expected := common.HexToAddress("0xcA11bde05977b3631167028862bE2a173976CA11")

	for _, chainID := range []int64{1, 137, 42161, 10, 43114} {
		addr := Multicall3DeployedAddress(chainID)
		assert.Equal(t, expected, addr, "chainID=%d", chainID)
	}
}

func TestExt_MulticallCaller_WithMockCaller(t *testing.T) {
	mc, err := NewMulticallCaller(&mockEthCaller{}, 1, zap.NewNop())
	require.NoError(t, err)
	assert.NotNil(t, mc)
	assert.Equal(t, int64(1), mc.chainID)
}

func TestExt_MulticallCaller_Aggregate3_EmptyCalls(t *testing.T) {
	mc, err := NewMulticallCaller(&mockEthCaller{}, 1, zap.NewNop())
	require.NoError(t, err)

	results, err := mc.Aggregate3(context.Background(), nil)
	assert.NoError(t, err)
	assert.Nil(t, results)

	results, err = mc.Aggregate3(context.Background(), []MulticallCall3{})
	assert.NoError(t, err)
	assert.Nil(t, results)
}

func TestExt_MulticallCaller_BatchCall_RPCError(t *testing.T) {
	mc, err := NewMulticallCaller(&mockEthCaller{}, 1, zap.NewNop())
	require.NoError(t, err)

	targets := []common.Address{
		common.HexToAddress("0x1111111111111111111111111111111111111111"),
		common.HexToAddress("0x2222222222222222222222222222222222222222"),
	}
	callDatas := [][]byte{{0x01}, {0x02}}

	results, err := mc.BatchCall(context.Background(), targets, callDatas)
	assert.Error(t, err)
	_ = results
}

func TestExt_MulticallCaller_BatchBalanceOfERC20_Empty(t *testing.T) {
	mc, err := NewMulticallCaller(&mockEthCaller{}, 1, zap.NewNop())
	require.NoError(t, err)

	owner := common.HexToAddress("0xabcdefabcdefabcdefabcdefabcdefabcdefabcd")
	balances, err := mc.BatchBalanceOfERC20(context.Background(), nil, owner)
	assert.NoError(t, err)
	assert.Empty(t, balances)
}

func TestExt_MulticallCaller_BatchBalanceOfERC20_WithResults(t *testing.T) {
	mock := &mockEthCaller{
		callContractFn: func(ctx context.Context, call ethereum.CallMsg, blockNumber *big.Int) ([]byte, error) {
			return nil, fmt.Errorf("rpc error")
		},
	}
	mc, err := NewMulticallCaller(mock, 1, zap.NewNop())
	require.NoError(t, err)

	owner := common.HexToAddress("0xabcdefabcdefabcdefabcdefabcdefabcdefabcd")
	tokenAddrs := []common.Address{
		common.HexToAddress("0x1111111111111111111111111111111111111111"),
		common.HexToAddress("0x2222222222222222222222222222222222222222"),
	}

	balances, err := mc.BatchBalanceOfERC20(context.Background(), tokenAddrs, owner)
	assert.Error(t, err)
	assert.Nil(t, balances)
}

func TestExt_MulticallCaller_Aggregate3_RPCError(t *testing.T) {
	mock := &mockEthCaller{
		callContractFn: func(ctx context.Context, call ethereum.CallMsg, blockNumber *big.Int) ([]byte, error) {
			return nil, fmt.Errorf("rpc timeout")
		},
	}
	mc, err := NewMulticallCaller(mock, 1, zap.NewNop())
	require.NoError(t, err)

	calls := []MulticallCall3{
		{
			Target:       common.HexToAddress("0x1111111111111111111111111111111111111111"),
			AllowFailure: true,
			CallData:     []byte{0x01},
		},
	}

	_, err = mc.Aggregate3(context.Background(), calls)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "multicall3 aggregate3 failed")
}

func TestExt_ContractContentRegistry_RegisterContent_InvalidHash(t *testing.T) {
	cr := &ContractContentRegistry{
		Address: "0x1234567890123456789012345678901234567890",
		ABI:     ContentRegistryABI,
	}
	ci := NewContractInteractor(&mockEthCaller{}, zap.NewNop())

	_, err := cr.RegisterContent(context.Background(), ci, "not_a_hash", "owner", "metadata")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "invalid content hash")
}

func TestExt_ContractContentRegistry_VerifyContent_InvalidHash(t *testing.T) {
	cr := &ContractContentRegistry{
		Address: "0x1234567890123456789012345678901234567890",
		ABI:     ContentRegistryABI,
	}
	ci := NewContractInteractor(&mockEthCaller{}, zap.NewNop())

	_, err := cr.VerifyContent(context.Background(), ci, "bad_hash")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "invalid content hash")
}

func TestExt_ContractContentRegistry_GetContentInfo_InvalidHash(t *testing.T) {
	cr := &ContractContentRegistry{
		Address: "0x1234567890123456789012345678901234567890",
		ABI:     ContentRegistryABI,
	}
	ci := NewContractInteractor(&mockEthCaller{}, zap.NewNop())

	_, err := cr.GetContentInfo(context.Background(), ci, "bad_hash")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "invalid content hash")
}

func TestExt_ContractContentRegistry_RegisterContent_Success(t *testing.T) {
	cr := &ContractContentRegistry{
		Address: "0x1234567890123456789012345678901234567890",
		ABI:     ContentRegistryABI,
	}
	ci := NewContractInteractor(&mockEthCaller{}, zap.NewNop())

	hash := "0x" + strings.Repeat("ab", 32)
	data, err := cr.RegisterContent(context.Background(), ci, hash, "owner", "test metadata")
	require.NoError(t, err)
	assert.NotEmpty(t, data)
}

func TestExt_ContractContentRegistry_VerifyContent_RPCError(t *testing.T) {
	mock := &mockEthCaller{
		callContractFn: func(ctx context.Context, call ethereum.CallMsg, blockNumber *big.Int) ([]byte, error) {
			return nil, fmt.Errorf("rpc error")
		},
	}
	cr := &ContractContentRegistry{
		Address: "0x1234567890123456789012345678901234567890",
		ABI:     ContentRegistryABI,
	}
	ci := NewContractInteractor(mock, zap.NewNop())

	hash := "0x" + strings.Repeat("ab", 32)
	_, err := cr.VerifyContent(context.Background(), ci, hash)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "verifyContent call failed")
}

func TestExt_ContractContentRegistry_GetContentInfo_RPCError(t *testing.T) {
	mock := &mockEthCaller{
		callContractFn: func(ctx context.Context, call ethereum.CallMsg, blockNumber *big.Int) ([]byte, error) {
			return nil, fmt.Errorf("rpc error")
		},
	}
	cr := &ContractContentRegistry{
		Address: "0x1234567890123456789012345678901234567890",
		ABI:     ContentRegistryABI,
	}
	ci := NewContractInteractor(mock, zap.NewNop())

	hash := "0x" + strings.Repeat("ab", 32)
	_, err := cr.GetContentInfo(context.Background(), ci, hash)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "getContentInfo call failed")
}

func TestExt_RevertError_Error_Panic(t *testing.T) {
	re := &RevertError{
		Reason:    "",
		RawData:   []byte{0x01},
		IsPanic:   true,
		PanicCode: 0x11,
	}
	msg := re.Error()
	assert.Contains(t, msg, "panic")
	assert.Contains(t, msg, "0x11")
	assert.Contains(t, msg, "arithmetic overflow")
}

func TestExt_RevertError_Error_Revert(t *testing.T) {
	re := &RevertError{
		Reason:  "not owner",
		RawData: []byte{0x01},
	}
	msg := re.Error()
	assert.Contains(t, msg, "revert")
	assert.Contains(t, msg, "not owner")
}

func TestExt_ParseRevertReason_PanicCode(t *testing.T) {
	data := make([]byte, 36)
	copy(data[:4], []byte{0x4e, 0x48, 0x7b, 0x71})
	data[35] = 0x12

	re := ParseRevertReason(data)
	require.NotNil(t, re)
	assert.True(t, re.IsPanic)
	assert.Equal(t, uint64(0x12), re.PanicCode)
}

func TestExt_ExtractRevertData_NoHexPrefix(t *testing.T) {
	data := ExtractRevertData("some error without hex data")
	assert.Nil(t, data)
}

func TestExt_ExtractRevertData_ShortHexData(t *testing.T) {
	data := ExtractRevertData("error: 0xabcd")
	assert.Nil(t, data)
}

func TestExt_PanicCodeName_Table(t *testing.T) {
	tests := []struct {
		code     uint64
		contains string
	}{
		{0x01, "assertion"},
		{0x11, "arithmetic"},
		{0x12, "division"},
		{0x21, "enum"},
		{0x22, "storage"},
		{0x31, "pop"},
		{0x32, "out-of-bounds"},
		{0x41, "memory"},
		{0x51, "function pointer"},
		{0x99, "unknown"},
	}

	for _, tc := range tests {
		t.Run(fmt.Sprintf("0x%x", tc.code), func(t *testing.T) {
			name := panicCodeName(tc.code)
			assert.Contains(t, name, tc.contains)
		})
	}
}

func TestExt_IsHexChar(t *testing.T) {
	assert.True(t, isHexChar('0'))
	assert.True(t, isHexChar('9'))
	assert.True(t, isHexChar('a'))
	assert.True(t, isHexChar('f'))
	assert.True(t, isHexChar('A'))
	assert.True(t, isHexChar('F'))
	assert.False(t, isHexChar('g'))
	assert.False(t, isHexChar('G'))
	assert.False(t, isHexChar('z'))
	assert.False(t, isHexChar(' '))
}

func TestExt_ContractInteractor_InvalidateProxyCache_NonExistent(t *testing.T) {
	ci := NewContractInteractor(&mockEthCaller{}, zap.NewNop())
	ci.InvalidateProxyCache("0x1111111111111111111111111111111111111111")
}

func TestExt_ContractInteractor_CallContractFunction_WithDeadline(t *testing.T) {
	mock := &mockEthCaller{
		callContractFn: func(ctx context.Context, call ethereum.CallMsg, blockNumber *big.Int) ([]byte, error) {
			_, ok := ctx.Deadline()
			assert.True(t, ok, "context should have a deadline")
			return make([]byte, 32), nil
		},
	}
	ci := NewContractInteractor(mock, zap.NewNop())

	ctx := context.Background()
	_, err := ci.CallContractFunction(ctx, "0x1111111111111111111111111111111111111111", simpleABI, "balanceOf", "", common.HexToAddress("0x1"))
	require.NoError(t, err)
}

func TestExt_DualError_ErrorsAs(t *testing.T) {
	de := &DualError{
		Primary:   fmt.Errorf("primary"),
		Secondary: fmt.Errorf("secondary"),
	}

	var target *DualError
	assert.True(t, errors.As(de, &target))
	assert.Equal(t, "primary", target.Primary.Error())
	assert.Equal(t, "secondary", target.Secondary.Error())
}

func TestExt_MulticallResult_Fields(t *testing.T) {
	result := MulticallResult{
		Success:    true,
		ReturnData: []byte{0x01},
	}
	assert.True(t, result.Success)
	assert.Equal(t, []byte{0x01}, result.ReturnData)
}

func TestExt_ContractContentRegistry_Fields(t *testing.T) {
	cr := &ContractContentRegistry{
		Address: "0x1234",
		ABI:     "[]",
	}
	assert.Equal(t, "0x1234", cr.Address)
	assert.Equal(t, "[]", cr.ABI)
}

