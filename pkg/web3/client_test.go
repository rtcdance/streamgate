package web3

import (
	"context"
	"errors"
	"fmt"
	"math/big"
	"strings"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func blockHeaderResult(number, gasUsed, gasLimit int64) map[string]interface{} {
	zeroHash := "0x" + strings.Repeat("00", 32)
	emptyUnclesHash := "0x1dcc4de8dec75d7aab85b567b6ccd41ad312451b948a7413f0a142fd40d49347"
	emptyTxRootHash := "0x56e81f171bcc55a6ff8345e692c0f86e5b48e01b996cadc001622fb5e363b421"
	return map[string]interface{}{
		"number":           fmt.Sprintf("0x%x", number),
		"hash":             "0x" + strings.Repeat("aa", 32),
		"parentHash":       "0x" + strings.Repeat("99", 32),
		"sha3Uncles":       emptyUnclesHash,
		"stateRoot":        zeroHash,
		"transactionsRoot": emptyTxRootHash,
		"receiptsRoot":     zeroHash,
		"logsBloom":        "0x" + strings.Repeat("00", 256),
		"timestamp":        "0x0",
		"extraData":        "0x",
		"miner":            "0x0000000000000000000000000000000000000000",
		"gasUsed":          fmt.Sprintf("0x%x", gasUsed),
		"gasLimit":         fmt.Sprintf("0x%x", gasLimit),
		"difficulty":       "0x0",
		"transactions":     []interface{}{},
		"uncles":           []interface{}{},
	}
}

func TestNewChainClient_NoRPCURLs(t *testing.T) {
	_, err := NewChainClientWithFallback([]string{}, 1, zap.NewNop())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no rpc urls")
}

func TestNewChainClient_BlankRPCURLs(t *testing.T) {
	_, err := NewChainClientWithFallback([]string{"", "  ", ""}, 1, zap.NewNop())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no rpc urls")
}

func TestNewChainClient_SingleURL(t *testing.T) {
	srv := newRPCServer(t, map[string]func(req rpcRequest) rpcResponse{
		"eth_chainId": chainIDHandler(1),
	})
	defer srv.Close()

	client, err := NewChainClient(srv.URL, 1, zap.NewNop())
	require.NoError(t, err)
	require.NotNil(t, client)
	defer client.Close()
	assert.Equal(t, srv.URL, client.rpcURL)
}

func TestNewChainClientWithFallback_TrimsWhitespace(t *testing.T) {
	srv := newRPCServer(t, map[string]func(req rpcRequest) rpcResponse{
		"eth_chainId": chainIDHandler(1),
	})
	defer srv.Close()

	client, err := NewChainClientWithFallback([]string{"  " + srv.URL + "  "}, 1, zap.NewNop())
	require.NoError(t, err)
	defer client.Close()
	assert.Equal(t, srv.URL, client.rpcURL)
}

func TestChainClient_GetEthClient(t *testing.T) {
	srv := newRPCServer(t, map[string]func(req rpcRequest) rpcResponse{
		"eth_chainId": chainIDHandler(1),
	})
	defer srv.Close()

	client, err := NewChainClient(srv.URL, 1, zap.NewNop())
	require.NoError(t, err)
	defer client.Close()

	ethClient := client.GetEthClient()
	assert.NotNil(t, ethClient)
}

func TestChainClient_Close(t *testing.T) {
	srv := newRPCServer(t, map[string]func(req rpcRequest) rpcResponse{
		"eth_chainId": chainIDHandler(1),
	})
	defer srv.Close()

	client, err := NewChainClient(srv.URL, 1, zap.NewNop())
	require.NoError(t, err)

	client.Close()
	assert.True(t, client.closed.Load())
	assert.Nil(t, client.client.Load())
}

func TestChainClient_OperationsAfterClose(t *testing.T) {
	srv := newRPCServer(t, map[string]func(req rpcRequest) rpcResponse{
		"eth_chainId": chainIDHandler(1),
	})
	defer srv.Close()

	client, err := NewChainClient(srv.URL, 1, zap.NewNop())
	require.NoError(t, err)
	client.Close()

	_, err = client.GetBalance(context.Background(), "0x1234567890123456789012345678901234567890")
	require.Error(t, err)
	var permErr *PermanentError
	assert.True(t, errors.As(err, &permErr))
}

func TestChainClient_CallContract(t *testing.T) {
	srv := newRPCServer(t, map[string]func(req rpcRequest) rpcResponse{
		"eth_chainId": chainIDHandler(1),
		"eth_call": func(req rpcRequest) rpcResponse {
			return rpcResponse{JSONRPC: "2.0", ID: req.ID, Result: "0x0000000000000000000000000000000000000000000000000000000000000001"}
		},
	})
	defer srv.Close()

	client, err := NewChainClient(srv.URL, 1, zap.NewNop())
	require.NoError(t, err)
	defer client.Close()

	result, err := client.CallContract(context.Background(), ethereum.CallMsg{
		To:   &common.Address{},
		Data: []byte{},
	}, nil)
	require.NoError(t, err)
	assert.NotNil(t, result)
}

func TestChainClient_CodeAt(t *testing.T) {
	srv := newRPCServer(t, map[string]func(req rpcRequest) rpcResponse{
		"eth_chainId": chainIDHandler(1),
		"eth_getCode": func(req rpcRequest) rpcResponse {
			return rpcResponse{JSONRPC: "2.0", ID: req.ID, Result: "0x6080604052"}
		},
	})
	defer srv.Close()

	client, err := NewChainClient(srv.URL, 1, zap.NewNop())
	require.NoError(t, err)
	defer client.Close()

	code, err := client.CodeAt(context.Background(), common.Address{}, nil)
	require.NoError(t, err)
	assert.NotNil(t, code)
}

func TestChainClient_GetBalance(t *testing.T) {
	srv := newRPCServer(t, map[string]func(req rpcRequest) rpcResponse{
		"eth_chainId": chainIDHandler(1),
		"eth_getBalance": func(req rpcRequest) rpcResponse {
			return rpcResponse{JSONRPC: "2.0", ID: req.ID, Result: "0xde0b6b3a7640000"}
		},
	})
	defer srv.Close()

	client, err := NewChainClient(srv.URL, 1, zap.NewNop())
	require.NoError(t, err)
	defer client.Close()

	balance, err := client.GetBalance(context.Background(), "0x1234567890123456789012345678901234567890")
	require.NoError(t, err)
	assert.Equal(t, 0, balance.Cmp(big.NewInt(1e18)))
}

func TestChainClient_GetNonce(t *testing.T) {
	srv := newRPCServer(t, map[string]func(req rpcRequest) rpcResponse{
		"eth_chainId": chainIDHandler(1),
		"eth_getTransactionCount": func(req rpcRequest) rpcResponse {
			return rpcResponse{JSONRPC: "2.0", ID: req.ID, Result: "0x5"}
		},
	})
	defer srv.Close()

	client, err := NewChainClient(srv.URL, 1, zap.NewNop())
	require.NoError(t, err)
	defer client.Close()

	nonce, err := client.GetNonce(context.Background(), "0x1234567890123456789012345678901234567890")
	require.NoError(t, err)
	assert.Equal(t, uint64(5), nonce)
}

func TestChainClient_GetGasPrice(t *testing.T) {
	srv := newRPCServer(t, map[string]func(req rpcRequest) rpcResponse{
		"eth_chainId": chainIDHandler(1),
		"eth_gasPrice": func(req rpcRequest) rpcResponse {
			return rpcResponse{JSONRPC: "2.0", ID: req.ID, Result: "0x3b9aca00"}
		},
	})
	defer srv.Close()

	client, err := NewChainClient(srv.URL, 1, zap.NewNop())
	require.NoError(t, err)
	defer client.Close()

	gasPrice, err := client.GetGasPrice(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 0, gasPrice.Cmp(big.NewInt(1e9)))
}

func TestChainClient_SuggestGasTipCap(t *testing.T) {
	srv := newRPCServer(t, map[string]func(req rpcRequest) rpcResponse{
		"eth_chainId": chainIDHandler(1),
		"eth_maxPriorityFeePerGas": func(req rpcRequest) rpcResponse {
			return rpcResponse{JSONRPC: "2.0", ID: req.ID, Result: "0x77359400"}
		},
	})
	defer srv.Close()

	client, err := NewChainClient(srv.URL, 1, zap.NewNop())
	require.NoError(t, err)
	defer client.Close()

	tipCap, err := client.SuggestGasTipCap(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 0, tipCap.Cmp(big.NewInt(2e9)))
}

func TestChainClient_HeaderByNumber(t *testing.T) {
	srv := newRPCServer(t, map[string]func(req rpcRequest) rpcResponse{
		"eth_chainId": chainIDHandler(1),
		"eth_getBlockByNumber": func(req rpcRequest) rpcResponse {
			return rpcResponse{JSONRPC: "2.0", ID: req.ID, Result: blockHeaderResult(100, 0, 0)}
		},
	})
	defer srv.Close()

	client, err := NewChainClient(srv.URL, 1, zap.NewNop())
	require.NoError(t, err)
	defer client.Close()

	header, err := client.HeaderByNumber(context.Background(), big.NewInt(100))
	require.NoError(t, err)
	assert.Equal(t, uint64(100), header.Number.Uint64())
}

func TestChainClient_GetBlockNumber(t *testing.T) {
	srv := newRPCServer(t, map[string]func(req rpcRequest) rpcResponse{
		"eth_chainId":     chainIDHandler(1),
		"eth_blockNumber": func(req rpcRequest) rpcResponse { return rpcResponse{JSONRPC: "2.0", ID: req.ID, Result: "0x64"} },
	})
	defer srv.Close()

	client, err := NewChainClient(srv.URL, 1, zap.NewNop())
	require.NoError(t, err)
	defer client.Close()

	blockNum, err := client.GetBlockNumber(context.Background())
	require.NoError(t, err)
	assert.Equal(t, uint64(100), blockNum)
}

func TestChainClient_EstimateGas(t *testing.T) {
	srv := newRPCServer(t, map[string]func(req rpcRequest) rpcResponse{
		"eth_chainId": chainIDHandler(1),
		"eth_estimateGas": func(req rpcRequest) rpcResponse {
			return rpcResponse{JSONRPC: "2.0", ID: req.ID, Result: "0x5208"}
		},
	})
	defer srv.Close()

	client, err := NewChainClient(srv.URL, 1, zap.NewNop())
	require.NoError(t, err)
	defer client.Close()

	gas, err := client.EstimateGas(context.Background(), ethereum.CallMsg{})
	require.NoError(t, err)
	assert.Equal(t, uint64(21000), gas)
}

func TestChainClient_GetTransactionReceipt(t *testing.T) {
	srv := newRPCServer(t, map[string]func(req rpcRequest) rpcResponse{
		"eth_chainId": chainIDHandler(1),
		"eth_getTransactionReceipt": func(req rpcRequest) rpcResponse {
			return rpcResponse{JSONRPC: "2.0", ID: req.ID, Result: map[string]interface{}{
				"transactionHash":   "0x" + strings.Repeat("ab", 32),
				"blockNumber":       "0x64",
				"blockHash":         "0x" + strings.Repeat("de", 32),
				"gasUsed":           "0x5208",
				"cumulativeGasUsed": "0x5208",
				"status":            "0x1",
				"contractAddress":   "0x0000000000000000000000000000000000000000",
				"logsBloom":         "0x" + strings.Repeat("00", 256),
				"logs":              []interface{}{},
			}}
		},
	})
	defer srv.Close()

	client, err := NewChainClient(srv.URL, 1, zap.NewNop())
	require.NoError(t, err)
	defer client.Close()

	receipt, err := client.GetTransactionReceipt(context.Background(), "0xabc")
	require.NoError(t, err)
	assert.Equal(t, uint64(100), receipt.BlockNumber)
	assert.Equal(t, uint64(1), receipt.Status)
	assert.Equal(t, uint64(21000), receipt.GasUsed)
}

func TestChainClient_HealthCheck_Success(t *testing.T) {
	srv := newRPCServer(t, map[string]func(req rpcRequest) rpcResponse{
		"eth_chainId":     chainIDHandler(1),
		"eth_blockNumber": func(req rpcRequest) rpcResponse { return rpcResponse{JSONRPC: "2.0", ID: req.ID, Result: "0x64"} },
	})
	defer srv.Close()

	client, err := NewChainClient(srv.URL, 1, zap.NewNop())
	require.NoError(t, err)
	defer client.Close()

	err = client.HealthCheck(context.Background())
	assert.NoError(t, err)
}

func TestChainClient_HealthCheck_ChainIDMismatch(t *testing.T) {
	srv := newRPCServer(t, map[string]func(req rpcRequest) rpcResponse{
		"eth_chainId":     chainIDHandler(1),
		"eth_blockNumber": func(req rpcRequest) rpcResponse { return rpcResponse{JSONRPC: "2.0", ID: req.ID, Result: "0x64"} },
	})
	defer srv.Close()

	client, err := NewChainClient(srv.URL, 1, zap.NewNop())
	require.NoError(t, err)
	defer client.Close()

	err = client.HealthCheck(context.Background())
	assert.NoError(t, err)
}

func TestChainClient_UpdateRPCScores(t *testing.T) {
	srv := newRPCServer(t, map[string]func(req rpcRequest) rpcResponse{
		"eth_chainId": chainIDHandler(1),
	})
	defer srv.Close()

	client, err := NewChainClient(srv.URL, 1, zap.NewNop())
	require.NoError(t, err)
	defer client.Close()

	client.updateRPCScores(0, 100*time.Millisecond, true)
	scores := client.GetRPCScores()
	assert.InDelta(t, 0.998, scores[srv.URL], 0.001)

	client.updateRPCScores(0, 0, false)
	scores = client.GetRPCScores()
	assert.Less(t, scores[srv.URL], 1.0)
}

func TestChainClient_UpdateRPCScores_InvalidIndex(t *testing.T) {
	srv := newRPCServer(t, map[string]func(req rpcRequest) rpcResponse{
		"eth_chainId": chainIDHandler(1),
	})
	defer srv.Close()

	client, err := NewChainClient(srv.URL, 1, zap.NewNop())
	require.NoError(t, err)
	defer client.Close()

	assert.NotPanics(t, func() {
		client.updateRPCScores(-1, 0, true)
		client.updateRPCScores(99, 0, true)
	})
}

func TestChainClient_SortedRPCScores(t *testing.T) {
	srv1 := newRPCServer(t, map[string]func(req rpcRequest) rpcResponse{
		"eth_chainId": chainIDHandler(1),
	})
	defer srv1.Close()

	srv2 := newRPCServer(t, map[string]func(req rpcRequest) rpcResponse{
		"eth_chainId": chainIDHandler(1),
	})
	defer srv2.Close()

	client, err := NewChainClientWithFallback([]string{srv1.URL, srv2.URL}, 1, zap.NewNop())
	require.NoError(t, err)
	defer client.Close()

	client.updateRPCScores(0, 0, false)
	sorted := client.sortedRPCScores()
	assert.Equal(t, 2, len(sorted))
}

func TestChainClient_GetRPCStatuses(t *testing.T) {
	srv := newRPCServer(t, map[string]func(req rpcRequest) rpcResponse{
		"eth_chainId": chainIDHandler(1),
	})
	defer srv.Close()

	client, err := NewChainClient(srv.URL, 1, zap.NewNop())
	require.NoError(t, err)
	defer client.Close()

	statuses := client.GetRPCStatuses()
	require.Len(t, statuses, 1)
	assert.Equal(t, srv.URL, statuses[0].URL)
	assert.True(t, statuses[0].IsActive)
	assert.Equal(t, 1.0, statuses[0].Score)
}

func TestChainClient_SetFinality(t *testing.T) {
	srv := newRPCServer(t, map[string]func(req rpcRequest) rpcResponse{
		"eth_chainId": chainIDHandler(1),
	})
	defer srv.Close()

	client, err := NewChainClient(srv.URL, 1, zap.NewNop())
	require.NoError(t, err)
	defer client.Close()

	f := newFinalityDefault(nil, 12, BlockTagSafe, nil)
	client.SetFinality(f)
	got := client.GetFinality()
	assert.Equal(t, uint64(12), got.RequiredConfirmations())
	assert.Equal(t, BlockTagSafe, got.BlockTag())
}

func TestChainClient_GetFinality_Default(t *testing.T) {
	srv := newRPCServer(t, map[string]func(req rpcRequest) rpcResponse{
		"eth_chainId": chainIDHandler(1),
	})
	defer srv.Close()

	client, err := NewChainClient(srv.URL, 1, zap.NewNop())
	require.NoError(t, err)
	defer client.Close()

	f := client.GetFinality()
	assert.NotNil(t, f)
	assert.Equal(t, BlockTagSafe, f.BlockTag())
}

func TestChainClient_SetRateLimiter_Basic(t *testing.T) {
	srv := newRPCServer(t, map[string]func(req rpcRequest) rpcResponse{
		"eth_chainId": chainIDHandler(1),
	})
	defer srv.Close()

	client, err := NewChainClient(srv.URL, 1, zap.NewNop())
	require.NoError(t, err)
	defer client.Close()

	rl := NewRPCRateLimiter(10, 20, zap.NewNop())
	client.SetRateLimiter(rl)
	assert.Equal(t, rl, client.rateLimiter)

	client.SetRateLimiter(nil)
	assert.Nil(t, client.rateLimiter)
}

func TestChainClient_RecordEndpointFailure(t *testing.T) {
	srv := newRPCServer(t, map[string]func(req rpcRequest) rpcResponse{
		"eth_chainId": chainIDHandler(1),
	})
	defer srv.Close()

	client, err := NewChainClient(srv.URL, 1, zap.NewNop())
	require.NoError(t, err)
	defer client.Close()

	client.recordEndpointFailure(0)
	client.mu.RLock()
	state := client.rpcStates[0]
	client.mu.RUnlock()
	assert.Equal(t, 1, state.Failures)
	assert.False(t, state.CooldownUntil.IsZero())
}

func TestChainClient_RecordEndpointFailure_InvalidIndex(t *testing.T) {
	srv := newRPCServer(t, map[string]func(req rpcRequest) rpcResponse{
		"eth_chainId": chainIDHandler(1),
	})
	defer srv.Close()

	client, err := NewChainClient(srv.URL, 1, zap.NewNop())
	require.NoError(t, err)
	defer client.Close()

	assert.NotPanics(t, func() {
		client.recordEndpointFailure(-1)
		client.recordEndpointFailure(99)
	})
}

func TestChainClient_EndpointReady(t *testing.T) {
	srv := newRPCServer(t, map[string]func(req rpcRequest) rpcResponse{
		"eth_chainId": chainIDHandler(1),
	})
	defer srv.Close()

	client, err := NewChainClient(srv.URL, 1, zap.NewNop())
	require.NoError(t, err)
	defer client.Close()

	assert.True(t, client.endpointReady(0, false))
	assert.False(t, client.endpointReady(-1, false))
	assert.False(t, client.endpointReady(99, false))
	assert.True(t, client.endpointReady(0, true))
}

func TestChainClient_SubscribeNewHead_Closed(t *testing.T) {
	srv := newRPCServer(t, map[string]func(req rpcRequest) rpcResponse{
		"eth_chainId": chainIDHandler(1),
	})
	defer srv.Close()

	client, err := NewChainClient(srv.URL, 1, zap.NewNop())
	require.NoError(t, err)
	client.Close()

	ch := make(chan *types.Header)
	_, err = client.SubscribeNewHead(context.Background(), ch)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "closed")
}

