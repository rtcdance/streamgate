package service

import (
	"context"
	"fmt"
	"math/big"
	"strings"
	"testing"

	"github.com/rtcdance/streamgate/pkg/core/config"
	"github.com/rtcdance/streamgate/pkg/web3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func TestNewWeb3Service_Coverage_WithDeps(t *testing.T) {
	mcm := web3.NewMultiChainManager(zap.NewNop())
	sv := web3.NewSignatureVerifier(zap.NewNop())
	solanaV := web3.NewSolanaVerifier(zap.NewNop(), "")
	defer solanaV.Close()
	eip712v := web3.NewEIP712Verifier(zap.NewNop())

	deps := Web3Deps{
		ChainManager: mcm,
		SigVerifier:  sv,
		SolanaVerif:  solanaV,
		EIP712Verif:  eip712v,
	}
	cfg := &config.Config{
		Web3: config.Web3Config{
			ChainID: 11155111,
		},
	}

	svc, err := NewWeb3Service(deps, cfg, zap.NewNop())
	require.NoError(t, err)
	require.NotNil(t, svc)
	defer svc.Close()

	assert.Equal(t, mcm, svc.GetMultiChainManager())
	assert.Equal(t, sv, svc.GetSignatureVerifier())
	assert.Equal(t, solanaV, svc.GetSolanaVerifier())
	assert.Equal(t, eip712v, svc.GetEIP712Verifier())
	assert.NotNil(t, svc.GetWalletManager())
	assert.NotNil(t, svc.GetTransactionQueue())
}

func TestDefaultWeb3Deps_Coverage(t *testing.T) {
	cfg := &config.Config{
		Web3: config.Web3Config{
			ChainID: 1,
		},
	}
	deps := DefaultWeb3Deps(cfg, zap.NewNop())
	assert.NotNil(t, deps.ChainManager)
	assert.NotNil(t, deps.SigVerifier)
	assert.NotNil(t, deps.SolanaVerif)
	assert.NotNil(t, deps.EIP712Verif)
}

func TestNewWeb3Service_Coverage_InvalidKey(t *testing.T) {
	mcm := web3.NewMultiChainManager(zap.NewNop())
	deps := Web3Deps{
		ChainManager: mcm,
		SigVerifier:  web3.NewSignatureVerifier(zap.NewNop()),
	}
	cfg := &config.Config{
		Web3: config.Web3Config{
			Transaction: config.TransactionConfig{
				PrivateKeyHex: "invalid-hex-key",
			},
		},
	}

	_, err := NewWeb3Service(deps, cfg, zap.NewNop())
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "secure private key")
}

func TestWeb3Service_Coverage_GetNFTInfo_NoChain(t *testing.T) {
	ws := &Web3Service{
		logger:            zap.NewNop(),
		multiChainManager: web3.NewMultiChainManager(zap.NewNop()),
	}
	_, err := ws.GetNFTInfo(context.Background(), 99999, "0x1", "1")
	assert.Error(t, err)
}

func TestWeb3Service_Coverage_DetectContractType_NoChain(t *testing.T) {
	ws := &Web3Service{
		logger:            zap.NewNop(),
		multiChainManager: web3.NewMultiChainManager(zap.NewNop()),
	}
	result := ws.DetectContractType(context.Background(), 99999, "0x1")
	assert.Equal(t, "unknown", result)
}

func TestWeb3Service_Coverage_VerifySignature_NilVerifier(t *testing.T) {
	ws := &Web3Service{logger: zap.NewNop()}
	assert.Panics(t, func() {
		ws.VerifySignature(context.Background(), "0x1", "msg", "sig")
	})
}

func TestWeb3Service_Coverage_Close_WithAllSubsystems(t *testing.T) {
	mcm := web3.NewMultiChainManager(zap.NewNop())
	sv := web3.NewSolanaVerifier(zap.NewNop(), "")
	ws := &Web3Service{
		logger:            zap.NewNop(),
		multiChainManager: mcm,
		solanaVerifier:    sv,
		transactionQueue:  web3.NewTransactionQueue(10),
	}
	assert.NotPanics(t, func() { ws.Close() })
}

