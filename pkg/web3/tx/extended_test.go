package tx

import (
	"context"
	"crypto/ecdsa"
	"fmt"
	"math/big"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

type mockTxClient struct {
	sendTxFn      func(ctx context.Context, tx *types.Transaction) error
	estimateGasFn func(ctx context.Context, msg ethereum.CallMsg) (uint64, error)
	suggestTipFn  func(ctx context.Context) (*big.Int, error)
	getGasPriceFn func(ctx context.Context) (*big.Int, error)
	headerByNumFn func(ctx context.Context, number *big.Int) (*types.Header, error)
	receiptFn     func(ctx context.Context, hash common.Hash) (*types.Receipt, error)
	blockNumberFn func(ctx context.Context) (uint64, error)
}

func (m *mockTxClient) SendTransaction(ctx context.Context, tx *types.Transaction) error {
	if m.sendTxFn != nil {
		return m.sendTxFn(ctx, tx)
	}
	return nil
}

func (m *mockTxClient) EstimateGas(ctx context.Context, msg ethereum.CallMsg) (uint64, error) {
	if m.estimateGasFn != nil {
		return m.estimateGasFn(ctx, msg)
	}
	return 200000, nil
}

func (m *mockTxClient) SuggestGasTipCap(ctx context.Context) (*big.Int, error) {
	if m.suggestTipFn != nil {
		return m.suggestTipFn(ctx)
	}
	return big.NewInt(2_000_000_000), nil
}

func (m *mockTxClient) GetGasPrice(ctx context.Context) (*big.Int, error) {
	if m.getGasPriceFn != nil {
		return m.getGasPriceFn(ctx)
	}
	return big.NewInt(3_000_000_000), nil
}

func (m *mockTxClient) HeaderByNumber(ctx context.Context, number *big.Int) (*types.Header, error) {
	if m.headerByNumFn != nil {
		return m.headerByNumFn(ctx, number)
	}
	return &types.Header{BaseFee: big.NewInt(1_000_000_000)}, nil
}

func (m *mockTxClient) TransactionReceipt(ctx context.Context, hash common.Hash) (*types.Receipt, error) {
	if m.receiptFn != nil {
		return m.receiptFn(ctx, hash)
	}
	return nil, nil
}

func (m *mockTxClient) GetBlockNumber(ctx context.Context) (uint64, error) {
	if m.blockNumberFn != nil {
		return m.blockNumberFn(ctx)
	}
	return 100, nil
}

type mockKeyProvider struct {
	key *ecdsa.PrivateKey
	err error
}

func (mkp *mockKeyProvider) UseKey(fn func(*ecdsa.PrivateKey) error) error {
	if mkp.err != nil {
		return mkp.err
	}
	return fn(mkp.key)
}

type mockNonceProvider struct {
	nonce     uint64
	nonceErr  error
	rollbacks map[string]map[uint64]struct{}
}

func (mnp *mockNonceProvider) NextNonce(ctx context.Context, address string) (uint64, error) {
	if mnp.nonceErr != nil {
		return 0, mnp.nonceErr
	}
	n := mnp.nonce
	mnp.nonce++
	return n, nil
}

func (mnp *mockNonceProvider) Rollback(address string, nonce uint64) {
	if mnp.rollbacks == nil {
		mnp.rollbacks = make(map[string]map[uint64]struct{})
	}
	if mnp.rollbacks[address] == nil {
		mnp.rollbacks[address] = make(map[uint64]struct{})
	}
	mnp.rollbacks[address][nonce] = struct{}{}
}

func (mnp *mockNonceProvider) Reset(address string) {}

func TestContractWriter_SendTx_EIP1559(t *testing.T) {
	privateKey, err := crypto.GenerateKey()
	require.NoError(t, err)

	client := &mockTxClient{
		sendTxFn: func(ctx context.Context, tx *types.Transaction) error {
			return nil
		},
	}

	keyProvider := &mockKeyProvider{key: privateKey}
	nonceMgr := &mockNonceProvider{nonce: 0}

	parsedABI := getTestABI(t)

	cw := NewContractWriter(ContractWriterConfig{
		Client:      client,
		Key:         keyProvider,
		NonceMgr:    nonceMgr,
		FromAddress: crypto.PubkeyToAddress(privateKey.PublicKey).Hex(),
		ChainID:     1,
		Logger:      zap.NewNop(),
	})

	result, err := cw.SendTx(context.Background(), ContractTxOpts{
		To:        "0x1234567890123456789012345678901234567890",
		Method:    "registerContent",
		ParsedABI: parsedABI,
		Args:      []interface{}{[32]byte{0x01}, "test"},
		GasLimit:  200000,
	})

	require.NoError(t, err)
	assert.NotEmpty(t, result.TxHash)
	assert.Equal(t, uint64(0), result.Nonce)
	assert.Equal(t, uint64(200000), result.GasLimit)
}

func TestContractWriter_SendTx_Legacy(t *testing.T) {
	privateKey, err := crypto.GenerateKey()
	require.NoError(t, err)

	client := &mockTxClient{
		suggestTipFn: func(ctx context.Context) (*big.Int, error) {
			return nil, assert.AnError
		},
		getGasPriceFn: func(ctx context.Context) (*big.Int, error) {
			return big.NewInt(3_000_000_000), nil
		},
	}

	keyProvider := &mockKeyProvider{key: privateKey}
	nonceMgr := &mockNonceProvider{nonce: 5}

	parsedABI := getTestABI(t)

	cw := NewContractWriter(ContractWriterConfig{
		Client:      client,
		Key:         keyProvider,
		NonceMgr:    nonceMgr,
		FromAddress: crypto.PubkeyToAddress(privateKey.PublicKey).Hex(),
		ChainID:     1,
		Logger:      zap.NewNop(),
	})

	result, err := cw.SendTx(context.Background(), ContractTxOpts{
		To:        "0x1234567890123456789012345678901234567890",
		Method:    "registerContent",
		ParsedABI: parsedABI,
		Args:      []interface{}{[32]byte{0x01}, "test"},
		GasLimit:  200000,
	})

	require.NoError(t, err)
	assert.NotEmpty(t, result.TxHash)
	assert.Equal(t, uint64(5), result.Nonce)
	assert.NotNil(t, result.GasPrice)
	assert.Nil(t, result.MaxFeePerGas)
}

func TestContractWriter_SendTx_NonceError(t *testing.T) {
	privateKey, err := crypto.GenerateKey()
	require.NoError(t, err)

	client := &mockTxClient{}
	keyProvider := &mockKeyProvider{key: privateKey}
	nonceMgr := &mockNonceProvider{nonceErr: assert.AnError}

	parsedABI := getTestABI(t)

	cw := NewContractWriter(ContractWriterConfig{
		Client:      client,
		Key:         keyProvider,
		NonceMgr:    nonceMgr,
		FromAddress: crypto.PubkeyToAddress(privateKey.PublicKey).Hex(),
		ChainID:     1,
		Logger:      zap.NewNop(),
	})

	_, err = cw.SendTx(context.Background(), ContractTxOpts{
		To:        "0x1234567890123456789012345678901234567890",
		Method:    "registerContent",
		ParsedABI: parsedABI,
		Args:      []interface{}{[32]byte{0x01}, "test"},
		GasLimit:  200000,
	})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "get nonce")
}