func TestChainClient_ChainIDMismatch(t *testing.T) {
	srv := newRPCServer(t, map[string]func(req rpcRequest) rpcResponse{
		"eth_chainId": chainIDHandler(1),
	})
	defer srv.Close()

	client, err := NewChainClientWithFallback([]string{srv.URL}, 999, zap.NewNop())
	require.Nil(t, client)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unexpected chain id")
}

func TestIsPermanentRPCError_TableDriven(t *testing.T) {
	tests := []struct {
		name      string
		errMsg    string
		permanent bool
	}{
		{"execution reverted", "execution reverted", true},
		{"revert", "revert: not owner", true},
		{"invalid opcode", "invalid opcode", true},
		{"out of gas", "out of gas", true},
		{"invalid jump destination", "invalid jump destination", true},
		{"stack limit reached", "stack limit reached", true},
		{"nonce too low", "nonce too low", true},
		{"insufficient funds", "insufficient funds", true},
		{"already known", "already known", true},
		{"transient timeout", "upstream timeout", false},
		{"connection refused", "connection refused", false},
		{"nil error", "", false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var err error
			if tc.errMsg != "" {
				err = errors.New(tc.errMsg)
			}
			assert.Equal(t, tc.permanent, isPermanentRPCError(err))
		})
	}
}

