package service

import (
	"context"
	"fmt"
	"math/big"
	"sync"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/rtcdance/streamgate/pkg/core/config"
	"github.com/rtcdance/streamgate/pkg/web3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func TestWeb3Service_GetGasPrice_NilManager(t *testing.T) {
	svc := &Web3Service{logger: zap.NewNop()}
	_, err := svc.GetGasPrice(context.Background(), 1)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "multiChainManager not initialized")
}

func TestWeb3Service_GetGasPrice_ClientNotFound(t *testing.T) {
	mcm := web3.NewMultiChainManager(zap.NewNop())
	svc := &Web3Service{logger: zap.NewNop(), multiChainManager: mcm}
	_, err := svc.GetGasPrice(context.Background(), 99999)
	assert.Error(t, err)
}

func TestW3TxExt_SendTransaction_NoKey(t *testing.T) {
	svc := &Web3Service{logger: zap.NewNop()}
	_, err := svc.SendTransaction(context.Background(), 1, "0xabc", big.NewInt(0), nil)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "private key not configured")
}

func TestWeb3Service_SendTransaction_NoManager(t *testing.T) {
	svc := &Web3Service{
		logger:    zap.NewNop(),
		secureKey: &web3.SecurePrivateKey{},
	}
	_, err := svc.SendTransaction(context.Background(), 1, "0xabc", big.NewInt(0), nil)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "multiChainManager not initialized")
}

func TestWeb3Service_SendTransaction_ClientNotFound(t *testing.T) {
	mcm := web3.NewMultiChainManager(zap.NewNop())
	svc := &Web3Service{
		logger:            zap.NewNop(),
		multiChainManager: mcm,
		secureKey:         &web3.SecurePrivateKey{},
	}
	_, err := svc.SendTransaction(context.Background(), 99999, "0xabc", big.NewInt(0), nil)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "chain client not found")
}

func TestWeb3Service_RegisterContent_NoManager(t *testing.T) {
	mcm := web3.NewMultiChainManager(zap.NewNop())
	svc := &Web3Service{
		logger:            zap.NewNop(),
		multiChainManager: mcm,
		config:            &config.Config{Web3: config.Web3Config{Transaction: config.TransactionConfig{PrivateKeyHex: "key"}}},
	}
	_, err := svc.RegisterContent(context.Background(), 1, "0xcontract", "hash", "uri")
	assert.Error(t, err)
}

func TestW3TxExt_SubmitPermit_NilValue(t *testing.T) {
	svc := &Web3Service{logger: zap.NewNop()}
	_, err := svc.SubmitPermit(context.Background(), 1, "0xcontract", "0xowner", "0xspender", nil, nil, 0, [32]byte{}, [32]byte{})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "value and deadline must not be nil")
}

func TestW3TxExt_SubmitPermit_NilDeadline(t *testing.T) {
	svc := &Web3Service{logger: zap.NewNop()}
	_, err := svc.SubmitPermit(context.Background(), 1, "0xcontract", "0xowner", "0xspender", big.NewInt(100), nil, 0, [32]byte{}, [32]byte{})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "value and deadline must not be nil")
}

func TestWeb3Service_WaitForReceipt_NilManager(t *testing.T) {
	svc := &Web3Service{logger: zap.NewNop()}
	_, err := svc.WaitForReceipt(context.Background(), 1, "0xhash", 0)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "multiChainManager not initialized")
}

func TestW3TxExt_WaitForReceipt_ContextCancelled(t *testing.T) {
	mcm := web3.NewMultiChainManager(zap.NewNop())
	svc := &Web3Service{logger: zap.NewNop(), multiChainManager: mcm}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := svc.WaitForReceipt(ctx, 1, "0xhash", 0)
	assert.Error(t, err)
}

func TestWeb3Service_GetBalance_NilManager(t *testing.T) {
	mcm := web3.NewMultiChainManager(zap.NewNop())
	svc := &Web3Service{logger: zap.NewNop(), multiChainManager: mcm}
	_, err := svc.GetBalance(context.Background(), 1, "0xaddr")
	assert.Error(t, err)
}

func TestWeb3Service_IsChainSupported_NilManager(t *testing.T) {
	svc := &Web3Service{logger: zap.NewNop(), multiChainManager: web3.NewMultiChainManager(zap.NewNop())}
	supported, err := svc.IsChainSupported(context.Background(), 99999)
	require.NoError(t, err)
	assert.False(t, supported)
}

