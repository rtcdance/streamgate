package service

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/ethereum/go-ethereum/crypto"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"streamgate/pkg/web3"
)

// --- VerifySignature tests (no RPC needed) ---

func TestWeb3Service_VerifySignature_Valid(t *testing.T) {
	ws := &Web3Service{
		logger:            zap.NewNop(),
		signatureVerifier: web3.NewSignatureVerifier(zap.NewNop()),
	}

	privateKey, err := crypto.GenerateKey()
	require.NoError(t, err)

	address := crypto.PubkeyToAddress(privateKey.PublicKey).Hex()
	message := "Sign this message to verify your wallet ownership"
	signature, err := ws.signatureVerifier.(*web3.SignatureVerifier).SignMessage(message, privateKey)
	require.NoError(t, err)

	valid, err := ws.VerifySignature(context.Background(), address, message, signature)
	require.NoError(t, err)
	assert.True(t, valid)
}

func TestWeb3Service_VerifySignature_Invalid(t *testing.T) {
	ws := &Web3Service{
		logger:            zap.NewNop(),
		signatureVerifier: web3.NewSignatureVerifier(zap.NewNop()),
	}

	valid, err := ws.VerifySignature(context.Background(), "0x742d35Cc6634C0532925a3b844Bc9e7595f2bD18", "msg", "0x1234")
	assert.False(t, valid)
	assert.Error(t, err) // invalid signature length
}

func TestWeb3Service_VerifySignature_WrongAddress(t *testing.T) {
	ws := &Web3Service{
		logger:            zap.NewNop(),
		signatureVerifier: web3.NewSignatureVerifier(zap.NewNop()),
	}

	privateKey, err := crypto.GenerateKey()
	require.NoError(t, err)

	message := "test message"
	signature, err := ws.signatureVerifier.(*web3.SignatureVerifier).SignMessage(message, privateKey)
	require.NoError(t, err)

	wrongAddr := "0x1111111111111111111111111111111111111111"
	valid, err := ws.VerifySignature(context.Background(), wrongAddr, message, signature)
	require.NoError(t, err)
	assert.False(t, valid)
}

// --- VerifyNFTOwnership routing tests ---

func TestWeb3Service_VerifyNFTOwnership_MissingChain(t *testing.T) {
	ws := &Web3Service{
		logger:            zap.NewNop(),
		multiChainManager: web3.NewMultiChainManager(zap.NewNop()),
		signatureVerifier: web3.NewSignatureVerifier(zap.NewNop()),
	}

	_, err := ws.VerifyNFTOwnership(context.Background(), 99999, "0x1234", "1", "0x5678")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "EVM chain client not found")
}

func TestWeb3Service_VerifyNFTOwnership_MissingSolanaChain(t *testing.T) {
	ws := &Web3Service{
		logger:            zap.NewNop(),
		multiChainManager: web3.NewMultiChainManager(zap.NewNop()),
		signatureVerifier: web3.NewSignatureVerifier(zap.NewNop()),
	}

	_, err := ws.VerifyNFTOwnership(context.Background(), -999, "0x1234", "1", "0x5678")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "solana chain client not found")
}

// --- GetNFTBalance routing tests ---

func TestWeb3Service_GetNFTBalance_MissingChain(t *testing.T) {
	ws := &Web3Service{
		logger:            zap.NewNop(),
		multiChainManager: web3.NewMultiChainManager(zap.NewNop()),
		signatureVerifier: web3.NewSignatureVerifier(zap.NewNop()),
	}

	_, err := ws.GetNFTBalance(context.Background(), 99999, "0x1234", "0x5678")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "EVM chain client not found")
}

func TestWeb3Service_GetNFTBalance_MissingSolanaChain(t *testing.T) {
	ws := &Web3Service{
		logger:            zap.NewNop(),
		multiChainManager: web3.NewMultiChainManager(zap.NewNop()),
		signatureVerifier: web3.NewSignatureVerifier(zap.NewNop()),
	}

	_, err := ws.GetNFTBalance(context.Background(), -999, "0x1234", "0x5678")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "solana chain client not found")
}

// --- GetGasPrice tests ---

func TestWeb3Service_GetGasPrice_MissingChain(t *testing.T) {
	ws := &Web3Service{
		logger:            zap.NewNop(),
		multiChainManager: web3.NewMultiChainManager(zap.NewNop()),
		signatureVerifier: web3.NewSignatureVerifier(zap.NewNop()),
	}

	_, err := ws.GetGasPrice(context.Background(), 99999)
	assert.Error(t, err)
}

// --- GetBalance tests ---

func TestWeb3Service_GetBalance_MissingChain(t *testing.T) {
	ws := &Web3Service{
		logger:            zap.NewNop(),
		multiChainManager: web3.NewMultiChainManager(zap.NewNop()),
		signatureVerifier: web3.NewSignatureVerifier(zap.NewNop()),
	}

	_, err := ws.GetBalance(context.Background(), 99999, "0x742d35Cc6634C0532925a3b844Bc9e7595f2bD18")
	assert.Error(t, err)
}

// --- Close tests ---

func TestWeb3Service_Close_NoPanic(t *testing.T) {
	ws := &Web3Service{
		logger:            zap.NewNop(),
		signatureVerifier: web3.NewSignatureVerifier(zap.NewNop()),
	}
	assert.NotPanics(t, func() { ws.Close() })
}

// --- RPC-backed integration tests ---