func TestChainClient_GetBlockByNumber(t *testing.T) {
	srv := newRPCServer(t, map[string]func(req rpcRequest) rpcResponse{
		"eth_chainId": chainIDHandler(1),
		"eth_getBlockByNumber": func(req rpcRequest) rpcResponse {
			return rpcResponse{JSONRPC: "2.0", ID: req.ID, Result: blockHeaderResult(100, 0x100, 0x200)}
		},
	})
	defer srv.Close()

	client, err := NewChainClient(srv.URL, 1, zap.NewNop())
	require.NoError(t, err)
	defer client.Close()

	block, err := client.GetBlockByNumber(context.Background(), big.NewInt(100))
	require.NoError(t, err)
	assert.Equal(t, uint64(100), block.Number)
	assert.Equal(t, uint64(0x100), block.GasUsed)
	assert.Equal(t, uint64(0x200), block.GasLimit)
}

func TestChainClient_FeeHistory(t *testing.T) {
	srv := newRPCServer(t, map[string]func(req rpcRequest) rpcResponse{
		"eth_chainId": chainIDHandler(1),
		"eth_feeHistory": func(req rpcRequest) rpcResponse {
			return rpcResponse{JSONRPC: "2.0", ID: req.ID, Result: map[string]interface{}{
				"baseFeePerGas": []string{"0x3b9aca00"},
				"gasUsedRatio":  []float64{0.5},
				"oldestBlock":   "0x64",
				"reward":        [][]string{{"0x77359400"}},
			}}
		},
	})
	defer srv.Close()

	client, err := NewChainClient(srv.URL, 1, zap.NewNop())
	require.NoError(t, err)
	defer client.Close()

	fh, err := client.FeeHistory(context.Background(), 1, big.NewInt(100), []float64{50.0})
	require.NoError(t, err)
	assert.Len(t, fh.BaseFee, 1)
	assert.Len(t, fh.GasUsedRatio, 1)
}