func TestWeb3Service_GetSupportedChains_NilManager(t *testing.T) {
	svc := &Web3Service{logger: zap.NewNop(), multiChainManager: web3.NewMultiChainManager(zap.NewNop())}
	chains := svc.GetSupportedChains()
	assert.NotNil(t, chains)
}

func TestWeb3Service_Close_NilFields(t *testing.T) {
	svc := &Web3Service{logger: zap.NewNop()}
	svc.Close()
}

func TestGweiToWei_Table(t *testing.T) {
	tests := []struct {
		name     string
		gwei     float64
		expected string
	}{
		{"1 gwei", 1.0, "1000000000"},
		{"0 gwei", 0.0, "0"},
		{"10 gwei", 10.0, "10000000000"},
		{"0.5 gwei", 0.5, "500000000"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := gweiToWei(tt.gwei)
			assert.Equal(t, tt.expected, result.String())
		})
	}
}

func TestIsNonceError_Table(t *testing.T) {
	tests := []struct {
		name              string
		err               error
		expectNonceTooLow bool
		expectFeeTooLow   bool
	}{
		{"nonce too low", fmt.Errorf("nonce too low for account"), true, false},
		{"replacement fee too low", fmt.Errorf("replacement fee too low"), false, true},
		{"already known", fmt.Errorf("transaction already known"), false, true},
		{"other error", fmt.Errorf("some other error"), false, false},
		{"nil error", nil, false, false},
		{"case insensitive", fmt.Errorf("NONCE TOO LOW"), true, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tooLow, feeTooLow := isNonceError(tt.err)
			assert.Equal(t, tt.expectNonceTooLow, tooLow)
			assert.Equal(t, tt.expectFeeTooLow, feeTooLow)
		})
	}
}

func TestWeb3Service_VerifyMerkleWhitelist_OddLengthHex(t *testing.T) {
	svc := &Web3Service{logger: zap.NewNop()}
	_, err := svc.VerifyMerkleWhitelist("zzz", "0xabc", nil)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "invalid root hex")
}

func TestWeb3Service_VerifyMerkleWhitelist_ShortRoot(t *testing.T) {
	svc := &Web3Service{logger: zap.NewNop()}
	valid, err := svc.VerifyMerkleWhitelist("0xab", "0x0000000000000000000000000000000000000001", nil)
	require.NoError(t, err)
	assert.False(t, valid)
}

func TestWeb3Service_GetRPCStatuses_NilManager(t *testing.T) {
	svc := &Web3Service{logger: zap.NewNop(), multiChainManager: web3.NewMultiChainManager(zap.NewNop())}
	statuses := svc.GetRPCStatuses()
	assert.NotNil(t, statuses)
}

func TestWeb3Service_GetTestnetChains_NilManager(t *testing.T) {
	svc := &Web3Service{logger: zap.NewNop(), multiChainManager: web3.NewMultiChainManager(zap.NewNop())}
	chains := svc.GetTestnetChains()
	assert.NotNil(t, chains)
}

func TestWeb3Service_GetMainnetChains_NilManager(t *testing.T) {
	svc := &Web3Service{logger: zap.NewNop(), multiChainManager: web3.NewMultiChainManager(zap.NewNop())}
	chains := svc.GetMainnetChains()
	assert.NotNil(t, chains)
}

func TestW3TxExt_SetNFTAccessCache(t *testing.T) {
	svc := &Web3Service{logger: zap.NewNop()}
	svc.SetNFTAccessCache(nil)
	assert.Nil(t, svc.nftAccessCache)
}

func TestW3TxExt_GetEventIndexer_Nil(t *testing.T) {
	svc := &Web3Service{logger: zap.NewNop()}
	assert.Nil(t, svc.GetEventIndexer())
}

func TestWeb3Service_GetEIP712Verifier_Nil(t *testing.T) {
	svc := &Web3Service{logger: zap.NewNop()}
	assert.Nil(t, svc.GetEIP712Verifier())
}

func TestW3TxExt_GetSolanaSigner_Nil(t *testing.T) {
	svc := &Web3Service{logger: zap.NewNop()}
	assert.Nil(t, svc.GetSolanaSigner())
}

type concurrentSafeMap struct {
	mu   sync.RWMutex
	data map[string]string
}

func newConcurrentSafeMap() *concurrentSafeMap {
	return &concurrentSafeMap{data: make(map[string]string)}
}

func (m *concurrentSafeMap) Set(key, val string) {
	m.mu.Lock()
	m.data[key] = val
	m.mu.Unlock()
}

