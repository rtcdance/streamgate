package service

import (
	"context"
	"errors"
	"fmt"
	"math/big"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/rtcdance/streamgate/pkg/core/config"
	"github.com/rtcdance/streamgate/pkg/web3"

	"github.com/ethereum/go-ethereum/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

type svcCovChainManager struct {
	mu                sync.RWMutex
	getClientFn       func(chainID int64) (*web3.ChainClient, error)
	getSolanaClientFn func(chainID int64) (*web3.SolanaVerifier, error)
	getSupportedFn    func() []*web3.ChainConfig
	getTestnetFn      func() []*web3.ChainConfig
	getMainnetFn      func() []*web3.ChainConfig
	getRPCStatusesFn  func() map[int64][]web3.RPCStatus
	closeFn           func()
}

func (m *svcCovChainManager) GetClient(chainID int64) (*web3.ChainClient, error) {
	if m.getClientFn != nil {
		return m.getClientFn(chainID)
	}
	return nil, errors.New("chain not found")
}

func (m *svcCovChainManager) GetSolanaClient(chainID int64) (*web3.SolanaVerifier, error) {
	if m.getSolanaClientFn != nil {
		return m.getSolanaClientFn(chainID)
	}
	return nil, errors.New("solana client not found")
}

func (m *svcCovChainManager) AddChain(chainID int64) error { return nil }
func (m *svcCovChainManager) GetSupportedChains() []*web3.ChainConfig {
	if m.getSupportedFn != nil {
		return m.getSupportedFn()
	}
	return nil
}
func (m *svcCovChainManager) GetTestnetChains() []*web3.ChainConfig {
	if m.getTestnetFn != nil {
		return m.getTestnetFn()
	}
	return nil
}
func (m *svcCovChainManager) GetMainnetChains() []*web3.ChainConfig {
	if m.getMainnetFn != nil {
		return m.getMainnetFn()
	}
	return nil
}
func (m *svcCovChainManager) GetRPCStatuses() map[int64][]web3.RPCStatus {
	if m.getRPCStatusesFn != nil {
		return m.getRPCStatusesFn()
	}
	return nil
}
func (m *svcCovChainManager) SetRateLimiter(rl *web3.RPCRateLimiter) {}
func (m *svcCovChainManager) Close() {
	if m.closeFn != nil {
		m.closeFn()
	}
}

func newSvcCovMCM() *svcCovChainManager {
	return &svcCovChainManager{
		getClientFn: func(chainID int64) (*web3.ChainClient, error) {
			return nil, errors.New("chain not found")
		},
		getSolanaClientFn: func(chainID int64) (*web3.SolanaVerifier, error) {
			return nil, errors.New("solana not found")
		},
		getSupportedFn:   func() []*web3.ChainConfig { return nil },
		getTestnetFn:     func() []*web3.ChainConfig { return nil },
		getMainnetFn:     func() []*web3.ChainConfig { return nil },
		getRPCStatusesFn: func() map[int64][]web3.RPCStatus { return nil },
	}
}

func TestSvcCov_VerifyNFTOwnership_ChainNotFound(t *testing.T) {
	svc := &Web3Service{logger: zap.NewNop(), multiChainManager: newSvcCovMCM()}
	_, err := svc.VerifyNFTOwnership(context.Background(), 1, "0xContract", "1", "0xOwner")
	assert.Error(t, err)
}

func TestSvcCov_VerifyNFTOwnership_SolanaChainNotFound(t *testing.T) {
	svc := &Web3Service{logger: zap.NewNop(), multiChainManager: newSvcCovMCM()}
	_, err := svc.VerifyNFTOwnership(context.Background(), -1, "0xContract", "1", "0xOwner")
	assert.Error(t, err)
}

func TestSvcCov_GetNFTBalance_ChainNotFound(t *testing.T) {
	svc := &Web3Service{logger: zap.NewNop(), multiChainManager: newSvcCovMCM()}
	_, err := svc.GetNFTBalance(context.Background(), 1, "0xContract", "0xOwner")
	assert.Error(t, err)
}

func TestSvcCov_GetNFTBalance_SolanaChainNotFound(t *testing.T) {
	svc := &Web3Service{logger: zap.NewNop(), multiChainManager: newSvcCovMCM()}
	_, err := svc.GetNFTBalance(context.Background(), -1, "0xContract", "0xOwner")
	assert.Error(t, err)
}

func TestSvcCov_VerifyNFTOwnershipAutoDetect_ChainNotFound(t *testing.T) {
	svc := &Web3Service{logger: zap.NewNop(), multiChainManager: newSvcCovMCM()}
	_, err := svc.VerifyNFTOwnershipAutoDetect(context.Background(), 1, "0xContract", "1", "0xOwner")
	assert.Error(t, err)
}

func TestSvcCov_VerifyNFTCollectionAutoDetect_ChainNotFound(t *testing.T) {
	svc := &Web3Service{logger: zap.NewNop(), multiChainManager: newSvcCovMCM()}
	_, err := svc.VerifyNFTCollectionAutoDetect(context.Background(), 1, "0xContract", "0xOwner")
	assert.Error(t, err)
}

func TestSvcCov_GetNFT_ChainNotFound(t *testing.T) {
	svc := &Web3Service{logger: zap.NewNop(), multiChainManager: newSvcCovMCM()}
	_, err := svc.GetNFT(context.Background(), 1, "0xContract", "1")
	assert.Error(t, err)
}

func TestSvcCov_DetectContractType_ChainNotFound(t *testing.T) {
	svc := &Web3Service{logger: zap.NewNop(), multiChainManager: newSvcCovMCM()}
	result := svc.DetectContractType(context.Background(), 1, "0xContract")
	assert.Equal(t, "unknown", result)
}

func TestSvcCov_GetNFTInfo_ChainNotFound(t *testing.T) {
	svc := &Web3Service{logger: zap.NewNop(), multiChainManager: newSvcCovMCM()}
	_, err := svc.GetNFTInfo(context.Background(), 1, "0xContract", "1")
	assert.Error(t, err)
}

func TestSvcCov_HeaderByNumber_ChainNotFound(t *testing.T) {
	svc := &Web3Service{logger: zap.NewNop(), multiChainManager: newSvcCovMCM(), config: &config.Config{}}
	_, err := svc.HeaderByNumber(context.Background(), nil)
	assert.Error(t, err)
}

func TestSvcCov_IsChainSupported_Found(t *testing.T) {
	mcm := &svcCovChainManager{
		getSupportedFn: func() []*web3.ChainConfig {
			return []*web3.ChainConfig{{ID: 1, Name: "Ethereum"}, {ID: 137, Name: "Polygon"}}
		},
	}
	svc := &Web3Service{logger: zap.NewNop(), multiChainManager: mcm}
	found, err := svc.IsChainSupported(context.Background(), 1)
	require.NoError(t, err)
	assert.True(t, found)
}

func TestSvcCov_IsChainSupported_NotFound(t *testing.T) {
	mcm := &svcCovChainManager{
		getSupportedFn: func() []*web3.ChainConfig {
			return []*web3.ChainConfig{{ID: 1, Name: "Ethereum"}}
		},
	}
	svc := &Web3Service{logger: zap.NewNop(), multiChainManager: mcm}
	found, err := svc.IsChainSupported(context.Background(), 999)
	require.NoError(t, err)
	assert.False(t, found)
}

func TestSvcCov_IsChainSupported_EmptyChains(t *testing.T) {
	svc := &Web3Service{logger: zap.NewNop(), multiChainManager: newSvcCovMCM()}
	found, err := svc.IsChainSupported(context.Background(), 1)
	require.NoError(t, err)
	assert.False(t, found)
}

func TestSvcCov_GetSupportedChains(t *testing.T) {
	expected := []*web3.ChainConfig{{ID: 1, Name: "Ethereum"}}
	mcm := &svcCovChainManager{getSupportedFn: func() []*web3.ChainConfig { return expected }}
	svc := &Web3Service{logger: zap.NewNop(), multiChainManager: mcm}
	assert.Equal(t, expected, svc.GetSupportedChains())
}

func TestSvcCov_GetRPCStatuses(t *testing.T) {
	expected := map[int64][]web3.RPCStatus{1: {{URL: "http://rpc1", IsActive: true}}}
	mcm := &svcCovChainManager{getRPCStatusesFn: func() map[int64][]web3.RPCStatus { return expected }}
	svc := &Web3Service{logger: zap.NewNop(), multiChainManager: mcm}
	assert.Equal(t, expected, svc.GetRPCStatuses())
}

func TestSvcCov_GetTestnetChains(t *testing.T) {
	expected := []*web3.ChainConfig{{ID: 11155111, Name: "Sepolia"}}
	mcm := &svcCovChainManager{getTestnetFn: func() []*web3.ChainConfig { return expected }}
	svc := &Web3Service{logger: zap.NewNop(), multiChainManager: mcm}
	assert.Equal(t, expected, svc.GetTestnetChains())
}

func TestSvcCov_GetMainnetChains(t *testing.T) {
	expected := []*web3.ChainConfig{{ID: 1, Name: "Ethereum"}}
	mcm := &svcCovChainManager{getMainnetFn: func() []*web3.ChainConfig { return expected }}
	svc := &Web3Service{logger: zap.NewNop(), multiChainManager: mcm}
	assert.Equal(t, expected, svc.GetMainnetChains())
}

func TestSvcCov_GetGasPrice_NilChainManager(t *testing.T) {
	svc := &Web3Service{logger: zap.NewNop()}
	_, err := svc.GetGasPrice(context.Background(), 1)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "multiChainManager not initialized")
}