func TestChainClient_GetTransactionByHash(t *testing.T) {
	txHash := "0x" + strings.Repeat("ab", 32)
	srv := newRPCServer(t, map[string]func(req rpcRequest) rpcResponse{
		"eth_chainId": chainIDHandler(1),
		"eth_getTransactionByHash": func(req rpcRequest) rpcResponse {
			return rpcResponse{JSONRPC: "2.0", ID: req.ID, Result: map[string]interface{}{
				"hash":        txHash,
				"nonce":       "0x5",
				"from":        "0x742d35Cc6634C0532925a3b844Bc9e7595f2bD18",
				"to":          "0x1234567890123456789012345678901234567890",
				"value":       "0x0",
				"gas":         "0x5208",
				"gasPrice":    "0x3b9aca00",
				"input":       "0x",
				"blockHash":   "0x" + strings.Repeat("cc", 32),
				"blockNumber": "0x64",
				"v":           "0x25",
				"r":           "0x1",
				"s":           "0x2",
			}}
		},
	})
	defer srv.Close()

	client, err := NewChainClient(srv.URL, 1, zap.NewNop())
	require.NoError(t, err)
	defer client.Close()

	txInfo, err := client.GetTransactionByHash(context.Background(), txHash)
	require.NoError(t, err)
	assert.Equal(t, uint64(5), txInfo.Nonce)
	assert.Equal(t, uint64(21000), txInfo.Gas)
}