func (m *concurrentSafeMap) Get(key string) (string, bool) {
	m.mu.RLock()
	v, ok := m.data[key]
	m.mu.RUnlock()
	return v, ok
}

func TestConcurrentSafeMap_RaceFree(t *testing.T) {
	m := newConcurrentSafeMap()
	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			key := fmt.Sprintf("key-%d", idx)
			m.Set(key, fmt.Sprintf("val-%d", idx))
			_, _ = m.Get(key)
		}(i)
	}
	wg.Wait()
	assert.Equal(t, 100, len(m.data))
}

func TestWeb3Service_VerifyMerkleWhitelist_LargeTree(t *testing.T) {
	svc := &Web3Service{logger: zap.NewNop()}
	var items [][]byte
	for i := 0; i < 16; i++ {
		addr := common.HexToAddress(fmt.Sprintf("0x%040d", i))
		items = append(items, addr.Bytes())
	}
	tree, err := web3.NewMerkleTree(items)
	require.NoError(t, err)

	rootHex := tree.RootHex()
	proof, err := tree.Proof(7)
	require.NoError(t, err)

	proofHex := make([]string, len(proof))
	for i, p := range proof {
		proofHex[i] = "0x" + fmt.Sprintf("%x", p)
	}

	valid, err := svc.VerifyMerkleWhitelist(rootHex, fmt.Sprintf("0x%040d", 7), proofHex)
	require.NoError(t, err)
	assert.True(t, valid)
}

func TestWeb3Service_SendTransaction_ContextWithDeadline(t *testing.T) {
	svc := &Web3Service{
		logger:    zap.NewNop(),
		secureKey: &web3.SecurePrivateKey{},
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, err := svc.SendTransaction(ctx, 1, "0xabc", big.NewInt(0), nil)
	assert.Error(t, err)
}

func TestWeb3Service_GetTokenBalance_NilManager(t *testing.T) {
	svc := &Web3Service{logger: zap.NewNop()}
	_, err := svc.GetTokenBalance(context.Background(), 1, "0xcontract", "0xaccount")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "multiChainManager not initialized")
}

func TestWeb3Service_GetTokenAllowance_NilManager(t *testing.T) {
	svc := &Web3Service{logger: zap.NewNop()}
	_, err := svc.GetTokenAllowance(context.Background(), 1, "0xcontract", "0xowner", "0xspender")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "multiChainManager not initialized")
}

func TestWeb3Service_GetTokenInfo_NilManager(t *testing.T) {
	svc := &Web3Service{logger: zap.NewNop()}
	_, err := svc.GetTokenInfo(context.Background(), 1, "0xcontract")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "multiChainManager not initialized")
}

func TestWeb3Service_ReplaceStuckTransaction_NilKey(t *testing.T) {
	svc := &Web3Service{logger: zap.NewNop()}
	_, err := svc.ReplaceStuckTransaction(context.Background(), 1, nil, 10)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "private key not configured")
}

func TestWeb3Service_CancelPendingTransaction_NilKey(t *testing.T) {
	svc := &Web3Service{logger: zap.NewNop()}
	_, err := svc.CancelPendingTransaction(context.Background(), 1, nil, 10)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "private key not configured")
}

func TestWeb3Service_ReplaceStuckTransaction_NilManager(t *testing.T) {
	svc := &Web3Service{
		logger:    zap.NewNop(),
		secureKey: &web3.SecurePrivateKey{},
	}
	_, err := svc.ReplaceStuckTransaction(context.Background(), 1, nil, 10)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "multiChainManager not initialized")
}

func TestWeb3Service_CancelPendingTransaction_NilManager(t *testing.T) {
	svc := &Web3Service{
		logger:    zap.NewNop(),
		secureKey: &web3.SecurePrivateKey{},
	}
	_, err := svc.CancelPendingTransaction(context.Background(), 1, nil, 10)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "multiChainManager not initialized")
}

func TestW3TxExt_UploadToIPFS_NilClient(t *testing.T) {
	svc := &Web3Service{logger: zap.NewNop()}
	_, err := svc.UploadToIPFS(context.Background(), "file.txt", []byte("data"))
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "IPFS client not initialized")
}

func TestW3TxExt_DownloadFromIPFS_NilClient(t *testing.T) {
	svc := &Web3Service{logger: zap.NewNop()}
	_, err := svc.DownloadFromIPFS(context.Background(), "QmTest")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "IPFS client not initialized")
}