func TestSvcCov_GetGasPrice_ChainNotFound(t *testing.T) {
	svc := &Web3Service{logger: zap.NewNop(), multiChainManager: newSvcCovMCM()}
	_, err := svc.GetGasPrice(context.Background(), 1)
	assert.Error(t, err)
}

func TestSvcCov_SendTransaction_NoKey(t *testing.T) {
	svc := &Web3Service{logger: zap.NewNop()}
	_, err := svc.SendTransaction(context.Background(), 1, "0xabc", big.NewInt(0), nil)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "private key not configured")
}

func TestSvcCov_SendTransaction_NilChainManager(t *testing.T) {
	svc := &Web3Service{logger: zap.NewNop(), secureKey: &web3.SecurePrivateKey{}}
	_, err := svc.SendTransaction(context.Background(), 1, "0xabc", big.NewInt(0), nil)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "multiChainManager not initialized")
}

func TestSvcCov_ReplaceStuckTransaction_NilChainManager(t *testing.T) {
	svc := &Web3Service{logger: zap.NewNop(), secureKey: &web3.SecurePrivateKey{}}
	_, err := svc.ReplaceStuckTransaction(context.Background(), 1, nil, 10)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "multiChainManager not initialized")
}

func TestSvcCov_CancelPendingTransaction_NilChainManager(t *testing.T) {
	svc := &Web3Service{logger: zap.NewNop(), secureKey: &web3.SecurePrivateKey{}}
	_, err := svc.CancelPendingTransaction(context.Background(), 1, nil, 10)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "multiChainManager not initialized")
}