func TestWeb3Service_Coverage_SetNFTAccessCache(t *testing.T) {
	ws := &Web3Service{logger: zap.NewNop()}
	cache := &mockNFTAccessCacheForWeb3{}
	ws.SetNFTAccessCache(cache)
	assert.Equal(t, cache, ws.nftAccessCache)
}

func TestWeb3Service_Coverage_GetIPFSClient_NilSvc(t *testing.T) {
	ws := &Web3Service{logger: zap.NewNop()}
	assert.Nil(t, ws.GetIPFSClient())
}

func TestWeb3Service_Coverage_GetTransactionQueue_Set(t *testing.T) {
	tq := web3.NewTransactionQueue(50)
	ws := &Web3Service{
		logger:           zap.NewNop(),
		transactionQueue: tq,
	}
	assert.Equal(t, tq, ws.GetTransactionQueue())
}

func TestWeb3Service_Coverage_GetEIP712Verifier_Set(t *testing.T) {
	ev := web3.NewEIP712Verifier(zap.NewNop())
	ws := &Web3Service{
		logger:         zap.NewNop(),
		eip712Verifier: ev,
	}
	assert.Equal(t, ev, ws.GetEIP712Verifier())
}

func TestWeb3Service_Coverage_GetEventIndexer_NilSvc(t *testing.T) {
	ws := &Web3Service{logger: zap.NewNop()}
	assert.Nil(t, ws.GetEventIndexer())
}

func TestWeb3Service_Coverage_ListNFTs(t *testing.T) {
	ws := &Web3Service{logger: zap.NewNop()}
	results, err := ws.ListNFTs(context.Background(), 0, 10)
	assert.NoError(t, err)
	assert.Empty(t, results)
}

func TestGweiToWei_Coverage_TableDriven(t *testing.T) {
	tests := []struct {
		name     string
		gwei     float64
		expected string
	}{
		{"zero", 0.0, "0"},
		{"one gwei", 1.0, "1000000000"},
		{"ten gwei", 10.0, "10000000000"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := gweiToWei(tt.gwei)
			assert.Equal(t, tt.expected, result.String())
		})
	}
}

func TestIsNonceError_Coverage_TableDriven(t *testing.T) {
	tests := []struct {
		name                  string
		err                   error
		wantNonceTooLow       bool
		wantReplacementFeeLow bool
	}{
		{"nil", nil, false, false},
		{"nonce too low", fmt.Errorf("nonce too low for account"), true, false},
		{"replacement fee too low", fmt.Errorf("replacement fee too low"), false, true},
		{"already known", fmt.Errorf("transaction already known"), false, true},
		{"unrelated", fmt.Errorf("insufficient funds"), false, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			nonceTooLow, replacementFeeTooLow := isNonceError(tt.err)
			assert.Equal(t, tt.wantNonceTooLow, nonceTooLow)
			assert.Equal(t, tt.wantReplacementFeeLow, replacementFeeTooLow)
		})
	}
}

func TestWeb3Service_Coverage_SendTransaction_NoKey(t *testing.T) {
	ws := &Web3Service{logger: zap.NewNop()}
	_, err := ws.SendTransaction(context.Background(), 1, "0xabc", nil, nil)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "private key not configured")
}

func TestWeb3Service_Coverage_ReplaceStuckTransaction_NoKey(t *testing.T) {
	ws := &Web3Service{logger: zap.NewNop()}
	_, err := ws.ReplaceStuckTransaction(context.Background(), 1, nil, 10)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "private key not configured")
}

func TestWeb3Service_Coverage_CancelPendingTransaction_NoKey(t *testing.T) {
	ws := &Web3Service{logger: zap.NewNop()}
	_, err := ws.CancelPendingTransaction(context.Background(), 1, nil, 10)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "private key not configured")
}

func TestWeb3Service_Coverage_SubmitPermit_NilValue(t *testing.T) {
	ws := &Web3Service{logger: zap.NewNop()}
	_, err := ws.SubmitPermit(context.Background(), 1, "0x1", "0x2", "0x3", nil, big.NewInt(0), 0, [32]byte{}, [32]byte{})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "value and deadline must not be nil")
}

