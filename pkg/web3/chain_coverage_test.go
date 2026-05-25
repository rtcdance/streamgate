package web3

import (
	"context"
	"math/big"
	"strings"
	"testing"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func TestChainClient_VerifyNFTOwnershipByRequest_GatingAny(t *testing.T) {
	balanceData := "0x0000000000000000000000000000000000000000000000000000000000000001"
	srv := newRPCServer(t, map[string]func(req rpcRequest) rpcResponse{
		"eth_chainId": chainIDHandler(1),
		"eth_call": func(req rpcRequest) rpcResponse {
			return rpcResponse{JSONRPC: "2.0", ID: req.ID, Result: balanceData}
		},
	})
	defer srv.Close()

	client, err := NewChainClient(srv.URL, 1, zap.NewNop())
	require.NoError(t, err)
	defer client.Close()

	ok, err := client.VerifyNFTOwnershipByRequest(context.Background(), VerifyRequest{
		WalletAddress: "0x1234567890123456789012345678901234567890",
		Contract:      "0x8667b7bdf8f27e76200fa450bf48aa78bbbcc61f",
		Mode:          GatingAny,
	})
	require.NoError(t, err)
	assert.True(t, ok)
}

func TestChainClient_VerifyNFTOwnershipByRequest_GatingAny_ZeroBalance(t *testing.T) {
	balanceData := "0x0000000000000000000000000000000000000000000000000000000000000000"
	srv := newRPCServer(t, map[string]func(req rpcRequest) rpcResponse{
		"eth_chainId": chainIDHandler(1),
		"eth_call": func(req rpcRequest) rpcResponse {
			return rpcResponse{JSONRPC: "2.0", ID: req.ID, Result: balanceData}
		},
	})
	defer srv.Close()

	client, err := NewChainClient(srv.URL, 1, zap.NewNop())
	require.NoError(t, err)
	defer client.Close()

	ok, err := client.VerifyNFTOwnershipByRequest(context.Background(), VerifyRequest{
		WalletAddress: "0x1234567890123456789012345678901234567890",
		Contract:      "0x8667b7bdf8f27e76200fa450bf48aa78bbbcc61f",
		Mode:          GatingAny,
	})
	require.NoError(t, err)
	assert.False(t, ok)
}

func TestChainClient_VerifyNFTOwnershipByRequest_GatingMinBalance(t *testing.T) {
	balanceData := "0x0000000000000000000000000000000000000000000000000000000000000003"
	srv := newRPCServer(t, map[string]func(req rpcRequest) rpcResponse{
		"eth_chainId": chainIDHandler(1),
		"eth_call": func(req rpcRequest) rpcResponse {
			return rpcResponse{JSONRPC: "2.0", ID: req.ID, Result: balanceData}
		},
	})
	defer srv.Close()

	client, err := NewChainClient(srv.URL, 1, zap.NewNop())
	require.NoError(t, err)
	defer client.Close()

	ok, err := client.VerifyNFTOwnershipByRequest(context.Background(), VerifyRequest{
		WalletAddress: "0x1234567890123456789012345678901234567890",
		Contract:      "0x8667b7bdf8f27e76200fa450bf48aa78bbbcc61f",
		MinBalance:    2,
		Mode:          GatingMinBalance,
	})
	require.NoError(t, err)
	assert.True(t, ok)
}

func TestChainClient_VerifyNFTOwnershipByRequest_GatingSpecificID(t *testing.T) {
	ownerData := "0x0000000000000000000000001234567890123456789012345678901234567890"
	srv := newRPCServer(t, map[string]func(req rpcRequest) rpcResponse{
		"eth_chainId": chainIDHandler(1),
		"eth_call": func(req rpcRequest) rpcResponse {
			return rpcResponse{JSONRPC: "2.0", ID: req.ID, Result: ownerData}
		},
	})
	defer srv.Close()

	client, err := NewChainClient(srv.URL, 1, zap.NewNop())
	require.NoError(t, err)
	defer client.Close()

	ok, err := client.VerifyNFTOwnershipByRequest(context.Background(), VerifyRequest{
		WalletAddress: "0x1234567890123456789012345678901234567890",
		Contract:      "0x8667b7bdf8f27e76200fa450bf48aa78bbbcc61f",
		TokenID:       "42",
		Mode:          GatingSpecificID,
	})
	require.NoError(t, err)
	assert.True(t, ok)
}

func TestChainClient_VerifyNFTOwnershipByRequest_GatingCombination(t *testing.T) {
	balanceData := "0x0000000000000000000000000000000000000000000000000000000000000005"
	srv := newRPCServer(t, map[string]func(req rpcRequest) rpcResponse{
		"eth_chainId": chainIDHandler(1),
		"eth_call": func(req rpcRequest) rpcResponse {
			return rpcResponse{JSONRPC: "2.0", ID: req.ID, Result: balanceData}
		},
	})
	defer srv.Close()

	client, err := NewChainClient(srv.URL, 1, zap.NewNop())
	require.NoError(t, err)
	defer client.Close()

	ok, err := client.VerifyNFTOwnershipByRequest(context.Background(), VerifyRequest{
		WalletAddress: "0x1234567890123456789012345678901234567890",
		Contract:      "0x8667b7bdf8f27e76200fa450bf48aa78bbbcc61f",
		MinBalance:    3,
		Mode:          GatingCombination,
	})
	require.NoError(t, err)
	assert.True(t, ok)
}

func TestChainClient_VerifyNFTOwnershipByRequest_UnsupportedMode(t *testing.T) {
	srv := newRPCServer(t, map[string]func(req rpcRequest) rpcResponse{
		"eth_chainId": chainIDHandler(1),
	})
	defer srv.Close()

	client, err := NewChainClient(srv.URL, 1, zap.NewNop())
	require.NoError(t, err)
	defer client.Close()

	_, err = client.VerifyNFTOwnershipByRequest(context.Background(), VerifyRequest{
		WalletAddress: "0x1234567890123456789012345678901234567890",
		Contract:      "0x8667b7bdf8f27e76200fa450bf48aa78bbbcc61f",
		Mode:          GatingMode(99),
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unsupported gating mode")
}

func TestChainClient_VerifyNFTOwnershipByRequest_GatingSpecificID_InvalidTokenID(t *testing.T) {
	srv := newRPCServer(t, map[string]func(req rpcRequest) rpcResponse{
		"eth_chainId": chainIDHandler(1),
	})
	defer srv.Close()

	client, err := NewChainClient(srv.URL, 1, zap.NewNop())
	require.NoError(t, err)
	defer client.Close()

	_, err = client.VerifyNFTOwnershipByRequest(context.Background(), VerifyRequest{
		WalletAddress: "0x1234567890123456789012345678901234567890",
		Contract:      "0x8667b7bdf8f27e76200fa450bf48aa78bbbcc61f",
		TokenID:       "notanumber",
		Mode:          GatingSpecificID,
	})
	require.Error(t, err)
}

func TestChainClient_GetWalletNFTBalance(t *testing.T) {
	balanceData := "0x0000000000000000000000000000000000000000000000000000000000000003"
	srv := newRPCServer(t, map[string]func(req rpcRequest) rpcResponse{
		"eth_chainId": chainIDHandler(1),
		"eth_call": func(req rpcRequest) rpcResponse {
			return rpcResponse{JSONRPC: "2.0", ID: req.ID, Result: balanceData}
		},
	})
	defer srv.Close()

	client, err := NewChainClient(srv.URL, 1, zap.NewNop())
	require.NoError(t, err)
	defer client.Close()

	balance, err := client.GetWalletNFTBalance(context.Background(),
		"0x1234567890123456789012345678901234567890",
		"0x8667b7bdf8f27e76200fa450bf48aa78bbbcc61f")
	require.NoError(t, err)
	assert.Equal(t, big.NewInt(3), balance)
}

func TestChainClient_CallContractAtBlock_Latest(t *testing.T) {
	srv := newRPCServer(t, map[string]func(req rpcRequest) rpcResponse{
		"eth_chainId": chainIDHandler(1),
		"eth_call": func(req rpcRequest) rpcResponse {
			return rpcResponse{JSONRPC: "2.0", ID: req.ID, Result: "0x01"}
		},
	})
	defer srv.Close()

	client, err := NewChainClient(srv.URL, 1, zap.NewNop())
	require.NoError(t, err)
	defer client.Close()

	result, err := client.CallContractAtBlock(context.Background(), ethereum.CallMsg{
		To:   &common.Address{},
		Data: []byte{},
	}, BlockTagLatest)
	require.NoError(t, err)
	assert.NotNil(t, result)
}

func TestChainClient_CallContractAtBlock_Safe_Error(t *testing.T) {
	srv := newRPCServer(t, map[string]func(req rpcRequest) rpcResponse{
		"eth_chainId": chainIDHandler(1),
		"eth_call": func(req rpcRequest) rpcResponse {
			return rpcResponse{
				JSONRPC: "2.0",
				ID:      req.ID,
				Error:   map[string]interface{}{"code": -32000, "message": "unsupported block tag"},
			}
		},
	})
	defer srv.Close()

	client, err := NewChainClient(srv.URL, 1, zap.NewNop())
	require.NoError(t, err)
	defer client.Close()

	_, err = client.CallContractAtBlock(context.Background(), ethereum.CallMsg{
		To:   &common.Address{},
		Data: []byte{},
	}, BlockTagSafe)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "fallback to latest disabled")
}

func TestChainClient_VerifyNFTOwnership(t *testing.T) {
	srv := newRPCServer(t, map[string]func(req rpcRequest) rpcResponse{
		"eth_chainId": chainIDHandler(1),
		"eth_call": func(req rpcRequest) rpcResponse {
			return rpcResponse{JSONRPC: "2.0", ID: req.ID, Result: "0x0000000000000000000000001234567890123456789012345678901234567890"}
		},
	})
	defer srv.Close()

	client, err := NewChainClient(srv.URL, 1, zap.NewNop())
	require.NoError(t, err)
	defer client.Close()

	ok, err := client.VerifyNFTOwnership(context.Background(),
		"0x8667b7bdf8f27e76200fa450bf48aa78bbbcc61f",
		"1",
		"0x1234567890123456789012345678901234567890")
	require.NoError(t, err)
	assert.True(t, ok)
}

func TestChainClient_VerifyNFTOwnershipAutoDetect(t *testing.T) {
	balanceData := "0x0000000000000000000000000000000000000000000000000000000000000001"
	srv := newRPCServer(t, map[string]func(req rpcRequest) rpcResponse{
		"eth_chainId": chainIDHandler(1),
		"eth_call": func(req rpcRequest) rpcResponse {
			return rpcResponse{JSONRPC: "2.0", ID: req.ID, Result: balanceData}
		},
	})
	defer srv.Close()

	client, err := NewChainClient(srv.URL, 1, zap.NewNop())
	require.NoError(t, err)
	defer client.Close()

	ok, err := client.VerifyNFTOwnershipAutoDetect(context.Background(),
		"0x8667b7bdf8f27e76200fa450bf48aa78bbbcc61f",
		"1",
		"0x1234567890123456789012345678901234567890")
	require.NoError(t, err)
	assert.True(t, ok)
}

func TestChainClient_VerifyNFTCollectionAutoDetect(t *testing.T) {
	balanceData := "0x0000000000000000000000000000000000000000000000000000000000000001"
	srv := newRPCServer(t, map[string]func(req rpcRequest) rpcResponse{
		"eth_chainId": chainIDHandler(1),
		"eth_call": func(req rpcRequest) rpcResponse {
			return rpcResponse{JSONRPC: "2.0", ID: req.ID, Result: balanceData}
		},
	})
	defer srv.Close()

	client, err := NewChainClient(srv.URL, 1, zap.NewNop())
	require.NoError(t, err)
	defer client.Close()

	_, err = client.VerifyNFTCollectionAutoDetect(context.Background(),
		"0x8667b7bdf8f27e76200fa450bf48aa78bbbcc61f",
		"0x1234567890123456789012345678901234567890")
	assert.Error(t, err)
}

func TestGatingMode_Values(t *testing.T) {
	assert.Equal(t, GatingMode(0), GatingAny)
	assert.Equal(t, GatingMode(1), GatingMinBalance)
	assert.Equal(t, GatingMode(2), GatingSpecificID)
	assert.Equal(t, GatingMode(3), GatingCombination)
}

func TestVerifyRequest_Fields(t *testing.T) {
	req := VerifyRequest{
		WalletAddress: "0xabc",
		Contract:      "0xdef",
		TokenID:       "42",
		MinBalance:    5,
		Mode:          GatingMinBalance,
	}
	assert.Equal(t, "0xabc", req.WalletAddress)
	assert.Equal(t, "0xdef", req.Contract)
	assert.Equal(t, "42", req.TokenID)
	assert.Equal(t, 5, req.MinBalance)
	assert.Equal(t, GatingMinBalance, req.Mode)
}

func TestBlockInfo_Fields(t *testing.T) {
	bi := &BlockInfo{
		Number:       100,
		Hash:         "0xaaa",
		ParentHash:   "0x999",
		Timestamp:    1234567890,
		Miner:        "0xminer",
		GasUsed:      1000,
		GasLimit:     2000,
		Difficulty:   "100",
		Transactions: 5,
	}
	assert.Equal(t, uint64(100), bi.Number)
	assert.Equal(t, uint64(1000), bi.GasUsed)
	assert.Equal(t, uint64(5), bi.Transactions)
}

func TestTransactionInfo_Fields(t *testing.T) {
	ti := &TransactionInfo{
		Hash:      "0xabc",
		From:      "0xfrom",
		To:        "0xto",
		Value:     "1000000000000000000",
		Gas:       21000,
		GasPrice:  "1000000000",
		Nonce:     5,
		Data:      "0x",
		IsPending: false,
	}
	assert.Equal(t, "0xabc", ti.Hash)
	assert.Equal(t, uint64(21000), ti.Gas)
	assert.Equal(t, uint64(5), ti.Nonce)
}

func TestNFTMetadata_Fields(t *testing.T) {
	m := &NFTMetadata{
		Name:        "Test NFT",
		Description: "A test",
		Image:       "https://example.com/img.png",
		Attributes: []NFTAttribute{
			{TraitType: "color", Value: "blue"},
		},
		ContractAddress: "0xabc",
		TokenID:         "1",
	}
	assert.Equal(t, "Test NFT", m.Name)
	assert.Len(t, m.Attributes, 1)
}

func TestChainClient_GetNFTMetadata_FetchFails(t *testing.T) {
	tokenURIData := "0x0000000000000000000000000000000000000000000000000000000000000020000000000000000000000000000000000000000000000000000000000000001f68747470733a2f2f6578616d706c652e636f6d2f6d65746164617461"
	srv := newRPCServer(t, map[string]func(req rpcRequest) rpcResponse{
		"eth_chainId": chainIDHandler(1),
		"eth_call": func(req rpcRequest) rpcResponse {
			return rpcResponse{JSONRPC: "2.0", ID: req.ID, Result: tokenURIData}
		},
	})
	defer srv.Close()

	client, err := NewChainClient(srv.URL, 1, zap.NewNop())
	require.NoError(t, err)
	defer client.Close()

	_, err = client.GetNFTMetadata(context.Background(),
		"0x8667b7bdf8f27e76200fa450bf48aa78bbbcc61f", "1")
	require.Error(t, err)
}

func TestChainClient_GetNFTMetadata_TokenURIFails(t *testing.T) {
	srv := newRPCServer(t, map[string]func(req rpcRequest) rpcResponse{
		"eth_chainId": chainIDHandler(1),
		"eth_call": func(req rpcRequest) rpcResponse {
			return rpcResponse{
				JSONRPC: "2.0",
				ID:      req.ID,
				Error:   map[string]interface{}{"code": -32000, "message": "revert"},
			}
		},
	})
	defer srv.Close()

	client, err := NewChainClient(srv.URL, 1, zap.NewNop())
	require.NoError(t, err)
	defer client.Close()

	_, err = client.GetNFTMetadata(context.Background(),
		"0x8667b7bdf8f27e76200fa450bf48aa78bbbcc61f", "1")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "token URI")
}

func TestChainClient_FetchMetadataFromURI(t *testing.T) {
	cc := &ChainClient{logger: zap.NewNop()}
	_, err := cc.fetchMetadataFromURI(context.Background(), "ftp://invalid.com/file")
	require.Error(t, err)
}

func TestChainClient_GetTokenURI_InvalidTokenID(t *testing.T) {
	cc := &ChainClient{logger: zap.NewNop()}
	_, err := cc.getTokenURI(context.Background(), common.Address{}, "notanumber")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid token id")
}

func TestChainClient_GetNFTBalanceAtBlock_RPCError(t *testing.T) {
	srv := newRPCServer(t, map[string]func(req rpcRequest) rpcResponse{
		"eth_chainId": chainIDHandler(1),
		"eth_call": func(req rpcRequest) rpcResponse {
			return rpcResponse{
				JSONRPC: "2.0",
				ID:      req.ID,
				Error:   map[string]interface{}{"code": -32000, "message": "block tag error"},
			}
		},
	})
	defer srv.Close()

	client, err := NewChainClient(srv.URL, 1, zap.NewNop())
	require.NoError(t, err)
	defer client.Close()

	_, err = client.getNFTBalanceAtBlock(context.Background(),
		common.HexToAddress("0x1234567890123456789012345678901234567890"),
		common.HexToAddress("0x8667b7bdf8f27e76200fa450bf48aa78bbbcc61f"),
		BlockTagSafe)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "fallback to latest disabled")
}