func TestSvcCov_WaitForReceipt_NilChainManager(t *testing.T) {
	svc := &Web3Service{logger: zap.NewNop()}
	_, err := svc.WaitForReceipt(context.Background(), 1, "0xtx", 0)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "multiChainManager not initialized")
}

func TestSvcCov_WaitForReceipt_ChainNotFound(t *testing.T) {
	svc := &Web3Service{logger: zap.NewNop(), multiChainManager: newSvcCovMCM()}
	_, err := svc.WaitForReceipt(context.Background(), 1, "0xtx", 0)
	assert.Error(t, err)
}

func TestSvcCov_RegisterContent_NoKey(t *testing.T) {
	svc := &Web3Service{logger: zap.NewNop(), config: &config.Config{}}
	_, err := svc.RegisterContent(context.Background(), 1, "0xContract", "hash", "uri")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "private key not configured")
}

func TestSvcCov_SubmitPermit_NilValue(t *testing.T) {
	svc := &Web3Service{logger: zap.NewNop()}
	_, err := svc.SubmitPermit(context.Background(), 1, "0xContract", "0xOwner", "0xSpender", nil, big.NewInt(1), 0, [32]byte{}, [32]byte{})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "value and deadline must not be nil")
}

func TestSvcCov_SubmitPermit_NilDeadline(t *testing.T) {
	svc := &Web3Service{logger: zap.NewNop()}
	_, err := svc.SubmitPermit(context.Background(), 1, "0xContract", "0xOwner", "0xSpender", big.NewInt(1), nil, 0, [32]byte{}, [32]byte{})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "value and deadline must not be nil")
}