func TestWeb3Service_Coverage_SubmitPermit_NilDeadline(t *testing.T) {
	ws := &Web3Service{logger: zap.NewNop()}
	_, err := ws.SubmitPermit(context.Background(), 1, "0x1", "0x2", "0x3", big.NewInt(1), nil, 0, [32]byte{}, [32]byte{})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "value and deadline must not be nil")
}

func TestWeb3Service_Coverage_GetTokenBalance_NoMgr(t *testing.T) {
	ws := &Web3Service{logger: zap.NewNop()}
	_, err := ws.GetTokenBalance(context.Background(), 1, "0x1", "0x2")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "multiChainManager not initialized")
}

func TestWeb3Service_Coverage_GetTokenAllowance_NoMgr(t *testing.T) {
	ws := &Web3Service{logger: zap.NewNop()}
	_, err := ws.GetTokenAllowance(context.Background(), 1, "0x1", "0x2", "0x3")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "multiChainManager not initialized")
}

func TestWeb3Service_Coverage_GetTokenInfo_NoMgr(t *testing.T) {
	ws := &Web3Service{logger: zap.NewNop()}
	_, err := ws.GetTokenInfo(context.Background(), 1, "0x1")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "multiChainManager not initialized")
}

func TestWeb3Service_Coverage_WaitForReceipt_NoMgr(t *testing.T) {
	ws := &Web3Service{logger: zap.NewNop()}
	_, err := ws.WaitForReceipt(context.Background(), 1, "0xabc", 0)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "multiChainManager not initialized")
}

func TestWeb3Service_Coverage_GetGasPrice_NoMgr(t *testing.T) {
	ws := &Web3Service{logger: zap.NewNop()}
	_, err := ws.GetGasPrice(context.Background(), 1)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "multiChainManager not initialized")
}

func TestWeb3Service_Coverage_GetBalance_NoMgr(t *testing.T) {
	ws := &Web3Service{
		logger:            zap.NewNop(),
		multiChainManager: web3.NewMultiChainManager(zap.NewNop()),
	}
	_, err := ws.GetBalance(context.Background(), 1, "0x1")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "chain client not found")
}

func TestWeb3Service_Coverage_RegisterContent_NoKey(t *testing.T) {
	ws := &Web3Service{
		logger: zap.NewNop(),
		config: &config.Config{},
	}
	_, err := ws.RegisterContent(context.Background(), 1, "0xcontract", "hash", "uri")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "private key not configured")
}

func TestWeb3Service_Coverage_VerifyMerkleWhitelist_BadRoot(t *testing.T) {
	ws := &Web3Service{logger: zap.NewNop()}
	_, err := ws.VerifyMerkleWhitelist("not-hex", "0xabc", nil)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "invalid root hex")
}

func TestWeb3Service_Coverage_VerifyMerkleWhitelist_BadProof(t *testing.T) {
	ws := &Web3Service{logger: zap.NewNop()}
	root := "0x" + strings.Repeat("ab", 32)
	_, err := ws.VerifyMerkleWhitelist(root, "0x742d35Cc6634C0532925a3b844Bc9e7595f2bD18", []string{"not-hex"})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "invalid proof element")
}

func TestWeb3Service_Coverage_VerifySolanaTokenAccount_NilV(t *testing.T) {
	ws := &Web3Service{logger: zap.NewNop()}
	_, err := ws.VerifySolanaTokenAccount(context.Background(), "acct", "owner")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "solana verifier not initialized")
}

func TestWeb3Service_Coverage_VerifySolanaMintAuthority_NilV(t *testing.T) {
	ws := &Web3Service{logger: zap.NewNop()}
	_, err := ws.VerifySolanaMintAuthority(context.Background(), "mint", "auth")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "solana verifier not initialized")
}

func TestWeb3Service_Coverage_VerifySolanaMetaplexNFT_NilV(t *testing.T) {
	ws := &Web3Service{logger: zap.NewNop()}
	_, err := ws.VerifySolanaMetaplexNFTOwnership(context.Background(), "mint", "owner")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "solana verifier not initialized")
}

func TestWeb3Service_Coverage_UploadToIPFS_NilClient(t *testing.T) {
	ws := &Web3Service{logger: zap.NewNop()}
	_, err := ws.UploadToIPFS(context.Background(), "file.txt", []byte("data"))
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "IPFS client not initialized")
}

