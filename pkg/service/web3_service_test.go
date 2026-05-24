package service

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/ethereum/go-ethereum/crypto"
	"github.com/rtcdance/streamgate/pkg/middleware"
	"github.com/rtcdance/streamgate/pkg/web3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
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
		defer func() { _ = r.Body.Close() }()
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

func TestWeb3Service_IsChainSupported(t *testing.T) {
	mgr := web3.NewMultiChainManager(zap.NewNop())
	_ = mgr.AddChain(1)
	_ = mgr.AddChain(137)

	ws := &Web3Service{
		logger:            zap.NewNop(),
		multiChainManager: mgr,
		signatureVerifier: web3.NewSignatureVerifier(zap.NewNop()),
	}

	supported, err := ws.IsChainSupported(context.Background(), 1)
	require.NoError(t, err)
	assert.True(t, supported)

	supported, err = ws.IsChainSupported(context.Background(), 137)
	require.NoError(t, err)
	assert.True(t, supported)

	supported, err = ws.IsChainSupported(context.Background(), 99999)
	require.NoError(t, err)
	assert.False(t, supported)
}

func TestWeb3Service_GetSupportedChains(t *testing.T) {
	mgr := web3.NewMultiChainManager(zap.NewNop())
	_ = mgr.AddChain(1)

	ws := &Web3Service{
		logger:            zap.NewNop(),
		multiChainManager: mgr,
		signatureVerifier: web3.NewSignatureVerifier(zap.NewNop()),
	}

	chains := ws.GetSupportedChains()
	assert.NotEmpty(t, chains)
}

func TestWeb3Service_GetRPCStatuses(t *testing.T) {
	mgr := web3.NewMultiChainManager(zap.NewNop())

	ws := &Web3Service{
		logger:            zap.NewNop(),
		multiChainManager: mgr,
		signatureVerifier: web3.NewSignatureVerifier(zap.NewNop()),
	}

	statuses := ws.GetRPCStatuses()
	assert.NotNil(t, statuses)
}

func TestWeb3Service_GetTestnetChains(t *testing.T) {
	mgr := web3.NewMultiChainManager(zap.NewNop())

	ws := &Web3Service{
		logger:            zap.NewNop(),
		multiChainManager: mgr,
		signatureVerifier: web3.NewSignatureVerifier(zap.NewNop()),
	}

	chains := ws.GetTestnetChains()
	assert.NotNil(t, chains)
}

func TestWeb3Service_GetMainnetChains(t *testing.T) {
	mgr := web3.NewMultiChainManager(zap.NewNop())

	ws := &Web3Service{
		logger:            zap.NewNop(),
		multiChainManager: mgr,
		signatureVerifier: web3.NewSignatureVerifier(zap.NewNop()),
	}

	chains := ws.GetMainnetChains()
	assert.NotNil(t, chains)
}

func TestWeb3Service_GetMultiChainManager(t *testing.T) {
	mgr := web3.NewMultiChainManager(zap.NewNop())
	ws := &Web3Service{
		logger:            zap.NewNop(),
		multiChainManager: mgr,
		signatureVerifier: web3.NewSignatureVerifier(zap.NewNop()),
	}

	assert.Equal(t, mgr, ws.GetMultiChainManager())
}

func TestWeb3Service_GetSignatureVerifier(t *testing.T) {
	sv := web3.NewSignatureVerifier(zap.NewNop())
	ws := &Web3Service{
		logger:            zap.NewNop(),
		signatureVerifier: sv,
	}

	assert.Equal(t, sv, ws.GetSignatureVerifier())
}

func TestWeb3Service_GetWalletManager(t *testing.T) {
	wm := web3.NewWalletManager(zap.NewNop())
	ws := &Web3Service{
		logger:        zap.NewNop(),
		walletManager: wm,
	}

	assert.Equal(t, wm, ws.GetWalletManager())
}

func TestWeb3Service_GetGasMonitor_Nil(t *testing.T) {
	ws := &Web3Service{logger: zap.NewNop()}
	assert.Nil(t, ws.GetGasMonitor())
}

func TestWeb3Service_GetTransactionQueue(t *testing.T) {
	ws := &Web3Service{
		logger:           zap.NewNop(),
		transactionQueue: web3.NewTransactionQueue(100),
	}
	assert.NotNil(t, ws.GetTransactionQueue())
}

func TestWeb3Service_GetEIP712Verifier(t *testing.T) {
	ev := web3.NewEIP712Verifier(zap.NewNop())
	ws := &Web3Service{
		logger:         zap.NewNop(),
		eip712Verifier: ev,
	}
	assert.Equal(t, ev, ws.GetEIP712Verifier())
}

func TestWeb3Service_GetSolanaSigner_Nil(t *testing.T) {
	ws := &Web3Service{logger: zap.NewNop()}
	assert.Nil(t, ws.GetSolanaSigner())
}

func TestWeb3Service_SetNFTAccessCache(t *testing.T) {
	ws := &Web3Service{logger: zap.NewNop()}
	cache := &mockNFTAccessCacheForWeb3{}
	ws.SetNFTAccessCache(cache)
	assert.Equal(t, cache, ws.nftAccessCache)
}

func TestWeb3Service_GetEventIndexer_Nil(t *testing.T) {
	ws := &Web3Service{logger: zap.NewNop()}
	assert.Nil(t, ws.GetEventIndexer())
}

type mockNFTAccessCacheForWeb3 struct{}

func (m *mockNFTAccessCacheForWeb3) Get(_ context.Context, _ string) (middleware.NFTAccessEntry, bool) {
	return middleware.NFTAccessEntry{}, false
}
func (m *mockNFTAccessCacheForWeb3) Set(_ context.Context, _ string, _ middleware.NFTAccessEntry) {}
func (m *mockNFTAccessCacheForWeb3) Delete(_ context.Context, _ string)                           {}
func (m *mockNFTAccessCacheForWeb3) DeleteByPrefix(_ context.Context, _ string)                   {}