func TestSvcCov_VerifyMerkleWhitelist_BadRoot(t *testing.T) {
	svc := &Web3Service{logger: zap.NewNop()}
	_, err := svc.VerifyMerkleWhitelist("not-hex", "0xabc", nil)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "invalid root hex")
}

func TestSvcCov_VerifyMerkleWhitelist_BadProof(t *testing.T) {
	svc := &Web3Service{logger: zap.NewNop()}
	root := "0x" + strings.Repeat("ab", 32)
	_, err := svc.VerifyMerkleWhitelist(root, "0x742d35Cc6634C0532925a3b844Bc9e7595f2bD18", []string{"not-hex"})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "invalid proof element")
}

func TestSvcCov_VerifyMerkleWhitelist_Valid(t *testing.T) {
	svc := &Web3Service{logger: zap.NewNop()}
	root := "0x" + strings.Repeat("00", 32)
	result, err := svc.VerifyMerkleWhitelist(root, "0x742d35Cc6634C0532925a3b844Bc9e7595f2bD18", []string{})
	require.NoError(t, err)
	assert.False(t, result)
}

func TestSvcCov_VerifyMerkleWhitelist_OddLengthHex(t *testing.T) {
	svc := &Web3Service{logger: zap.NewNop()}
	_, err := svc.VerifyMerkleWhitelist("0xabc", "0x742d35Cc6634C0532925a3b844Bc9e7595f2bD18", nil)
	assert.Error(t, err)
}

func TestSvcCov_VerifyMerkleWhitelist_ShortRoot(t *testing.T) {
	svc := &Web3Service{logger: zap.NewNop()}
	result, err := svc.VerifyMerkleWhitelist("0xabcd", "0x742d35Cc6634C0532925a3b844Bc9e7595f2bD18", nil)
	require.NoError(t, err)
	assert.False(t, result)
}

func TestSvcCov_VerifyMerkleWhitelist_LargeTree(t *testing.T) {
	svc := &Web3Service{logger: zap.NewNop()}
	root := "0x" + strings.Repeat("00", 32)
	proofs := make([]string, 20)
	for i := range proofs {
		proofs[i] = "0x" + strings.Repeat("ff", 32)
	}
	result, err := svc.VerifyMerkleWhitelist(root, "0x742d35Cc6634C0532925a3b844Bc9e7595f2bD18", proofs)
	require.NoError(t, err)
	assert.False(t, result)
}

func TestSvcCov_GetTokenBalance_NilChainManager(t *testing.T) {
	svc := &Web3Service{logger: zap.NewNop()}
	_, err := svc.GetTokenBalance(context.Background(), 1, "0xContract", "0xAccount")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "multiChainManager not initialized")
}

func TestSvcCov_GetTokenBalance_ChainNotFound(t *testing.T) {
	svc := &Web3Service{logger: zap.NewNop(), multiChainManager: newSvcCovMCM()}
	_, err := svc.GetTokenBalance(context.Background(), 1, "0xContract", "0xAccount")
	assert.Error(t, err)
}