func TestWeb3Service_Coverage_DownloadFromIPFS_NilClient(t *testing.T) {
	ws := &Web3Service{logger: zap.NewNop()}
	_, err := ws.DownloadFromIPFS(context.Background(), "QmTest")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "IPFS client not initialized")
}

func TestWeb3Service_Coverage_GetGasPriceLevels_NilMonitor(t *testing.T) {
	ws := &Web3Service{logger: zap.NewNop()}
	_, err := ws.GetGasPriceLevels(context.Background(), 1)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "gas monitor not initialized")
}

func TestWeb3Service_Coverage_GetSolanaSigner_NilSvc(t *testing.T) {
	ws := &Web3Service{logger: zap.NewNop()}
	assert.Nil(t, ws.GetSolanaSigner())
}

func TestWeb3Service_Coverage_GetSolanaVerifier_Set(t *testing.T) {
	sv := web3.NewSolanaVerifier(zap.NewNop())
	defer sv.Close()
	ws := &Web3Service{logger: zap.NewNop(), solanaVerifier: sv}
	assert.Equal(t, sv, ws.GetSolanaVerifier())
}

func TestWeb3Service_Coverage_Close_NilDeps(t *testing.T) {
	ws := &Web3Service{logger: zap.NewNop()}
	assert.NotPanics(t, func() { ws.Close() })
}

func TestWeb3Service_Coverage_IsChainSupported(t *testing.T) {
	mgr := web3.NewMultiChainManager(zap.NewNop())
	_ = mgr.AddChain(1)
	ws := &Web3Service{
		logger:            zap.NewNop(),
		multiChainManager: mgr,
		signatureVerifier: web3.NewSignatureVerifier(zap.NewNop()),
	}

	supported, err := ws.IsChainSupported(context.Background(), 1)
	require.NoError(t, err)
	assert.True(t, supported)

	supported, err = ws.IsChainSupported(context.Background(), 99999)
	require.NoError(t, err)
	assert.False(t, supported)
}

func TestWeb3Service_Coverage_GetSupportedChains(t *testing.T) {
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

func TestWeb3Service_Coverage_GetRPCStatuses(t *testing.T) {
	mgr := web3.NewMultiChainManager(zap.NewNop())
	ws := &Web3Service{
		logger:            zap.NewNop(),
		multiChainManager: mgr,
		signatureVerifier: web3.NewSignatureVerifier(zap.NewNop()),
	}

	statuses := ws.GetRPCStatuses()
	assert.NotNil(t, statuses)
}

func TestWeb3Service_Coverage_GetTestnetChains(t *testing.T) {
	mgr := web3.NewMultiChainManager(zap.NewNop())
	ws := &Web3Service{
		logger:            zap.NewNop(),
		multiChainManager: mgr,
		signatureVerifier: web3.NewSignatureVerifier(zap.NewNop()),
	}

	chains := ws.GetTestnetChains()
	assert.NotNil(t, chains)
}

func TestWeb3Service_Coverage_GetMainnetChains(t *testing.T) {
	mgr := web3.NewMultiChainManager(zap.NewNop())
	ws := &Web3Service{
		logger:            zap.NewNop(),
		multiChainManager: mgr,
		signatureVerifier: web3.NewSignatureVerifier(zap.NewNop()),
	}

	chains := ws.GetMainnetChains()
	assert.NotNil(t, chains)
}

func TestWeb3Service_Coverage_SendTransaction_NoMgr(t *testing.T) {
	sk, err := web3.NewSecurePrivateKeyFromHex("ac0974bec39a17e36ba4a6b4d238ff944bacb478cbed5efcae784d7bf4f2ff80")
	require.NoError(t, err)
	ws := &Web3Service{logger: zap.NewNop(), secureKey: sk}
	_, err = ws.SendTransaction(context.Background(), 1, "0x1234", big.NewInt(0), nil)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "multiChainManager not initialized")
}

func TestWeb3Service_Coverage_ReplaceStuckTransaction_NoMgr(t *testing.T) {
	sk, err := web3.NewSecurePrivateKeyFromHex("ac0974bec39a17e36ba4a6b4d238ff944bacb478cbed5efcae784d7bf4f2ff80")
	require.NoError(t, err)
	ws := &Web3Service{logger: zap.NewNop(), secureKey: sk}
	_, err = ws.ReplaceStuckTransaction(context.Background(), 1, &web3.PendingTx{}, 10)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "multiChainManager not initialized")
}