// newTestRPCServer creates an httptest server that handles JSON-RPC requests.
func newTestRPCServer(t *testing.T, handlers map[string]func(reqParams []json.RawMessage) interface{}) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer r.Body.Close()
		var req struct {
			ID     interface{}       `json:"id"`
			Method string            `json:"method"`
			Params []json.RawMessage `json:"params"`
		}
		require.NoError(t, json.NewDecoder(r.Body).Decode(&req))

		handler, ok := handlers[req.Method]
		if !ok {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		result := handler(req.Params)

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"jsonrpc": "2.0",
			"id":      req.ID,
			"result":  result,
		})
	}))
}



func TestWeb3Service_GetGasPrice_EVM(t *testing.T) {
	srv := newTestRPCServer(t, map[string]func(reqParams []json.RawMessage) interface{}{
		"eth_chainId": func(_ []json.RawMessage) interface{} {
			return "0xaa36a7"
		},
		"eth_blockNumber": func(_ []json.RawMessage) interface{} {
			return "0x1"
		},
		"eth_gasPrice": func(_ []json.RawMessage) interface{} {
			return "0x3b9aca00" // 1 Gwei
		},
	})
	defer srv.Close()

	client, err := web3.NewChainClientWithFallback([]string{srv.URL}, 11155111, zap.NewNop())
	require.NoError(t, err)
	defer client.Close()

	mgr := web3.NewMultiChainManager(zap.NewNop())
	mgr.AddChainWithClient(11155111, client)

	ws := &Web3Service{
		logger:            zap.NewNop(),
		multiChainManager: mgr,
		signatureVerifier: web3.NewSignatureVerifier(zap.NewNop()),
	}

	gasPrice, err := ws.GetGasPrice(context.Background(), 11155111)
	require.NoError(t, err)
	assert.Equal(t, "1000000000", gasPrice) // 1 Gwei in wei
}

func TestWeb3Service_GetBalance_EVM(t *testing.T) {
	srv := newTestRPCServer(t, map[string]func(reqParams []json.RawMessage) interface{}{
		"eth_chainId": func(_ []json.RawMessage) interface{} {
			return "0xaa36a7"
		},
		"eth_blockNumber": func(_ []json.RawMessage) interface{} {
			return "0x1"
		},
		"eth_getBalance": func(_ []json.RawMessage) interface{} {
			return "0xde0b6b3a7640000" // 1 ETH in wei
		},
	})
	defer srv.Close()

	client, err := web3.NewChainClientWithFallback([]string{srv.URL}, 11155111, zap.NewNop())
	require.NoError(t, err)
	defer client.Close()

	mgr := web3.NewMultiChainManager(zap.NewNop())
	mgr.AddChainWithClient(11155111, client)

	ws := &Web3Service{
		logger:            zap.NewNop(),
		multiChainManager: mgr,
		signatureVerifier: web3.NewSignatureVerifier(zap.NewNop()),
	}

	balance, err := ws.GetBalance(context.Background(), 11155111, "0x742d35Cc6634C0532925a3b844Bc9e7595f2bD18")
	require.NoError(t, err)
	assert.Equal(t, "1000000000000000000", balance.String()) // 1 ETH
}

func TestWeb3Service_VerifyNFTOwnership_EVMRoute(t *testing.T) {
	srv := newTestRPCServer(t, map[string]func(reqParams []json.RawMessage) interface{}{
		"eth_chainId": func(_ []json.RawMessage) interface{} {
			return "0xaa36a7"
		},
		"eth_blockNumber": func(_ []json.RawMessage) interface{} {
			return "0x1"
		},
		"eth_call": func(_ []json.RawMessage) interface{} {
			// Return zero address for ownerOf → ownership check fails
			return "0x0000000000000000000000000000000000000000000000000000000000000000"
		},
	})
	defer srv.Close()

	client, err := web3.NewChainClientWithFallback([]string{srv.URL}, 11155111, zap.NewNop())
	require.NoError(t, err)
	defer client.Close()

	mgr := web3.NewMultiChainManager(zap.NewNop())
	mgr.AddChainWithClient(11155111, client)

	ws := &Web3Service{
		logger:            zap.NewNop(),
		multiChainManager: mgr,
		signatureVerifier: web3.NewSignatureVerifier(zap.NewNop()),
	}

	owned, err := ws.VerifyNFTOwnership(context.Background(), 11155111, "0x1234567890123456789012345678901234567890", "1", "0x742d35Cc6634C0532925a3b844Bc9e7595f2bD18")
	assert.NoError(t, err)
	assert.False(t, owned) // zero address != owner
}

func TestWeb3Service_HealthCheck_Healthy(t *testing.T) {
	srv := newTestRPCServer(t, map[string]func(reqParams []json.RawMessage) interface{}{
		"eth_chainId": func(_ []json.RawMessage) interface{} {
			return "0xaa36a7"
		},
		"eth_blockNumber": func(_ []json.RawMessage) interface{} {
			return "0x64"
		},
	})
	defer srv.Close()

	client, err := web3.NewChainClientWithFallback([]string{srv.URL}, 11155111, zap.NewNop())
	require.NoError(t, err)
	defer client.Close()

	err = client.HealthCheck(context.Background())
	require.NoError(t, err)
}

func TestWeb3Service_WalletManager(t *testing.T) {
	ws := &Web3Service{
		logger:            zap.NewNop(),
		walletManager:     web3.NewWalletManager(zap.NewNop()),
		signatureVerifier: web3.NewSignatureVerifier(zap.NewNop()),
	}

	assert.NotNil(t, ws.walletManager)

	wallet, err := ws.walletManager.CreateWallet()
	require.NoError(t, err)
	assert.NotEmpty(t, wallet.Address)
}