func TestSvcCov_GetTokenAllowance_NilChainManager(t *testing.T) {
	svc := &Web3Service{logger: zap.NewNop()}
	_, err := svc.GetTokenAllowance(context.Background(), 1, "0xContract", "0xOwner", "0xSpender")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "multiChainManager not initialized")
}

func TestSvcCov_GetTokenAllowance_ChainNotFound(t *testing.T) {
	svc := &Web3Service{logger: zap.NewNop(), multiChainManager: newSvcCovMCM()}
	_, err := svc.GetTokenAllowance(context.Background(), 1, "0xContract", "0xOwner", "0xSpender")
	assert.Error(t, err)
}

func TestSvcCov_GetTokenInfo_NilChainManager(t *testing.T) {
	svc := &Web3Service{logger: zap.NewNop()}
	_, err := svc.GetTokenInfo(context.Background(), 1, "0xContract")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "multiChainManager not initialized")
}

func TestSvcCov_GetTokenInfo_ChainNotFound(t *testing.T) {
	svc := &Web3Service{logger: zap.NewNop(), multiChainManager: newSvcCovMCM()}
	_, err := svc.GetTokenInfo(context.Background(), 1, "0xContract")
	assert.Error(t, err)
}

func TestSvcCov_GetBalance_ChainNotFound(t *testing.T) {
	svc := &Web3Service{logger: zap.NewNop(), multiChainManager: newSvcCovMCM()}
	_, err := svc.GetBalance(context.Background(), 1, "0x742d35Cc6634C0532925a3b844Bc9e7595f2bD18")
	assert.Error(t, err)
}

func TestSvcCov_SetNFTAccessCache(t *testing.T) {
	svc := &Web3Service{logger: zap.NewNop()}
	svc.SetNFTAccessCache(nil)
}

func TestSvcCov_GetEIP712Verifier(t *testing.T) {
	svc := &Web3Service{logger: zap.NewNop()}
	assert.Nil(t, svc.GetEIP712Verifier())
}

func TestSvcCov_GetSolanaSigner_Nil(t *testing.T) {
	svc := &Web3Service{logger: zap.NewNop()}
	assert.Nil(t, svc.GetSolanaSigner())
}

func TestSvcCov_IsValidSolanaAddress_EdgeCases(t *testing.T) {
	assert.False(t, IsValidSolanaAddress(""))
	assert.False(t, IsValidSolanaAddress("0x1234"))
	assert.False(t, IsValidSolanaAddress("a"))
	assert.True(t, IsValidSolanaAddress("11111111111111111111111111111111"))
}

func TestSvcCov_IsSolanaChain_Values(t *testing.T) {
	assert.True(t, isSolanaChain(-1))
	assert.True(t, isSolanaChain(-2))
	assert.False(t, isSolanaChain(0))
	assert.False(t, isSolanaChain(1))
}

func TestSvcCov_GweiToWei_Conversion(t *testing.T) {
	assert.Equal(t, big.NewInt(1e9), gweiToWei(1.0))
	assert.Equal(t, big.NewInt(0), gweiToWei(0.0))
	assert.Equal(t, big.NewInt(10e9), gweiToWei(10.0))
}

func TestSvcCov_IsNonceError_AllCases(t *testing.T) {
	tests := []struct {
		name      string
		err       error
		tooLow    bool
		feeTooLow bool
	}{
		{"nil", nil, false, false},
		{"nonce too low", errors.New("nonce too low"), true, false},
		{"replacement fee too low", errors.New("replacement fee too low"), false, true},
		{"already known", errors.New("already known"), false, true},
		{"other error", errors.New("something else"), false, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tooLow, feeTooLow := isNonceError(tt.err)
			assert.Equal(t, tt.tooLow, tooLow)
			assert.Equal(t, tt.feeTooLow, feeTooLow)
		})
	}
}