func TestContractWriter_SendTx_SendError_Rollback(t *testing.T) {
	privateKey, err := crypto.GenerateKey()
	require.NoError(t, err)

	client := &mockTxClient{
		sendTxFn: func(ctx context.Context, tx *types.Transaction) error {
			return assert.AnError
		},
	}

	keyProvider := &mockKeyProvider{key: privateKey}
	nonceMgr := &mockNonceProvider{nonce: 0}

	parsedABI := getTestABI(t)

	cw := NewContractWriter(ContractWriterConfig{
		Client:      client,
		Key:         keyProvider,
		NonceMgr:    nonceMgr,
		FromAddress: crypto.PubkeyToAddress(privateKey.PublicKey).Hex(),
		ChainID:     1,
		Logger:      zap.NewNop(),
	})

	_, err = cw.SendTx(context.Background(), ContractTxOpts{
		To:        "0x1234567890123456789012345678901234567890",
		Method:    "registerContent",
		ParsedABI: parsedABI,
		Args:      []interface{}{[32]byte{0x01}, "test"},
		GasLimit:  200000,
	})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "send tx")
	assert.NotNil(t, nonceMgr.rollbacks)
}

func TestContractWriter_SendTx_AutoEstimateGas(t *testing.T) {
	privateKey, err := crypto.GenerateKey()
	require.NoError(t, err)

	client := &mockTxClient{
		estimateGasFn: func(ctx context.Context, msg ethereum.CallMsg) (uint64, error) {
			return 100000, nil
		},
	}

	keyProvider := &mockKeyProvider{key: privateKey}
	nonceMgr := &mockNonceProvider{nonce: 0}

	parsedABI := getTestABI(t)

	cw := NewContractWriter(ContractWriterConfig{
		Client:      client,
		Key:         keyProvider,
		NonceMgr:    nonceMgr,
		FromAddress: crypto.PubkeyToAddress(privateKey.PublicKey).Hex(),
		ChainID:     1,
		Logger:      zap.NewNop(),
	})

	result, err := cw.SendTx(context.Background(), ContractTxOpts{
		To:        "0x1234567890123456789012345678901234567890",
		Method:    "registerContent",
		ParsedABI: parsedABI,
		Args:      []interface{}{[32]byte{0x01}, "test"},
		GasLimit:  0,
	})
	require.NoError(t, err)
	assert.Equal(t, uint64(120000), result.GasLimit)
}