func TestWeb3Service_Coverage_CancelPendingTransaction_NoMgr(t *testing.T) {
	sk, err := web3.NewSecurePrivateKeyFromHex("ac0974bec39a17e36ba4a6b4d238ff944bacb478cbed5efcae784d7bf4f2ff80")
	require.NoError(t, err)
	ws := &Web3Service{logger: zap.NewNop(), secureKey: sk}
	_, err = ws.CancelPendingTransaction(context.Background(), 1, &web3.PendingTx{}, 10)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "multiChainManager not initialized")
}

func TestWeb3Service_Coverage_RegisterContent_WithKeyNoChain(t *testing.T) {
	cfg := &config.Config{}
	cfg.Web3.Transaction.PrivateKeyHex = "ac0974bec39a17e36ba4a6b4d238ff944bacb478cbed5efcae784d7bf4f2ff80"
	ws := &Web3Service{
		logger:            zap.NewNop(),
		config:            cfg,
		multiChainManager: web3.NewMultiChainManager(zap.NewNop()),
	}
	_, err := ws.RegisterContent(context.Background(), 1, "0x1", "hash", "uri")
	assert.Error(t, err)
}

func TestWeb3Service_Coverage_HeaderByNumber_NoChain(t *testing.T) {
	ws := &Web3Service{
		logger:            zap.NewNop(),
		multiChainManager: web3.NewMultiChainManager(zap.NewNop()),
		config:            &config.Config{Web3: config.Web3Config{ChainID: 99999}},
	}
	_, err := ws.HeaderByNumber(context.Background(), big.NewInt(1))
	assert.Error(t, err)
}

func TestWeb3Service_Coverage_VerifyNFTOwnershipAutoDetect_NoChain(t *testing.T) {
	ws := &Web3Service{
		logger:            zap.NewNop(),
		multiChainManager: web3.NewMultiChainManager(zap.NewNop()),
	}
	_, err := ws.VerifyNFTOwnershipAutoDetect(context.Background(), 99999, "0x1", "1", "0x2")
	assert.Error(t, err)
}

func TestWeb3Service_Coverage_VerifyNFTCollectionAutoDetect_NoChain(t *testing.T) {
	ws := &Web3Service{
		logger:            zap.NewNop(),
		multiChainManager: web3.NewMultiChainManager(zap.NewNop()),
	}
	_, err := ws.VerifyNFTCollectionAutoDetect(context.Background(), 99999, "0x1", "0x2")
	assert.Error(t, err)
}

func TestWeb3Service_Coverage_GetNFT_NoChain(t *testing.T) {
	ws := &Web3Service{
		logger:            zap.NewNop(),
		multiChainManager: web3.NewMultiChainManager(zap.NewNop()),
	}
	_, err := ws.GetNFT(context.Background(), 99999, "0x1", "1")
	assert.Error(t, err)
}

func TestWeb3Service_Coverage_GetNFTBalance_NoChain(t *testing.T) {
	ws := &Web3Service{
		logger:            zap.NewNop(),
		multiChainManager: web3.NewMultiChainManager(zap.NewNop()),
	}
	_, err := ws.GetNFTBalance(context.Background(), 99999, "0x1", "0x2")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "EVM chain client not found")
}

func TestWeb3Service_Coverage_GetNFTBalance_SolanaNoChain(t *testing.T) {
	ws := &Web3Service{
		logger:            zap.NewNop(),
		multiChainManager: web3.NewMultiChainManager(zap.NewNop()),
	}
	_, err := ws.GetNFTBalance(context.Background(), -999, "0x1", "0x2")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "solana chain client not found")
}

func TestWeb3Service_Coverage_VerifyNFTOwnership_SolanaNoChain(t *testing.T) {
	ws := &Web3Service{
		logger:            zap.NewNop(),
		multiChainManager: web3.NewMultiChainManager(zap.NewNop()),
	}
	_, err := ws.VerifyNFTOwnership(context.Background(), -999, "0x1", "1", "0x2")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "solana chain client not found")
}