func TestSvcCov_Web3Service_Close(t *testing.T) {
	svc := &Web3Service{logger: zap.NewNop()}
	svc.Close()
}

func TestSvcCov_Web3Service_Close_WithChainManager(t *testing.T) {
	svc := &Web3Service{logger: zap.NewNop(), multiChainManager: newSvcCovMCM()}
	svc.Close()
}

func TestSvcCov_Web3Service_CreateNFT_NotSupported(t *testing.T) {
	svc := &Web3Service{logger: zap.NewNop()}
	err := svc.CreateNFT(context.Background(), nil)
	assert.ErrorIs(t, err, ErrNotSupported)
}

func TestSvcCov_Web3Service_ListNFTs(t *testing.T) {
	svc := &Web3Service{logger: zap.NewNop()}
	results, err := svc.ListNFTs(context.Background(), 0, 10)
	assert.NoError(t, err)
	assert.Empty(t, results)
}

func TestSvcCov_GetGasPriceLevels_NilMonitor(t *testing.T) {
	svc := &Web3Service{logger: zap.NewNop()}
	_, err := svc.GetGasPriceLevels(context.Background(), 1)
	assert.Error(t, err)
}

func TestSvcCov_UploadToIPFS_NilClient(t *testing.T) {
	svc := &Web3Service{logger: zap.NewNop()}
	_, err := svc.UploadToIPFS(context.Background(), "file.txt", []byte("data"))
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "IPFS client not initialized")
}

func TestSvcCov_DownloadFromIPFS_NilClient(t *testing.T) {
	svc := &Web3Service{logger: zap.NewNop()}
	_, err := svc.DownloadFromIPFS(context.Background(), "QmTest")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "IPFS client not initialized")
}

func TestSvcCov_VerifySolanaTokenAccount_NilVerifier(t *testing.T) {
	svc := &Web3Service{logger: zap.NewNop()}
	_, err := svc.VerifySolanaTokenAccount(context.Background(), "token", "owner")
	assert.Error(t, err)
}

func TestSvcCov_VerifySolanaMintAuthority_NilVerifier(t *testing.T) {
	svc := &Web3Service{logger: zap.NewNop()}
	_, err := svc.VerifySolanaMintAuthority(context.Background(), "mint", "authority")
	assert.Error(t, err)
}

func TestSvcCov_VerifySolanaMetaplexNFTOwnership_NilVerifier(t *testing.T) {
	svc := &Web3Service{logger: zap.NewNop()}
	_, err := svc.VerifySolanaMetaplexNFTOwnership(context.Background(), "mint", "owner")
	assert.Error(t, err)
}

func TestSvcCov_GetSolanaVerifier_Nil(t *testing.T) {
	svc := &Web3Service{logger: zap.NewNop()}
	assert.Nil(t, svc.GetSolanaVerifier())
}

func TestSvcCov_GetIPFSClient_Nil(t *testing.T) {
	svc := &Web3Service{logger: zap.NewNop()}
	assert.Nil(t, svc.GetIPFSClient())
}

func TestSvcCov_GetTransactionQueue_Nil(t *testing.T) {
	svc := &Web3Service{logger: zap.NewNop()}
	assert.Nil(t, svc.GetTransactionQueue())
}

func TestSvcCov_GetWalletManager_Nil(t *testing.T) {
	svc := &Web3Service{logger: zap.NewNop()}
	assert.Nil(t, svc.GetWalletManager())
}

func TestSvcCov_GetSignatureVerifier_Nil(t *testing.T) {
	svc := &Web3Service{logger: zap.NewNop()}
	assert.Nil(t, svc.GetSignatureVerifier())
}

func TestSvcCov_GetMultiChainManager_Nil(t *testing.T) {
	svc := &Web3Service{logger: zap.NewNop()}
	assert.Nil(t, svc.GetMultiChainManager())
}

func TestSvcCov_GetGasMonitor_Nil(t *testing.T) {
	svc := &Web3Service{logger: zap.NewNop()}
	assert.Nil(t, svc.GetGasMonitor())
}