func TestContractWriter_EstimateGas_Fallback(t *testing.T) {
	privateKey, err := crypto.GenerateKey()
	require.NoError(t, err)

	client := &mockTxClient{
		estimateGasFn: func(ctx context.Context, msg ethereum.CallMsg) (uint64, error) {
			return 0, assert.AnError
		},
	}

	keyProvider := &mockKeyProvider{key: privateKey}
	nonceMgr := &mockNonceProvider{nonce: 0}

	parsedABI := getTestABI(t)

	cw := NewContractWriter(ContractWriterConfig{
		Client:      client,
		Key:         keyProvider,
		NonceMgr:    nonceMgr,
		FromAddress: crypto.PubkeyToAddress(privateKey.PublicKey).Hex(),
		ChainID:     1,
		Logger:      zap.NewNop(),
	})

	result, err := cw.SendTx(context.Background(), ContractTxOpts{
		To:        "0x1234567890123456789012345678901234567890",
		Method:    "registerContent",
		ParsedABI: parsedABI,
		Args:      []interface{}{[32]byte{0x01}, "test"},
		GasLimit:  0,
	})
	require.NoError(t, err)
	assert.Equal(t, uint64(300000), result.GasLimit)
}

func TestTxTracker_BumpGas_EIP1559(t *testing.T) {
	privateKey, err := crypto.GenerateKey()
	require.NoError(t, err)

	client := &mockTxClient{
		headerByNumFn: func(ctx context.Context, number *big.Int) (*types.Header, error) {
			return &types.Header{BaseFee: big.NewInt(2_000_000_000)}, nil
		},
		sendTxFn: func(ctx context.Context, tx *types.Transaction) error {
			return nil
		},
	}

	tt := NewTxTracker(client, zap.NewNop())

	pending := &PendingTx{
		Hash:         "0xold",
		Nonce:        5,
		GasTipCap:    big.NewInt(2_000_000_000),
		MaxFeePerGas: big.NewInt(10_000_000_000),
		IsEIP1559:    true,
		To:           crypto.PubkeyToAddress(privateKey.PublicKey).Hex(),
		Value:        big.NewInt(0),
		Data:         nil,
		GasLimit:     200000,
		SentAt:       time.Now(),
		ChainID:      1,
	}

	newHash, err := tt.BumpGas(context.Background(), privateKey, pending, 10)
	require.NoError(t, err)
	assert.NotEmpty(t, newHash)
	assert.NotEqual(t, "0xold", newHash)
}