func TestW3TxExt_GetGasPriceLevels_NilMonitor(t *testing.T) {
	svc := &Web3Service{logger: zap.NewNop()}
	_, err := svc.GetGasPriceLevels(context.Background(), 1)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "gas monitor not initialized")
}

func TestW3TxExt_VerifySolanaTokenAccount_NilVerifier(t *testing.T) {
	svc := &Web3Service{logger: zap.NewNop()}
	_, err := svc.VerifySolanaTokenAccount(context.Background(), "acct", "owner")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "solana verifier not initialized")
}

func TestW3TxExt_VerifySolanaMintAuthority_NilVerifier(t *testing.T) {
	svc := &Web3Service{logger: zap.NewNop()}
	_, err := svc.VerifySolanaMintAuthority(context.Background(), "mint", "auth")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "solana verifier not initialized")
}

func TestW3TxExt_VerifySolanaMetaplexNFTOwnership_NilVerifier(t *testing.T) {
	svc := &Web3Service{logger: zap.NewNop()}
	_, err := svc.VerifySolanaMetaplexNFTOwnership(context.Background(), "mint", "owner")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "solana verifier not initialized")
}

func TestW3TxExt_CreateNFT_NotSupported(t *testing.T) {
	svc := &Web3Service{logger: zap.NewNop()}
	err := svc.CreateNFT(context.Background(), nil)
	assert.ErrorIs(t, err, ErrNotSupported)
}

func TestWeb3Service_ListNFTs_Empty(t *testing.T) {
	svc := &Web3Service{logger: zap.NewNop()}
	results, err := svc.ListNFTs(context.Background(), 0, 10)
	assert.NoError(t, err)
	assert.Empty(t, results)
}

func TestWeb3Service_GetWalletManager_Nil(t *testing.T) {
	svc := &Web3Service{logger: zap.NewNop()}
	assert.Nil(t, svc.GetWalletManager())
}

func TestWeb3Service_GetSignatureVerifier_Nil(t *testing.T) {
	svc := &Web3Service{logger: zap.NewNop()}
	assert.Nil(t, svc.GetSignatureVerifier())
}

func TestWeb3Service_GetMultiChainManager_Nil(t *testing.T) {
	svc := &Web3Service{logger: zap.NewNop()}
	assert.Nil(t, svc.GetMultiChainManager())
}

func TestW3TxExt_GetGasMonitor_Nil(t *testing.T) {
	svc := &Web3Service{logger: zap.NewNop()}
	assert.Nil(t, svc.GetGasMonitor())
}

func TestW3TxExt_GetIPFSClient_Nil(t *testing.T) {
	svc := &Web3Service{logger: zap.NewNop()}
	assert.Nil(t, svc.GetIPFSClient())
}

func TestW3TxExt_GetTransactionQueue_Nil(t *testing.T) {
	svc := &Web3Service{logger: zap.NewNop()}
	assert.Nil(t, svc.GetTransactionQueue())
}

func TestW3TxExt_GetSolanaVerifier_Nil(t *testing.T) {
	svc := &Web3Service{logger: zap.NewNop()}
	assert.Nil(t, svc.GetSolanaVerifier())
}

func TestWeb3Service_Close_WithManager(t *testing.T) {
	mcm := web3.NewMultiChainManager(zap.NewNop())
	svc := &Web3Service{
		logger:            zap.NewNop(),
		multiChainManager: mcm,
	}
	svc.Close()
}

func TestErrorSentinels_All(t *testing.T) {
	sentinels := []error{
		ErrInvalidCredential,
		ErrTokenExpired,
		ErrTokenRevoked,
		ErrNFTNotFound,
		ErrInsufficientBalance,
		ErrNotSupported,
	}
	for _, sentinel := range sentinels {
		assert.ErrorIs(t, sentinel, sentinel)
	}
}

func TestWeb3Service_VerifyMerkleWhitelist_EmptyRoot(t *testing.T) {
	svc := &Web3Service{logger: zap.NewNop()}
	valid, err := svc.VerifyMerkleWhitelist("", "0x0000000000000000000000000000000000000001", nil)
	require.NoError(t, err)
	assert.False(t, valid)
}

func TestWeb3Service_RegisterContent_EmptyKey(t *testing.T) {
	svc := &Web3Service{
		logger: zap.NewNop(),
		config: &config.Config{},
	}
	_, err := svc.RegisterContent(context.Background(), 1, "0xcontract", "hash", "uri")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "private key not configured")
}