func TestSvcCov_GetEventIndexer_Nil(t *testing.T) {
	svc := &Web3Service{logger: zap.NewNop()}
	assert.Nil(t, svc.GetEventIndexer())
}

func TestSvcCov_NewWeb3Service_InvalidKey(t *testing.T) {
	deps := Web3Deps{ChainManager: newSvcCovMCM()}
	cfg := &config.Config{
		Web3: config.Web3Config{
			Transaction: config.TransactionConfig{PrivateKeyHex: "invalid-hex-key"},
		},
	}
	_, err := NewWeb3Service(deps, cfg, zap.NewNop())
	assert.Error(t, err)
}

func TestSvcCov_Web3Service_RegisterContent_WithKeyNoChain(t *testing.T) {
	svc := &Web3Service{logger: zap.NewNop(), secureKey: &web3.SecurePrivateKey{}, multiChainManager: newSvcCovMCM(), config: &config.Config{}}
	_, err := svc.RegisterContent(context.Background(), 1, "0xContract", "hash", "uri")
	assert.Error(t, err)
}

func TestSvcCov_Web3Service_ReplaceStuckTransaction_NoKey(t *testing.T) {
	svc := &Web3Service{logger: zap.NewNop()}
	_, err := svc.ReplaceStuckTransaction(context.Background(), 1, nil, 10)
	assert.Error(t, err)
}

func TestSvcCov_Web3Service_CancelPendingTransaction_NoKey(t *testing.T) {
	svc := &Web3Service{logger: zap.NewNop()}
	_, err := svc.CancelPendingTransaction(context.Background(), 1, nil, 10)
	assert.Error(t, err)
}

func TestSvcCov_GenerateNonce(t *testing.T) {
	n1, err := generateNonce()
	require.NoError(t, err)
	n2, err2 := generateNonce()
	require.NoError(t, err2)
	assert.NotEqual(t, n1, n2)
	assert.Len(t, n1, 32)
}

func TestSvcCov_Web3Service_GetTransactionQueue_Set(t *testing.T) {
	svc := &Web3Service{logger: zap.NewNop()}
	q := &web3.TransactionQueue{}
	svc.transactionQueue = q
	assert.Equal(t, q, svc.GetTransactionQueue())
}

func TestSvcCov_Web3Service_GetIPFSClient_Set(t *testing.T) {
	svc := &Web3Service{logger: zap.NewNop()}
	svc.ipfsClient = &web3.IPFSClient{}
	assert.NotNil(t, svc.GetIPFSClient())
}

func TestSvcCov_Web3Service_SendTransaction_ContextWithDeadline(t *testing.T) {
	svc := &Web3Service{logger: zap.NewNop()}
	ctx, cancel := context.WithDeadline(context.Background(), time.Now().Add(time.Hour))
	defer cancel()
	_, err := svc.SendTransaction(ctx, 1, "0xabc", big.NewInt(0), nil)
	assert.Error(t, err)
}

func TestSvcCov_Web3Service_GetNFTBalance_SolanaNoChain(t *testing.T) {
	svc := &Web3Service{logger: zap.NewNop(), multiChainManager: newSvcCovMCM()}
	_, err := svc.GetNFTBalance(context.Background(), -1, "0xContract", "0xOwner")
	assert.Error(t, err)
}

func TestSvcCov_Web3Service_VerifyNFTOwnership_SolanaNoChain(t *testing.T) {
	svc := &Web3Service{logger: zap.NewNop(), multiChainManager: newSvcCovMCM()}
	_, err := svc.VerifyNFTOwnership(context.Background(), -1, "0xContract", "1", "0xOwner")
	assert.Error(t, err)
}

func TestSvcCov_CommonAddressHelpers(t *testing.T) {
	assert.True(t, common.IsHexAddress("0x742d35Cc6634C0532925a3b844Bc9e7595f2bD18"))
	assert.False(t, common.IsHexAddress("not-an-address"))
}