func TestTxTracker_BumpGas_Legacy(t *testing.T) {
	privateKey, err := crypto.GenerateKey()
	require.NoError(t, err)

	client := &mockTxClient{
		sendTxFn: func(ctx context.Context, tx *types.Transaction) error {
			return nil
		},
	}

	tt := NewTxTracker(client, zap.NewNop())

	pending := &PendingTx{
		Hash:      "0xold",
		Nonce:     5,
		GasPrice:  big.NewInt(1_000_000_000),
		IsEIP1559: false,
		To:        crypto.PubkeyToAddress(privateKey.PublicKey).Hex(),
		Value:     big.NewInt(0),
		Data:      nil,
		GasLimit:  200000,
		SentAt:    time.Now(),
		ChainID:   1,
	}

	newHash, err := tt.BumpGas(context.Background(), privateKey, pending, 10)
	require.NoError(t, err)
	assert.NotEmpty(t, newHash)
}

func TestTxTracker_CancelTx_EIP1559(t *testing.T) {
	privateKey, err := crypto.GenerateKey()
	require.NoError(t, err)

	client := &mockTxClient{
		headerByNumFn: func(ctx context.Context, number *big.Int) (*types.Header, error) {
			return &types.Header{BaseFee: big.NewInt(2_000_000_000)}, nil
		},
		sendTxFn: func(ctx context.Context, tx *types.Transaction) error {
			return nil
		},
	}

	tt := NewTxTracker(client, zap.NewNop())

	pending := &PendingTx{
		Hash:         "0xold",
		Nonce:        5,
		GasTipCap:    big.NewInt(2_000_000_000),
		MaxFeePerGas: big.NewInt(10_000_000_000),
		IsEIP1559:    true,
		To:           crypto.PubkeyToAddress(privateKey.PublicKey).Hex(),
		Value:        big.NewInt(0),
		Data:         nil,
		GasLimit:     200000,
		SentAt:       time.Now(),
		ChainID:      1,
	}

	cancelHash, err := tt.CancelTx(context.Background(), privateKey, pending, 10)
	require.NoError(t, err)
	assert.NotEmpty(t, cancelHash)
}

func TestTxTracker_CancelTx_Legacy(t *testing.T) {
	privateKey, err := crypto.GenerateKey()
	require.NoError(t, err)

	client := &mockTxClient{
		sendTxFn: func(ctx context.Context, tx *types.Transaction) error {
			return nil
		},
	}

	tt := NewTxTracker(client, zap.NewNop())

	pending := &PendingTx{
		Hash:      "0xold",
		Nonce:     5,
		GasPrice:  big.NewInt(1_000_000_000),
		IsEIP1559: false,
		To:        crypto.PubkeyToAddress(privateKey.PublicKey).Hex(),
		Value:     big.NewInt(0),
		Data:      nil,
		GasLimit:  200000,
		SentAt:    time.Now(),
		ChainID:   1,
	}

	cancelHash, err := tt.CancelTx(context.Background(), privateKey, pending, 10)
	require.NoError(t, err)
	assert.NotEmpty(t, cancelHash)
}

func TestTxTracker_CancelTx_DefaultBumpPercent(t *testing.T) {
	privateKey, err := crypto.GenerateKey()
	require.NoError(t, err)

	client := &mockTxClient{
		sendTxFn: func(ctx context.Context, tx *types.Transaction) error {
			return nil
		},
	}

	tt := NewTxTracker(client, zap.NewNop())

	pending := &PendingTx{
		Hash:      "0xold",
		Nonce:     5,
		GasPrice:  big.NewInt(1_000_000_000),
		IsEIP1559: false,
		To:        crypto.PubkeyToAddress(privateKey.PublicKey).Hex(),
		Value:     big.NewInt(0),
		SentAt:    time.Now(),
		ChainID:   1,
	}

	cancelHash, err := tt.CancelTx(context.Background(), privateKey, pending, 0)
	require.NoError(t, err)
	assert.NotEmpty(t, cancelHash)
}