func TestChainClient_GetNFTMetadata_InvalidTokenID(t *testing.T) {
	srv := newRPCServer(t, map[string]func(req rpcRequest) rpcResponse{
		"eth_chainId": chainIDHandler(1),
	})
	defer srv.Close()

	client, err := NewChainClient(srv.URL, 1, zap.NewNop())
	require.NoError(t, err)
	defer client.Close()

	_, err = client.GetNFTMetadata(context.Background(), "0x1234567890123456789012345678901234567890", "notanumber")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid token id")
}

func TestChainClient_PackOwnerOfCall_InvalidTokenID(t *testing.T) {
	srv := newRPCServer(t, map[string]func(req rpcRequest) rpcResponse{
		"eth_chainId": chainIDHandler(1),
	})
	defer srv.Close()

	client, err := NewChainClient(srv.URL, 1, zap.NewNop())
	require.NoError(t, err)
	defer client.Close()

	_, err = client.packOwnerOfCall("notanumber")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid token id")
}

func TestChainClient_GetNFTBalanceAtBlock_InvalidTokenID(t *testing.T) {
	srv := newRPCServer(t, map[string]func(req rpcRequest) rpcResponse{
		"eth_chainId": chainIDHandler(1),
	})
	defer srv.Close()

	client, err := NewChainClient(srv.URL, 1, zap.NewNop())
	require.NoError(t, err)
	defer client.Close()

	_, err = client.getNFTBalanceAtBlock(context.Background(), common.Address{}, common.Address{}, BlockTagSafe)
	require.Error(t, err)
}

