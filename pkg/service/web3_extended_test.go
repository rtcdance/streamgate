package service

import (
	"context"
	"encoding/json"
	"fmt"
	"math/big"
	"testing"

	"github.com/rtcdance/streamgate/pkg/core/config"
	"github.com/rtcdance/streamgate/pkg/web3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func TestWeb3Service_GetGasPriceLevels_NoMonitor(t *testing.T) {
	ws := &Web3Service{logger: zap.NewNop()}
	_, err := ws.GetGasPriceLevels(context.Background(), 1)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "gas monitor not initialized")
}

func TestWeb3Service_UploadToIPFS_NoClient(t *testing.T) {
	ws := &Web3Service{logger: zap.NewNop()}
	_, err := ws.UploadToIPFS(context.Background(), "test.txt", []byte("data"))
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "IPFS client not initialized")
}

func TestWeb3Service_DownloadFromIPFS_NoClient(t *testing.T) {
	ws := &Web3Service{logger: zap.NewNop()}
	_, err := ws.DownloadFromIPFS(context.Background(), "QmTest")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "IPFS client not initialized")
}

func TestWeb3Service_SendTransaction_NoChainManager(t *testing.T) {
	sk, err := web3.NewSecurePrivateKeyFromHex("ac0974bec39a17e36ba4a6b4d238ff944bacb478cbed5efcae784d7bf4f2ff80")
	require.NoError(t, err)
	ws := &Web3Service{logger: zap.NewNop(), secureKey: sk}
	_, err = ws.SendTransaction(context.Background(), 1, "0x1234", big.NewInt(0), nil)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "multiChainManager not initialized")
}

func TestWeb3Service_VerifySolanaTokenAccount_NoVerifier(t *testing.T) {
	ws := &Web3Service{logger: zap.NewNop()}
	_, err := ws.VerifySolanaTokenAccount(context.Background(), "acct", "owner")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "solana verifier not initialized")
}

func TestWeb3Service_VerifySolanaMintAuthority_NoVerifier(t *testing.T) {
	ws := &Web3Service{logger: zap.NewNop()}
	_, err := ws.VerifySolanaMintAuthority(context.Background(), "mint", "auth")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "solana verifier not initialized")
}

func TestWeb3Service_VerifySolanaMetaplexNFTOwnership_NoVerifier(t *testing.T) {
	ws := &Web3Service{logger: zap.NewNop()}
	_, err := ws.VerifySolanaMetaplexNFTOwnership(context.Background(), "mint", "owner")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "solana verifier not initialized")
}

func TestWeb3Service_GetTokenBalance_NoChainManager(t *testing.T) {
	ws := &Web3Service{logger: zap.NewNop()}
	_, err := ws.GetTokenBalance(context.Background(), 1, "0x1", "0x2")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "multiChainManager not initialized")
}

func TestWeb3Service_GetTokenAllowance_NoChainManager(t *testing.T) {
	ws := &Web3Service{logger: zap.NewNop()}
	_, err := ws.GetTokenAllowance(context.Background(), 1, "0x1", "0x2", "0x3")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "multiChainManager not initialized")
}

func TestWeb3Service_GetTokenInfo_NoChainManager(t *testing.T) {
	ws := &Web3Service{logger: zap.NewNop()}
	_, err := ws.GetTokenInfo(context.Background(), 1, "0x1")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "multiChainManager not initialized")
}

func TestWeb3Service_SubmitPermit_NilValue(t *testing.T) {
	ws := &Web3Service{logger: zap.NewNop()}
	_, err := ws.SubmitPermit(context.Background(), 1, "0x1", "0x2", "0x3", nil, big.NewInt(0), 0, [32]byte{}, [32]byte{})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "value and deadline must not be nil")
}

func TestWeb3Service_SubmitPermit_NilDeadline(t *testing.T) {
	ws := &Web3Service{logger: zap.NewNop()}
	_, err := ws.SubmitPermit(context.Background(), 1, "0x1", "0x2", "0x3", big.NewInt(1), nil, 0, [32]byte{}, [32]byte{})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "value and deadline must not be nil")
}

func TestWeb3Service_ReplaceStuckTransaction_NoChainManager(t *testing.T) {
	sk, err := web3.NewSecurePrivateKeyFromHex("ac0974bec39a17e36ba4a6b4d238ff944bacb478cbed5efcae784d7bf4f2ff80")
	require.NoError(t, err)
	ws := &Web3Service{logger: zap.NewNop(), secureKey: sk}
	_, err = ws.ReplaceStuckTransaction(context.Background(), 1, &web3.PendingTx{}, 10)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "multiChainManager not initialized")
}