func TestTxTracker_CancelTx_SendError(t *testing.T) {
	privateKey, err := crypto.GenerateKey()
	require.NoError(t, err)

	client := &mockTxClient{
		sendTxFn: func(ctx context.Context, tx *types.Transaction) error {
			return assert.AnError
		},
	}

	tt := NewTxTracker(client, zap.NewNop())

	pending := &PendingTx{
		Hash:      "0xold",
		Nonce:     5,
		GasPrice:  big.NewInt(1_000_000_000),
		IsEIP1559: false,
		To:        crypto.PubkeyToAddress(privateKey.PublicKey).Hex(),
		Value:     big.NewInt(0),
		SentAt:    time.Now(),
		ChainID:   1,
	}

	_, err = tt.CancelTx(context.Background(), privateKey, pending, 10)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to send cancel tx")
}

func TestBuildResult_EIP1559(t *testing.T) {
	pending := &PendingTx{
		Hash:         "0xabc",
		GasTipCap:    big.NewInt(2_000_000_000),
		MaxFeePerGas: big.NewInt(10_000_000_000),
		IsEIP1559:    true,
		SentAt:       time.Now(),
	}
	result := buildResult(pending, 5, 200000)
	assert.Equal(t, "0xabc", result.TxHash)
	assert.Equal(t, uint64(5), result.Nonce)
	assert.Equal(t, uint64(200000), result.GasLimit)
	assert.NotNil(t, result.MaxFeePerGas)
	assert.NotNil(t, result.TipCap)
	assert.Nil(t, result.GasPrice)
}

func TestBuildResult_Legacy(t *testing.T) {
	pending := &PendingTx{
		Hash:      "0xdef",
		GasPrice:  big.NewInt(3_000_000_000),
		IsEIP1559: false,
		SentAt:    time.Now(),
	}
	result := buildResult(pending, 10, 100000)
	assert.Equal(t, "0xdef", result.TxHash)
	assert.NotNil(t, result.GasPrice)
	assert.Nil(t, result.MaxFeePerGas)
	assert.Nil(t, result.TipCap)
}

func TestNonceManager_NextNonce_WithNetworkSync(t *testing.T) {
	var callCount int
	mockClient := &mockNonceClient{
		nonce: 42,
		getNonceFn: func(ctx context.Context, addr string) (uint64, error) {
			callCount++
			return 42, nil
		},
	}

	nm := NewNonceManager(mockClient, zap.NewNop())

	nonce, err := nm.NextNonce(context.Background(), "0xaddr")
	require.NoError(t, err)
	assert.Equal(t, uint64(42), nonce)
	assert.Equal(t, 1, callCount)

	nonce2, err := nm.NextNonce(context.Background(), "0xaddr")
	require.NoError(t, err)
	assert.Equal(t, uint64(43), nonce2)
}

func TestNonceManager_NextNonce_NetworkError_NoCache(t *testing.T) {
	mockClient := &mockNonceClient{
		getNonceFn: func(ctx context.Context, addr string) (uint64, error) {
			return 0, assert.AnError
		},
	}

	nm := NewNonceManager(mockClient, zap.NewNop())
	_, err := nm.NextNonce(context.Background(), "0xaddr")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "network nonce")
}

func TestNonceManager_ResetAddresses(t *testing.T) {
	mockClient := &mockNonceClient{nonce: 0}
	nm := NewNonceManager(mockClient, zap.NewNop())

	nm.states = map[string]*nonceState{
		"0xa": {next: 10, pending: make(map[uint64]struct{})},
		"0xb": {next: 20, pending: make(map[uint64]struct{})},
	}
	nm.lastSync = map[string]time.Time{
		"0xa": time.Now(),
		"0xb": time.Now(),
	}

	nm.ResetAddresses([]string{"0xa", "0xb"})

	nm.mu.Lock()
	defer nm.mu.Unlock()
	_, existsA := nm.states["0xa"]
	_, existsB := nm.states["0xb"]
	assert.False(t, existsA)
	assert.False(t, existsB)
}

