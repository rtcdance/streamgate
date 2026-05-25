package web3

import (
	"context"
	"math/big"
	"strings"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func TestChainClient_SendTransaction_Closed(t *testing.T) {
	srv := newRPCServer(t, map[string]func(req rpcRequest) rpcResponse{
		"eth_chainId": chainIDHandler(1),
	})
	defer srv.Close()

	client, err := NewChainClient(srv.URL, 1, zap.NewNop())
	require.NoError(t, err)
	client.Close()

	tx := types.NewTransaction(0, common.Address{}, big.NewInt(0), 21000, big.NewInt(1), nil)
	err = client.SendTransaction(context.Background(), tx)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "chain client closed")
}

func TestChainClient_TransactionReceipt(t *testing.T) {
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

	receipt, err := client.TransactionReceipt(context.Background(), common.HexToHash("0xabc"))
	require.NoError(t, err)
	assert.Equal(t, uint64(100), receipt.BlockNumber.Uint64())
}

func TestChainClient_IsTxPending_NoClient(t *testing.T) {
	cc := &ChainClient{logger: zap.NewNop()}
	_, err := cc.isTxPending(context.Background(), common.Hash{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no client available")
}

func TestChainClient_SendTransaction_NoRPC(t *testing.T) {
	cc := &ChainClient{
		rpcURLs:   []string{},
		rpcStates: []rpcEndpointState{},
		logger:    zap.NewNop(),
	}
	tx := types.NewTransaction(0, common.Address{}, big.NewInt(0), 21000, big.NewInt(1), nil)
	err := cc.SendTransaction(context.Background(), tx)
	require.Error(t, err)
}

func TestChainClient_ParseReceiptEvents_NoReceipt(t *testing.T) {
	srv := newRPCServer(t, map[string]func(req rpcRequest) rpcResponse{
		"eth_chainId": chainIDHandler(1),
		"eth_getTransactionReceipt": func(req rpcRequest) rpcResponse {
			return rpcResponse{
				JSONRPC: "2.0",
				ID:      req.ID,
				Error:   map[string]interface{}{"code": -32000, "message": "not found"},
			}
		},
	})
	defer srv.Close()

	client, err := NewChainClient(srv.URL, 1, zap.NewNop())
	require.NoError(t, err)
	defer client.Close()

	receipt := &ReceiptInfo{TransactionHash: "0x" + strings.Repeat("ab", 32)}
	err = client.ParseReceiptEvents(context.Background(), receipt, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to fetch receipt")
}

func TestChainClient_SendTransaction_Success(t *testing.T) {
	privateKey, err := crypto.GenerateKey()
	require.NoError(t, err)

	srv := newRPCServer(t, map[string]func(req rpcRequest) rpcResponse{
		"eth_chainId":     chainIDHandler(1),
		"eth_sendRawTransaction": func(req rpcRequest) rpcResponse {
			return rpcResponse{JSONRPC: "2.0", ID: req.ID, Result: "0x" + strings.Repeat("00", 32)}
		},
	})
	defer srv.Close()

	client, err := NewChainClient(srv.URL, 1, zap.NewNop())
	require.NoError(t, err)
	defer client.Close()

	signer := types.LatestSignerForChainID(big.NewInt(1))
	signedTx, err := types.SignTx(types.NewTransaction(0, common.Address{}, big.NewInt(0), 21000, big.NewInt(1), nil), signer, privateKey)
	require.NoError(t, err)

	err = client.SendTransaction(context.Background(), signedTx)
	require.NoError(t, err)
}

func TestChainClient_SendTransaction_RPCError(t *testing.T) {
	privateKey, err := crypto.GenerateKey()
	require.NoError(t, err)

	srv := newRPCServer(t, map[string]func(req rpcRequest) rpcResponse{
		"eth_chainId": chainIDHandler(1),
		"eth_sendRawTransaction": func(req rpcRequest) rpcResponse {
			return rpcResponse{
				JSONRPC: "2.0",
				ID:      req.ID,
				Error:   map[string]interface{}{"code": -32000, "message": "nonce too low"},
			}
		},
		"eth_getTransactionByHash": func(req rpcRequest) rpcResponse {
			return rpcResponse{
				JSONRPC: "2.0",
				ID:      req.ID,
				Error:   map[string]interface{}{"code": -32000, "message": "not found"},
			}
		},
	})
	defer srv.Close()

	client, err := NewChainClient(srv.URL, 1, zap.NewNop())
	require.NoError(t, err)
	defer client.Close()

	signer := types.LatestSignerForChainID(big.NewInt(1))
	signedTx, err := types.SignTx(types.NewTransaction(0, common.Address{}, big.NewInt(0), 21000, big.NewInt(1), nil), signer, privateKey)
	require.NoError(t, err)

	err = client.SendTransaction(context.Background(), signedTx)
	require.Error(t, err)
}

func TestReceiptInfo_Fields(t *testing.T) {
	r := &ReceiptInfo{
		TransactionHash: "0xabc",
		BlockNumber:     100,
		BlockHash:       "0xdef",
		GasUsed:         21000,
		Status:          1,
		ContractAddress: "0x123",
		LogCount:        3,
	}
	assert.Equal(t, "0xabc", r.TransactionHash)
	assert.Equal(t, uint64(100), r.BlockNumber)
	assert.Equal(t, uint64(21000), r.GasUsed)
	assert.Equal(t, uint64(1), r.Status)
	assert.Equal(t, uint64(3), r.LogCount)
}