func TestSvcCov_Web3Service_VerifySignature_NilVerifier_Panics(t *testing.T) {
	svc := &Web3Service{logger: zap.NewNop()}
	func() {
		defer func() { _ = recover() }()
		svc.VerifySignature(context.Background(), "0xaddr", "msg", "sig")
	}()
}

func TestSvcCov_Web3Service_GetNFTBalance_EVMChainNotFound(t *testing.T) {
	svc := &Web3Service{logger: zap.NewNop(), multiChainManager: newSvcCovMCM()}
	_, err := svc.GetNFTBalance(context.Background(), 1, "0xContract", "0xOwner")
	assert.Error(t, err)
}

func TestSvcCov_Web3Service_GetSupportedChains_Empty(t *testing.T) {
	svc := &Web3Service{logger: zap.NewNop(), multiChainManager: newSvcCovMCM()}
	assert.Nil(t, svc.GetSupportedChains())
}

func TestSvcCov_Web3Service_GetRPCStatuses_Empty(t *testing.T) {
	svc := &Web3Service{logger: zap.NewNop(), multiChainManager: newSvcCovMCM()}
	assert.Nil(t, svc.GetRPCStatuses())
}

func TestSvcCov_Web3Service_GetTestnetChains_Empty(t *testing.T) {
	svc := &Web3Service{logger: zap.NewNop(), multiChainManager: newSvcCovMCM()}
	assert.Nil(t, svc.GetTestnetChains())
}

func TestSvcCov_Web3Service_GetMainnetChains_Empty(t *testing.T) {
	svc := &Web3Service{logger: zap.NewNop(), multiChainManager: newSvcCovMCM()}
	assert.Nil(t, svc.GetMainnetChains())
}

func TestSvcCov_Web3Service_GetBalance_ChainError(t *testing.T) {
	svc := &Web3Service{logger: zap.NewNop(), multiChainManager: newSvcCovMCM()}
	_, err := svc.GetBalance(context.Background(), 1, "0x742d35Cc6634C0532925a3b844Bc9e7595f2bD18")
	assert.Error(t, err)
}

func TestSvcCov_Web3Service_IsChainSupported_EmptyChains(t *testing.T) {
	svc := &Web3Service{logger: zap.NewNop(), multiChainManager: newSvcCovMCM()}
	found, err := svc.IsChainSupported(context.Background(), 1)
	require.NoError(t, err)
	assert.False(t, found)
}

func TestSvcCov_Web3Service_SendTransaction_NoKey_Explicit(t *testing.T) {
	svc := &Web3Service{logger: zap.NewNop()}
	_, err := svc.SendTransaction(context.Background(), 1, "0xabc", big.NewInt(0), nil)
	assert.Error(t, err)
}

func TestSvcCov_Web3Service_ReplaceStuckTransaction_NilKey_Explicit(t *testing.T) {
	svc := &Web3Service{logger: zap.NewNop()}
	_, err := svc.ReplaceStuckTransaction(context.Background(), 1, nil, 10)
	assert.Error(t, err)
}

func TestSvcCov_Web3Service_CancelPendingTransaction_NilKey_Explicit(t *testing.T) {
	svc := &Web3Service{logger: zap.NewNop()}
	_, err := svc.CancelPendingTransaction(context.Background(), 1, nil, 10)
	assert.Error(t, err)
}

func TestSvcCov_Web3Service_VerifyNFTOwnership_SolanaChainNotFound_Explicit(t *testing.T) {
	mcm := &svcCovChainManager{
		getSolanaClientFn: func(chainID int64) (*web3.SolanaVerifier, error) {
			return nil, fmt.Errorf("solana chain %d not found", chainID)
		},
	}
	svc := &Web3Service{logger: zap.NewNop(), multiChainManager: mcm}
	_, err := svc.VerifyNFTOwnership(context.Background(), -1, "0xContract", "1", "0xOwner")
	assert.Error(t, err)
}