func TestNonceManager_EvictStaleLocked(t *testing.T) {
	mockClient := &mockNonceClient{nonce: 0}
	nm := NewNonceManager(mockClient, zap.NewNop())
	nm.evictTTL = 1 * time.Nanosecond

	nm.states = map[string]*nonceState{
		"0xa": {next: 10, pending: make(map[uint64]struct{})},
	}
	nm.lastSync = map[string]time.Time{
		"0xa": time.Now().Add(-1 * time.Hour),
	}

	nm.mu.Lock()
	nm.evictStaleLocked()
	nm.mu.Unlock()

	nm.mu.Lock()
	_, exists := nm.states["0xa"]
	nm.mu.Unlock()
	assert.False(t, exists, "stale entry should be evicted")
}

type mockNonceClient struct {
	nonce      uint64
	getNonceFn func(ctx context.Context, addr string) (uint64, error)
}

func (m *mockNonceClient) GetNonce(ctx context.Context, addr string) (uint64, error) {
	if m.getNonceFn != nil {
		return m.getNonceFn(ctx, addr)
	}
	return m.nonce, nil
}

func TestContractWriter_SendTx_KeyError(t *testing.T) {
	cw := NewContractWriter(ContractWriterConfig{
		Client:      &mockTxClient{},
		Key:         &mockKeyProvider{err: fmt.Errorf("key unavailable")},
		NonceMgr:    &mockNonceProvider{nonce: 0},
		FromAddress: "0x742d35Cc6634C0532925a3b844Bc9e7595f2bD18",
		ChainID:     1,
		Logger:      zap.NewNop(),
	})

	parsedABI := getTestABI(t)
	_, err := cw.SendTx(context.Background(), ContractTxOpts{
		To:        "0x1234567890123456789012345678901234567890",
		Method:    "registerContent",
		ParsedABI: parsedABI,
		Args:      []interface{}{[32]byte{0x01}, "test"},
		GasLimit:  200000,
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "key unavailable")
}

func TestContractWriter_SendTx_BothGasFail(t *testing.T) {
	privateKey, err := crypto.GenerateKey()
	require.NoError(t, err)

	client := &mockTxClient{
		suggestTipFn: func(ctx context.Context) (*big.Int, error) {
			return nil, fmt.Errorf("no tipcap")
		},
		getGasPriceFn: func(ctx context.Context) (*big.Int, error) {
			return nil, fmt.Errorf("no gas price")
		},
	}

	cw := NewContractWriter(ContractWriterConfig{
		Client:      client,
		Key:         &mockKeyProvider{key: privateKey},
		NonceMgr:    &mockNonceProvider{nonce: 0},
		FromAddress: crypto.PubkeyToAddress(privateKey.PublicKey).Hex(),
		ChainID:     1,
		Logger:      zap.NewNop(),
	})

	parsedABI := getTestABI(t)
	_, err = cw.SendTx(context.Background(), ContractTxOpts{
		To:        "0x1234567890123456789012345678901234567890",
		Method:    "registerContent",
		ParsedABI: parsedABI,
		Args:      []interface{}{[32]byte{0x01}, "test"},
		GasLimit:  200000,
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "get gas price")
}

func TestContractWriter_SendTx_GasEstimateFailed_Default(t *testing.T) {
	privateKey, err := crypto.GenerateKey()
	require.NoError(t, err)

	client := &mockTxClient{
		estimateGasFn: func(ctx context.Context, msg ethereum.CallMsg) (uint64, error) {
			return 0, fmt.Errorf("estimate failed")
		},
	}

	cw := NewContractWriter(ContractWriterConfig{
		Client:      client,
		Key:         &mockKeyProvider{key: privateKey},
		NonceMgr:    &mockNonceProvider{nonce: 0},
		FromAddress: crypto.PubkeyToAddress(privateKey.PublicKey).Hex(),
		ChainID:     1,
		Logger:      zap.NewNop(),
	})

	parsedABI := getTestABI(t)
	result, err := cw.SendTx(context.Background(), ContractTxOpts{
		To:        "0x1234567890123456789012345678901234567890",
		Method:    "registerContent",
		ParsedABI: parsedABI,
		Args:      []interface{}{[32]byte{0x01}, "test"},
		GasLimit:  0,
	})
	require.NoError(t, err)
	assert.Equal(t, uint64(300000), result.GasLimit)
}

func TestContractWriter_SendTx_WithTracker(t *testing.T) {
	privateKey, err := crypto.GenerateKey()
	require.NoError(t, err)

	client := &mockTxClient{}

	cw := NewContractWriter(ContractWriterConfig{
		Client:      client,
		Key:         &mockKeyProvider{key: privateKey},
		NonceMgr:    &mockNonceProvider{nonce: 0},
		FromAddress: crypto.PubkeyToAddress(privateKey.PublicKey).Hex(),
		ChainID:     1,
		Logger:      zap.NewNop(),
	})

	tracker := &TxTracker{}
	returned := cw.WithTracker(tracker)
	assert.Equal(t, cw, returned)
	assert.Equal(t, tracker, cw.tracker)
}

func TestContractWriter_SendTx_EIP1559_NoBaseFee(t *testing.T) {
	privateKey, err := crypto.GenerateKey()
	require.NoError(t, err)

	client := &mockTxClient{
		headerByNumFn: func(ctx context.Context, number *big.Int) (*types.Header, error) {
			return &types.Header{BaseFee: nil}, nil
		},
	}

	cw := NewContractWriter(ContractWriterConfig{
		Client:      client,
		Key:         &mockKeyProvider{key: privateKey},
		NonceMgr:    &mockNonceProvider{nonce: 0},
		FromAddress: crypto.PubkeyToAddress(privateKey.PublicKey).Hex(),
		ChainID:     1,
		Logger:      zap.NewNop(),
	})

	parsedABI := getTestABI(t)
	result, err := cw.SendTx(context.Background(), ContractTxOpts{
		To:        "0x1234567890123456789012345678901234567890",
		Method:    "registerContent",
		ParsedABI: parsedABI,
		Args:      []interface{}{[32]byte{0x01}, "test"},
		GasLimit:  200000,
	})
	require.NoError(t, err)
	assert.NotNil(t, result.MaxFeePerGas)
	assert.NotNil(t, result.TipCap)
}

func TestContractWriter_SendTx_EIP1559_HeaderError(t *testing.T) {
	privateKey, err := crypto.GenerateKey()
	require.NoError(t, err)

	client := &mockTxClient{
		headerByNumFn: func(ctx context.Context, number *big.Int) (*types.Header, error) {
			return nil, fmt.Errorf("header error")
		},
	}

	cw := NewContractWriter(ContractWriterConfig{
		Client:      client,
		Key:         &mockKeyProvider{key: privateKey},
		NonceMgr:    &mockNonceProvider{nonce: 0},
		FromAddress: crypto.PubkeyToAddress(privateKey.PublicKey).Hex(),
		ChainID:     1,
		Logger:      zap.NewNop(),
	})

	parsedABI := getTestABI(t)
	result, err := cw.SendTx(context.Background(), ContractTxOpts{
		To:        "0x1234567890123456789012345678901234567890",
		Method:    "registerContent",
		ParsedABI: parsedABI,
		Args:      []interface{}{[32]byte{0x01}, "test"},
		GasLimit:  200000,
	})
	require.NoError(t, err)
	assert.NotNil(t, result.MaxFeePerGas)
	assert.NotNil(t, result.TipCap)
}

func TestContractWriter_SendTx_WithNilValue(t *testing.T) {
	privateKey, err := crypto.GenerateKey()
	require.NoError(t, err)

	client := &mockTxClient{}

	cw := NewContractWriter(ContractWriterConfig{
		Client:      client,
		Key:         &mockKeyProvider{key: privateKey},
		NonceMgr:    &mockNonceProvider{nonce: 0},
		FromAddress: crypto.PubkeyToAddress(privateKey.PublicKey).Hex(),
		ChainID:     1,
		Logger:      zap.NewNop(),
	})

	parsedABI := getTestABI(t)
	result, err := cw.SendTx(context.Background(), ContractTxOpts{
		To:        "0x1234567890123456789012345678901234567890",
		Method:    "registerContent",
		ParsedABI: parsedABI,
		Args:      []interface{}{[32]byte{0x01}, "test"},
		Value:     nil,
		GasLimit:  200000,
	})
	require.NoError(t, err)
	assert.NotNil(t, result)
}

func TestContractWriteResult_Fields(t *testing.T) {
	now := time.Now()
	result := &ContractWriteResult{
		TxHash:       "0xabc",
		Nonce:        5,
		GasLimit:     200000,
		GasPrice:     big.NewInt(20e9),
		MaxFeePerGas: nil,
		TipCap:       nil,
		SentAt:       now,
	}
	assert.Equal(t, "0xabc", result.TxHash)
	assert.Equal(t, uint64(5), result.Nonce)
	assert.Equal(t, uint64(200000), result.GasLimit)
	assert.NotNil(t, result.GasPrice)
	assert.Nil(t, result.MaxFeePerGas)
	assert.Nil(t, result.TipCap)
	assert.Equal(t, now, result.SentAt)
}

func TestContractTxOpts_Fields(t *testing.T) {
	opts := ContractTxOpts{
		To:        "0x1",
		Method:    "transfer",
		ParsedABI: nil,
		Args:      []interface{}{big.NewInt(1)},
		Value:     big.NewInt(0),
		GasLimit:  100000,
	}
	assert.Equal(t, "0x1", opts.To)
	assert.Equal(t, "transfer", opts.Method)
	assert.Nil(t, opts.ParsedABI)
	assert.Equal(t, uint64(100000), opts.GasLimit)
}

func TestContractWriter_SendTx_TableDriven(t *testing.T) {
	privateKey, err := crypto.GenerateKey()
	require.NoError(t, err)
	fromAddr := crypto.PubkeyToAddress(privateKey.PublicKey).Hex()

	parsedABI := getTestABI(t)

	tests := []struct {
		name    string
		client  *mockTxClient
		nonce   *mockNonceProvider
		wantErr string
	}{
		{
			"success_eip1559",
			&mockTxClient{},
			&mockNonceProvider{nonce: 0},
			"",
		},
		{
			"success_legacy",
			&mockTxClient{
				suggestTipFn: func(ctx context.Context) (*big.Int, error) {
					return nil, assert.AnError
				},
				getGasPriceFn: func(ctx context.Context) (*big.Int, error) {
					return big.NewInt(3_000_000_000), nil
				},
			},
			&mockNonceProvider{nonce: 0},
			"",
		},
		{
			"nonce_error",
			&mockTxClient{},
			&mockNonceProvider{nonceErr: assert.AnError},
			"get nonce",
		},
		{
			"send_error_rollback",
			&mockTxClient{
				sendTxFn: func(ctx context.Context, tx *types.Transaction) error {
					return assert.AnError
				},
			},
			&mockNonceProvider{nonce: 0},
			"send tx",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cw := NewContractWriter(ContractWriterConfig{
				Client:      tc.client,
				Key:         &mockKeyProvider{key: privateKey},
				NonceMgr:    tc.nonce,
				FromAddress: fromAddr,
				ChainID:     1,
				Logger:      zap.NewNop(),
			})

			result, err := cw.SendTx(context.Background(), ContractTxOpts{
				To:        "0x1234567890123456789012345678901234567890",
				Method:    "registerContent",
				ParsedABI: parsedABI,
				Args:      []interface{}{[32]byte{0x01}, "test"},
				GasLimit:  200000,
			})

			if tc.wantErr != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tc.wantErr)
			} else {
				require.NoError(t, err)
				assert.NotEmpty(t, result.TxHash)
			}
		})
	}
}