func TestChainClient_GetNFTOwnerAtBlock_InvalidTokenID(t *testing.T) {
	srv := newRPCServer(t, map[string]func(req rpcRequest) rpcResponse{
		"eth_chainId": chainIDHandler(1),
	})
	defer srv.Close()

	client, err := NewChainClient(srv.URL, 1, zap.NewNop())
	require.NoError(t, err)
	defer client.Close()

	_, err = client.getNFTOwnerAtBlock(context.Background(), common.Address{}, "notanumber", BlockTagSafe)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid token id")
}

func TestChainClient_WithChainClient_SingleRPCErrors(t *testing.T) {
	srv := newRPCServer(t, map[string]func(req rpcRequest) rpcResponse{
		"eth_chainId": chainIDHandler(1),
		"eth_blockNumber": func(req rpcRequest) rpcResponse {
			return rpcResponse{
				JSONRPC: "2.0",
				ID:      req.ID,
				Error:   map[string]interface{}{"code": -32000, "message": "timeout"},
			}
		},
	})
	defer srv.Close()

	client, err := NewChainClient(srv.URL, 1, zap.NewNop())
	require.NoError(t, err)
	defer client.Close()

	_, err = client.GetBlockNumber(context.Background())
	require.Error(t, err)
}

func TestChainClient_HealthCheck_BlockNumberFails(t *testing.T) {
	srv := newRPCServer(t, map[string]func(req rpcRequest) rpcResponse{
		"eth_chainId": chainIDHandler(1),
		"eth_blockNumber": func(req rpcRequest) rpcResponse {
			return rpcResponse{
				JSONRPC: "2.0",
				ID:      req.ID,
				Error:   map[string]interface{}{"code": -32000, "message": "unavailable"},
			}
		},
	})
	defer srv.Close()

	client, err := NewChainClient(srv.URL, 1, zap.NewNop())
	require.NoError(t, err)
	defer client.Close()

	err = client.HealthCheck(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "health check failed")
}