func TestChainClient_GetNFTOwnerAtBlock_RPCError(t *testing.T) {
	srv := newRPCServer(t, map[string]func(req rpcRequest) rpcResponse{
		"eth_chainId": chainIDHandler(1),
		"eth_call": func(req rpcRequest) rpcResponse {
			return rpcResponse{
				JSONRPC: "2.0",
				ID:      req.ID,
				Error:   map[string]interface{}{"code": -32000, "message": strings.Repeat("x", 100)},
			}
		},
	})
	defer srv.Close()

	client, err := NewChainClient(srv.URL, 1, zap.NewNop())
	require.NoError(t, err)
	defer client.Close()

	_, err = client.getNFTOwnerAtBlock(context.Background(),
		common.HexToAddress("0x8667b7bdf8f27e76200fa450bf48aa78bbbcc61f"),
		"1",
		BlockTagSafe)
	require.Error(t, err)
}

func TestChainClient_GetNFTBalance_RPCError(t *testing.T) {
	srv := newRPCServer(t, map[string]func(req rpcRequest) rpcResponse{
		"eth_chainId": chainIDHandler(1),
		"eth_call": func(req rpcRequest) rpcResponse {
			return rpcResponse{
				JSONRPC: "2.0",
				ID:      req.ID,
				Error:   map[string]interface{}{"code": -32000, "message": "revert"},
			}
		},
	})
	defer srv.Close()

	client, err := NewChainClient(srv.URL, 1, zap.NewNop())
	require.NoError(t, err)
	defer client.Close()

	_, err = client.getNFTBalance(context.Background(),
		common.HexToAddress("0x1234567890123456789012345678901234567890"),
		common.HexToAddress("0x8667b7bdf8f27e76200fa450bf48aa78bbbcc61f"))
	require.Error(t, err)
}