func TestWeb3Service_CancelPendingTransaction_NoChainManager(t *testing.T) {
	sk, err := web3.NewSecurePrivateKeyFromHex("ac0974bec39a17e36ba4a6b4d238ff944bacb478cbed5efcae784d7bf4f2ff80")
	require.NoError(t, err)
	ws := &Web3Service{logger: zap.NewNop(), secureKey: sk}
	_, err = ws.CancelPendingTransaction(context.Background(), 1, &web3.PendingTx{}, 10)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "multiChainManager not initialized")
}

func TestWeb3Service_WaitForReceipt_NoChainManager(t *testing.T) {
	ws := &Web3Service{logger: zap.NewNop()}
	_, err := ws.WaitForReceipt(context.Background(), 1, "0xabc", 1)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "multiChainManager not initialized")
}

func TestIsNonceError_TableDriven(t *testing.T) {
	tests := []struct {
		name                  string
		err                   error
		wantNonceTooLow       bool
		wantReplacementFeeLow bool
	}{
		{"nil error", nil, false, false},
		{"nonce too low", fmt.Errorf("nonce too low"), true, false},
		{"replacement fee too low", fmt.Errorf("replacement fee too low"), false, true},
		{"already known", fmt.Errorf("already known"), false, true},
		{"other error", fmt.Errorf("insufficient funds"), false, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			nonceTooLow, replacementFeeTooLow := isNonceError(tt.err)
			assert.Equal(t, tt.wantNonceTooLow, nonceTooLow)
			assert.Equal(t, tt.wantReplacementFeeLow, replacementFeeTooLow)
		})
	}
}

func TestWeb3Service_Close_WithSubsystems(t *testing.T) {
	ws := &Web3Service{
		logger:            zap.NewNop(),
		multiChainManager: web3.NewMultiChainManager(zap.NewNop()),
		solanaVerifier:    web3.NewSolanaVerifier(zap.NewNop()),
	}
	assert.NotPanics(t, func() { ws.Close() })
}

func TestWeb3Service_GetSolanaVerifier_Set(t *testing.T) {
	sv := web3.NewSolanaVerifier(zap.NewNop())
	defer sv.Close()
	ws := &Web3Service{logger: zap.NewNop(), solanaVerifier: sv}
	assert.Equal(t, sv, ws.GetSolanaVerifier())
}

func TestWeb3Service_DetectContractType_NoChain(t *testing.T) {
	ws := &Web3Service{
		logger:            zap.NewNop(),
		multiChainManager: web3.NewMultiChainManager(zap.NewNop()),
	}
	result := ws.DetectContractType(context.Background(), 99999, "0x1")
	assert.Equal(t, "unknown", result)
}

func TestWeb3Service_VerifyNFTOwnershipAutoDetect_NoChain(t *testing.T) {
	ws := &Web3Service{
		logger:            zap.NewNop(),
		multiChainManager: web3.NewMultiChainManager(zap.NewNop()),
	}
	_, err := ws.VerifyNFTOwnershipAutoDetect(context.Background(), 99999, "0x1", "1", "0x2")
	assert.Error(t, err)
}

func TestWeb3Service_VerifyNFTCollectionAutoDetect_NoChain(t *testing.T) {
	ws := &Web3Service{
		logger:            zap.NewNop(),
		multiChainManager: web3.NewMultiChainManager(zap.NewNop()),
	}
	_, err := ws.VerifyNFTCollectionAutoDetect(context.Background(), 99999, "0x1", "0x2")
	assert.Error(t, err)
}

func TestWeb3Service_GetNFT_NoChain(t *testing.T) {
	ws := &Web3Service{
		logger:            zap.NewNop(),
		multiChainManager: web3.NewMultiChainManager(zap.NewNop()),
	}
	_, err := ws.GetNFT(context.Background(), 99999, "0x1", "1")
	assert.Error(t, err)
}

func TestWeb3Service_GetNFTInfo_NoChain(t *testing.T) {
	ws := &Web3Service{
		logger:            zap.NewNop(),
		multiChainManager: web3.NewMultiChainManager(zap.NewNop()),
	}
	_, err := ws.GetNFTInfo(context.Background(), 99999, "0x1", "1")
	assert.Error(t, err)
}