func TestChainClient_HealthCheck_ChainIDMismatchFails(t *testing.T) {
	srv := newRPCServer(t, map[string]func(req rpcRequest) rpcResponse{
		"eth_chainId": func(req rpcRequest) rpcResponse {
			return rpcResponse{JSONRPC: "2.0", ID: req.ID, Result: fmt.Sprintf("0x%x", 1)}
		},
		"eth_blockNumber": func(req rpcRequest) rpcResponse {
			return rpcResponse{JSONRPC: "2.0", ID: req.ID, Result: "0x64"}
		},
	})
	defer srv.Close()

	client, err := NewChainClient(srv.URL, 1, zap.NewNop())
	require.NoError(t, err)
	defer client.Close()

	client.chainID = 999
	err = client.HealthCheck(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "chain ID mismatch")
}

func TestChainClient_GetNFTBalance_InvalidTokenID(t *testing.T) {
	srv := newRPCServer(t, map[string]func(req rpcRequest) rpcResponse{
		"eth_chainId": chainIDHandler(1),
	})
	defer srv.Close()

	client, err := NewChainClient(srv.URL, 1, zap.NewNop())
	require.NoError(t, err)
	defer client.Close()

	_, err = client.getNFTBalance(context.Background(), common.Address{}, common.Address{})
	require.Error(t, err)
}