func TestWeb3Service_HeaderByNumber_NoChain(t *testing.T) {
	ws := &Web3Service{
		logger:            zap.NewNop(),
		multiChainManager: web3.NewMultiChainManager(zap.NewNop()),
		config:            &config.Config{Web3: config.Web3Config{ChainID: 99999}},
	}
	_, err := ws.HeaderByNumber(context.Background(), big.NewInt(1))
	assert.Error(t, err)
}

func TestWeb3Service_GetGasPrice_NoChainManager(t *testing.T) {
	ws := &Web3Service{logger: zap.NewNop()}
	_, err := ws.GetGasPrice(context.Background(), 1)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "multiChainManager not initialized")
}

func TestWeb3Service_GetBalance_NoChainManager(t *testing.T) {
	ws := &Web3Service{
		logger:            zap.NewNop(),
		multiChainManager: web3.NewMultiChainManager(zap.NewNop()),
	}
	_, err := ws.GetBalance(context.Background(), 1, "0x1")
	assert.Error(t, err)
}

func TestWeb3Service_RegisterContent_NoChain(t *testing.T) {
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

func TestWeb3Service_VerifyMerkleWhitelist_InvalidRootHex(t *testing.T) {
	ws := &Web3Service{logger: zap.NewNop()}
	_, err := ws.VerifyMerkleWhitelist("not-hex", "0x742d35Cc6634C0532925a3b844Bc9e7595f2bD18", []string{})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "invalid root hex")
}

func TestWeb3Service_VerifyMerkleWhitelist_InvalidProofHex(t *testing.T) {
	ws := &Web3Service{logger: zap.NewNop()}
	root := "0000000000000000000000000000000000000000000000000000000000000001"
	_, err := ws.VerifyMerkleWhitelist(root, "0x742d35Cc6634C0532925a3b844Bc9e7595f2bD18", []string{"not-hex"})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "invalid proof element")
}

func TestWeb3Service_WaitForReceipt_ContextCancelled(t *testing.T) {
	srv := newTestRPCServer(t, map[string]func(reqParams []json.RawMessage) interface{}{
		"eth_chainId":    func(_ []json.RawMessage) interface{} { return "0xaa36a7" },
		"eth_blockNumber": func(_ []json.RawMessage) interface{} { return "0x1" },
		"eth_getTransactionReceipt": func(_ []json.RawMessage) interface{} {
			return nil
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
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err = ws.WaitForReceipt(ctx, 11155111, "0xabc", 0)
	assert.Error(t, err)
}

func TestWeb3Service_GetSolanaSigner_NotSolanaSigner(t *testing.T) {
	ws := &Web3Service{logger: zap.NewNop()}
	signer := ws.GetSolanaSigner()
	assert.Nil(t, signer)
}

func TestWeb3Service_GetNonceManager_LazyCreation(t *testing.T) {
	srv := newTestRPCServer(t, map[string]func(reqParams []json.RawMessage) interface{}{
		"eth_chainId":    func(_ []json.RawMessage) interface{} { return "0xaa36a7" },
		"eth_blockNumber": func(_ []json.RawMessage) interface{} { return "0x1" },
	})
	defer srv.Close()

	client, err := web3.NewChainClientWithFallback([]string{srv.URL}, 11155111, zap.NewNop())
	require.NoError(t, err)
	defer client.Close()

	ws := &Web3Service{
		logger:            zap.NewNop(),
		multiChainManager: web3.NewMultiChainManager(zap.NewNop()),
		nonceManagers:     make(map[int64]web3.NonceProvider),
	}

	nm1 := ws.getNonceManager(11155111, client)
	assert.NotNil(t, nm1)

	nm2 := ws.getNonceManager(11155111, client)
	assert.Equal(t, nm1, nm2)

	nm3 := ws.getNonceManager(80002, client)
	assert.NotNil(t, nm3)
}

func TestWeb3Service_VerifyMerkleWhitelist_CorrectProof(t *testing.T) {
	ws := &Web3Service{logger: zap.NewNop()}

	root := "1eff7870c4ace369df2f0d164c2f7767d4cf782f867e47e0a0e0b41a0c000000"
	address := "0x742d35Cc6634C0532925a3b844Bc9e7595f2bD18"

	result, err := ws.VerifyMerkleWhitelist(root, address, []string{})
	assert.NoError(t, err)
	assert.False(t, result)
}