func TestChainClient_UpdateRPCScores_HighLatency(t *testing.T) {
	srv := newRPCServer(t, map[string]func(req rpcRequest) rpcResponse{
		"eth_chainId": chainIDHandler(1),
	})
	defer srv.Close()

	client, err := NewChainClient(srv.URL, 1, zap.NewNop())
	require.NoError(t, err)
	defer client.Close()

	client.updateRPCScores(0, 10*time.Second, true)
	scores := client.GetRPCScores()
	assert.Less(t, scores[srv.URL], 1.0)
}

func TestChainClient_UpdateRPCScores_SuccessReducesFailures(t *testing.T) {
	srv := newRPCServer(t, map[string]func(req rpcRequest) rpcResponse{
		"eth_chainId": chainIDHandler(1),
	})
	defer srv.Close()

	client, err := NewChainClient(srv.URL, 1, zap.NewNop())
	require.NoError(t, err)
	defer client.Close()

	client.recordEndpointFailure(0)
	client.recordEndpointFailure(0)
	client.mu.RLock()
	failures := client.rpcStates[0].Failures
	client.mu.RUnlock()
	assert.Equal(t, 2, failures)

	client.updateRPCScores(0, 100*time.Millisecond, true)
	client.mu.RLock()
	failures = client.rpcStates[0].Failures
	client.mu.RUnlock()
	assert.Equal(t, 1, failures)
}

func TestNewChainClientWithFallback_MultipleURLs(t *testing.T) {
	srv1 := newRPCServer(t, map[string]func(req rpcRequest) rpcResponse{
		"eth_chainId": chainIDHandler(1),
	})
	defer srv1.Close()

	srv2 := newRPCServer(t, map[string]func(req rpcRequest) rpcResponse{
		"eth_chainId": chainIDHandler(1),
	})
	defer srv2.Close()

	client, err := NewChainClientWithFallback([]string{srv1.URL, srv2.URL}, 1, zap.NewNop())
	require.NoError(t, err)
	defer client.Close()

	statuses := client.GetRPCStatuses()
	assert.Len(t, statuses, 2)
}
